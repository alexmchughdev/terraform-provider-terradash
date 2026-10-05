package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type unknown struct{}

// Unknown marks a value Terraform does not know yet.
var Unknown = unknown{}

func IsUnknown(v any) bool {
	_, ok := v.(unknown)
	return ok
}

func HasUnknown(v any) bool {
	switch x := v.(type) {
	case unknown:
		return true
	case map[string]any:
		for _, e := range x {
			if HasUnknown(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if HasUnknown(e) {
				return true
			}
		}
	}
	return false
}

// FromValue converts a Terraform value into plain Go values: map[string]any,
// []any, string, json.Number, bool, nil or Unknown.
func FromValue(v tftypes.Value) (any, error) {
	if !v.IsKnown() {
		return Unknown, nil
	}
	if v.IsNull() {
		return nil, nil
	}
	typ := v.Type()
	switch {
	case typ.Is(tftypes.String):
		var s string
		err := v.As(&s)
		return s, err
	case typ.Is(tftypes.Bool):
		var b bool
		err := v.As(&b)
		return b, err
	case typ.Is(tftypes.Number):
		f := new(big.Float)
		if err := v.As(&f); err != nil {
			return nil, err
		}
		if err := checkRange(f); err != nil {
			return nil, err
		}
		return NumberFromFloat(f), nil
	case typ.Is(tftypes.Object{}), typ.Is(tftypes.Map{}):
		return objectFromValue(v)
	case typ.Is(tftypes.List{}), typ.Is(tftypes.Set{}), typ.Is(tftypes.Tuple{}):
		return listFromValue(v)
	}
	return nil, fmt.Errorf("unsupported type %s", typ)
}

func objectFromValue(v tftypes.Value) (map[string]any, error) {
	var attrs map[string]tftypes.Value
	if err := v.As(&attrs); err != nil {
		return nil, err
	}
	out := make(map[string]any, len(attrs))
	for k, e := range attrs {
		x, err := FromValue(e)
		if err != nil {
			return nil, err
		}
		out[k] = x
	}
	return out, nil
}

func listFromValue(v tftypes.Value) ([]any, error) {
	var elems []tftypes.Value
	if err := v.As(&elems); err != nil {
		return nil, err
	}
	out := make([]any, len(elems))
	for i, e := range elems {
		x, err := FromValue(e)
		if err != nil {
			return nil, err
		}
		out[i] = x
	}
	return out, nil
}

// ToValue builds a Terraform value for block b from a plain Go tree.
func ToValue(b Block, m map[string]any) (tftypes.Value, error) {
	vals := make(map[string]tftypes.Value, len(b.Attributes)+len(b.Blocks))
	for _, a := range b.Attributes {
		v, err := attributeValue(a, m[a.Name])
		if err != nil {
			return tftypes.Value{}, fmt.Errorf("%s: %w", a.Name, err)
		}
		vals[a.Name] = v
	}
	for _, nb := range b.Blocks {
		v, err := blockListValue(nb, m[nb.Name])
		if err != nil {
			return tftypes.Value{}, fmt.Errorf("%s: %w", nb.Name, err)
		}
		vals[nb.Name] = v
	}
	return tftypes.NewValue(b.Type(), vals), nil
}

func blockListValue(b Block, v any) (tftypes.Value, error) {
	items, _ := v.([]any)
	vals := make([]tftypes.Value, len(items))
	types := make([]tftypes.Type, len(items))
	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("element %d is %T, want object", i, item)
		}
		val, err := ToValue(b, m)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals[i] = val
		types[i] = val.Type()
	}
	if b.ListType().Is(tftypes.DynamicPseudoType) {
		return tftypes.NewValue(tftypes.Tuple{ElementTypes: types}, vals), nil
	}
	return tftypes.NewValue(b.ListType(), vals), nil
}

func attributeValue(a Attribute, v any) (tftypes.Value, error) {
	if v == nil {
		return tftypes.NewValue(a.Type(), nil), nil
	}
	if IsUnknown(v) {
		return tftypes.NewValue(a.Type(), tftypes.UnknownValue), nil
	}
	switch a.Kind {
	case Dynamic:
		return Infer(v)
	case Object:
		m, ok := v.(map[string]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("got %T, want object", v)
		}
		return ToValue(Block{Attributes: a.Attributes}, m)
	case StringList:
		items, ok := v.([]any)
		if !ok {
			return tftypes.Value{}, fmt.Errorf("got %T, want list", v)
		}
		vals := make([]tftypes.Value, len(items))
		for i, item := range items {
			s, ok := item.(string)
			if !ok {
				return tftypes.Value{}, fmt.Errorf("element %d is %T, want string", i, item)
			}
			vals[i] = tftypes.NewValue(tftypes.String, s)
		}
		return tftypes.NewValue(a.Type(), vals), nil
	}
	val, err := Infer(v)
	if err != nil {
		return tftypes.Value{}, err
	}
	if !val.Type().Is(a.Type()) {
		return tftypes.Value{}, fmt.Errorf("got %s, want %s", val.Type(), a.Type())
	}
	return val, nil
}

// Infer types a JSON-like value the way HCL would: objects and tuples.
func Infer(v any) (tftypes.Value, error) {
	switch x := v.(type) {
	case nil:
		return tftypes.NewValue(tftypes.DynamicPseudoType, nil), nil
	case unknown:
		return tftypes.NewValue(tftypes.DynamicPseudoType, tftypes.UnknownValue), nil
	case string:
		return tftypes.NewValue(tftypes.String, x), nil
	case bool:
		return tftypes.NewValue(tftypes.Bool, x), nil
	case json.Number:
		f, err := ParseNumber(x)
		if err != nil {
			return tftypes.Value{}, err
		}
		return tftypes.NewValue(tftypes.Number, f), nil
	case map[string]any:
		return inferObject(x)
	case []any:
		return inferTuple(x)
	}
	return tftypes.Value{}, fmt.Errorf("unsupported value %T", v)
}

func inferObject(m map[string]any) (tftypes.Value, error) {
	vals := make(map[string]tftypes.Value, len(m))
	types := make(map[string]tftypes.Type, len(m))
	for k, e := range m {
		val, err := Infer(e)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals[k] = val
		types[k] = val.Type()
	}
	return tftypes.NewValue(tftypes.Object{AttributeTypes: types}, vals), nil
}

func inferTuple(items []any) (tftypes.Value, error) {
	vals := make([]tftypes.Value, len(items))
	types := make([]tftypes.Type, len(items))
	for i, e := range items {
		val, err := Infer(e)
		if err != nil {
			return tftypes.Value{}, err
		}
		vals[i] = val
		types[i] = val.Type()
	}
	return tftypes.NewValue(tftypes.Tuple{ElementTypes: types}, vals), nil
}

// maxExponentBits keeps numbers within float64 range, which is all Grafana
// can represent. Formatting far larger exponents in decimal takes minutes.
const maxExponentBits = 1100

func checkRange(f *big.Float) error {
	if f.IsInf() || f.MantExp(nil) > maxExponentBits || f.MantExp(nil) < -maxExponentBits {
		return errors.New("number is out of range")
	}
	return nil
}

func ParseNumber(n json.Number) (*big.Float, error) {
	f, _, err := big.ParseFloat(string(n), 10, 512, big.ToNearestEven)
	if err != nil {
		return nil, fmt.Errorf("invalid number %q: %w", n, err)
	}
	return f, checkRange(f)
}

// NumberFromFloat formats f, which must be within checkRange.
func NumberFromFloat(f *big.Float) json.Number {
	if f.IsInt() {
		return json.Number(f.Text('f', 0))
	}
	return json.Number(f.Text('g', -1))
}
