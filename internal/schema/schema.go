package schema

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type Kind int

const (
	String Kind = iota
	Number
	Bool
	StringList
	StringMap
	Dynamic
	Object
)

type Attribute struct {
	Name        string
	Kind        Kind
	Attributes  []Attribute
	Description string
	Required    bool
	Optional    bool
	Computed    bool
	Sensitive   bool
}

type Block struct {
	Name        string
	Description string
	Attributes  []Attribute
	Blocks      []Block
}

func (a Attribute) Type() tftypes.Type {
	switch a.Kind {
	case String:
		return tftypes.String
	case Number:
		return tftypes.Number
	case Bool:
		return tftypes.Bool
	case StringList:
		return tftypes.List{ElementType: tftypes.String}
	case StringMap:
		return tftypes.Map{ElementType: tftypes.String}
	case Object:
		types := make(map[string]tftypes.Type, len(a.Attributes))
		for _, sub := range a.Attributes {
			types[sub.Name] = sub.Type()
		}
		return tftypes.Object{AttributeTypes: types}
	default:
		return tftypes.DynamicPseudoType
	}
}

func (a Attribute) dynamic() bool {
	if a.Kind == Dynamic {
		return true
	}
	for _, sub := range a.Attributes {
		if sub.dynamic() {
			return true
		}
	}
	return false
}

func (b Block) dynamic() bool {
	for _, a := range b.Attributes {
		if a.dynamic() {
			return true
		}
	}
	for _, nb := range b.Blocks {
		if nb.dynamic() {
			return true
		}
	}
	return false
}

// ListType mirrors Terraform core: list blocks containing dynamic types are tuples.
func (b Block) ListType() tftypes.Type {
	if b.dynamic() {
		return tftypes.DynamicPseudoType
	}
	return tftypes.List{ElementType: b.Type()}
}

func (b Block) Type() tftypes.Object {
	types := make(map[string]tftypes.Type, len(b.Attributes)+len(b.Blocks))
	for _, a := range b.Attributes {
		types[a.Name] = a.Type()
	}
	for _, nb := range b.Blocks {
		types[nb.Name] = nb.ListType()
	}
	return tftypes.Object{AttributeTypes: types}
}

func (b Block) Attribute(name string) (Attribute, bool) {
	for _, a := range b.Attributes {
		if a.Name == name {
			return a, true
		}
	}
	return Attribute{}, false
}

func (b Block) Proto() *tfprotov6.SchemaBlock {
	out := &tfprotov6.SchemaBlock{
		Description:     b.Description,
		DescriptionKind: tfprotov6.StringKindMarkdown,
	}
	for _, a := range b.Attributes {
		out.Attributes = append(out.Attributes, a.proto())
	}
	for _, nb := range b.Blocks {
		out.BlockTypes = append(out.BlockTypes, &tfprotov6.SchemaNestedBlock{
			TypeName: nb.Name,
			Nesting:  tfprotov6.SchemaNestedBlockNestingModeList,
			Block:    nb.Proto(),
		})
	}
	return out
}

func (a Attribute) proto() *tfprotov6.SchemaAttribute {
	out := &tfprotov6.SchemaAttribute{
		Name:            a.Name,
		Description:     a.Description,
		DescriptionKind: tfprotov6.StringKindMarkdown,
		Required:        a.Required,
		Optional:        a.Optional,
		Computed:        a.Computed,
		Sensitive:       a.Sensitive,
	}
	if a.Kind != Object {
		out.Type = a.Type()
		return out
	}
	nested := &tfprotov6.SchemaObject{Nesting: tfprotov6.SchemaObjectNestingModeSingle}
	for _, sub := range a.Attributes {
		nested.Attributes = append(nested.Attributes, sub.proto())
	}
	out.NestedType = nested
	return out
}
