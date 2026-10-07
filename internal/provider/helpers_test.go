package provider

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/dashboard"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/tfschema"
)

func dynamicValue(t *testing.T, b schema.Block, tree map[string]any) *tfprotov6.DynamicValue {
	t.Helper()
	v, err := schema.ToValue(b, tree)
	if err != nil {
		t.Fatal(err)
	}
	dv, err := tfprotov6.NewDynamicValue(b.Type(), v)
	if err != nil {
		t.Fatal(err)
	}
	return &dv
}

func nullState(t *testing.T) *tfprotov6.DynamicValue {
	t.Helper()
	dv, err := tfprotov6.NewDynamicValue(tfschema.Resource.Type(), tftypes.NewValue(tfschema.Resource.Type(), nil))
	if err != nil {
		t.Fatal(err)
	}
	return &dv
}

// stateTree decodes a resource state; a null state yields nil.
func stateTree(t *testing.T, dv *tfprotov6.DynamicValue) map[string]any {
	t.Helper()
	v, err := dv.Unmarshal(tfschema.Resource.Type())
	if err != nil {
		t.Fatal(err)
	}
	tr, err := tree(v)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func num(s string) json.Number { return json.Number(s) }

// testDashboard returns a resource tree with a single timeseries panel.
func testDashboard() map[string]any {
	return map[string]any{
		"uid":   "acc-unit",
		"title": "Unit",
		"panel": []any{map[string]any{
			"type":    "timeseries",
			"title":   "Requests",
			"options": map[string]any{"legend": map[string]any{"showLegend": true}},
		}},
	}
}

// stored returns testDashboard as saved in state.
func stored() map[string]any {
	t := testDashboard()
	t["url"] = "http://grafana.test/d/acc-unit/unit"
	t["version"] = num("1")
	return t
}

func errorSummaries(diags []*tfprotov6.Diagnostic) string {
	var out []string
	for _, d := range diags {
		out = append(out, d.Summary+": "+d.Detail)
	}
	return strings.Join(out, "\n")
}

type savedRequest struct {
	Dashboard map[string]any `json:"dashboard"`
	FolderUID string         `json:"folderUid"`
	Overwrite bool           `json:"overwrite"`
	Message   string         `json:"message"`
}

// fakeGrafana emulates the dashboard endpoints of the Grafana HTTP API.
type fakeGrafana struct {
	*httptest.Server
	mu         sync.Mutex
	dashboards map[string]map[string]any
	folders    map[string]string
	versions   map[string]int
	saves      []savedRequest
	deletes    []string
}

func newFakeGrafana(t *testing.T) *fakeGrafana {
	t.Helper()
	g := &fakeGrafana{
		dashboards: map[string]map[string]any{},
		folders:    map[string]string{},
		versions:   map[string]int{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/dashboards/db", g.save)
	mux.HandleFunc("GET /api/dashboards/uid/{uid}", g.get)
	mux.HandleFunc("DELETE /api/dashboards/uid/{uid}", g.delete)
	g.Server = httptest.NewServer(mux)
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGrafana) put(uid string, model map[string]any, folder string) {
	model = maps.Clone(model)
	model["uid"] = uid
	g.dashboards[uid] = model
	g.folders[uid] = folder
	g.versions[uid]++
}

func (g *fakeGrafana) save(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var req savedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	g.saves = append(g.saves, req)
	uid, _ := req.Dashboard["uid"].(string)
	if uid == "" {
		uid = "generated"
	}
	if _, exists := g.dashboards[uid]; exists && !req.Overwrite {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"message": "a dashboard with the same uid already exists"})
		return
	}
	g.put(uid, req.Dashboard, req.FolderUID)
	json.NewEncoder(w).Encode(map[string]any{"uid": uid, "url": "/d/" + uid + "/slug", "version": g.versions[uid]})
}

func (g *fakeGrafana) get(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	uid := r.PathValue("uid")
	model, ok := g.dashboards[uid]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Dashboard not found"})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"dashboard": model,
		"meta":      map[string]any{"url": "/d/" + uid + "/slug", "folderUid": g.folders[uid], "version": g.versions[uid]},
	})
}

func (g *fakeGrafana) delete(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	uid := r.PathValue("uid")
	g.deletes = append(g.deletes, uid)
	delete(g.dashboards, uid)
}

// render encodes a resource tree the way the provider saves it.
func render(t *testing.T, tr map[string]any) map[string]any {
	t.Helper()
	model, err := dashboard.Encode(dashboardBody(tr))
	if err != nil {
		t.Fatal(err)
	}
	return model
}

// configuredProvider returns a provider talking to the given Grafana URL.
func configuredProvider(t *testing.T, url string) *Provider {
	t.Helper()
	p := New("test").(*Provider)
	resp, err := p.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{
		Config: dynamicValue(t, tfschema.Provider, map[string]any{"url": url, "auth": "admin:admin"}),
	})
	if err != nil || len(resp.Diagnostics) > 0 {
		t.Fatalf("configure: %v %s", err, errorSummaries(resp.Diagnostics))
	}
	return p
}
