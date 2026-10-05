package dashboard

import (
	"encoding/json"
	"testing"
)

func TestDecodeFallsBackToExtra(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		key     string // key expected in top-level extra
		notWant string // tree key that must be absent
	}{
		{
			name:    "panel after collapsed row",
			model:   `{"panels": [{"type": "row", "collapsed": true, "panels": []}, {"type": "stat"}]}`,
			key:     "panels",
			notWant: "row",
		},
		{
			name:    "non-object panel entry",
			model:   `{"panels": [1]}`,
			key:     "panels",
			notWant: "panel",
		},
		{
			name:    "panel with type row inside collapsed row",
			model:   `{"panels": [{"type": "row", "collapsed": true, "panels": [{"type": "row"}]}]}`,
			key:     "panels",
			notWant: "row",
		},
		{
			name:    "row with non-list panels",
			model:   `{"panels": [{"type": "row", "panels": "x"}]}`,
			key:     "panels",
			notWant: "row",
		},
		{
			name:    "expanded row with nested panels",
			model:   `{"panels": [{"type": "row", "collapsed": false, "panels": [{"type": "stat"}]}]}`,
			key:     "panels",
			notWant: "row",
		},
		{
			name:    "collapsed row with non-object nested panel",
			model:   `{"panels": [{"type": "row", "collapsed": true, "panels": [3]}]}`,
			key:     "panels",
			notWant: "row",
		},
		{
			name:    "templating with extra keys",
			model:   `{"templating": {"list": [{"name": "a", "type": "custom"}], "enable": true}}`,
			key:     "templating",
			notWant: "variable",
		},
		{
			name:    "variable without name",
			model:   `{"templating": {"list": [{"type": "custom"}]}}`,
			key:     "templating",
			notWant: "variable",
		},
		{
			name:    "annotations with non-object element",
			model:   `{"annotations": {"list": ["x"]}}`,
			key:     "annotations",
			notWant: "annotation",
		},
		{
			name:    "links that are not a list",
			model:   `{"links": {}}`,
			key:     "links",
			notWant: "link",
		},
		{
			name:    "time with extra keys",
			model:   `{"time": {"from": "now-1h", "to": "now", "zone": "utc"}}`,
			key:     "time",
			notWant: "time",
		},
		{
			name:    "time missing required key",
			model:   `{"time": {"from": "now-1h", "to": null}}`,
			key:     "time",
			notWant: "time",
		},
		{
			name:    "type mismatch on dashboard field",
			model:   `{"title": 5, "refresh": false, "editable": "yes", "graphTooltip": "1", "tags": ["a", 1]}`,
			key:     "title",
			notWant: "title",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := parse(t, tt.model)
			tree := Decode(model)
			extra, _ := tree["extra"].(map[string]any)
			if _, ok := extra[tt.key]; !ok {
				t.Errorf("extra = %v, want key %q", extra, tt.key)
			}
			if _, ok := tree[tt.notWant]; ok {
				t.Errorf("tree has %q: %v", tt.notWant, tree[tt.notWant])
			}
			if !Matches(tree, withSchema(model)) {
				t.Errorf("tree does not render back to the model: %v", tree)
			}
		})
	}
}

func withSchema(model map[string]any) map[string]any {
	out := map[string]any{"schemaVersion": json.Number("41")}
	for k, v := range model {
		out[k] = v
	}
	return out
}

func TestDecodeTypeMismatchKeepsAllFieldsInExtra(t *testing.T) {
	tree := Decode(parse(t, `{"title": 5, "refresh": false, "editable": "yes", "graphTooltip": "1", "tags": ["a", 1]}`))
	for _, key := range []string{"title", "refresh", "editable", "graph_tooltip", "tags"} {
		if _, ok := tree[key]; ok {
			t.Errorf("tree has typed %q", key)
		}
	}
	extra := tree["extra"].(map[string]any)
	for _, key := range []string{"title", "refresh", "editable", "graphTooltip", "tags"} {
		if _, ok := extra[key]; !ok {
			t.Errorf("extra lacks %q", key)
		}
	}
}

func TestDecodePanelMismatches(t *testing.T) {
	tree := Decode(parse(t, `{"panels": [{
		"type": "stat",
		"title": 5,
		"gridPos": {"x": 0, "y": 0, "w": 1, "h": 1, "static": true},
		"maxDataPoints": "100",
		"options": {"a": 1}
	}]}`))
	panel := blockList(tree["panel"])[0]
	if panel["type"] != "stat" || panel["options"] == nil {
		t.Errorf("typed fields lost: %v", panel)
	}
	for _, key := range []string{"title", "grid_pos", "max_data_points"} {
		if _, ok := panel[key]; ok {
			t.Errorf("panel has typed %q", key)
		}
	}
	extra := panel["extra"].(map[string]any)
	for _, key := range []string{"title", "gridPos", "maxDataPoints"} {
		if _, ok := extra[key]; !ok {
			t.Errorf("panel extra lacks %q", key)
		}
	}
}

func TestDecodeDropsEmptyAndNull(t *testing.T) {
	tree := Decode(parse(t, `{
		"id": 9,
		"version": 4,
		"uid": "",
		"title": "",
		"description": "",
		"refresh": null,
		"timezone": null,
		"panels": [{"type": "stat", "title": null, "description": "", "datasource": null}]
	}`))
	if tree["extra"] != nil {
		t.Errorf("extra = %v, want nil", tree["extra"])
	}
	if title, ok := tree["title"]; !ok || title != "" {
		t.Errorf("required empty title should be kept, got %v (present %v)", title, ok)
	}
	for _, key := range []string{"id", "version", "uid", "description", "refresh", "timezone"} {
		if _, ok := tree[key]; ok {
			t.Errorf("tree has %q", key)
		}
	}
	panel := blockList(tree["panel"])[0]
	for _, key := range []string{"title", "description", "datasource"} {
		if _, ok := panel[key]; ok {
			t.Errorf("panel has %q", key)
		}
	}
	if panel["extra"] != nil {
		t.Errorf("panel extra = %v, want nil", panel["extra"])
	}
}

func TestDecodeRowAndPanelNesting(t *testing.T) {
	tree := Decode(parse(t, `{"panels": [
		{"id": 1, "type": "stat"},
		{"id": 2, "type": "row", "collapsed": true, "title": "closed", "panels": [{"id": 3, "type": "stat"}]},
		{"id": 4, "type": "row", "collapsed": false, "title": "open", "panels": []},
		{"id": 5, "type": "stat"},
		{"id": 6, "type": "stat"}
	]}`))
	if got := len(blockList(tree["panel"])); got != 1 {
		t.Errorf("top-level panels = %d, want 1", got)
	}
	rows := blockList(tree["row"])
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if got := len(blockList(rows[0]["panel"])); got != 1 {
		t.Errorf("collapsed row panels = %d, want 1", got)
	}
	if got := len(blockList(rows[1]["panel"])); got != 2 {
		t.Errorf("open row panels = %d, want 2", got)
	}
	if extra, _ := rows[0]["extra"].(map[string]any); extra["type"] != nil {
		t.Error("row type leaked into extra")
	}
}

func TestDecodeLibraryPanelWithoutTypeStaysTyped(t *testing.T) {
	tree := Decode(parse(t, `{"panels": [{"id": 1, "gridPos": {"x": 0, "y": 0, "w": 8, "h": 8}, "libraryPanel": {"uid": "x", "name": "n"}, "title": "t"}]}`))
	if len(blockList(tree["panel"])) != 1 {
		t.Errorf("library panel not decoded to a panel block: %v", tree)
	}
}
