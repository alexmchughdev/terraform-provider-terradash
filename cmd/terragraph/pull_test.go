package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeDashboard struct {
	model       map[string]any
	folder      string
	provisioned bool
}

type fakeGrafana struct {
	dashboards map[string]fakeDashboard
	searches   []string
}

func (g *fakeGrafana) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/api/search":
		g.searches = append(g.searches, r.URL.RawQuery)
		folders := r.URL.Query()["folderUIDs"]
		hits := []map[string]any{}
		for uid, d := range g.dashboards {
			if len(folders) == 0 || slices.Contains(folders, d.folder) {
				hits = append(hits, map[string]any{"uid": uid, "title": d.model["title"], "type": "dash-db"})
			}
		}
		slices.SortFunc(hits, func(a, b map[string]any) int { return strings.Compare(a["uid"].(string), b["uid"].(string)) })
		json.NewEncoder(w).Encode(hits)
	case strings.HasPrefix(r.URL.Path, "/api/dashboards/uid/"):
		d, ok := g.dashboards[strings.TrimPrefix(r.URL.Path, "/api/dashboards/uid/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"message": "Dashboard not found"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"dashboard": d.model,
			"meta":      map[string]any{"folderUid": d.folder, "provisioned": d.provisioned, "version": 4},
		})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFakeGrafana(t *testing.T) (*fakeGrafana, string) {
	t.Helper()
	t.Setenv("GRAFANA_URL", "")
	t.Setenv("GRAFANA_AUTH", "")
	t.Setenv("GRAFANA_ORG_ID", "")
	t.Setenv("GRAFANA_CA_CERT", "")
	g := &fakeGrafana{dashboards: map[string]fakeDashboard{
		"a1": {model: map[string]any{"id": 11, "uid": "a1", "version": 4, "title": "Alpha", "panels": []any{
			map[string]any{"id": 1, "type": "stat", "title": "P", "gridPos": map[string]any{"x": 0, "y": 0, "w": 12, "h": 8}},
		}}, folder: "f1"},
		"b1": {model: map[string]any{"uid": "b1", "title": "Beta"}, folder: "f2", provisioned: true},
		"c1": {model: map[string]any{"uid": "c1", "title": "Alpha"}, folder: "f1"},
	}}
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return g, srv.URL
}

func TestPullByUID(t *testing.T) {
	_, url := newFakeGrafana(t)
	code, stdout, stderr := runCLI(t, "", "pull", "-url", url, "a1", "c1")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if got, want := resourceNames(t, stdout), []string{"alpha", "alpha_2"}; !slices.Equal(got, want) {
		t.Errorf("resources = %v, want %v", got, want)
	}
	contains(t, stdout,
		"to = terragraph_dashboard.alpha\n", `id = "a1"`,
		"to = terragraph_dashboard.alpha_2\n", `id = "c1"`,
		`folder_uid = "f1"`,
	)
	if strings.Contains(stdout, "version") {
		t.Errorf("server-side fields leaked into HCL:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestPullNoImport(t *testing.T) {
	_, url := newFakeGrafana(t)
	code, stdout, stderr := runCLI(t, "", "pull", "-url", url, "-import=false", "a1")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	parseHCL(t, stdout)
	if strings.Contains(stdout, "import {") {
		t.Errorf("unexpected import block:\n%s", stdout)
	}
}

func TestPullURLFromEnv(t *testing.T) {
	_, url := newFakeGrafana(t)
	t.Setenv("GRAFANA_URL", url)
	code, stdout, stderr := runCLI(t, "", "pull", "a1")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	contains(t, stdout, `"terragraph_dashboard" "alpha"`)
}

func TestPullSelection(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantNames    []string
		wantSearches int
	}{
		{"all", []string{"-all"}, []string{"alpha", "beta", "alpha_2"}, 1},
		{"folder", []string{"-folder", "f1"}, []string{"alpha", "alpha_2"}, 1},
		{"folders", []string{"-folder", "f1,f2"}, []string{"alpha", "beta", "alpha_2"}, 1},
		{"folder plus explicit uid", []string{"-folder", "f1", "b1"}, []string{"beta", "alpha", "alpha_2"}, 1},
		{"explicit uid already in folder", []string{"-folder", "f1", "a1"}, []string{"alpha", "alpha_2"}, 1},
		{"unknown folder", []string{"-folder", "nope"}, nil, 1},
		{"explicit only", []string{"b1"}, []string{"beta"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, url := newFakeGrafana(t)
			code, stdout, stderr := runCLI(t, "", append([]string{"pull", "-url", url}, tt.args...)...)
			if code != 0 {
				t.Fatalf("code = %d, stderr: %s", code, stderr)
			}
			if got := resourceNames(t, stdout); !slices.Equal(got, tt.wantNames) {
				t.Errorf("resources = %v, want %v", got, tt.wantNames)
			}
			if len(g.searches) != tt.wantSearches {
				t.Errorf("searches = %v, want %d", g.searches, tt.wantSearches)
			}
		})
	}
}

func TestPullFolderFilterQuery(t *testing.T) {
	g, url := newFakeGrafana(t)
	if code, _, stderr := runCLI(t, "", "pull", "-url", url, "-folder", "f1,f2"); code != 0 {
		t.Fatalf("stderr: %s", stderr)
	}
	contains(t, g.searches[0], "folderUIDs=f1", "folderUIDs=f2", "type=dash-db")
}

func TestPullProvisionedWarning(t *testing.T) {
	_, url := newFakeGrafana(t)
	code, stdout, stderr := runCLI(t, "", "pull", "-url", url, "b1")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	contains(t, stderr, "warning: b1: dashboard is provisioned from files")
	parseHCL(t, stdout)

	_, _, stderr = runCLI(t, "", "pull", "-url", url, "a1")
	if strings.Contains(stderr, "provisioned") {
		t.Errorf("unexpected warning: %s", stderr)
	}
}

func TestPullOutputDirectory(t *testing.T) {
	_, url := newFakeGrafana(t)
	out := filepath.Join(t.TempDir(), "tf")
	code, _, stderr := runCLI(t, "", "pull", "-url", url, "-all", "-o", out)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	for _, name := range []string{"alpha.tf", "alpha_2.tf", "beta.tf"} {
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		parseHCL(t, string(data))
	}
	if code, _, _ := runCLI(t, "", "pull", "-url", url, "-all", "-o", out); code != 1 {
		t.Errorf("overwrite without -force: code = %d, want 1", code)
	}
	if code, _, stderr := runCLI(t, "", "pull", "-url", url, "-all", "-o", out, "-force"); code != 0 {
		t.Errorf("-force: code = %d, stderr: %s", code, stderr)
	}
}

func TestPullErrors(t *testing.T) {
	_, url := newFakeGrafana(t)
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no url", []string{"pull", "a1"}, "set -url or GRAFANA_URL"},
		{"bad url", []string{"pull", "-url", "localhost:3000", "a1"}, "terragraph:"},
		{"no selection", []string{"pull", "-url", url}, "specify dashboard UIDs, -all or -folder"},
		{"unknown uid", []string{"pull", "-url", url, "nope"}, "Dashboard not found"},
		{"missing CA file", []string{"pull", "-url", url, "-ca-cert", "/nonexistent/ca.pem", "a1"}, "no such file"},
		{"org-id not integer", []string{"pull", "-url", url, "-org-id", "abc", "a1"}, "not a non-negative integer"},
		{"folder empty", []string{"pull", "-url", url, "-folder", " , "}, "at least one folder UID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, "", tt.args...)
			if code != 1 {
				t.Errorf("code = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q", stdout)
			}
			contains(t, stderr, "terragraph:", tt.wantErr)
		})
	}
}

func TestPullV2DashboardRejected(t *testing.T) {
	g, url := newFakeGrafana(t)
	g.dashboards["v2"] = fakeDashboard{model: map[string]any{"title": "V2", "elements": map[string]any{}}}
	code, _, stderr := runCLI(t, "", "pull", "-url", url, "v2")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	contains(t, stderr, "v2:", "v2 dashboards are not supported")
}

func TestPullSendsAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{"dashboard": map[string]any{"title": "T"}, "meta": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GRAFANA_AUTH", "glsa_token")
	if code, _, stderr := runCLI(t, "", "pull", "-url", srv.URL, "x"); code != 0 {
		t.Fatalf("stderr: %s", stderr)
	}
	if gotAuth != "Bearer glsa_token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestPullEmptySearchToDir(t *testing.T) {
	g, url := newFakeGrafana(t)
	g.dashboards = map[string]fakeDashboard{}
	out := filepath.Join(t.TempDir(), "tf")

	code, stdout, stderr := runCLI(t, "", "pull", "-url", url, "-all", "-o", out)
	if code != 0 {
		t.Errorf("code = %d, stderr: %s", code, stderr)
	}
	contains(t, stderr, "no dashboards found")
	if stdout != "" {
		t.Errorf("stdout = %q", stdout)
	}
}
