package provider

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/dashboard"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/tfschema"
)

func (p *Provider) ValidateDataResourceConfig(_ context.Context, req *tfprotov6.ValidateDataResourceConfigRequest) (*tfprotov6.ValidateDataResourceConfigResponse, error) {
	resp := &tfprotov6.ValidateDataResourceConfigResponse{}
	t, err := decode(req.Config, tfschema.DataSource)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid configuration", err)
		return resp, nil
	}
	resp.Diagnostics = validationDiags(dashboard.Validate(dashboardBody(t)))
	return resp, nil
}

func (p *Provider) ReadDataSource(_ context.Context, req *tfprotov6.ReadDataSourceRequest) (*tfprotov6.ReadDataSourceResponse, error) {
	resp := &tfprotov6.ReadDataSourceResponse{}
	cfg, err := unmarshal(req.Config, tfschema.DataSource)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid configuration", err)
		return resp, nil
	}
	t, err := tree(cfg)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid configuration", err)
		return resp, nil
	}
	model, err := dashboard.Encode(dashboardBody(t))
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to render dashboard", err)
		return resp, nil
	}
	out, err := marshalJSON(model)
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to render dashboard", err)
		return resp, nil
	}
	state, err := withAttributes(cfg, map[string]tftypes.Value{"json": tftypes.NewValue(tftypes.String, string(out))})
	if err == nil {
		resp.State, err = marshal(tfschema.DataSource, state)
	}
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to record state", err)
	}
	return resp, nil
}

// marshalJSON renders a dashboard model as indented JSON without HTML escaping.
func marshalJSON(model map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(model); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
