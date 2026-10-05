package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

const (
	dashA = `{"uid":"aaa","title":"A","panels":[]}`
	dashB = `{"uid":"bbb","title":"B","panels":[]}`
)

func setup(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mustRun(t *testing.T, files map[string]string) Result {
	t.Helper()
	res, err := Run(setup(t, files))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range res.Outputs {
		if _, diags := hclsyntax.ParseConfig(o.HCL, o.Name+".tf", hcl.InitialPos); diags.HasErrors() {
			t.Fatalf("invalid HCL: %s\n%s", diags, o.HCL)
		}
	}
	return res
}

func hasWarning(res Result, want string) bool {
	for _, w := range res.Warnings {
		if strings.Contains(w, want) {
			return true
		}
	}
	return false
}

func wantContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestSingleFile(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = file("${path.module}/d.json")
  folder      = grafana_folder.x.uid
  overwrite   = true
  message     = "migrated"
}
`,
		"d.json": dashA,
	})
	if len(res.Outputs) != 1 || len(res.Warnings) != 0 {
		t.Fatalf("outputs=%d warnings=%v", len(res.Outputs), res.Warnings)
	}
	if res.Outputs[0].Name != "x" {
		t.Errorf("name = %q", res.Outputs[0].Name)
	}
	if len(res.Sources) != 1 || res.Sources[0].Name != "x" || filepath.Base(res.Sources[0].File) != "main.tf" {
		t.Errorf("sources = %+v", res.Sources)
	}
	wantContains(t, string(res.Outputs[0].HCL),
		"removed {", "from = grafana_dashboard.x", "destroy = false",
		"import {", `to = terragraph_dashboard.x`, `id = "aaa"`,
		`resource "terragraph_dashboard" "x"`,
		"folder_uid = grafana_folder.x.uid", "overwrite", "message", `"migrated"`)
}

func TestForEachFileset(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "d" {
  for_each    = fileset("${path.module}/dashboards", "*.json")
  config_json = file("${path.module}/dashboards/${each.key}")
}
`,
		"dashboards/a.json": dashA,
		"dashboards/b.json": dashB,
	})
	if len(res.Outputs) != 2 || len(res.Sources) != 1 {
		t.Fatalf("outputs=%d sources=%d warnings=%v", len(res.Outputs), len(res.Sources), res.Warnings)
	}
	if res.Outputs[0].Name != "d_a" || res.Outputs[1].Name != "d_b" {
		t.Errorf("names = %q, %q", res.Outputs[0].Name, res.Outputs[1].Name)
	}
	removed := 0
	for _, o := range res.Outputs {
		removed += strings.Count(string(o.HCL), "removed {")
	}
	if removed != 1 {
		t.Errorf("removed blocks = %d", removed)
	}
	wantContains(t, string(res.Outputs[0].HCL), "removed {", `id = "aaa"`, `"terragraph_dashboard" "d_a"`)
	wantContains(t, string(res.Outputs[1].HCL), `id = "bbb"`, `"terragraph_dashboard" "d_b"`)
}

func TestForEachMapAndDedupe(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "d" {
  for_each    = { "x/a.json" = "one", "y/a.json" = "two" }
  config_json = jsonencode({ uid = each.value, title = each.value })
}
`,
	})
	if len(res.Outputs) != 2 || res.Outputs[0].Name != "d_a" || res.Outputs[1].Name != "d_a_2" {
		t.Fatalf("outputs = %+v", res.Outputs)
	}
	wantContains(t, string(res.Outputs[0].HCL), `id = "one"`)
	wantContains(t, string(res.Outputs[1].HCL), `id = "two"`)
}

func TestForEachFolderEach(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "d" {
  for_each    = { a = "f" }
  config_json = jsonencode({ uid = "u", title = "T" })
  folder      = each.value
}
`,
	})
	if len(res.Outputs) != 1 || strings.Contains(string(res.Outputs[0].HCL), "folder_uid") {
		t.Fatalf("outputs = %+v", res.Outputs)
	}
	if !hasWarning(res, "depends on each") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestJSONEncodeInline(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = jsonencode({ uid = "inline", title = "Inline", panels = [] })
}
`,
	})
	if len(res.Outputs) != 1 || len(res.Warnings) != 0 {
		t.Fatalf("outputs=%d warnings=%v", len(res.Outputs), res.Warnings)
	}
	wantContains(t, string(res.Outputs[0].HCL), `id = "inline"`, `"Inline"`)
}

func TestTemplatefile(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = templatefile("${path.module}/t.json.tpl", { uid = "tpl-uid", title = "Templated" })
}
`,
		"t.json.tpl": `{"uid":"${uid}","title":"${title}","panels":[]}`,
	})
	if len(res.Outputs) != 1 || len(res.Warnings) != 0 {
		t.Fatalf("outputs=%d warnings=%v", len(res.Outputs), res.Warnings)
	}
	wantContains(t, string(res.Outputs[0].HCL), `id = "tpl-uid"`, `"Templated"`)
}

func TestJSONDecodeTrimspace(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = jsonencode(jsondecode(trimspace(file("d.json"))))
}
`,
		"d.json": "\n" + dashA + "\n",
	})
	if len(res.Outputs) != 1 {
		t.Fatalf("warnings=%v", res.Warnings)
	}
	wantContains(t, string(res.Outputs[0].HCL), `id = "aaa"`)
}

func TestInputsBecomeVariables(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = file("d.json")
}
`,
		"d.json": `{"uid":"u","title":"T","__inputs":[{"name":"DS_PROM","label":"Prom","type":"datasource","pluginId":"prometheus"}],"panels":[{"datasource":"${DS_PROM}"}]}`,
	})
	if len(res.Outputs) != 1 || len(res.Outputs[0].Variables) == 0 {
		t.Fatalf("outputs=%+v", res.Outputs)
	}
}

func TestNonStaticConfigJSON(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = var.json
}
`,
	})
	if len(res.Outputs) != 0 || len(res.Sources) != 0 {
		t.Errorf("outputs=%d sources=%d", len(res.Outputs), len(res.Sources))
	}
	if !hasWarning(res, "grafana_dashboard.x: skipped") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestSkippedCases(t *testing.T) {
	tests := []struct {
		name string
		tf   string
		want string
	}{
		{"no config_json", `resource "grafana_dashboard" "x" {}`, "no config_json"},
		{"missing file", `resource "grafana_dashboard" "x" { config_json = file("nope.json") }`, "skipped"},
		{"invalid json", `resource "grafana_dashboard" "x" { config_json = "not json" }`, "skipped"},
		{"non-string", `resource "grafana_dashboard" "x" { config_json = 5 == 5 }`, "skipped"},
		{"for_each var", `resource "grafana_dashboard" "x" {
  for_each = var.m
  config_json = "{}"
}`, "for_each cannot be evaluated"},
		{"for_each list", `resource "grafana_dashboard" "x" {
  for_each = ["a"]
  config_json = "{}"
}`, "for_each"},
		{"fileset doublestar", `resource "grafana_dashboard" "x" {
  for_each    = fileset(path.module, "**/*.json")
  config_json = file(each.key)
}`, "**"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := mustRun(t, map[string]string{"main.tf": tt.tf})
			if len(res.Outputs) != 0 {
				t.Errorf("outputs = %d", len(res.Outputs))
			}
			if !hasWarning(res, tt.want) {
				t.Errorf("warnings = %v, want %q", res.Warnings, tt.want)
			}
		})
	}
}

func TestUnsupportedArguments(t *testing.T) {
	tests := []struct {
		name, extra, want string
	}{
		{"count", "count = 1", `"count"`},
		{"org_id", `org_id = "2"`, `"org_id"`},
		{"provider", "provider = grafana.other", `"provider"`},
		{"depends_on", "depends_on = [grafana_folder.x]", `"depends_on"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := mustRun(t, map[string]string{
				"main.tf": "resource \"grafana_dashboard\" \"x\" {\n  " + tt.extra + "\n  config_json = jsonencode({ uid = \"u\", title = \"T\" })\n}\n",
			})
			if len(res.Outputs) != 1 {
				t.Errorf("outputs = %d", len(res.Outputs))
			}
			if !hasWarning(res, tt.want) {
				t.Errorf("warnings = %v", res.Warnings)
			}
		})
	}
}

func TestFolderID(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = jsonencode({ uid = "u", title = "T" })
  folder      = grafana_folder.x.id
}
`,
	})
	if len(res.Outputs) != 1 || !hasWarning(res, "numeric folder id") {
		t.Fatalf("outputs=%d warnings=%v", len(res.Outputs), res.Warnings)
	}
	wantContains(t, string(res.Outputs[0].HCL), "folder_uid = grafana_folder.x.id")
}

func TestMissingUID(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "x" {
  config_json = jsonencode({ title = "No uid" })
}
`,
	})
	if len(res.Outputs) != 1 || !hasWarning(res, "no uid") {
		t.Fatalf("outputs=%d warnings=%v", len(res.Outputs), res.Warnings)
	}
	out := string(res.Outputs[0].HCL)
	if strings.Contains(out, "import {") {
		t.Errorf("unexpected import:\n%s", out)
	}
	wantContains(t, out, "removed {", `"terragraph_dashboard" "x"`)
}

func TestIgnoresOtherBlocks(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_folder" "f" { title = "f" }
resource "terragraph_dashboard" "t" {}
data "grafana_dashboard" "d" {}
variable "v" {}
`,
	})
	if len(res.Outputs) != 0 || len(res.Warnings) != 0 || len(res.Sources) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestMultipleFiles(t *testing.T) {
	res := mustRun(t, map[string]string{
		"b.tf": `resource "grafana_dashboard" "second" { config_json = jsonencode({ uid = "2", title = "T" }) }`,
		"a.tf": `resource "grafana_dashboard" "first" { config_json = jsonencode({ uid = "1", title = "T" }) }
resource "grafana_dashboard" "third" { config_json = jsonencode({ uid = "3", title = "T" }) }`,
		"notes.txt": "ignored",
	})
	var names []string
	for _, o := range res.Outputs {
		names = append(names, o.Name)
		if strings.Count(string(o.HCL), "removed {") != 1 {
			t.Errorf("%s: want one removed block", o.Name)
		}
	}
	if strings.Join(names, ",") != "first,third,second" {
		t.Errorf("names = %v", names)
	}
	if len(res.Sources) != 3 || filepath.Base(res.Sources[2].File) != "b.tf" {
		t.Errorf("sources = %+v", res.Sources)
	}
}

func TestInvalidHCL(t *testing.T) {
	if _, err := Run(setup(t, map[string]string{"bad.tf": `resource "grafana_dashboard" "x" {`})); err == nil {
		t.Fatal("expected error")
	}
}

func TestEmptyDir(t *testing.T) {
	res, err := Run(t.TempDir())
	if err != nil || len(res.Outputs) != 0 {
		t.Errorf("res=%+v err=%v", res, err)
	}
}

func TestFilesetBehaviour(t *testing.T) {
	res := mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "d" {
  for_each    = fileset("dash", "*.json")
  config_json = file("dash/${each.value}")
}
`,
		"dash/a.json": dashA,
		"dash/b.txt":  "x",
	})
	if len(res.Outputs) != 1 || res.Outputs[0].Name != "d_a" {
		t.Errorf("outputs = %+v", res.Outputs)
	}
	res = mustRun(t, map[string]string{
		"main.tf": `resource "grafana_dashboard" "d" {
  for_each    = fileset("dash", "*.json")
  config_json = file("dash/${each.value}")
}
`,
	})
	if len(res.Outputs) != 0 {
		t.Errorf("empty fileset produced outputs")
	}
}

func TestRunForEachToset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"uid": "a", "title": "A"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tf := `resource "grafana_dashboard" "d" {
  for_each    = toset(["a.json"])
  config_json = file(each.value)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tf), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Outputs) != 1 || res.Outputs[0].Name != "d_a" {
		t.Errorf("outputs = %+v, warnings = %v", res.Outputs, res.Warnings)
	}
}
