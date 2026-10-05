package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/tfschema"
)

func TestReadDataSource(t *testing.T) {
	config := map[string]any{
		"title": "Rendered",
		"panel": []any{map[string]any{
			"type":    "stat",
			"title":   "Latency <5ms & more",
			"options": map[string]any{"reduceOptions": map[string]any{"calcs": []any{"lastNotNull"}}},
		}},
	}
	resp, err := New("test").ReadDataSource(t.Context(), &tfprotov6.ReadDataSourceRequest{
		TypeName: tfschema.DataSourceType,
		Config:   dynamicValue(t, tfschema.DataSource, config),
	})
	if err != nil || len(resp.Diagnostics) > 0 {
		t.Fatalf("read: %v %s", err, errorSummaries(resp.Diagnostics))
	}
	v, err := resp.State.Unmarshal(tfschema.DataSource.Type())
	if err != nil {
		t.Fatal(err)
	}
	state, err := tree(v)
	if err != nil {
		t.Fatal(err)
	}
	out := state["json"].(string)
	if state["title"] != "Rendered" {
		t.Errorf("title = %v, want config preserved", state["title"])
	}
	if !strings.Contains(out, "Latency <5ms & more") {
		t.Errorf("json is HTML-escaped:\n%s", out)
	}

	var model struct {
		Title         string `json:"title"`
		SchemaVersion int    `json:"schemaVersion"`
		Panels        []struct {
			Type    string         `json:"type"`
			Options map[string]any `json:"options"`
		} `json:"panels"`
	}
	if err := json.Unmarshal([]byte(out), &model); err != nil {
		t.Fatal(err)
	}
	if model.Title != "Rendered" || model.SchemaVersion != 41 {
		t.Errorf("model = %+v", model)
	}
	if len(model.Panels) != 1 || model.Panels[0].Type != "stat" || model.Panels[0].Options["reduceOptions"] == nil {
		t.Errorf("panels = %+v", model.Panels)
	}
}

func TestValidateDataResourceConfig(t *testing.T) {
	resp, err := New("test").ValidateDataResourceConfig(t.Context(), &tfprotov6.ValidateDataResourceConfigRequest{
		TypeName: tfschema.DataSourceType,
		Config: dynamicValue(t, tfschema.DataSource, map[string]any{
			"title": "Bad",
			"panel": []any{map[string]any{"type": "row"}},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Diagnostics) != 1 {
		t.Errorf("diagnostics = %q, want one", errorSummaries(resp.Diagnostics))
	}
}
