package provider

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/dashboard"
	"github.com/alexmchughdev/terraform-provider-terragraph/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terragraph/internal/tfschema"
)

func unmarshal(dv *tfprotov6.DynamicValue, b schema.Block) (tftypes.Value, error) {
	if dv == nil {
		return tftypes.NewValue(b.Type(), nil), nil
	}
	return dv.Unmarshal(b.Type())
}

func decode(dv *tfprotov6.DynamicValue, b schema.Block) (map[string]any, error) {
	v, err := unmarshal(dv, b)
	if err != nil {
		return nil, err
	}
	return tree(v)
}

func decodeResource(dv *tfprotov6.DynamicValue) (tftypes.Value, map[string]any, error) {
	v, err := unmarshal(dv, tfschema.Resource)
	if err != nil {
		return tftypes.Value{}, nil, err
	}
	t, err := tree(v)
	return v, t, err
}

func tree(v tftypes.Value) (map[string]any, error) {
	t, err := schema.FromValue(v)
	if err != nil {
		return nil, err
	}
	m, _ := t.(map[string]any)
	return m, nil
}

func marshal(b schema.Block, v tftypes.Value) (*tfprotov6.DynamicValue, error) {
	dv, err := tfprotov6.NewDynamicValue(b.Type(), v)
	return &dv, err
}

func withAttributes(v tftypes.Value, updates map[string]tftypes.Value) (tftypes.Value, error) {
	var attrs map[string]tftypes.Value
	if err := v.As(&attrs); err != nil {
		return tftypes.Value{}, err
	}
	for k, u := range updates {
		attrs[k] = u
	}
	return tftypes.NewValue(v.Type(), attrs), nil
}

func errorDiag(summary string, err error) []*tfprotov6.Diagnostic {
	return []*tfprotov6.Diagnostic{{
		Severity: tfprotov6.DiagnosticSeverityError,
		Summary:  summary,
		Detail:   err.Error(),
	}}
}

func validationDiags(diags []dashboard.Diagnostic) []*tfprotov6.Diagnostic {
	out := make([]*tfprotov6.Diagnostic, 0, len(diags))
	for _, d := range diags {
		severity := tfprotov6.DiagnosticSeverityError
		if d.Warning {
			severity = tfprotov6.DiagnosticSeverityWarning
		}
		out = append(out, &tfprotov6.Diagnostic{
			Severity:  severity,
			Summary:   d.Summary,
			Attribute: attributePath(d.Path),
		})
	}
	return out
}

func attributePath(steps []any) *tftypes.AttributePath {
	p := tftypes.NewAttributePath()
	for _, s := range steps {
		switch x := s.(type) {
		case string:
			p = p.WithAttributeName(x)
		case int:
			p = p.WithElementKeyInt(x)
		}
	}
	return p
}
