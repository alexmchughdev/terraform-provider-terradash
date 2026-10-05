package migrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

type instance struct {
	key   string
	label string
	ctx   *hcl.EvalContext
}

func expand(dir string, block *hclsyntax.Block) ([]instance, error) {
	base := evalContext(dir)
	attr, ok := block.Body.Attributes["for_each"]
	if !ok {
		return []instance{{ctx: base}}, nil
	}
	v, diags := attr.Expr.Value(base)
	if diags.HasErrors() {
		return nil, fmt.Errorf("for_each cannot be evaluated statically: %s", diags.Error())
	}
	if !v.IsWhollyKnown() || v.IsNull() || !(v.CanIterateElements()) {
		return nil, errors.New("for_each is not a known map or set")
	}
	var out []instance
	for it := v.ElementIterator(); it.Next(); {
		k, val := it.Element()
		if v.Type().IsSetType() {
			k = val
		}
		if !k.Type().Equals(cty.String) {
			return nil, errors.New("for_each keys must be strings")
		}
		ctx := base.NewChild()
		ctx.Variables = map[string]cty.Value{"each": cty.ObjectVal(map[string]cty.Value{"key": k, "value": val})}
		out = append(out, instance{key: k.AsString(), label: fmt.Sprintf("[%q]", k.AsString()), ctx: ctx})
	}
	return out, nil
}

func evalContext(dir string) *hcl.EvalContext {
	path := cty.StringVal(dir)
	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"path": cty.ObjectVal(map[string]cty.Value{"module": path, "root": path, "cwd": path}),
		},
		Functions: map[string]function.Function{
			"file":         fileFunc(dir),
			"fileset":      filesetFunc(dir),
			"templatefile": templatefileFunc(dir),
			"jsonencode":   stdlib.JSONEncodeFunc,
			"jsondecode":   stdlib.JSONDecodeFunc,
			"trimspace":    stdlib.TrimSpaceFunc,
			"toset":        tosetFunc,
		},
	}
}

func resolve(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func fileFunc(dir string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "path", Type: cty.String}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			data, err := os.ReadFile(resolve(dir, args[0].AsString()))
			if err != nil {
				return cty.NilVal, err
			}
			return cty.StringVal(string(data)), nil
		},
	})
}

func filesetFunc(dir string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "path", Type: cty.String}, {Name: "pattern", Type: cty.String}},
		Type:   function.StaticReturnType(cty.Set(cty.String)),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			pattern := args[1].AsString()
			if strings.Contains(pattern, "**") {
				return cty.NilVal, errors.New("fileset patterns with ** are not supported")
			}
			root := resolve(dir, args[0].AsString())
			matches, err := filepath.Glob(filepath.Join(root, pattern))
			if err != nil {
				return cty.NilVal, err
			}
			slices.Sort(matches)
			if len(matches) == 0 {
				return cty.SetValEmpty(cty.String), nil
			}
			vals := make([]cty.Value, len(matches))
			for i, m := range matches {
				rel, err := filepath.Rel(root, m)
				if err != nil {
					return cty.NilVal, err
				}
				vals[i] = cty.StringVal(filepath.ToSlash(rel))
			}
			return cty.SetVal(vals), nil
		},
	})
}

func templatefileFunc(dir string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "path", Type: cty.String}, {Name: "vars", Type: cty.DynamicPseudoType}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			name := resolve(dir, args[0].AsString())
			src, err := os.ReadFile(name)
			if err != nil {
				return cty.NilVal, err
			}
			tmpl, diags := hclsyntax.ParseTemplate(src, name, hcl.InitialPos)
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			vars := map[string]cty.Value{}
			if v := args[1]; v.IsKnown() && !v.IsNull() && v.CanIterateElements() {
				vars = v.AsValueMap()
			}
			out, diags := tmpl.Value(&hcl.EvalContext{Variables: vars, Functions: evalContext(dir).Functions})
			if diags.HasErrors() {
				return cty.NilVal, diags
			}
			return out, nil
		},
	})
}

var tosetFunc = function.New(&function.Spec{
	Params: []function.Parameter{{Name: "v", Type: cty.DynamicPseudoType}},
	Type:   function.StaticReturnType(cty.Set(cty.String)),
	Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
		return convert.Convert(args[0], cty.Set(cty.String))
	},
})
