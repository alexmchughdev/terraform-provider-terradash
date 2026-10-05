package provider

import (
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terragraph/internal/tfschema"
)

func read(t *testing.T, p *Provider, state map[string]any) *tfprotov6.ReadResourceResponse {
	t.Helper()
	resp, err := p.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{
		TypeName:     tfschema.ResourceType,
		CurrentState: dynamicValue(t, tfschema.Resource, state),
	})
	if err != nil || len(resp.Diagnostics) > 0 {
		t.Fatalf("read: %v %s", err, errorSummaries(resp.Diagnostics))
	}
	return resp
}

func apply(t *testing.T, p *Provider, prior, planned *tfprotov6.DynamicValue) *tfprotov6.ApplyResourceChangeResponse {
	t.Helper()
	resp, err := p.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     tfschema.ResourceType,
		PriorState:   prior,
		PlannedState: planned,
	})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func panelTitle(tr map[string]any) any {
	return tr["panel"].([]any)[0].(map[string]any)["title"]
}

func TestReadResource(t *testing.T) {
	t.Run("deleted remotely removes from state", func(t *testing.T) {
		g := newFakeGrafana(t)
		resp := read(t, configuredProvider(t, g.URL), stored())
		if stateTree(t, resp.NewState) != nil {
			t.Error("state should be null")
		}
	})

	t.Run("in sync refreshes url and version only", func(t *testing.T) {
		g := newFakeGrafana(t)
		g.put("acc-unit", render(t, testDashboard()), "")
		g.versions["acc-unit"] = 7
		got := stateTree(t, read(t, configuredProvider(t, g.URL), stored()).NewState)
		if panelTitle(got) != "Requests" || got["title"] != "Unit" {
			t.Errorf("config changed: %v", got)
		}
		if got["url"] != g.URL+"/d/acc-unit/slug" || got["version"] != num("7") {
			t.Errorf("url, version = %v, %v", got["url"], got["version"])
		}
		if got["folder_uid"] != nil {
			t.Errorf("folder_uid = %v, want null", got["folder_uid"])
		}
	})

	t.Run("drift is reconciled", func(t *testing.T) {
		g := newFakeGrafana(t)
		remote := render(t, testDashboard())
		remote["title"] = "Edited in UI"
		remote["panels"].([]any)[0].(map[string]any)["title"] = "Edited panel"
		g.put("acc-unit", remote, "team-a")
		got := stateTree(t, read(t, configuredProvider(t, g.URL), stored()).NewState)
		if got["title"] != "Edited in UI" || panelTitle(got) != "Edited panel" || got["folder_uid"] != "team-a" {
			t.Errorf("drift not reflected: title=%v panel=%v folder=%v", got["title"], panelTitle(got), got["folder_uid"])
		}
		options := got["panel"].([]any)[0].(map[string]any)["options"]
		if options == nil {
			t.Error("unchanged panel options were lost")
		}
	})

	t.Run("import decodes the remote dashboard", func(t *testing.T) {
		g := newFakeGrafana(t)
		g.put("acc-unit", render(t, testDashboard()), "team-a")
		got := stateTree(t, read(t, configuredProvider(t, g.URL), map[string]any{"uid": "acc-unit"}).NewState)
		if got["title"] != "Unit" || panelTitle(got) != "Requests" || got["folder_uid"] != "team-a" {
			t.Errorf("imported = %v", got)
		}
	})

	t.Run("keeps overwrite and message", func(t *testing.T) {
		g := newFakeGrafana(t)
		remote := render(t, testDashboard())
		remote["title"] = "Edited"
		g.put("acc-unit", remote, "")
		state := stored()
		state["overwrite"], state["message"] = true, "why"
		got := stateTree(t, read(t, configuredProvider(t, g.URL), state).NewState)
		if got["overwrite"] != true || got["message"] != "why" {
			t.Errorf("overwrite, message = %v, %v", got["overwrite"], got["message"])
		}
	})
}

func TestApplyCreate(t *testing.T) {
	g := newFakeGrafana(t)
	p := configuredProvider(t, g.URL)

	planned := testDashboard()
	delete(planned, "uid")
	planned["uid"] = schema.Unknown
	planned["url"], planned["version"] = schema.Unknown, schema.Unknown
	planned["folder_uid"], planned["message"] = "team-a", "first"

	resp := apply(t, p, nullState(t), dynamicValue(t, tfschema.Resource, planned))
	if len(resp.Diagnostics) > 0 {
		t.Fatal(errorSummaries(resp.Diagnostics))
	}
	got := stateTree(t, resp.NewState)
	if got["uid"] != "generated" || got["url"] != g.URL+"/d/generated/slug" || got["version"] != num("1") {
		t.Errorf("computed values = %v, %v, %v", got["uid"], got["url"], got["version"])
	}
	if len(g.saves) != 1 {
		t.Fatalf("saves = %d", len(g.saves))
	}
	if s := g.saves[0]; s.Overwrite || s.FolderUID != "team-a" || s.Message != "first" || s.Dashboard["title"] != "Unit" {
		t.Errorf("save request = %+v", s)
	}
	if _, ok := g.saves[0].Dashboard["uid"]; ok {
		t.Error("unknown uid should be omitted from the request")
	}
}

func TestApplyCreateConflict(t *testing.T) {
	g := newFakeGrafana(t)
	g.put("acc-unit", render(t, testDashboard()), "")
	p := configuredProvider(t, g.URL)

	planned := testDashboard()
	planned["url"], planned["version"] = schema.Unknown, schema.Unknown
	resp := apply(t, p, nullState(t), dynamicValue(t, tfschema.Resource, planned))
	if got := errorSummaries(resp.Diagnostics); !strings.Contains(got, "Import the existing dashboard") {
		t.Errorf("diagnostics = %q, want import hint", got)
	}
	if stateTree(t, resp.NewState) != nil {
		t.Error("failed create should leave no state")
	}

	planned["overwrite"] = true
	resp = apply(t, p, nullState(t), dynamicValue(t, tfschema.Resource, planned))
	if len(resp.Diagnostics) > 0 {
		t.Errorf("overwrite = true should succeed: %s", errorSummaries(resp.Diagnostics))
	}
}

func TestApplyUpdate(t *testing.T) {
	t.Run("unchanged JSON skips the API", func(t *testing.T) {
		g := newFakeGrafana(t)
		p := configuredProvider(t, g.URL)
		planned := stored()
		planned["message"] = "only a message"
		resp := apply(t, p, dynamicValue(t, tfschema.Resource, stored()), dynamicValue(t, tfschema.Resource, planned))
		if len(resp.Diagnostics) > 0 || len(g.saves) != 0 {
			t.Fatalf("saves = %d, diagnostics = %q", len(g.saves), errorSummaries(resp.Diagnostics))
		}
		if got := stateTree(t, resp.NewState); got["message"] != "only a message" || got["version"] != num("1") {
			t.Errorf("state = %v", got)
		}
	})

	t.Run("changed JSON saves with overwrite", func(t *testing.T) {
		g := newFakeGrafana(t)
		g.put("acc-unit", render(t, testDashboard()), "")
		p := configuredProvider(t, g.URL)
		planned := stored()
		planned["title"] = "Renamed"
		planned["url"], planned["version"] = schema.Unknown, schema.Unknown
		resp := apply(t, p, dynamicValue(t, tfschema.Resource, stored()), dynamicValue(t, tfschema.Resource, planned))
		if len(resp.Diagnostics) > 0 || len(g.saves) != 1 || !g.saves[0].Overwrite {
			t.Fatalf("saves = %+v, diagnostics = %q", g.saves, errorSummaries(resp.Diagnostics))
		}
		if got := stateTree(t, resp.NewState); got["title"] != "Renamed" || got["version"] != num("2") {
			t.Errorf("state = %v", got)
		}
	})
}

func TestApplyDelete(t *testing.T) {
	g := newFakeGrafana(t)
	g.put("acc-unit", render(t, testDashboard()), "")
	resp := apply(t, configuredProvider(t, g.URL), dynamicValue(t, tfschema.Resource, stored()), nullState(t))
	if len(resp.Diagnostics) > 0 {
		t.Fatal(errorSummaries(resp.Diagnostics))
	}
	if len(g.deletes) != 1 || g.deletes[0] != "acc-unit" || len(g.dashboards) != 0 {
		t.Errorf("deletes = %v, remaining = %v", g.deletes, g.dashboards)
	}
	if stateTree(t, resp.NewState) != nil {
		t.Error("state should be null")
	}
}

func TestApplyDeleteMissingIsNotAnError(t *testing.T) {
	g := newFakeGrafana(t)
	resp := apply(t, configuredProvider(t, g.URL), dynamicValue(t, tfschema.Resource, stored()), nullState(t))
	if len(resp.Diagnostics) > 0 {
		t.Error(errorSummaries(resp.Diagnostics))
	}
}
