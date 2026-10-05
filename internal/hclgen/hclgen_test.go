package hclgen

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
	ctyjson "github.com/zclconf/go-cty/cty/json"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/schema"
)

var testBlock = schema.Block{
	Attributes: []schema.Attribute{
		{Name: "title", Kind: schema.String, Optional: true},
		{Name: "tags", Kind: schema.StringList, Optional: true},
		{Name: "v", Kind: schema.Dynamic, Optional: true},
		{Name: "url", Kind: schema.String, Computed: true},
		{Name: "uid", Kind: schema.String, Optional: true, Computed: true},
		{Name: "time", Kind: schema.Object, Optional: true, Attributes: []schema.Attribute{
			{Name: "from", Kind: schema.String, Required: true},
			{Name: "to", Kind: schema.String, Optional: true},
		}},
	},
	Blocks: []schema.Block{{
		Name: "panel",
		Attributes: []schema.Attribute{
			{Name: "title", Kind: schema.String, Optional: true},
			{Name: "options", Kind: schema.Dynamic, Optional: true},
		},
	}},
}

func generate(t *testing.T, m map[string]any, opts Options) string {
	t.Helper()
	f := hclwrite.NewEmptyFile()
	Resource(f.Body(), "terragraph_dashboard", "main", testBlock, m, opts)
	out := string(f.Bytes())
	if got := string(hclwrite.Format([]byte(out))); got != out {
		t.Errorf("output is not formatted:\n%s\nwant:\n%s", out, got)
	}
	return out
}

func parse(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()
	file, diags := hclsyntax.ParseConfig([]byte(src), "test.tf", hcl.InitialPos)
	if diags.HasErrors() {
		t.Fatalf("parse: %s\n%s", diags, src)
	}
	return file.Body.(*hclsyntax.Body)
}

func evalContext(vars map[string]string) *hcl.EvalContext {
	obj := map[string]cty.Value{}
	for k, v := range vars {
		obj[k] = cty.StringVal(v)
	}
	return &hcl.EvalContext{
		Variables: map[string]cty.Value{"var": cty.ObjectVal(obj)},
		Functions: map[string]function.Function{"chomp": stdlib.ChompFunc},
	}
}

func attrJSON(t *testing.T, body *hclsyntax.Body, name string, ctx *hcl.EvalContext) string {
	t.Helper()
	attr, ok := body.Attributes[name]
	if !ok {
		t.Fatalf("attribute %q not found", name)
	}
	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		t.Fatalf("eval %s: %s", name, diags)
	}
	data, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func resourceBody(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()
	body := parse(t, src)
	if len(body.Blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(body.Blocks))
	}
	return body.Blocks[0].Body
}

func TestValuesRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   any
	}{
		{"plain", "hello"},
		{"empty", ""},
		{"interpolation", "a ${foo} b"},
		{"directive", "%{if x}y%{endif}"},
		{"escaped interpolation", "$${x}"},
		{"escaped directive", "%%{x}"},
		{"dollar and percent", "100% $5 $ % {x}"},
		{"unclosed interpolation", "${foo"},
		{"quotes", `say "hi" and 'bye'`},
		{"backslashes", `C:\path\to\n and \"`},
		{"tab", "a\tb"},
		{"carriage return", "a\r\nb"},
		{"unicode", "héllo ☃ 日本語 😀"},
		{"control chars", "a\x00b\x01c\x1fd\x7fe"},
		{"newline single line", "a\nb"},
		{"trailing newline only", "abc\n"},
		{"multi-line", "line1\nline2\nline3"},
		{"multi-line trailing newline", "line1\nline2\n"},
		{"multi-line double trailing newline", "line1\nline2\n\n"},
		{"multi-line blank lines", "a\n\n\nb"},
		{"multi-line indented", "  a\n\tb\n    c"},
		{"multi-line trailing space", "a\nb  "},
		{"multi-line with EOT line", "a\nEOT\nb"},
		{"multi-line with EOT and EOT1 lines", "a\nEOT\nEOT1\n  EOT2  \nb\n"},
		{"multi-line interpolation", "a ${x}\n%{if y}\nb"},
		{"multi-line escapes", "a\\n \"q\"\n$${x}"},
		{"multi-line with tabs", "a\tb\nc"},
		{"multi-line with control char", "a\nb\x01"},
		{"true", true},
		{"false", false},
		{"int", json.Number("42")},
		{"negative", json.Number("-3")},
		{"fraction", json.Number("0.5")},
		{"empty list", []any{}},
		{"empty object", map[string]any{}},
		{"scalar list", []any{"a", json.Number("1"), true, nil}},
		{"list of multi-line strings", []any{"a\nb", "c"}},
		{"list of lists", []any{[]any{"a"}, []any{}, []any{json.Number("1"), json.Number("2")}}},
		{"list of objects", []any{
			map[string]any{"expr": "up", "refId": "A"},
			map[string]any{"expr": "down", "refId": "B", "hide": true},
		}},
		{"nulls in object", map[string]any{"a": nil, "b": map[string]any{"c": nil}, "d": []any{nil}}},
		{"nested", map[string]any{"a": map[string]any{"b": []any{map[string]any{"c": "d\ne"}}}}},
		{"odd keys", map[string]any{
			"Value #A":    "1",
			"0":           "2",
			"__systemRef": "3",
			"null":        "4",
			"true":        "5",
			"false":       "6",
			"for":         "7",
			"if":          "8",
			"in":          "9",
			"a-b":         "10",
			"a.b":         "11",
			"":            "12",
			"${x}":        "13",
			`q"uote`:      "14",
			"with\nnl":    "15",
			"héllo":       "16",
			"plain_ident": "17",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := generate(t, map[string]any{"v": tt.in}, Options{})
			got := attrJSON(t, resourceBody(t, src), "v", evalContext(nil))
			want, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !jsonEqual(got, string(want)) {
				t.Errorf("evaluated %s, want %s\n%s", got, want, src)
			}
		})
	}
}

func jsonEqual(a, b string) bool {
	var x, y any
	d1 := json.NewDecoder(strings.NewReader(a))
	d1.UseNumber()
	d2 := json.NewDecoder(strings.NewReader(b))
	d2.UseNumber()
	if d1.Decode(&x) != nil || d2.Decode(&y) != nil {
		return false
	}
	return cmp.Equal(x, y)
}

func TestNumbers(t *testing.T) {
	for _, in := range []string{"0", "0.1", "12345678901234567890", "1e-07", "2500", "-0.25", "1000000000000000000000"} {
		t.Run(in, func(t *testing.T) {
			src := generate(t, map[string]any{"v": json.Number(in)}, Options{})
			attr := resourceBody(t, src).Attributes["v"]
			val, diags := attr.Expr.Value(nil)
			if diags.HasErrors() {
				t.Fatal(diags)
			}
			want, err := schema.ParseNumber(json.Number(in))
			if err != nil {
				t.Fatal(err)
			}
			if val.AsBigFloat().Cmp(want) != 0 {
				t.Errorf("got %s, want %s", val.AsBigFloat().Text('g', -1), in)
			}
		})
	}
}

func TestStringForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"quoted", `a "b"`, `v = "a \"b\""`},
		{"escaped interpolation", "${x}", `v = "$${x}"`},
		{"escaped directive", "%{x}", `v = "%%{x}"`},
		{"tab", "a\tb", `v = "a\tb"`},
		{"control char", "a\x01", `v = "a\u0001"`},
		{"heredoc with trailing newline", "a\nb\n", "v = <<EOT\na\nb\nEOT"},
		{"chomped heredoc", "a\nb", "v = chomp(<<EOT\na\nb\nEOT\n  )"},
		{"delimiter collision", "a\nEOT\n", "v = <<EOT1\na\nEOT\nEOT1"},
		{"multi-line with control char is quoted", "a\nb\x01", `v = "a\nb\u0001"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := generate(t, map[string]any{"v": tt.in}, Options{})
			if !strings.Contains(src, tt.want) {
				t.Errorf("output missing %q:\n%s", tt.want, src)
			}
		})
	}
}

func TestVariables(t *testing.T) {
	opts := Options{Variables: map[string]string{"DS_PROMETHEUS": "ds_prometheus", "DS_LOKI": "ds_loki"}}
	ctx := evalContext(map[string]string{"ds_prometheus": "prom-uid", "ds_loki": "loki-uid"})
	tests := []struct {
		name string
		in   any
		want string
		ref  string
	}{
		{"whole string", "${DS_PROMETHEUS}", `"prom-uid"`, "v = var.ds_prometheus"},
		{"embedded", "x ${DS_PROMETHEUS} y", `"x prom-uid y"`, `v = "x ${var.ds_prometheus} y"`},
		{"two placeholders", "${DS_PROMETHEUS}/${DS_LOKI}", `"prom-uid/loki-uid"`, `v = "${var.ds_prometheus}/${var.ds_loki}"`},
		{"unknown placeholder", "${OTHER}", `"${OTHER}"`, `v = "$${OTHER}"`},
		{"unknown next to known", "${OTHER} ${DS_LOKI}", `"${OTHER} loki-uid"`, `v = "$${OTHER} ${var.ds_loki}"`},
		{"multi-line", "a\n${DS_PROMETHEUS}\nb", `"a\nprom-uid\nb"`, "${var.ds_prometheus}"},
		{"nested in object", map[string]any{"uid": "${DS_LOKI}"}, `{"uid":"loki-uid"}`, "uid = var.ds_loki"},
		{"in list", []any{"${DS_LOKI}"}, `["loki-uid"]`, "[var.ds_loki]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := generate(t, map[string]any{"v": tt.in}, opts)
			got := attrJSON(t, resourceBody(t, src), "v", ctx)
			if !jsonEqual(got, tt.want) {
				t.Errorf("evaluated %s, want %s\n%s", got, tt.want, src)
			}
			if !strings.Contains(src, tt.ref) {
				t.Errorf("output missing %q:\n%s", tt.ref, src)
			}
		})
	}
}

func TestVariablePlaceholderAfterDollar(t *testing.T) {
	opts := Options{Variables: map[string]string{"DS": "ds"}}
	src := generate(t, map[string]any{"v": "$${DS}"}, opts)
	got := attrJSON(t, resourceBody(t, src), "v", evalContext(map[string]string{"ds": "uid"}))
	if want := `"$uid"`; got != want {
		t.Errorf("evaluated %s, want %s\n%s", got, want, src)
	}
}

func TestResourceStructure(t *testing.T) {
	m := map[string]any{
		"title": "T",
		"tags":  []any{"a", "b"},
		"url":   "computed-only is skipped",
		"uid":   "abc",
		"time":  map[string]any{"from": "now-1h", "to": nil},
		"panel": []any{
			map[string]any{"title": "P1", "options": map[string]any{"x": json.Number("1")}},
			map[string]any{"title": "P2"},
		},
	}
	src := generate(t, m, Options{})
	want := `resource "terragraph_dashboard" "main" {
  title = "T"
  tags  = ["a", "b"]
  uid   = "abc"
  time = {
    from = "now-1h"
  }

  panel {
    title = "P1"
    options = {
      x = 1
    }
  }

  panel {
    title = "P2"
  }
}
`
	if diff := cmp.Diff(want, src); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestResourceEmptyObjectAttribute(t *testing.T) {
	src := generate(t, map[string]any{"time": map[string]any{}}, Options{})
	if !strings.Contains(src, "time = {}") {
		t.Errorf("want empty object:\n%s", src)
	}
	parse(t, src)
}

func TestImport(t *testing.T) {
	f := hclwrite.NewEmptyFile()
	Import(f.Body(), "terragraph_dashboard", "main", `a"b`)
	got := string(f.Bytes())
	want := "import {\n  to = terragraph_dashboard.main\n  id = \"a\\\"b\"\n}\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	body := resourceBody(t, got)
	if v, _ := body.Attributes["id"].Expr.Value(nil); v.AsString() != `a"b` {
		t.Errorf("id = %q", v.AsString())
	}
}

func TestVariable(t *testing.T) {
	def := "prom"
	tests := []struct {
		name string
		desc string
		def  *string
		want string
	}{
		{"bare", "", nil, "variable \"v\" {\n  type = string\n}\n"},
		{"description", "A datasource", nil, "variable \"v\" {\n  type        = string\n  description = \"A datasource\"\n}\n"},
		{"default", "", &def, "variable \"v\" {\n  type    = string\n  default = \"prom\"\n}\n"},
		{"empty default", "d", new(string), "variable \"v\" {\n  type        = string\n  description = \"d\"\n  default     = \"\"\n}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hclwrite.NewEmptyFile()
			Variable(f.Body(), "v", tt.desc, tt.def)
			got := string(hclwrite.Format(f.Bytes()))
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
			parse(t, got)
		})
	}
}

func TestUnsupportedValueIsNull(t *testing.T) {
	src := generate(t, map[string]any{"v": schema.Unknown}, Options{})
	if v, _ := resourceBody(t, src).Attributes["v"].Expr.Value(nil); !v.IsNull() {
		t.Errorf("got %#v, want null", v)
	}
}
