package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const migrateModule = `resource "grafana_folder" "x" {
  title = "x"
}

resource "grafana_dashboard" "good" {
  config_json = jsonencode({ uid = "g1", title = "Good" })
  folder      = grafana_folder.x.uid
}

resource "grafana_dashboard" "dynamic" {
  config_json = var.json
}

variable "json" {
  type = string
}
`

func TestMigrateErrors(t *testing.T) {
	empty := t.TempDir()
	writeFiles(t, empty, map[string]string{"main.tf": `resource "grafana_folder" "x" {}`})
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no dir", []string{"migrate"}, "exactly one"},
		{"nothing migratable", []string{"migrate", empty}, "no grafana_dashboard resources"},
		{"bad flag", []string{"migrate", "-nope", empty}, "nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, "", tt.args...)
			if code == 0 {
				t.Errorf("code = %d, stderr = %s", code, stderr)
			}
			contains(t, stderr, tt.want)
		})
	}
}

func TestMigrateStdout(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.tf": migrateModule})
	code, stdout, stderr := runCLI(t, "", "migrate", dir)
	if code != 0 {
		t.Fatalf("code = %d: %s", code, stderr)
	}
	parseHCL(t, stdout)
	contains(t, stdout, "removed {", "from = grafana_dashboard.good", "import {", `id = "g1"`,
		`resource "terradash_dashboard" "good"`, "folder_uid = grafana_folder.x.uid")
	contains(t, stderr, "warning:", "grafana_dashboard.dynamic")
	src, _ := os.ReadFile(filepath.Join(dir, "main.tf"))
	if string(src) != migrateModule {
		t.Errorf("source modified without -remove")
	}
}

func TestMigrateOutputFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"main.tf": migrateModule})
	out := filepath.Join(t.TempDir(), "out.tf")
	code, stdout, stderr := runCLI(t, "", "migrate", "-o", out, dir)
	if code != 0 {
		t.Fatalf("code = %d: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q", stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	parseHCL(t, string(data))
	contains(t, string(data), "removed {", "terradash_dashboard")

	code, _, stderr = runCLI(t, "", "migrate", "-o", out, dir)
	if code != 1 {
		t.Errorf("overwrite without -force: code = %d, stderr = %s", code, stderr)
	}
	if code, _, stderr = runCLI(t, "", "migrate", "-force", "-o", out, dir); code != 0 {
		t.Errorf("-force: code = %d, stderr = %s", code, stderr)
	}
}

func TestMigrateRemove(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"main.tf": migrateModule,
		"other.tf": `resource "grafana_dashboard" "second" {
  config_json = jsonencode({ uid = "s1", title = "Second" })
}
`,
	})
	out := filepath.Join(t.TempDir(), "out.tf")
	code, _, stderr := runCLI(t, "", "migrate", "-remove", "-o", out, dir)
	if code != 0 {
		t.Fatalf("code = %d: %s", code, stderr)
	}

	main, _ := os.ReadFile(filepath.Join(dir, "main.tf"))
	body := parseHCL(t, string(main))
	var kept []string
	for _, b := range body.Blocks {
		kept = append(kept, strings.Join(append([]string{b.Type}, b.Labels...), "."))
	}
	if got := strings.Join(kept, ","); got != "resource.grafana_folder.x,resource.grafana_dashboard.dynamic,variable.json" {
		t.Errorf("remaining blocks = %s", got)
	}

	other, _ := os.ReadFile(filepath.Join(dir, "other.tf"))
	if len(parseHCL(t, string(other)).Blocks) != 0 {
		t.Errorf("other.tf still has blocks:\n%s", other)
	}

	data, _ := os.ReadFile(out)
	contains(t, string(data), `"terradash_dashboard" "good"`, `"terradash_dashboard" "second"`)
	if strings.Contains(string(data), "dynamic") {
		t.Errorf("skipped dashboard in output:\n%s", data)
	}
}
