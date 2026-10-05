package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func loadFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return parse(t, string(b))
}

func extraKeys(tree map[string]any) []string {
	m, _ := tree["extra"].(map[string]any)
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		file      string
		generated bool // lacks panel ids, gridPos and schemaVersion
		extra     []string
		panels    int
		rows      int
	}{
		{file: "simple.json", panels: 3, extra: []string{"style"}},
		{file: "rows_expanded.json", panels: 1, rows: 2},
		{file: "rows_collapsed.json", panels: 1, rows: 3},
		{file: "full.json", panels: 3},
		{file: "legacy.json", panels: 1, extra: []string{"__inputs", "__requires", "gnetId", "refresh", "rows", "style"}},
		{file: "bare.json", generated: true, panels: 2, rows: 1},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			model := loadFixture(t, tt.file)
			tree := Decode(model)

			if got := extraKeys(tree); !slices.Equal(got, tt.extra) {
				t.Errorf("extra keys = %v, want %v", got, tt.extra)
			}
			if got := len(blockList(tree["panel"])); got != tt.panels {
				t.Errorf("panels = %d, want %d", got, tt.panels)
			}
			if got := len(blockList(tree["row"])); got != tt.rows {
				t.Errorf("rows = %d, want %d", got, tt.rows)
			}
			if tt.generated {
				assertOnlyGeneratedAdded(t, viaJSON(t, encode(t, tree)), model)
			} else if !Matches(tree, model) {
				t.Errorf("tree does not render to the model:\n%v", encode(t, tree))
			}

			// Decode(Encode(tree)) is stable and, for complete models, equal to tree.
			again := Decode(viaJSON(t, encode(t, tree)))
			if !tt.generated && !Equal(again, tree) {
				t.Errorf("Decode(Encode(tree)) = %v, want %v", again, tree)
			}
			third := Decode(viaJSON(t, encode(t, again)))
			if !Equal(third, again) {
				t.Errorf("second round trip changed the tree:\n%v\n%v", third, again)
			}
		})
	}
}

func assertOnlyGeneratedAdded(t *testing.T, out, model map[string]any) {
	t.Helper()
	if out["schemaVersion"] == nil {
		t.Error("schemaVersion not added")
	}
	delete(out, "schemaVersion")
	var strip func(panels []any)
	strip = func(panels []any) {
		for _, p := range panels {
			m := p.(map[string]any)
			if m["id"] == nil || m["gridPos"] == nil {
				t.Errorf("panel %v lacks generated id or gridPos", m["title"])
			}
			delete(m, "id")
			delete(m, "gridPos")
			nested, _ := m["panels"].([]any)
			strip(nested)
		}
	}
	strip(out["panels"].([]any))
	if !Equal(out, model) {
		t.Errorf("encoded model differs beyond generated fields:\n%v\nwant\n%v", out, model)
	}
}

func TestRoundTripFullVariables(t *testing.T) {
	tree := Decode(loadFixture(t, "full.json"))
	vars := blockList(tree["variable"])
	if len(vars) != 5 {
		t.Fatalf("variables = %d, want 5", len(vars))
	}
	wantExtra := map[string][]string{
		"datasource": nil,
		"cluster":    nil,
		"env":        nil,
		"step":       {"auto", "auto_count", "auto_min"},
		"filters":    {"filters"},
	}
	for _, v := range vars {
		name := v["name"].(string)
		if got := extraKeys(v); !slices.Equal(got, wantExtra[name]) {
			t.Errorf("variable %s extra = %v, want %v", name, got, wantExtra[name])
		}
	}
	if got := len(blockList(tree["annotation"])); got != 1 {
		t.Errorf("annotations = %d, want 1", got)
	}
	if got := len(blockList(tree["link"])); got != 2 {
		t.Errorf("links = %d, want 2", got)
	}
}

func TestRoundTripLegacyOddTypes(t *testing.T) {
	tree := Decode(loadFixture(t, "legacy.json"))
	extra := tree["extra"].(map[string]any)
	if extra["refresh"] != false {
		t.Errorf("refresh = %v, want false in extra", extra["refresh"])
	}
	if _, ok := tree["uid"]; ok {
		t.Error("empty uid should be dropped")
	}
	vr := blockList(tree["variable"])[0]
	if hide := vr["extra"].(map[string]any)["hide"]; hide != true {
		t.Errorf("variable hide = %v, want true in extra", hide)
	}
	if id := blockList(tree["panel"])[0]["id"]; id != json.Number("0") {
		t.Errorf("panel id = %v, want 0", id)
	}
}
