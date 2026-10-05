package schema

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestNumberFromFloat(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"0", "0"},
		{"42", "42"},
		{"-7", "-7"},
		{"0.1", "0.1"},
		{"1.5", "1.5"},
		{"12345678901234567890", "12345678901234567890"},
		{"1e21", "1000000000000000000000"},
		{"1e-7", "1e-07"},
		{"2.5e3", "2500"},
		{"123456789012345678901234567890", "123456789012345678901234567890"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			f, err := ParseNumber(json.Number(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if got := NumberFromFloat(f); string(got) != tt.want {
				t.Errorf("NumberFromFloat(%s) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseNumberInvalid(t *testing.T) {
	if _, err := ParseNumber("abc"); err == nil {
		t.Error("want error")
	}
}

func TestInferFromValueRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   any
	}{
		{"nil", nil},
		{"string", "hello"},
		{"empty string", ""},
		{"bool", true},
		{"zero", json.Number("0")},
		{"fraction", json.Number("0.1")},
		{"big int", json.Number("12345678901234567890")},
		{"negative", json.Number("-3")},
		{"empty tuple", []any{}},
		{"empty object", map[string]any{}},
		{"mixed tuple", []any{"a", json.Number("1"), true, nil, []any{}}},
		{"nested", map[string]any{
			"a":    map[string]any{"b": []any{map[string]any{"c": nil, "d": "x"}, json.Number("2")}},
			"null": nil,
			"list": []any{},
		}},
		{"unknown", Unknown},
		{"unknown nested", map[string]any{"a": Unknown, "b": []any{Unknown, "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := Infer(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := FromValue(v)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.in, got); diff != "" {
				t.Errorf("round trip (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInferErrors(t *testing.T) {
	for _, in := range []any{1, 1.5, []string{"a"}, map[string]any{"a": struct{}{}}, []any{int64(1)}, json.Number("x")} {
		if _, err := Infer(in); err == nil {
			t.Errorf("Infer(%#v) succeeded, want error", in)
		}
	}
}

func TestUnknownHelpers(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{"unknown", Unknown, true},
		{"string", "x", false},
		{"nil", nil, false},
		{"nested map", map[string]any{"a": []any{"x", Unknown}}, true},
		{"nested no unknown", map[string]any{"a": []any{"x", nil}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasUnknown(tt.in); got != tt.want {
				t.Errorf("HasUnknown = %v, want %v", got, tt.want)
			}
		})
	}
	if !IsUnknown(Unknown) || IsUnknown("x") || IsUnknown(nil) {
		t.Error("IsUnknown misreports")
	}
}

func TestFromValueTypes(t *testing.T) {
	strList := tftypes.List{ElementType: tftypes.String}
	tests := []struct {
		name string
		in   tftypes.Value
		want any
	}{
		{"unknown", tftypes.NewValue(tftypes.String, tftypes.UnknownValue), Unknown},
		{"null string", tftypes.NewValue(tftypes.String, nil), nil},
		{"list", tftypes.NewValue(strList, []tftypes.Value{tftypes.NewValue(tftypes.String, "a")}), []any{"a"}},
		{"map", tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{
			"k": tftypes.NewValue(tftypes.String, "v"),
		}), map[string]any{"k": "v"}},
		{"set", tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "a"),
		}), []any{"a"}},
		{"number", tftypes.NewValue(tftypes.Number, big.NewFloat(2.5)), json.Number("2.5")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromValue(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("(-want +got):\n%s", diff)
			}
		})
	}
}

func TestToValueTypedAttributes(t *testing.T) {
	b := Block{
		Attributes: []Attribute{
			{Name: "s", Kind: String},
			{Name: "n", Kind: Number},
			{Name: "tags", Kind: StringList},
			timeAttr,
		},
		Blocks: []Block{{Name: "link", Attributes: []Attribute{{Name: "url", Kind: String}}}},
	}
	in := map[string]any{
		"s":    "x",
		"n":    json.Number("1"),
		"tags": []any{"a", "b"},
		"time": map[string]any{"from": "now-1h", "to": "now"},
		"link": []any{map[string]any{"url": "u"}},
	}
	v, err := ToValue(b, in)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Type().Equal(b.Type()) {
		t.Errorf("type = %s, want %s", v.Type(), b.Type())
	}
	got, err := FromValue(v)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(in, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestToValueNullAndUnknown(t *testing.T) {
	b := Block{Attributes: []Attribute{
		{Name: "s", Kind: String},
		{Name: "tags", Kind: StringList},
		{Name: "d", Kind: Dynamic},
		timeAttr,
	}}
	v, err := ToValue(b, map[string]any{"s": Unknown, "d": Unknown})
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromValue(v)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"s": Unknown, "tags": nil, "d": Unknown, "time": nil}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestToValueErrors(t *testing.T) {
	b := Block{
		Attributes: []Attribute{
			{Name: "s", Kind: String},
			{Name: "tags", Kind: StringList},
			timeAttr,
		},
		Blocks: []Block{{Name: "link", Attributes: []Attribute{{Name: "url", Kind: String}}}},
	}
	tests := []struct {
		name string
		in   map[string]any
	}{
		{"wrong scalar", map[string]any{"s": json.Number("1")}},
		{"list not list", map[string]any{"tags": "a"}},
		{"list element", map[string]any{"tags": []any{json.Number("1")}}},
		{"object not object", map[string]any{"time": "now"}},
		{"unsupported", map[string]any{"s": 1}},
		{"block element", map[string]any{"link": []any{"x"}}},
		{"block attribute", map[string]any{"link": []any{map[string]any{"url": true}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ToValue(b, tt.in); err == nil {
				t.Error("want error")
			}
		})
	}
}

func TestToValueDynamicBlockIsTuple(t *testing.T) {
	b := Block{Blocks: []Block{{Name: "panel", Attributes: []Attribute{{Name: "options", Kind: Dynamic}}}}}
	v, err := ToValue(b, map[string]any{"panel": []any{
		map[string]any{"options": map[string]any{"a": "x"}},
		map[string]any{"options": []any{json.Number("1")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	attrs := map[string]tftypes.Value{}
	if err := v.As(&attrs); err != nil {
		t.Fatal(err)
	}
	panels := attrs["panel"]
	if !panels.Type().Is(tftypes.Tuple{}) {
		t.Fatalf("panel type = %s, want tuple", panels.Type())
	}
}

func TestNumberOutOfRange(t *testing.T) {
	for _, n := range []string{"1e100000000", "-1e400", "1e-400"} {
		if _, err := ParseNumber(json.Number(n)); err == nil {
			t.Errorf("ParseNumber(%s) succeeded, want range error", n)
		}
	}
	f, _, _ := big.ParseFloat("1e100000000", 10, 512, big.ToNearestEven)
	if _, err := FromValue(tftypes.NewValue(tftypes.Number, f)); err == nil {
		t.Error("FromValue accepted an out-of-range number")
	}
}
