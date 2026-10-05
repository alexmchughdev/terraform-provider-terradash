package dashboard

import (
	"encoding/json"
	"reflect"
	"testing"
)

type box struct{ id, x, y, w, h int }

func toInt(t *testing.T, v any) int {
	t.Helper()
	switch n := v.(type) {
	case int:
		return n
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			t.Fatal(err)
		}
		return int(i)
	}
	t.Fatalf("not an integer: %#v", v)
	return 0
}

func boxes(t *testing.T, panels []any) []box {
	t.Helper()
	var out []box
	for _, p := range panels {
		m := p.(map[string]any)
		pos := m["gridPos"].(map[string]any)
		out = append(out, box{toInt(t, m["id"]), toInt(t, pos["x"]), toInt(t, pos["y"]), toInt(t, pos["w"]), toInt(t, pos["h"])})
	}
	return out
}

func TestLayout(t *testing.T) {
	tests := []struct {
		name string
		tree string
		want []box
	}{
		{
			name: "auto placement wraps with default size",
			tree: `{"panel": [{"type": "stat"}, {"type": "stat"}, {"type": "stat"}]}`,
			want: []box{{1, 0, 0, 12, 8}, {2, 12, 0, 12, 8}, {3, 0, 8, 12, 8}},
		},
		{
			name: "explicit position kept and auto panels follow it",
			tree: `{"panel": [{"type": "stat", "grid_pos": {"x": 3, "y": 5, "w": 6, "h": 4}}, {"type": "stat"}]}`,
			want: []box{{1, 3, 5, 6, 4}, {2, 9, 5, 12, 8}},
		},
		{
			name: "partial grid_pos only sets size",
			tree: `{"panel": [
				{"type": "stat", "grid_pos": {"w": 24, "h": 3}},
				{"type": "stat", "grid_pos": {"w": 6}},
				{"type": "stat"}
			]}`,
			want: []box{{1, 0, 0, 24, 3}, {2, 0, 3, 6, 8}, {3, 6, 3, 12, 8}},
		},
		{
			name: "expanded rows emit their panels flat after the row",
			tree: `{
				"panel": [{"type": "stat"}, {"type": "stat"}],
				"row": [
					{"title": "R1", "panel": [{"type": "stat"}, {"type": "stat"}]},
					{"title": "R2", "panel": [{"type": "stat"}]}
				]
			}`,
			want: []box{
				{1, 0, 0, 12, 8}, {2, 12, 0, 12, 8},
				{3, 0, 8, 24, 1}, {4, 0, 9, 12, 8}, {5, 12, 9, 12, 8},
				{6, 0, 17, 24, 1}, {7, 0, 18, 12, 8},
			},
		},
		{
			name: "collapsed row does not consume vertical space",
			tree: `{
				"panel": [{"type": "stat"}],
				"row": [
					{"title": "R1", "collapsed": true, "panel": [{"type": "stat"}, {"type": "stat"}]},
					{"title": "R2", "panel": [{"type": "stat"}]}
				]
			}`,
			want: []box{{1, 0, 0, 12, 8}, {2, 0, 8, 24, 1}, {5, 0, 9, 24, 1}, {6, 0, 10, 12, 8}},
		},
		{
			name: "auto ids skip explicit ids",
			tree: `{
				"panel": [{"type": "stat", "id": 2}, {"type": "stat"}],
				"row": [{"title": "R", "panel": [{"type": "stat", "id": 1}, {"type": "stat"}]}]
			}`,
			want: []box{{2, 0, 0, 12, 8}, {3, 12, 0, 12, 8}, {4, 0, 8, 24, 1}, {1, 0, 9, 12, 8}, {5, 12, 9, 12, 8}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := parse(t, tt.tree)
			out := encode(t, tree)
			if got := boxes(t, out["panels"].([]any)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("boxes = %v, want %v", got, tt.want)
			}
			if again := encode(t, tree); !reflect.DeepEqual(again, out) {
				t.Errorf("Encode is not deterministic:\n%v\n%v", again, out)
			}
		})
	}
}

func TestLayoutCollapsedRowNestsPanels(t *testing.T) {
	tree := parse(t, `{
		"row": [{"title": "R", "collapsed": true, "panel": [{"type": "stat"}, {"type": "stat"}]}]
	}`)
	panels := encode(t, tree)["panels"].([]any)
	if len(panels) != 1 {
		t.Fatalf("flat panels = %d, want only the row", len(panels))
	}
	row := panels[0].(map[string]any)
	if row["type"] != "row" {
		t.Errorf("type = %v, want row", row["type"])
	}
	want := []box{{2, 0, 1, 12, 8}, {3, 12, 1, 12, 8}}
	if got := boxes(t, row["panels"].([]any)); !reflect.DeepEqual(got, want) {
		t.Errorf("nested boxes = %v, want %v", got, want)
	}
}

func TestLayoutExpandedRowHasNoNestedPanels(t *testing.T) {
	tree := parse(t, `{"row": [{"title": "R", "panel": [{"type": "stat"}]}]}`)
	row := encode(t, tree)["panels"].([]any)[0].(map[string]any)
	if _, ok := row["panels"]; ok {
		t.Errorf("expanded row has nested panels: %v", row["panels"])
	}
}
