package convert

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
)

func parseHCL(t *testing.T, src []byte) *hclsyntax.Body {
	t.Helper()
	f, diags := hclsyntax.ParseConfig(src, "test.tf", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatalf("parse: %s\n%s", diags, src)
	}
	return f.Body.(*hclsyntax.Body)
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantTitle  string
		wantUID    any
		wantFolder string
	}{
		{"raw model", `{"title":"T","uid":"u1"}`, "T", "u1", ""},
		{"api response", `{"dashboard":{"title":"T","uid":"u1"},"meta":{"folderUid":"f1"}}`, "T", "u1", "f1"},
		{"api response without meta", `{"dashboard":{"title":"T"}}`, "T", nil, ""},
		{"v1 resource", `{
			"apiVersion": "dashboard.grafana.app/v1beta1",
			"kind": "Dashboard",
			"metadata": {"name": "res-uid", "annotations": {"grafana.app/folder": "f2"}},
			"spec": {"title": "T"}
		}`, "T", "res-uid", "f2"},
		{"v1 resource keeps spec uid", `{
			"kind": "Dashboard",
			"metadata": {"name": "res-uid"},
			"spec": {"title": "T", "uid": "spec-uid"}
		}`, "T", "spec-uid", ""},
		{"v1 resource without metadata", `{"kind":"Dashboard","spec":{"title":"T"}}`, "T", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, err := Load([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if src.Model["title"] != tt.wantTitle {
				t.Errorf("title = %v, want %q", src.Model["title"], tt.wantTitle)
			}
			if src.Model["uid"] != tt.wantUID {
				t.Errorf("uid = %v, want %v", src.Model["uid"], tt.wantUID)
			}
			if src.FolderUID != tt.wantFolder {
				t.Errorf("folder = %q, want %q", src.FolderUID, tt.wantFolder)
			}
		})
	}
}

func TestLoadKeepsNumbersExact(t *testing.T) {
	src, err := Load([]byte(`{"title":"T","big":12345678901234567890,"small":0.1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := src.Model["big"].(interface{ String() string }).String(); got != "12345678901234567890" {
		t.Errorf("big = %s", got)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"invalid JSON", `{"title":`, "invalid JSON"},
		{"not an object", `[1,2]`, "invalid JSON"},
		{"empty", ``, "empty input"},
		{"yaml list", "- a\n- b", "invalid JSON"},
		{"missing title", `{"uid":"u"}`, "missing title"},
		{"non-string title", `{"title":5}`, "missing title"},
		{"elements", `{"title":"T","elements":{}}`, "v2 dashboards"},
		{"elements in api response", `{"dashboard":{"title":"T","elements":{}}}`, "v2 dashboards"},
		{"v2 apiVersion", `{"apiVersion":"dashboard.grafana.app/v2beta1","kind":"Dashboard","spec":{"title":"T"}}`, "not supported"},
		{"v2alpha1 apiVersion", `{"apiVersion":"dashboard.grafana.app/v2alpha1","kind":"Dashboard","spec":{"title":"T"}}`, "not supported"},
		{"resource without spec", `{"kind":"Dashboard","metadata":{"name":"x"}}`, "no spec"},
		{"resource without title", `{"kind":"Dashboard","spec":{}}`, "missing title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load([]byte(tt.in))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestCheck(t *testing.T) {
	if _, err := Check(Source{Model: map[string]any{"title": "T"}}); err != nil {
		t.Errorf("valid model: %v", err)
	}
	if _, err := Check(Source{Model: map[string]any{"title": "T", "elements": nil}}); err == nil {
		t.Error("elements accepted")
	}
	if _, err := Check(Source{}); err == nil {
		t.Error("empty source accepted")
	}
}

func TestConvertImport(t *testing.T) {
	model := func() map[string]any { return map[string]any{"title": "My Dash", "uid": "abc"} }
	noUID := func() map[string]any { return map[string]any{"title": "My Dash"} }

	tests := []struct {
		name         string
		model        map[string]any
		opts         Options
		wantImport   bool
		wantWarnings []string
	}{
		{"import with uid", model(), Options{Import: true}, true, nil},
		{"no import requested", model(), Options{}, false, nil},
		{"import without uid warns", noUID(), Options{Import: true}, false, []string{"my_dash: no uid, skipping import block"}},
		{"no warning when not importing", noUID(), Options{}, false, nil},
		{"empty uid warns", map[string]any{"title": "My Dash", "uid": ""}, Options{Import: true}, false, []string{"my_dash: no uid, skipping import block"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Convert(Source{Model: tt.model}, "my_dash", tt.opts)
			body := parseHCL(t, out.HCL)
			hasImport := false
			for _, b := range body.Blocks {
				hasImport = hasImport || b.Type == "import"
			}
			if hasImport != tt.wantImport {
				t.Errorf("import block = %v, want %v\n%s", hasImport, tt.wantImport, out.HCL)
			}
			if tt.wantImport && !strings.Contains(string(out.HCL), "to = terradash_dashboard.my_dash") {
				t.Errorf("import target missing:\n%s", out.HCL)
			}
			if diff := cmp.Diff(tt.wantWarnings, out.Warnings); diff != "" {
				t.Errorf("warnings (-want +got):\n%s", diff)
			}
			if out.Name != "my_dash" {
				t.Errorf("Name = %q", out.Name)
			}
		})
	}
}

func TestConvertResource(t *testing.T) {
	src := Source{Model: map[string]any{
		"title":   "My Dash",
		"uid":     "abc",
		"id":      float64(7),
		"version": float64(3),
		"panels": []any{
			map[string]any{"type": "stat", "title": "P", "gridPos": map[string]any{"x": 0, "y": 0, "w": 12, "h": 8}},
		},
	}}
	out := Convert(src, "main", Options{})
	body := parseHCL(t, out.HCL)
	if len(body.Blocks) != 1 || body.Blocks[0].Type != "resource" ||
		strings.Join(body.Blocks[0].Labels, ".") != "terradash_dashboard.main" {
		t.Fatalf("unexpected blocks:\n%s", out.HCL)
	}
	res := body.Blocks[0].Body
	if len(res.Blocks) != 1 || res.Blocks[0].Type != "panel" {
		t.Errorf("want one panel block:\n%s", out.HCL)
	}
	for _, name := range []string{"title", "uid"} {
		if _, ok := res.Attributes[name]; !ok {
			t.Errorf("attribute %s missing", name)
		}
	}
	for _, name := range []string{"id", "version", "folder_uid"} {
		if _, ok := res.Attributes[name]; ok {
			t.Errorf("attribute %s should be absent", name)
		}
	}
	if got := string(hclwrite.Format(out.HCL)); got != string(out.HCL) {
		t.Errorf("HCL not formatted:\n%s", out.HCL)
	}
	if len(out.Variables) != 0 {
		t.Errorf("Variables = %v", out.Variables)
	}
}

func TestConvertDoesNotMutateSource(t *testing.T) {
	model := map[string]any{
		"title":    "T",
		"__inputs": []any{map[string]any{"name": "DS", "type": "datasource"}},
	}
	Convert(Source{Model: model}, "t", Options{})
	if _, ok := model["__inputs"]; !ok {
		t.Error("Convert removed __inputs from the source model")
	}
}

func TestConvertFolder(t *testing.T) {
	tests := []struct {
		name string
		src  string
		opts Options
		want string
	}{
		{"none", "", Options{}, ""},
		{"from source", "src-folder", Options{}, `"src-folder"`},
		{"override", "src-folder", Options{FolderUID: "override"}, `"override"`},
		{"override without source folder", "", Options{FolderUID: "override"}, `"override"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Convert(Source{Model: map[string]any{"title": "T"}, FolderUID: tt.src}, "t", tt.opts)
			attr, ok := parseHCL(t, out.HCL).Blocks[0].Body.Attributes["folder_uid"]
			if tt.want == "" {
				if ok {
					t.Errorf("folder_uid present:\n%s", out.HCL)
				}
				return
			}
			if !ok {
				t.Fatalf("folder_uid missing:\n%s", out.HCL)
			}
			v, _ := attr.Expr.Value(nil)
			if got := `"` + v.AsString() + `"`; got != tt.want {
				t.Errorf("folder_uid = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestConvertInputs(t *testing.T) {
	src := Source{Model: map[string]any{
		"title": "T",
		"__inputs": []any{
			map[string]any{"name": "DS_PROMETHEUS", "label": "Prometheus", "description": "Main", "pluginName": "Prometheus", "type": "datasource", "value": ""},
			map[string]any{"name": "VAR_ENV", "label": "Env", "type": "constant", "value": "prod"},
			map[string]any{"name": "DS-LOKI", "type": "datasource"},
			map[string]any{"label": "no name"},
		},
		"__requires": []any{map[string]any{"type": "panel", "id": "stat"}},
		"__elements": map[string]any{},
		"panels": []any{
			map[string]any{"type": "stat", "datasource": map[string]any{"type": "prometheus", "uid": "${DS_PROMETHEUS}"}, "title": "env=${VAR_ENV}"},
		},
	}}
	out := Convert(src, "t", Options{})

	prod := "prod"
	wantVars := map[string]Variable{
		"ds_prometheus": {Description: "Prometheus - Main - Prometheus"},
		"var_env":       {Description: "Env", Default: &prod},
		"ds_loki":       {},
	}
	if diff := cmp.Diff(wantVars, out.Variables); diff != "" {
		t.Errorf("variables (-want +got):\n%s", diff)
	}

	hclSrc := string(out.HCL)
	for _, want := range []string{"uid  = var.ds_prometheus", `title = "env=${var.var_env}"`} {
		if !strings.Contains(hclSrc, want) {
			t.Errorf("HCL missing %q:\n%s", want, hclSrc)
		}
	}
	for _, bad := range []string{"__inputs", "__requires", "__elements", "DS_PROMETHEUS"} {
		if strings.Contains(hclSrc, bad) {
			t.Errorf("HCL contains %q:\n%s", bad, hclSrc)
		}
	}
	parseHCL(t, out.HCL)
}

func TestConvertSpecialFieldsKeptWithoutInputs(t *testing.T) {
	out := Convert(Source{Model: map[string]any{"title": "T", "__requires": []any{}}}, "t", Options{})
	if !strings.Contains(string(out.HCL), "__requires") {
		t.Errorf("__requires dropped without __inputs:\n%s", out.HCL)
	}
}

func TestVariablesHCL(t *testing.T) {
	def := "x"
	got := VariablesHCL(map[string]Variable{
		"b": {Description: "Second", Default: &def},
		"a": {},
		"c": {Description: `has "quotes"`},
	})
	want := `variable "a" {
  type = string
}

variable "b" {
  type        = string
  description = "Second"
  default     = "x"
}

variable "c" {
  type        = string
  description = "has \"quotes\""
}
`
	if diff := cmp.Diff(want, string(got)); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
	parseHCL(t, got)
	if got := VariablesHCL(nil); len(got) != 0 {
		t.Errorf("empty = %q", got)
	}
}

func TestIdentifier(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"My Dashboard", "my_dashboard"},
		{"already_ok", "already_ok"},
		{"  spaces  and---dashes  ", "spaces_and_dashes"},
		{"Node Exporter / Full (v2)", "node_exporter_full_v2"},
		{"UPPER", "upper"},
		{"123 services", "d_123_services"},
		{"1", "d_1"},
		{"", "d"},
		{"!!!", "d"},
		{"___", "d"},
		{"日本語", "d"},
		{"Ünï côdé", "n_c_d"},
		{"日本語 dash", "dash"},
		{"a__b", "a_b"},
		{"a -- b", "a_b"},
		{strings.Repeat("a", 100), strings.Repeat("a", 64)},
		{strings.Repeat("a", 63) + " bbb", strings.Repeat("a", 63)},
		{"x" + strings.Repeat("y", 100), "x" + strings.Repeat("y", 63)},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := Identifier(tt.in); got != tt.want {
				t.Errorf("Identifier(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !hclsyntax.ValidIdentifier(Identifier(tt.in)) {
				t.Errorf("Identifier(%q) is not a valid HCL identifier", tt.in)
			}
		})
	}
}

func TestNamer(t *testing.T) {
	var n Namer
	titles := []string{"CPU", "cpu", "CPU!", "Memory", "cpu_2", "cpu", "", "?"}
	want := []string{"cpu", "cpu_2", "cpu_3", "memory", "cpu_2_2", "cpu_4", "d", "d_2"}
	for i, title := range titles {
		if got := n.Name(title); got != want[i] {
			t.Errorf("Name(%q) = %q, want %q", title, got, want[i])
		}
	}
	if got := n.Name("Variables"); got != "variables_2" {
		t.Errorf("Name(\"Variables\") = %q, want \"variables_2\"", got)
	}
}

func TestSourceTitle(t *testing.T) {
	tests := []struct {
		name string
		src  Source
		want string
	}{
		{"present", Source{Model: map[string]any{"title": "My Dashboard"}}, "My Dashboard"},
		{"missing", Source{Model: map[string]any{"uid": "abc"}}, ""},
		{"not a string", Source{Model: map[string]any{"title": 123}}, ""},
		{"empty model", Source{Model: map[string]any{}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.src.Title(); got != tt.want {
				t.Errorf("Title() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadYAML(t *testing.T) {
	src, err := Load([]byte("apiVersion: dashboard.grafana.app/v1beta1\nkind: Dashboard\nmetadata:\n  name: yaml-uid\nspec:\n  title: From YAML\n  panels:\n    - type: stat\n      gridPos: {x: 0, y: 0, w: 6, h: 4}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if src.Model["uid"] != "yaml-uid" || src.Title() != "From YAML" {
		t.Errorf("model = %v", src.Model)
	}
	pos := src.Model["panels"].([]any)[0].(map[string]any)["gridPos"].(map[string]any)
	if _, ok := pos["y"]; !ok {
		t.Errorf("gridPos = %v, want key y kept as a string", pos)
	}
}
