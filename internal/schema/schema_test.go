package schema

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

var timeAttr = Attribute{Name: "time", Kind: Object, Attributes: []Attribute{
	{Name: "from", Kind: String, Required: true},
	{Name: "to", Kind: String, Required: true},
}}

func TestAttributeType(t *testing.T) {
	tests := []struct {
		name string
		attr Attribute
		want tftypes.Type
	}{
		{"string", Attribute{Kind: String}, tftypes.String},
		{"number", Attribute{Kind: Number}, tftypes.Number},
		{"bool", Attribute{Kind: Bool}, tftypes.Bool},
		{"string list", Attribute{Kind: StringList}, tftypes.List{ElementType: tftypes.String}},
		{"string map", Attribute{Kind: StringMap}, tftypes.Map{ElementType: tftypes.String}},
		{"dynamic", Attribute{Kind: Dynamic}, tftypes.DynamicPseudoType},
		{"object", timeAttr, tftypes.Object{AttributeTypes: map[string]tftypes.Type{
			"from": tftypes.String,
			"to":   tftypes.String,
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.attr.Type(); !got.Equal(tt.want) {
				t.Errorf("Type() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestBlockListType(t *testing.T) {
	static := Block{Name: "link", Attributes: []Attribute{{Name: "url", Kind: String}}}
	dynamic := Block{Name: "panel", Attributes: []Attribute{{Name: "options", Kind: Dynamic}}}
	dynamicInObject := Block{Name: "panel", Attributes: []Attribute{
		{Name: "pos", Kind: Object, Attributes: []Attribute{{Name: "x", Kind: Dynamic}}},
	}}
	nested := Block{Name: "row", Attributes: []Attribute{{Name: "title", Kind: String}}, Blocks: []Block{dynamic}}

	tests := []struct {
		name  string
		block Block
		want  tftypes.Type
	}{
		{"static", static, tftypes.List{ElementType: static.Type()}},
		{"dynamic attribute", dynamic, tftypes.DynamicPseudoType},
		{"dynamic in object attribute", dynamicInObject, tftypes.DynamicPseudoType},
		{"dynamic in nested block", nested, tftypes.DynamicPseudoType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.block.ListType(); !got.Equal(tt.want) {
				t.Errorf("ListType() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestBlockType(t *testing.T) {
	link := Block{Name: "link", Attributes: []Attribute{{Name: "url", Kind: String}}}
	panel := Block{Name: "panel", Attributes: []Attribute{{Name: "options", Kind: Dynamic}}}
	b := Block{
		Attributes: []Attribute{{Name: "title", Kind: String}},
		Blocks:     []Block{link, panel},
	}
	want := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"title": tftypes.String,
		"link":  tftypes.List{ElementType: link.Type()},
		"panel": tftypes.DynamicPseudoType,
	}}
	if got := b.Type(); !got.Equal(want) {
		t.Errorf("Type() = %s, want %s", got, want)
	}
}

func TestBlockAttribute(t *testing.T) {
	b := Block{Attributes: []Attribute{{Name: "a", Kind: Number}}}
	if a, ok := b.Attribute("a"); !ok || a.Kind != Number {
		t.Errorf("Attribute(a) = %v, %v", a, ok)
	}
	if _, ok := b.Attribute("missing"); ok {
		t.Error("Attribute(missing) found")
	}
}

func TestProto(t *testing.T) {
	b := Block{
		Description: "desc",
		Attributes: []Attribute{
			{Name: "req", Kind: String, Required: true},
			{Name: "opt", Kind: Bool, Optional: true},
			{Name: "comp", Kind: Number, Computed: true},
			{Name: "secret", Kind: StringMap, Optional: true, Sensitive: true},
			{Name: "dyn", Kind: Dynamic, Optional: true},
			timeAttr,
		},
		Blocks: []Block{{Name: "link", Attributes: []Attribute{{Name: "url", Kind: String}}}},
	}
	got := b.Proto()

	if got.Description != "desc" || got.DescriptionKind != tfprotov6.StringKindMarkdown {
		t.Errorf("description = %q (%v)", got.Description, got.DescriptionKind)
	}
	flags := map[string][4]bool{}
	for _, a := range got.Attributes {
		flags[a.Name] = [4]bool{a.Required, a.Optional, a.Computed, a.Sensitive}
	}
	wantFlags := map[string][4]bool{
		"req":    {true, false, false, false},
		"opt":    {false, true, false, false},
		"comp":   {false, false, true, false},
		"secret": {false, true, false, true},
		"dyn":    {false, true, false, false},
		"time":   {false, false, false, false},
	}
	if diff := cmp.Diff(wantFlags, flags); diff != "" {
		t.Errorf("attribute flags (-want +got):\n%s", diff)
	}

	for _, a := range got.Attributes {
		switch a.Name {
		case "time":
			if a.Type != nil || a.NestedType == nil {
				t.Fatalf("time: Type = %v, NestedType = %v", a.Type, a.NestedType)
			}
			if a.NestedType.Nesting != tfprotov6.SchemaObjectNestingModeSingle {
				t.Errorf("time nesting = %v", a.NestedType.Nesting)
			}
			if len(a.NestedType.Attributes) != 2 || !a.NestedType.Attributes[0].Required {
				t.Errorf("time nested attributes = %v", a.NestedType.Attributes)
			}
		case "dyn":
			if !a.Type.Equal(tftypes.DynamicPseudoType) {
				t.Errorf("dyn type = %s", a.Type)
			}
		case "secret":
			if !a.Type.Equal(tftypes.Map{ElementType: tftypes.String}) {
				t.Errorf("secret type = %s", a.Type)
			}
		}
	}

	if len(got.BlockTypes) != 1 {
		t.Fatalf("got %d block types", len(got.BlockTypes))
	}
	nb := got.BlockTypes[0]
	if nb.TypeName != "link" || nb.Nesting != tfprotov6.SchemaNestedBlockNestingModeList || len(nb.Block.Attributes) != 1 {
		t.Errorf("block type = %+v", nb)
	}
}
