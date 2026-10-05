package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
)

func parse(t *testing.T, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return m
}

// viaJSON mimics a remote model: marshalled by Grafana and decoded with UseNumber.
func viaJSON(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return parse(t, string(b))
}

func encode(t *testing.T, tree map[string]any) map[string]any {
	t.Helper()
	out, err := Encode(tree)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return out
}
