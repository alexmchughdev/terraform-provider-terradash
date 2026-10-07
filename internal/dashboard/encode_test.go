package dashboard

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
)

func TestEncodeErrors(t *testing.T) {
	tests := []struct {
		name string
		tree map[string]any
		want string
	}{
		{"extra collides with typed field", parse(t, `{"title": "a", "extra": {"title": "b"}}`), `extra key "title"`},
		{"panel extra collides with typed field", parse(t, `{"panel": [{"type": "stat", "extra": {"type": "gauge"}}]}`), `extra key "type"`},
		{"variable extra collides with typed field", parse(t, `{"variable": [{"name": "a", "type": "custom", "extra": {"name": "b"}}]}`), `extra key "name"`},
		{"extra collides with nested panels", parse(t, `{"panel": [{"type": "stat"}], "extra": {"panels": []}}`), `extra key "panels"`},
		{"extra collides with schema version default", parse(t, `{"schema_version": 40, "extra": {"schemaVersion": 41}}`), `extra key "schemaVersion"`},
		{"row extra type", parse(t, `{"row": [{"title": "r", "extra": {"type": "row"}}]}`), `extra key "type"`},
		{"collapsed row extra panels", parse(t, `{"row": [{"collapsed": true, "extra": {"panels": []}}]}`), `extra key "panels"`},
		{"extra is not an object", parse(t, `{"extra": "nope"}`), "extra must be an object"},
		{"panel extra is not an object", parse(t, `{"panel": [{"type": "stat", "extra": [1]}]}`), "extra must be an object"},
		{"unknown value", map[string]any{"title": schema.Unknown}, "unknown values"},
		{"nested unknown value", map[string]any{"panel": []any{map[string]any{"options": map[string]any{"a": []any{schema.Unknown}}}}}, "unknown values"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Encode(tt.tree)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestEncodeDefaultsAndExtra(t *testing.T) {
	out := encode(t, parse(t, `{"title": "t"}`))
	if out["schemaVersion"] != json.Number("41") {
		t.Errorf("schemaVersion = %v, want 41", out["schemaVersion"])
	}
	if _, ok := out["panels"]; ok {
		t.Error("empty dashboard should not emit panels")
	}

	out = encode(t, parse(t, `{"title": "t", "extra": {"schemaVersion": 39, "style": "dark"}}`))
	if out["schemaVersion"] != json.Number("39") || out["style"] != "dark" {
		t.Errorf("extra not merged: %v", out)
	}
}

func TestEncodeBlocks(t *testing.T) {
	out := encode(t, parse(t, `{
		"title": "t",
		"time": {"from": "now-1h", "to": "now"},
		"variable": [{"name": "a", "type": "custom", "include_all": true, "extra": {"auto": true}}],
		"annotation": [{"name": "n", "icon_color": "red"}],
		"link": [{"title": "l", "as_dropdown": true}]
	}`))
	want := parse(t, `{
		"title": "t",
		"time": {"from": "now-1h", "to": "now"},
		"templating": {"list": [{"name": "a", "type": "custom", "includeAll": true, "auto": true}]},
		"annotations": {"list": [{"name": "n", "iconColor": "red"}]},
		"links": [{"title": "l", "asDropdown": true}],
		"schemaVersion": 41
	}`)
	if !Equal(out, want) {
		t.Errorf("Encode = %v, want %v", out, want)
	}
}
