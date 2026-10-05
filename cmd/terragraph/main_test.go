package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

func runCLI(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func parseHCL(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()
	f, diags := hclsyntax.ParseConfig([]byte(src), "test.tf", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatalf("output is not valid HCL: %s\n%s", diags, src)
	}
	return f.Body.(*hclsyntax.Body)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func resourceNames(t *testing.T, src string) []string {
	t.Helper()
	var names []string
	for _, b := range parseHCL(t, src).Blocks {
		if b.Type == "resource" {
			names = append(names, b.Labels[1])
		}
	}
	return names
}

func contains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRunCommands(t *testing.T) {
	old := version
	version = "1.2.3"
	t.Cleanup(func() { version = old })

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"no args", nil, 2, "", "Usage:"},
		{"unknown command", []string{"frobnicate"}, 2, "", `unknown command "frobnicate"`},
		{"help", []string{"help"}, 0, "Usage:", ""},
		{"-h", []string{"-h"}, 0, "Usage:", ""},
		{"--help", []string{"--help"}, 0, "Usage:", ""},
		{"version", []string{"version"}, 0, "1.2.3\n", ""},
		{"convert -h", []string{"convert", "-h"}, 0, "", "-force"},
		{"pull -h", []string{"pull", "-h"}, 0, "", "GRAFANA_AUTH"},
		{"convert bad flag", []string{"convert", "-nope"}, 2, "", "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, "", tt.args...)
			if code != tt.wantCode {
				t.Errorf("code = %d, want %d", code, tt.wantCode)
			}
			contains(t, stdout, tt.wantStdout)
			contains(t, stderr, tt.wantStderr)
		})
	}
}
