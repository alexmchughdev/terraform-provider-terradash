package dashboard

import (
	"encoding/json"
	"testing"
)

func TestEqual(t *testing.T) {
	n := func(s string) json.Number { return json.Number(s) }
	tests := []struct {
		name string
		a, b any
		want bool
	}{
		{"same integer", n("1"), n("1"), true},
		{"integer and decimal", n("1"), n("1.0"), true},
		{"integer and exponent", n("1"), n("1e0"), true},
		{"trailing zeros", n("1.50"), n("1.5"), true},
		{"exponent and integer", n("1e3"), n("1000"), true},
		{"different numbers", n("1"), n("2"), false},
		{"close decimals", n("0.1"), n("0.10000001"), false},
		{"big integer", n("12345678901234567890"), n("12345678901234567890.0"), true},
		{"big integers differ", n("12345678901234567890"), n("12345678901234567891"), false},
		{"float64 and number", 1.5, n("1.5"), true},
		{"int and number", 3, n("3"), true},
		{"number and string", n("1"), "1", false},
		{"invalid number equals itself", n("abc"), n("abc"), true},
		{"zero is not absent", map[string]any{"a": n("0")}, map[string]any{}, false},
		{"false is not absent", map[string]any{"a": false}, map[string]any{}, false},
		{"nested numbers", map[string]any{"a": []any{n("1")}}, map[string]any{"a": []any{n("1.0")}}, true},

		{"null absent in object", map[string]any{"a": nil}, map[string]any{}, true},
		{"empty string absent in object", map[string]any{"a": ""}, map[string]any{}, true},
		{"empty list absent in object", map[string]any{"a": []any{}}, map[string]any{}, true},
		{"empty object absent in object", map[string]any{"a": map[string]any{}}, map[string]any{}, true},
		{"nested empties absent", map[string]any{"a": map[string]any{"b": nil, "c": []any{}}}, map[string]any{}, true},
		{"absent against value", map[string]any{"a": "x"}, map[string]any{}, false},
		{"empty top-level values", nil, "", true},
		{"empty list and object", []any{}, map[string]any{}, true},
		{"nil and nonempty", nil, "x", false},
		{"nil and nil", nil, nil, true},
		{"strings", "a", "a", true},
		{"different strings", "a", "b", false},

		{"arrays are positional", []any{n("1"), n("2")}, []any{n("2"), n("1")}, false},
		{"arrays with same order", []any{n("1"), "a"}, []any{n("1.0"), "a"}, true},
		{"array length matters", []any{n("1")}, []any{n("1"), n("2")}, false},
		{"array nils are kept", []any{nil}, []any{}, false},
		{"array empty strings are kept", []any{"a", ""}, []any{"a"}, false},
		{"objects in arrays ignore empties", []any{map[string]any{"a": nil, "b": "x"}}, []any{map[string]any{"b": "x"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Equal(tt.a, tt.b); got != tt.want {
				t.Errorf("Equal(%#v, %#v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
			if got := Equal(tt.b, tt.a); got != tt.want {
				t.Errorf("Equal(%#v, %#v) = %v, want %v", tt.b, tt.a, got, tt.want)
			}
		})
	}
}
