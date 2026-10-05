package dashboard

import (
	"encoding/json"
	"reflect"
	"testing"
)

const priorSource = `{
	"title": "T",
	"panel": [
		{"type": "stat", "title": "A"},
		{"type": "stat", "title": "B"}
	],
	"row": [{"title": "R", "panel": [{"type": "stat", "title": "C"}]}],
	"variable": [
		{"name": "a", "type": "custom", "label": ""},
		{"name": "b", "type": "custom", "label": ""}
	]
}`

// remoteOf renders prior the way Grafana would return it, with volatile keys.
func remoteOf(t *testing.T, prior map[string]any) map[string]any {
	t.Helper()
	remote := viaJSON(t, encode(t, prior))
	remote["id"] = json.Number("17")
	remote["version"] = json.Number("2")
	return remote
}

func remotePanel(remote map[string]any, i int) map[string]any {
	return remote["panels"].([]any)[i].(map[string]any)
}

func TestReconcileUnchangedReturnsPrior(t *testing.T) {
	prior := parse(t, priorSource)
	got := Reconcile(prior, remoteOf(t, prior))
	want := parse(t, priorSource)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Reconcile = %v, want prior %v", got, want)
	}
	panel := blockList(got["panel"])[0]
	if panel["id"] != nil || panel["grid_pos"] != nil {
		t.Errorf("auto-generated id/grid_pos leaked into state: %v", panel)
	}
	if got["schema_version"] != nil {
		t.Errorf("schema_version = %v, want null", got["schema_version"])
	}
}

func TestReconcileChangedTitle(t *testing.T) {
	prior := parse(t, priorSource)
	remote := remoteOf(t, prior)
	remotePanel(remote, 1)["title"] = "B2"

	got := Reconcile(prior, remote)

	want := parse(t, priorSource)
	blockList(want["panel"])[1]["title"] = "B2"
	if !Equal(got, want) {
		t.Errorf("Reconcile = %v, want %v", got, want)
	}
	panels := blockList(got["panel"])
	for i, p := range panels {
		if p["id"] != nil || p["grid_pos"] != nil {
			t.Errorf("panel %d lost its null id/grid_pos: %v", i, p)
		}
	}
	if got["schema_version"] != nil {
		t.Errorf("schema_version = %v, want null", got["schema_version"])
	}
	row := blockList(got["row"])[0]
	if row["id"] != nil || blockList(row["panel"])[0]["grid_pos"] != nil {
		t.Errorf("row lost its null attributes: %v", row)
	}
}

func TestReconcileAddedRemotePanel(t *testing.T) {
	prior := parse(t, priorSource)
	remote := remoteOf(t, prior)
	remote["panels"] = append(remote["panels"].([]any), map[string]any{
		"id": json.Number("40"), "type": "gauge", "title": "New",
		"gridPos": map[string]any{"x": json.Number("0"), "y": json.Number("40"), "w": json.Number("6"), "h": json.Number("4")},
	})
	// The new panel is rendered after the row, so it joins the row.
	got := Reconcile(prior, remote)
	row := blockList(got["row"])[0]
	children := blockList(row["panel"])
	if len(children) != 2 {
		t.Fatalf("row panels = %d, want 2", len(children))
	}
	if children[1]["title"] != "New" || children[1]["id"] != json.Number("40") {
		t.Errorf("added panel = %v", children[1])
	}
	if children[0]["grid_pos"] != nil || blockList(got["panel"])[0]["id"] != nil {
		t.Error("existing panels lost their prior null attributes")
	}
}

func TestReconcileAddedTopLevelPanel(t *testing.T) {
	prior := parse(t, `{"title": "T", "panel": [{"type": "stat", "title": "A"}]}`)
	remote := remoteOf(t, prior)
	remote["panels"] = append(remote["panels"].([]any), map[string]any{
		"id": json.Number("40"), "type": "gauge", "title": "New",
	})
	got := Reconcile(prior, remote)
	panels := blockList(got["panel"])
	if len(panels) != 2 || panels[1]["title"] != "New" {
		t.Fatalf("panels = %v", panels)
	}
	if panels[0]["id"] != nil || panels[0]["grid_pos"] != nil {
		t.Errorf("existing panel lost its null attributes: %v", panels[0])
	}
}

func TestReconcileVariablesMatchedByName(t *testing.T) {
	prior := parse(t, priorSource)
	remote := remoteOf(t, prior)
	list := remote["templating"].(map[string]any)["list"].([]any)
	list[0], list[1] = list[1], list[0]
	remotePanel(remote, 0)["title"] = "A2"

	got := Reconcile(prior, remote)

	vars := blockList(got["variable"])
	if len(vars) != 2 || vars[0]["name"] != "b" || vars[1]["name"] != "a" {
		t.Fatalf("variables = %v", vars)
	}
	for _, v := range vars {
		if v["label"] != "" {
			t.Errorf("variable %v lost its prior empty label: %v", v["name"], v["label"])
		}
	}
}

func TestReconcileSchemaVersion(t *testing.T) {
	prior := parse(t, priorSource)
	remote := remoteOf(t, prior)
	remotePanel(remote, 0)["title"] = "A2"
	if got := Reconcile(prior, remote); got["schema_version"] != nil {
		t.Errorf("schema_version = %v, want null", got["schema_version"])
	}

	remote["schemaVersion"] = json.Number("42")
	if got := Reconcile(prior, remote); got["schema_version"] != json.Number("42") {
		t.Errorf("schema_version = %v, want 42", got["schema_version"])
	}
}

func TestReconcileInvalidPriorDecodesRemote(t *testing.T) {
	prior := parse(t, `{"title": "T", "extra": "bad"}`)
	remote := parse(t, `{"title": "T", "schemaVersion": 41}`)
	got := Reconcile(prior, remote)
	if !Equal(got, Decode(remote)) {
		t.Errorf("Reconcile = %v, want %v", got, Decode(remote))
	}
}

func TestMatches(t *testing.T) {
	tree := parse(t, `{"title": "T", "panel": [{"type": "stat"}]}`)
	if !Matches(tree, remoteOf(t, tree)) {
		t.Error("tree should match its own rendering")
	}
	other := remoteOf(t, tree)
	other["title"] = "U"
	if Matches(tree, other) {
		t.Error("tree should not match a changed model")
	}
	if Matches(parse(t, `{"extra": 1}`), other) {
		t.Error("an unencodable tree should not match")
	}
}
