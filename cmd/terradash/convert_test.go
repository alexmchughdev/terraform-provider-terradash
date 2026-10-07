package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	dashA      = `{"title":"Alpha Board","uid":"a1","panels":[{"type":"stat","title":"P","gridPos":{"x":0,"y":0,"w":12,"h":8}}]}`
	dashB      = `{"title":"Beta","uid":"b1"}`
	dashNoUID  = `{"title":"No Uid"}`
	dashInputs = `{
		"title": "Shared",
		"uid": "s1",
		"__inputs": [
			{"name": "DS_PROMETHEUS", "label": "Prometheus", "type": "datasource", "pluginName": "Prometheus"},
			{"name": "VAR_ENV", "type": "constant", "value": "prod"}
		],
		"panels": [{"type": "stat", "datasource": {"uid": "${DS_PROMETHEUS}"}}]
	}`
)

func TestConvertStdin(t *testing.T) {
	code, stdout, stderr := runCLI(t, dashA, "convert", "-")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if got := resourceNames(t, stdout); !slices.Equal(got, []string{"alpha_board"}) {
		t.Errorf("resources = %v", got)
	}
	contains(t, stdout, `title = "Alpha Board"`)
	if strings.Contains(stdout, "import {") {
		t.Errorf("unexpected import block:\n%s", stdout)
	}
}

func TestConvertFlags(t *testing.T) {
	tests := []struct {
		name       string
		stdin      string
		args       []string
		wantStdout []string
		wantAbsent []string
		wantStderr string
	}{
		{"import", dashA, []string{"-import"}, []string{"import {", "to = terradash_dashboard.alpha_board", `id = "a1"`}, nil, ""},
		{"import without uid warns", dashNoUID, []string{"-import"}, []string{`resource "terradash_dashboard" "no_uid"`}, []string{"import {"}, "warning: no_uid: no uid, skipping import block"},
		{"folder uid", dashA, []string{"-folder-uid", "f9"}, []string{`folder_uid = "f9"`}, nil, ""},
		{"name", dashA, []string{"-name", "custom"}, []string{`"terradash_dashboard" "custom"`}, []string{"alpha_board"}, ""},
		{"api response folder", `{"dashboard":` + dashA + `,"meta":{"folderUid":"f1"}}`, nil, []string{`folder_uid = "f1"`}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"convert"}, tt.args...)
			args = append(args, "-")
			code, stdout, stderr := runCLI(t, tt.stdin, args...)
			if code != 0 {
				t.Fatalf("code = %d, stderr: %s", code, stderr)
			}
			parseHCL(t, stdout)
			contains(t, stdout, tt.wantStdout...)
			for _, bad := range tt.wantAbsent {
				if strings.Contains(stdout, bad) {
					t.Errorf("unexpected %q in:\n%s", bad, stdout)
				}
			}
			contains(t, stderr, tt.wantStderr)
		})
	}
}

func TestConvertInputsToVariables(t *testing.T) {
	code, stdout, stderr := runCLI(t, dashInputs, "convert", "-")
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	body := parseHCL(t, stdout)
	var kinds []string
	for _, b := range body.Blocks {
		kinds = append(kinds, b.Type+"."+strings.Join(b.Labels, "."))
	}
	want := []string{"variable.ds_prometheus", "variable.var_env", "resource.terradash_dashboard.shared"}
	if !slices.Equal(kinds, want) {
		t.Errorf("blocks = %v, want %v", kinds, want)
	}
	contains(t, stdout, `default = "prod"`, "uid = var.ds_prometheus")
}

func TestConvertDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"b.json":  dashB,
		"a.json":  dashA,
		"c.json":  dashA,
		"skip.md": "not json",
	})
	code, stdout, stderr := runCLI(t, "", "convert", dir)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	want := []string{"alpha_board", "beta", "alpha_board_2"}
	if got := resourceNames(t, stdout); !slices.Equal(got, want) {
		t.Errorf("resources = %v, want %v", got, want)
	}
}

func TestConvertMixedInputs(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.json": dashA})
	file := filepath.Join(dir, "b.json")
	writeFiles(t, dir, map[string]string{"b.json": dashB})
	code, stdout, stderr := runCLI(t, dashNoUID, "convert", "-", file, dir)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	want := []string{"no_uid", "beta", "alpha_board", "beta_2"}
	if got := resourceNames(t, stdout); !slices.Equal(got, want) {
		t.Errorf("resources = %v, want %v", got, want)
	}
}

func TestConvertOutputFile(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	writeFiles(t, dir, map[string]string{"in.json": dashInputs})
	out := filepath.Join(dir, "out.tf")

	code, stdout, stderr := runCLI(t, "", "convert", "-o", out, in)
	if code != 0 || stdout != "" {
		t.Fatalf("code = %d, stdout = %q, stderr: %s", code, stdout, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, string(data), `variable "ds_prometheus"`, `resource "terradash_dashboard" "shared"`)
	parseHCL(t, string(data))

	code, _, stderr = runCLI(t, "", "convert", "-o", out, in)
	if code != 1 {
		t.Errorf("overwrite without -force: code = %d, want 1", code)
	}
	contains(t, stderr, "already exists (use -force to overwrite)")

	if err := os.WriteFile(out, []byte("stale content that is much longer than the new output "+strings.Repeat("x", 5000)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLI(t, "", "convert", "-force", "-o", out, in)
	if code != 0 {
		t.Fatalf("-force: code = %d, stderr: %s", code, stderr)
	}
	data, _ = os.ReadFile(out)
	if strings.Contains(string(data), "stale") {
		t.Error("file was not truncated")
	}
	parseHCL(t, string(data))
}

func TestConvertOutputDirectory(t *testing.T) {
	in := t.TempDir()
	writeFiles(t, in, map[string]string{"a.json": dashA, "b.json": dashInputs})
	out := filepath.Join(t.TempDir(), "nested", "tf")

	code, _, stderr := runCLI(t, "", "convert", "-o", out, in)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		data, err := os.ReadFile(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		parseHCL(t, string(data))
	}
	if want := []string{"alpha_board.tf", "shared.tf", "variables.tf"}; !slices.Equal(names, want) {
		t.Errorf("files = %v, want %v", names, want)
	}
	vars, _ := os.ReadFile(filepath.Join(out, "variables.tf"))
	contains(t, string(vars), `variable "ds_prometheus"`, `variable "var_env"`)

	code, _, stderr = runCLI(t, "", "convert", "-o", out, in)
	if code != 1 {
		t.Errorf("overwrite without -force: code = %d, want 1", code)
	}
	contains(t, stderr, "already exists")
	if code, _, stderr := runCLI(t, "", "convert", "-force", "-o", out, in); code != 0 {
		t.Errorf("-force: code = %d, stderr: %s", code, stderr)
	}
}

func TestConvertOutputDirectoryWithoutVariables(t *testing.T) {
	out := t.TempDir()
	if code, _, stderr := runCLI(t, dashA, "convert", "-o", out, "-"); code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, "variables.tf")); !os.IsNotExist(err) {
		t.Errorf("variables.tf should not exist, err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "alpha_board.tf")); err != nil {
		t.Error(err)
	}
}

func TestConvertErrors(t *testing.T) {
	dir := t.TempDir()
	empty := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.json": dashA, "b.json": dashB, "bad.json": "{", "v2.json": `{"title":"T","elements":{}}`})

	tests := []struct {
		name    string
		stdin   string
		args    []string
		wantErr string
	}{
		{"no inputs", "", []string{"convert"}, "no input files"},
		{"missing file", "", []string{"convert", filepath.Join(dir, "missing.json")}, "no such file"},
		{"empty directory", "", []string{"convert", empty}, "no .json, .yaml or .yml files"},
		{"invalid JSON", "{", []string{"convert", "-"}, "-: invalid JSON"},
		{"invalid JSON file", "", []string{"convert", filepath.Join(dir, "bad.json")}, "bad.json: invalid JSON"},
		{"v2 dashboard", "", []string{"convert", filepath.Join(dir, "v2.json")}, "v2 dashboards are not supported"},
		{"missing title", `{"uid":"x"}`, []string{"convert", "-"}, "missing title"},
		{"name with multiple inputs", "", []string{"convert", "-name", "x", filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")}, "-name requires exactly one input"},
		{"name with directory of many", "", []string{"convert", "-name", "x", dir}, "-name requires exactly one input"},
		{"name invalid identifier", dashA, []string{"convert", "-name", "my dash", "-"}, "not a valid Terraform identifier"},
		{"name starts with ..", dashA, []string{"convert", "-name", "../x", "-"}, "not a valid Terraform identifier"},
		{"stdin twice", "", []string{"convert", "-", "-"}, "can only be read once"},
		{"folder uid invalid", dashA, []string{"convert", "-folder-uid", "bad uid", "-"}, "must be 1-40 characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.stdin, tt.args...)
			if code != 1 {
				t.Errorf("code = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q", stdout)
			}
			contains(t, stderr, "terradash:", tt.wantErr)
		})
	}
}

func TestConvertErrorWritesNothing(t *testing.T) {
	in := t.TempDir()
	writeFiles(t, in, map[string]string{"a.json": dashA, "z.json": "{"})
	out := filepath.Join(t.TempDir(), "out")
	if code, _, _ := runCLI(t, "", "convert", "-o", out, in); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output directory created despite error, err = %v", err)
	}
}

func TestConvertDirectoryExistingFilePartial(t *testing.T) {
	in := t.TempDir()
	writeFiles(t, in, map[string]string{"a.json": dashA, "b.json": dashB})
	out := t.TempDir()
	alpha := filepath.Join(out, "alpha_board.tf")
	if err := os.WriteFile(alpha, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := runCLI(t, "", "convert", "-o", out, in)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q", stdout)
	}
	contains(t, stderr, "already exists")

	if _, err := os.Stat(filepath.Join(out, "beta.tf")); !os.IsNotExist(err) {
		t.Error("beta.tf should not have been created")
	}
}

func TestConvertVariableConflict(t *testing.T) {
	dash1 := `{
		"title": "Dash1",
		"__inputs": [{"name": "VAR_X", "type": "constant", "value": "val1"}],
		"panels": []
	}`
	dash2 := `{
		"title": "Dash2",
		"__inputs": [{"name": "VAR_X", "type": "constant", "value": "val2"}],
		"panels": []
	}`
	in := t.TempDir()
	writeFiles(t, in, map[string]string{"d1.json": dash1, "d2.json": dash2})

	code, _, stderr := runCLI(t, "", "convert", "-o", "", in)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	contains(t, stderr, "disagree on the default")
}

func TestConvertDirectorySkipsSubdirs(t *testing.T) {
	in := t.TempDir()
	writeFiles(t, in, map[string]string{"a.json": dashA})
	subdir := filepath.Join(in, "x.json")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, subdir, map[string]string{"b.json": dashB})

	code, stdout, stderr := runCLI(t, "", "convert", in)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if got := resourceNames(t, stdout); !slices.Equal(got, []string{"alpha_board"}) {
		t.Errorf("resources = %v, want [alpha_board]", got)
	}
}

func TestConvertDirectoryWithBrackets(t *testing.T) {
	parent := t.TempDir()
	in := filepath.Join(parent, "dir[test]")
	if err := os.Mkdir(in, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, in, map[string]string{"a.json": dashA})

	code, stdout, stderr := runCLI(t, "", "convert", in)
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if got := resourceNames(t, stdout); !slices.Equal(got, []string{"alpha_board"}) {
		t.Errorf("resources = %v", got)
	}
}

func TestConvertVariableDescriptionsDoNotWarn(t *testing.T) {
	in := t.TempDir()
	writeFiles(t, in, map[string]string{
		"d1.json": `{"title": "D1", "__inputs": [{"name": "DS_PROM", "label": "Prometheus"}]}`,
		"d2.json": `{"title": "D2", "__inputs": [{"name": "DS_PROM", "label": "prom"}]}`,
	})
	code, _, stderr := runCLI(t, "", "convert", in)
	if code != 0 || strings.Contains(stderr, "warning") {
		t.Errorf("code = %d, stderr = %q, want no warning", code, stderr)
	}
}

func TestConvertReservedName(t *testing.T) {
	in := t.TempDir()
	writeFiles(t, in, map[string]string{"d.json": `{"title": "D"}`})
	code, _, stderr := runCLI(t, "", "convert", "-name", "variables", "-o", t.TempDir(), filepath.Join(in, "d.json"))
	if code != 1 || !strings.Contains(stderr, "reserved") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}
