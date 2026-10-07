package dashboard

import (
	"reflect"
	"strings"
	"testing"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
)

func TestValidate(t *testing.T) {
	const (
		uidMsg = "uid must be 1-40 characters of letters, digits, '-' or '_'"
		idMsg  = "id must be a non-negative integer"
	)
	tests := []struct {
		name string
		tree string
		want []Diagnostic
	}{
		{name: "valid", tree: `{"uid": "ok_uid-1", "refresh": "5m", "graph_tooltip": 2, "panel": [{"type": "stat", "id": 1, "grid_pos": {"x": 0, "y": 0, "w": 24, "h": 1}}]}`},
		{name: "uid with bad characters", tree: `{"uid": "bad uid!"}`, want: []Diagnostic{{Path: []any{"uid"}, Summary: uidMsg}}},
		{name: "uid too long", tree: `{"uid": "` + strings.Repeat("a", 41) + `"}`, want: []Diagnostic{{Path: []any{"uid"}, Summary: uidMsg}}},
		{name: "empty uid", tree: `{"uid": ""}`, want: []Diagnostic{{Path: []any{"uid"}, Summary: uidMsg}}},
		{name: "refresh", tree: `{"refresh": "often"}`, want: []Diagnostic{{Path: []any{"refresh"}, Summary: "refresh must be an interval such as 30s, 5m or 1h"}}},
		{name: "refresh auto", tree: `{"refresh": "auto"}`},
		{name: "panel without type", tree: `{"panel": [{"title": "x"}]}`, want: []Diagnostic{{Path: []any{"panel", 0}, Summary: "type is required unless library_panel is set"}}},
		{name: "library panel without type", tree: `{"panel": [{"library_panel": {"uid": "x", "name": "n"}}]}`},
		{name: "graph_tooltip range", tree: `{"graph_tooltip": 3}`, want: []Diagnostic{{Path: []any{"graph_tooltip"}, Summary: "graph_tooltip must be an integer between 0 and 2"}}},
		{name: "graph_tooltip fraction", tree: `{"graph_tooltip": 1.5}`, want: []Diagnostic{{Path: []any{"graph_tooltip"}, Summary: "graph_tooltip must be an integer between 0 and 2"}}},
		{name: "fiscal year start month", tree: `{"fiscal_year_start_month": 12}`, want: []Diagnostic{{Path: []any{"fiscal_year_start_month"}, Summary: "fiscal_year_start_month must be an integer between 0 and 11"}}},
		{name: "schema version", tree: `{"schema_version": -1}`, want: []Diagnostic{{Path: []any{"schema_version"}, Summary: "schema_version must be an integer between 0 and 1000"}}},
		{
			name: "duplicate ids across rows",
			tree: `{"panel": [{"type": "stat", "id": 2}], "row": [{"id": 3, "panel": [{"type": "stat", "id": 2}]}]}`,
			want: []Diagnostic{{Path: []any{"row", 0, "panel", 0, "id"}, Summary: "duplicate panel id 2"}},
		},
		{
			name: "row id collides with panel id",
			tree: `{"panel": [{"type": "stat", "id": 2}], "row": [{"id": 2}]}`,
			want: []Diagnostic{{Path: []any{"row", 0, "id"}, Summary: "duplicate panel id 2"}},
		},
		{name: "repeated zero ids are fine", tree: `{"panel": [{"type": "stat", "id": 0}, {"type": "stat", "id": 0}]}`},
		{name: "negative id", tree: `{"panel": [{"type": "stat", "id": -1}]}`, want: []Diagnostic{{Path: []any{"panel", 0, "id"}, Summary: idMsg}}},
		{name: "fractional row id", tree: `{"row": [{"id": 1.5}]}`, want: []Diagnostic{{Path: []any{"row", 0, "id"}, Summary: idMsg}}},
		{
			name: "grid_pos ranges",
			tree: `{"panel": [{"type": "stat", "grid_pos": {"x": 24, "y": -1, "w": 0, "h": 0}}]}`,
			want: []Diagnostic{
				{Path: []any{"panel", 0, "grid_pos", "x"}, Summary: "x must be an integer between 0 and 23"},
				{Path: []any{"panel", 0, "grid_pos", "y"}, Summary: "y must be an integer between 0 and 1048576"},
				{Path: []any{"panel", 0, "grid_pos", "w"}, Summary: "w must be an integer between 1 and 24"},
				{Path: []any{"panel", 0, "grid_pos", "h"}, Summary: "h must be an integer between 1 and 1048576"},
			},
		},
		{
			name: "grid_pos width too large",
			tree: `{"row": [{"grid_pos": {"w": 25}}]}`,
			want: []Diagnostic{{Path: []any{"row", 0, "grid_pos", "w"}, Summary: "w must be an integer between 1 and 24"}},
		},
		{
			name: "grid_pos overflows the grid",
			tree: `{"row": [{"panel": [{"type": "stat", "grid_pos": {"x": 20, "w": 8}}]}]}`,
			want: []Diagnostic{{Path: []any{"row", 0, "panel", 0, "grid_pos"}, Summary: "x + w must not exceed 24"}},
		},
		{name: "grid_pos x without w", tree: `{"panel": [{"type": "stat", "grid_pos": {"x": 23}}]}`},
		{
			name: "row typed panel",
			tree: `{"panel": [{"type": "row"}]}`,
			want: []Diagnostic{{Path: []any{"panel", 0, "type"}, Summary: `use a row block instead of a panel with type "row"`}},
		},
		{
			name: "panel dynamic kinds",
			tree: `{"panel": [{"type": "stat", "datasource": 5, "targets": {}, "options": [], "field_config": "x", "transformations": {}, "links": "l", "library_panel": [], "max_per_row": 0}]}`,
			want: []Diagnostic{
				{Path: []any{"panel", 0, "datasource"}, Summary: "datasource must be of type object or string"},
				{Path: []any{"panel", 0, "targets"}, Summary: "targets must be of type list"},
				{Path: []any{"panel", 0, "options"}, Summary: "options must be of type object"},
				{Path: []any{"panel", 0, "field_config"}, Summary: "field_config must be of type object"},
				{Path: []any{"panel", 0, "transformations"}, Summary: "transformations must be of type list"},
				{Path: []any{"panel", 0, "links"}, Summary: "links must be of type list"},
				{Path: []any{"panel", 0, "library_panel"}, Summary: "library_panel must be of type object"},
				{Path: []any{"panel", 0, "max_per_row"}, Summary: "max_per_row must be an integer between 1 and 100"},
			},
		},
		{name: "panel datasource object or string", tree: `{"panel": [{"type": "stat", "datasource": "prom"}, {"type": "stat", "datasource": {"uid": "x"}}]}`},
		{
			name: "row datasource",
			tree: `{"row": [{"datasource": []}]}`,
			want: []Diagnostic{{Path: []any{"row", 0, "datasource"}, Summary: "datasource must be of type object or string"}},
		},
		{
			name: "duplicate refId",
			tree: `{"panel": [{"type": "stat", "targets": [{"refId": "A"}, {"refId": "B"}, {"refId": "A"}]}]}`,
			want: []Diagnostic{{Path: []any{"panel", 0, "targets", 2}, Summary: `duplicate refId "A"`, Warning: true}},
		},
		{
			name: "variable names",
			tree: `{"variable": [{"name": "bad-name", "type": "custom"}, {"name": "a", "type": "custom"}, {"name": "a", "type": "custom"}]}`,
			want: []Diagnostic{
				{Path: []any{"variable", 0, "name"}, Summary: "variable names may only contain letters, digits and '_'"},
				{Path: []any{"variable", 2, "name"}, Summary: `duplicate variable "a"`},
			},
		},
		{
			name: "variable type and fields",
			tree: `{"variable": [{"name": "a", "type": "magic", "hide": 3, "datasource": 1, "options": {}}]}`,
			want: []Diagnostic{
				{Path: []any{"variable", 0, "type"}, Summary: `unrecognised variable type "magic"`, Warning: true},
				{Path: []any{"variable", 0, "hide"}, Summary: "hide must be an integer between 0 and 2"},
				{Path: []any{"variable", 0, "datasource"}, Summary: "datasource must be of type object or string"},
				{Path: []any{"variable", 0, "options"}, Summary: "options must be of type list"},
			},
		},
		{name: "known variable types", tree: `{"variable": [{"name": "a", "type": "switch"}, {"name": "b", "type": "groupby"}]}`},
		{
			name: "extra conflicts with typed argument",
			tree: `{"title": "t", "extra": {"title": "x"}}`,
			want: []Diagnostic{{Path: []any{"extra"}, Summary: `extra key "title" conflicts with argument "title"`}},
		},
		{name: "extra key with unset argument", tree: `{"extra": {"title": "x"}}`},
		{
			name: "panel extra conflict",
			tree: `{"panel": [{"type": "stat", "extra": {"gridPos": {}}, "grid_pos": {"w": 4}}]}`,
			want: []Diagnostic{{Path: []any{"panel", 0, "extra"}, Summary: `extra key "gridPos" conflicts with argument "grid_pos"`}},
		},
		{
			name: "annotation and link extra conflicts",
			tree: `{"annotation": [{"name": "n", "extra": {"name": "m"}}], "link": [{"title": "l", "extra": {"title": "m"}}]}`,
			want: []Diagnostic{
				{Path: []any{"annotation", 0, "extra"}, Summary: `extra key "name" conflicts with argument "name"`},
				{Path: []any{"link", 0, "extra"}, Summary: `extra key "title" conflicts with argument "title"`},
			},
		},
		{name: "extra not an object", tree: `{"extra": [1]}`, want: []Diagnostic{{Path: []any{"extra"}, Summary: "extra must be an object"}}},
		{
			name: "reserved keys with blocks",
			tree: `{"panel": [{"type": "stat"}], "variable": [{"name": "a", "type": "custom"}], "annotation": [{"name": "n"}], "link": [{"title": "l"}],
				"extra": {"panels": [], "templating": {}, "annotations": {}, "links": []}}`,
			want: []Diagnostic{
				{Path: []any{"extra"}, Summary: `extra key "panels" conflicts with nested blocks`},
				{Path: []any{"extra"}, Summary: `extra key "templating" conflicts with nested blocks`},
				{Path: []any{"extra"}, Summary: `extra key "annotations" conflicts with nested blocks`},
				{Path: []any{"extra"}, Summary: `extra key "links" conflicts with nested blocks`},
			},
		},
		{name: "reserved keys without blocks", tree: `{"extra": {"panels": [], "templating": {}, "annotations": {}, "links": []}}`},
		{
			name: "row blocks count as nested panels",
			tree: `{"row": [{"title": "r"}], "extra": {"panels": []}}`,
			want: []Diagnostic{{Path: []any{"extra"}, Summary: fmtNested("panels")}},
		},
		{
			name: "row extra type and panels",
			tree: `{"row": [{"extra": {"type": "row"}}, {"extra": {"panels": []}, "panel": [{"type": "stat"}]}, {"extra": {"panels": []}}]}`,
			want: []Diagnostic{
				{Path: []any{"row", 0, "extra"}, Summary: fmtNested("type")},
				{Path: []any{"row", 1, "extra"}, Summary: fmtNested("panels")},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Validate(parse(t, tt.tree))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Validate =\n%#v\nwant\n%#v", got, tt.want)
			}
		})
	}
}

func fmtNested(key string) string {
	return `extra key "` + key + `" conflicts with nested blocks`
}

func TestValidateSkipsUnknown(t *testing.T) {
	u := schema.Unknown
	tree := map[string]any{
		"uid":           u,
		"refresh":       u,
		"graph_tooltip": u,
		"extra":         u,
		"panel": []any{map[string]any{
			"type":       u,
			"id":         u,
			"grid_pos":   map[string]any{"x": u, "w": u, "h": u},
			"datasource": u,
			"targets":    []any{map[string]any{"refId": u}, map[string]any{"refId": u}},
			"options":    map[string]any{"a": u},
			"extra":      u,
		}},
		"row": []any{map[string]any{"id": u, "grid_pos": u, "datasource": u, "extra": u}},
		"variable": []any{
			map[string]any{"name": u, "type": u, "hide": u, "options": u, "extra": u},
			map[string]any{"name": u, "type": u},
		},
	}
	if got := Validate(tree); len(got) != 0 {
		t.Errorf("Validate = %#v, want no diagnostics", got)
	}
}
