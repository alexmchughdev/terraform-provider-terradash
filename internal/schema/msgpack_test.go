package schema_test

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/dashboard"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/tfschema"
)

func TestMsgpackRoundTrip(t *testing.T) {
	tree := map[string]any{
		"title":         "Overview",
		"uid":           "abc",
		"tags":          []any{"a", "b"},
		"time":          map[string]any{"from": "now-6h", "to": "now"},
		"time_picker":   map[string]any{"hidden": false, "refresh_intervals": []any{"5s", "1m"}},
		"graph_tooltip": json.Number("1"),
		"panel": []any{
			map[string]any{
				"type":     "timeseries",
				"title":    "CPU",
				"grid_pos": map[string]any{"x": json.Number("0"), "w": json.Number("12")},
				"targets": []any{
					map[string]any{"expr": "up", "refId": "A", "hide": nil, "legend": []any{}},
				},
				"options": map[string]any{
					"legend":   map[string]any{"calcs": []any{}, "showLegend": true},
					"decimals": json.Number("0.1"),
					"big":      json.Number("12345678901234567890"),
					"tiny":     json.Number("1e-7"),
					"nothing":  nil,
				},
			},
			map[string]any{"type": "stat", "options": []any{}},
		},
		"row": []any{
			map[string]any{
				"title": "Row",
				"panel": []any{
					map[string]any{"type": "gauge", "options": map[string]any{"a": []any{nil, "x"}}},
					map[string]any{"type": "text"},
				},
			},
			map[string]any{"title": "Empty row"},
		},
		"variable": []any{
			map[string]any{"name": "ds", "type": "datasource", "query": "prometheus", "options": []any{}},
			map[string]any{"name": "env", "type": "custom", "options": []any{map[string]any{"text": "a", "selected": true}}},
		},
		"extra": map[string]any{"foo": map[string]any{"bar": json.Number("2")}},
	}

	for name, block := range map[string]schema.Block{"dashboard.Body": dashboard.Body, "tfschema.Resource": tfschema.Resource} {
		t.Run(name, func(t *testing.T) {
			v, err := schema.ToValue(block, tree)
			if err != nil {
				t.Fatal(err)
			}
			want, err := schema.FromValue(v)
			if err != nil {
				t.Fatal(err)
			}
			dv, err := tfprotov6.NewDynamicValue(block.Type(), v)
			if err != nil {
				t.Fatal(err)
			}
			back, err := dv.Unmarshal(block.Type())
			if err != nil {
				t.Fatal(err)
			}
			got, err := schema.FromValue(back)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}

			m := got.(map[string]any)
			options := m["panel"].([]any)[0].(map[string]any)["options"].(map[string]any)
			wantOptions := map[string]any{
				"legend":   map[string]any{"calcs": []any{}, "showLegend": true},
				"decimals": json.Number("0.1"),
				"big":      json.Number("12345678901234567890"),
				"tiny":     json.Number("1e-07"),
				"nothing":  nil,
			}
			if diff := cmp.Diff(wantOptions, options); diff != "" {
				t.Errorf("options (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMsgpackRoundTripUnknown(t *testing.T) {
	b := dashboard.Body
	v, err := schema.ToValue(b, map[string]any{
		"title": schema.Unknown,
		"panel": []any{map[string]any{"type": "stat", "options": map[string]any{"a": schema.Unknown, "b": "x"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want, err := schema.FromValue(v)
	if err != nil {
		t.Fatal(err)
	}
	dv, err := tfprotov6.NewDynamicValue(b.Type(), v)
	if err != nil {
		t.Fatal(err)
	}
	back, err := dv.Unmarshal(b.Type())
	if err != nil {
		t.Fatal(err)
	}
	got, err := schema.FromValue(back)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	if !schema.HasUnknown(got) {
		t.Error("unknown lost")
	}
}
