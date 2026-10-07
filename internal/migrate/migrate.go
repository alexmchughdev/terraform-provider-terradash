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
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/convert"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/hclgen"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/tfschema"
)

const grafanaDashboard = "grafana_dashboard"

// Result holds the generated configuration for every migrated dashboard.
type Result struct {
	Outputs  []convert.Output
	Sources  []Source
	Warnings []string
}

// Source identifies a grafana_dashboard block that was migrated.
type Source struct {
	File string
	Name string
}

// Run converts the grafana_dashboard resources of the module in dir.
func Run(dir string) (Result, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return Result{}, err
	}
	slices.Sort(files)
	var res Result
	var namer convert.Namer
	for _, file := range files {
		if err := migrateFile(dir, file, &namer, &res); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

func migrateFile(dir, file string, namer *convert.Namer, res *Result) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	syntax, diags := hclsyntax.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return diags
	}
	written, diags := hclwrite.ParseConfig(src, file, hcl.InitialPos)
	if diags.HasErrors() {
		return diags
	}
	for _, block := range syntax.Body.(*hclsyntax.Body).Blocks {
		if block.Type != "resource" || len(block.Labels) != 2 || block.Labels[0] != grafanaDashboard {
			continue
		}
		raw := written.Body().FirstMatchingBlock("resource", block.Labels)
		outputs, warnings := migrateBlock(dir, block, raw, namer)
		res.Warnings = append(res.Warnings, warnings...)
		if len(outputs) > 0 {
			res.Outputs = append(res.Outputs, outputs...)
			res.Sources = append(res.Sources, Source{File: file, Name: block.Labels[1]})
		}
	}
	return nil
}

func migrateBlock(dir string, block *hclsyntax.Block, raw *hclwrite.Block, namer *convert.Namer) ([]convert.Output, []string) {
	name := block.Labels[1]
	address := grafanaDashboard + "." + name
	warnings := unsupportedArguments(address, block)
	instances, err := expand(dir, block)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("%s: skipped: %v", address, err))
	}

	var outputs []convert.Output
	for i, inst := range instances {
		src, err := dashboardSource(block, inst.ctx)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s%s: skipped: %v", address, inst.label, err))
			continue
		}
		resourceName := name
		if inst.key != "" {
			resourceName = namer.Name(name + "_" + keyStem(inst.key))
		}
		out, warning := render(src, resourceName, address, raw, i == 0)
		outputs = append(outputs, out)
		warnings = append(warnings, warning...)
	}
	return outputs, warnings
}

func unsupportedArguments(address string, block *hclsyntax.Block) []string {
	var warnings []string
	for _, attr := range []string{"org_id", "provider", "depends_on", "count"} {
		if _, ok := block.Body.Attributes[attr]; ok {
			warnings = append(warnings, fmt.Sprintf("%s: %q is not migrated; review the generated resource", address, attr))
		}
	}
	return warnings
}

func dashboardSource(block *hclsyntax.Block, ctx *hcl.EvalContext) (convert.Source, error) {
	attr, ok := block.Body.Attributes["config_json"]
	if !ok {
		return convert.Source{}, errors.New("no config_json")
	}
	v, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		return convert.Source{}, fmt.Errorf("config_json cannot be evaluated statically: %s", diags.Error())
	}
	if !v.IsKnown() || v.IsNull() || !v.Type().Equals(cty.String) {
		return convert.Source{}, errors.New("config_json is not a known string")
	}
	return convert.Load([]byte(v.AsString()))
}

func render(src convert.Source, resourceName, address string, raw *hclwrite.Block, first bool) (convert.Output, []string) {
	t, placeholders, vars := convert.Tree(src, convert.Options{})
	out := convert.Output{Name: resourceName, Variables: vars}
	var warnings []string

	if folder := raw.Body().GetAttribute("folder"); folder != nil {
		tokens := folder.Expr().BuildTokens(nil)
		expr := strings.TrimSpace(string(tokens.Bytes()))
		switch {
		case strings.Contains(expr, "each."):
			warnings = append(warnings, fmt.Sprintf("%s: folder %q depends on each; set folder_uid on %s by hand", address, expr, resourceName))
		default:
			t["folder_uid"] = tokens
			if strings.HasSuffix(expr, ".id") {
				warnings = append(warnings, fmt.Sprintf("%s: folder %q looks like a numeric folder id; folder_uid needs the folder UID", address, expr))
			}
		}
	}
	for _, arg := range []string{"overwrite", "message"} {
		if attr := raw.Body().GetAttribute(arg); attr != nil {
			t[arg] = attr.Expr().BuildTokens(nil)
		}
	}

	f := hclwrite.NewEmptyFile()
	if first {
		removed := f.Body().AppendNewBlock("removed", nil)
		removed.Body().SetAttributeTraversal("from", hcl.Traversal{hcl.TraverseRoot{Name: grafanaDashboard}, hcl.TraverseAttr{Name: raw.Labels()[1]}})
		removed.Body().AppendNewline()
		removed.Body().AppendNewBlock("lifecycle", nil).Body().SetAttributeValue("destroy", cty.False)
		f.Body().AppendNewline()
	}
	if uid, ok := t["uid"].(string); ok {
		hclgen.Import(f.Body(), tfschema.ResourceType, resourceName, uid)
		f.Body().AppendNewline()
	} else {
		warnings = append(warnings, fmt.Sprintf("%s: dashboard JSON has no uid; add an import block for %s by hand", address, resourceName))
	}
	hclgen.Resource(f.Body(), tfschema.ResourceType, resourceName, tfschema.Resource, t, hclgen.Options{Variables: placeholders})
	out.HCL = hclwrite.Format(f.Bytes())
	return out, warnings
}

func keyStem(key string) string {
	base := filepath.Base(key)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
