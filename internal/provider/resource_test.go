package provider

import (
	"maps"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/tfschema"
)

func TestValidateResourceConfig(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(map[string]any)
		wantPath *tftypes.AttributePath
		wantWarn bool
	}{
		{name: "valid", mutate: func(map[string]any) {}},
		{
			name:     "bad uid",
			mutate:   func(d map[string]any) { d["uid"] = "has space" },
			wantPath: tftypes.NewAttributePath().WithAttributeName("uid"),
		},
		{
			name:     "bad folder uid",
			mutate:   func(d map[string]any) { d["folder_uid"] = "no/slashes" },
			wantPath: tftypes.NewAttributePath().WithAttributeName("folder_uid"),
		},
		{
			name:     "bad refresh",
			mutate:   func(d map[string]any) { d["refresh"] = "soon" },
			wantPath: tftypes.NewAttributePath().WithAttributeName("refresh"),
		},
		{
			name: "row panel type",
			mutate: func(d map[string]any) {
				d["panel"].([]any)[0].(map[string]any)["type"] = "row"
			},
			wantPath: tftypes.NewAttributePath().WithAttributeName("panel").WithElementKeyInt(0).WithAttributeName("type"),
		},
		{
			name: "grid overflow",
			mutate: func(d map[string]any) {
				d["panel"].([]any)[0].(map[string]any)["grid_pos"] = map[string]any{"x": num("20"), "w": num("12")}
			},
			wantPath: tftypes.NewAttributePath().WithAttributeName("panel").WithElementKeyInt(0).WithAttributeName("grid_pos"),
		},
		{
			name: "nested row panel options",
			mutate: func(d map[string]any) {
				d["row"] = []any{map[string]any{"title": "R", "panel": []any{map[string]any{"type": "stat", "options": "oops"}}}}
			},
			wantPath: tftypes.NewAttributePath().WithAttributeName("row").WithElementKeyInt(0).
				WithAttributeName("panel").WithElementKeyInt(0).WithAttributeName("options"),
		},
		{
			name: "variable type warning",
			mutate: func(d map[string]any) {
				d["variable"] = []any{map[string]any{"name": "env", "type": "mystery"}}
			},
			wantPath: tftypes.NewAttributePath().WithAttributeName("variable").WithElementKeyInt(0).WithAttributeName("type"),
			wantWarn: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testDashboard()
			tt.mutate(d)
			resp, err := New("test").ValidateResourceConfig(t.Context(), &tfprotov6.ValidateResourceConfigRequest{
				TypeName: tfschema.ResourceType,
				Config:   dynamicValue(t, tfschema.Resource, d),
			})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantPath == nil {
				if len(resp.Diagnostics) > 0 {
					t.Fatalf("unexpected diagnostics: %s", errorSummaries(resp.Diagnostics))
				}
				return
			}
			if len(resp.Diagnostics) != 1 {
				t.Fatalf("diagnostics = %q, want exactly one", errorSummaries(resp.Diagnostics))
			}
			diag := resp.Diagnostics[0]
			if diag.Attribute == nil || !diag.Attribute.Equal(tt.wantPath) {
				t.Errorf("path = %v, want %v", diag.Attribute, tt.wantPath)
			}
			if warned := diag.Severity == tfprotov6.DiagnosticSeverityWarning; warned != tt.wantWarn {
				t.Errorf("severity = %v, want warning: %v", diag.Severity, tt.wantWarn)
			}
		})
	}
}

func plan(t *testing.T, prior, proposed *tfprotov6.DynamicValue) *tfprotov6.PlanResourceChangeResponse {
	t.Helper()
	resp, err := New("test").PlanResourceChange(t.Context(), &tfprotov6.PlanResourceChangeRequest{
		TypeName:         tfschema.ResourceType,
		PriorState:       prior,
		ProposedNewState: proposed,
	})
	if err != nil || len(resp.Diagnostics) > 0 {
		t.Fatalf("plan: %v %s", err, errorSummaries(resp.Diagnostics))
	}
	return resp
}

func TestPlanResourceChange(t *testing.T) {
	tests := []struct {
		name        string
		prior       map[string]any
		proposed    func(map[string]any)
		unknown     []string
		replace     bool
		wantNoOp    bool
		wantUIDKept bool
	}{
		{
			name:    "create without uid",
			prior:   nil,
			unknown: []string{"uid", "url", "version"},
			proposed: func(d map[string]any) {
				delete(d, "uid")
			},
		},
		{
			name:        "create with uid",
			prior:       nil,
			unknown:     []string{"url", "version"},
			proposed:    func(map[string]any) {},
			wantUIDKept: true,
		},
		{
			name:     "message only",
			prior:    stored(),
			proposed: func(d map[string]any) { d["message"] = "tweak" },
			wantNoOp: true,
		},
		{
			name:     "overwrite only",
			prior:    stored(),
			proposed: func(d map[string]any) { d["overwrite"] = true },
			wantNoOp: true,
		},
		{
			name:     "empty values render identically",
			prior:    stored(),
			proposed: func(d map[string]any) { d["description"] = "" },
			wantNoOp: true,
		},
		{
			name:     "title change",
			prior:    stored(),
			proposed: func(d map[string]any) { d["title"] = "Renamed" },
			unknown:  []string{"url", "version"},
		},
		{
			name:  "panel option change",
			prior: stored(),
			proposed: func(d map[string]any) {
				d["panel"].([]any)[0].(map[string]any)["options"] = map[string]any{"legend": map[string]any{"showLegend": false}}
			},
			unknown: []string{"url", "version"},
		},
		{
			name:     "folder change",
			prior:    stored(),
			proposed: func(d map[string]any) { d["folder_uid"] = "team-a" },
			unknown:  []string{"url", "version"},
		},
		{
			name:     "uid change",
			prior:    stored(),
			proposed: func(d map[string]any) { d["uid"] = "acc-other" },
			unknown:  []string{"url", "version"},
			replace:  true,
		},
		{
			name:  "unknown title",
			prior: stored(),
			proposed: func(d map[string]any) {
				d["title"] = schema.Unknown
			},
			unknown: []string{"url", "version"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposed := testDashboard()
			prior := nullState(t)
			if tt.prior != nil {
				prior = dynamicValue(t, tfschema.Resource, tt.prior)
				proposed = maps.Clone(tt.prior)
				proposed["panel"] = testDashboard()["panel"]
			}
			tt.proposed(proposed)
			resp := plan(t, prior, dynamicValue(t, tfschema.Resource, proposed))
			planned := stateTree(t, resp.PlannedState)

			isUnknown := map[string]bool{}
			for _, k := range tt.unknown {
				isUnknown[k] = true
			}
			for _, k := range []string{"uid", "url", "version"} {
				if got := schema.IsUnknown(planned[k]); got != isUnknown[k] {
					t.Errorf("%s unknown = %v, want %v", k, got, isUnknown[k])
				}
			}
			if tt.wantNoOp && planned["url"] != "http://grafana.test/d/acc-unit/unit" {
				t.Errorf("url = %v, want prior url kept", planned["url"])
			}
			if tt.wantUIDKept && planned["uid"] != "acc-unit" {
				t.Errorf("uid = %v", planned["uid"])
			}
			if got := len(resp.RequiresReplace) == 1 && resp.RequiresReplace[0].Equal(tftypes.NewAttributePath().WithAttributeName("uid")); got != tt.replace {
				t.Errorf("requires replace = %v, want %v", resp.RequiresReplace, tt.replace)
			}
		})
	}
}

func TestPlanResourceChangeDestroy(t *testing.T) {
	resp := plan(t, dynamicValue(t, tfschema.Resource, stored()), nullState(t))
	if stateTree(t, resp.PlannedState) != nil {
		t.Error("planned state should stay null")
	}
}

func TestImportResourceState(t *testing.T) {
	tests := []struct {
		id      string
		wantErr bool
	}{
		{id: "abc123"},
		{id: "with-dash_and_underscore"},
		{id: strings.Repeat("a", 40)},
		{id: "", wantErr: true},
		{id: "has space", wantErr: true},
		{id: "folder/uid", wantErr: true},
		{id: strings.Repeat("a", 41), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			resp, err := New("test").ImportResourceState(t.Context(), &tfprotov6.ImportResourceStateRequest{TypeName: tfschema.ResourceType, ID: tt.id})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantErr {
				if len(resp.Diagnostics) == 0 || len(resp.ImportedResources) != 0 {
					t.Errorf("want error, got %v", resp)
				}
				return
			}
			if len(resp.Diagnostics) > 0 || len(resp.ImportedResources) != 1 {
				t.Fatalf("diagnostics: %s", errorSummaries(resp.Diagnostics))
			}
			imported := resp.ImportedResources[0]
			if imported.TypeName != tfschema.ResourceType || stateTree(t, imported.State)["uid"] != tt.id {
				t.Errorf("imported = %+v", imported)
			}
		})
	}
}

func TestUpgradeResourceState(t *testing.T) {
	// Dynamically typed block lists are stored as {"value", "type"} pairs.
	raw := `{"uid":"acc-unit","title":"Unit","removed_attribute":"x"`
	for _, a := range tfschema.Resource.Attributes {
		if a.Name != "uid" && a.Name != "title" {
			raw += `,"` + a.Name + `":null`
		}
	}
	for _, b := range tfschema.Resource.Blocks {
		raw += `,"` + b.Name + `":{"value":[],"type":["tuple",[]]}`
	}
	raw += "}"

	resp, err := New("test").UpgradeResourceState(t.Context(), &tfprotov6.UpgradeResourceStateRequest{
		TypeName: tfschema.ResourceType,
		RawState: &tfprotov6.RawState{JSON: []byte(raw)},
	})
	if err != nil || len(resp.Diagnostics) > 0 {
		t.Fatalf("upgrade: %v %s", err, errorSummaries(resp.Diagnostics))
	}
	got := stateTree(t, resp.UpgradedState)
	if got["uid"] != "acc-unit" || got["title"] != "Unit" {
		t.Errorf("upgraded = %v", got)
	}
}

func TestUpgradeResourceStateInvalid(t *testing.T) {
	resp, _ := New("test").UpgradeResourceState(t.Context(), &tfprotov6.UpgradeResourceStateRequest{
		TypeName: tfschema.ResourceType,
		RawState: &tfprotov6.RawState{JSON: []byte(`{"title": 5, "uid": [`)},
	})
	if len(resp.Diagnostics) == 0 {
		t.Error("want a diagnostic for malformed state")
	}
}
