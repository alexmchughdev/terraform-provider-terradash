package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/dashboard"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/grafana"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/tfschema"
)

func (p *Provider) ValidateResourceConfig(_ context.Context, req *tfprotov6.ValidateResourceConfigRequest) (*tfprotov6.ValidateResourceConfigResponse, error) {
	resp := &tfprotov6.ValidateResourceConfigResponse{}
	t, err := decode(req.Config, tfschema.Resource)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid configuration", err)
		return resp, nil
	}
	resp.Diagnostics = validationDiags(dashboard.Validate(dashboardBody(t)))
	if s, ok := t["folder_uid"].(string); ok && s != "" && !dashboard.ValidUID(s) {
		resp.Diagnostics = append(resp.Diagnostics, validationDiags([]dashboard.Diagnostic{{
			Path:    []any{"folder_uid"},
			Summary: "folder_uid " + dashboard.UIDRule,
		}})...)
	}
	return resp, nil
}

func (p *Provider) UpgradeResourceState(_ context.Context, req *tfprotov6.UpgradeResourceStateRequest) (*tfprotov6.UpgradeResourceStateResponse, error) {
	resp := &tfprotov6.UpgradeResourceStateResponse{}
	v, err := req.RawState.UnmarshalWithOpts(tfschema.Resource.Type(), tfprotov6.UnmarshalOpts{
		ValueFromJSONOpts: tftypes.ValueFromJSONOpts{IgnoreUndefinedAttributes: true},
	})
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to read state", err)
		return resp, nil
	}
	if resp.UpgradedState, err = marshal(tfschema.Resource, v); err != nil {
		resp.Diagnostics = errorDiag("Unable to upgrade state", err)
	}
	return resp, nil
}

func (p *Provider) PlanResourceChange(_ context.Context, req *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error) {
	resp := &tfprotov6.PlanResourceChangeResponse{PlannedState: req.ProposedNewState}
	proposed, proposedTree, err := decodeResource(req.ProposedNewState)
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to read plan", err)
		return resp, nil
	}
	prior, priorTree, err := decodeResource(req.PriorState)
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to read state", err)
		return resp, nil
	}
	if proposed.IsNull() {
		return resp, nil
	}

	unknown := map[string]tftypes.Value{
		"url":     tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		"version": tftypes.NewValue(tftypes.Number, tftypes.UnknownValue),
	}
	switch {
	case prior.IsNull():
		if proposedTree["uid"] == nil {
			unknown["uid"] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
		}
	case proposedTree["uid"] != priorTree["uid"]:
		resp.RequiresReplace = []*tftypes.AttributePath{tftypes.NewAttributePath().WithAttributeName("uid")}
	case !needsSave(priorTree, proposedTree):
		return resp, nil
	}
	planned, err := withAttributes(proposed, unknown)
	if err == nil {
		resp.PlannedState, err = marshal(tfschema.Resource, planned)
	}
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to plan", err)
	}
	return resp, nil
}

func (p *Provider) ApplyResourceChange(ctx context.Context, req *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	resp := &tfprotov6.ApplyResourceChangeResponse{NewState: req.PlannedState}
	planned, plannedTree, err := decodeResource(req.PlannedState)
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to read plan", err)
		return resp, nil
	}
	prior, priorTree, err := decodeResource(req.PriorState)
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to read state", err)
		return resp, nil
	}
	if !planned.IsNull() && !prior.IsNull() && !needsSave(priorTree, plannedTree) {
		return resp, nil
	}

	client, diags := p.requireClient()
	if diags != nil {
		resp.NewState = req.PriorState
		resp.Diagnostics = diags
		return resp, nil
	}
	if planned.IsNull() {
		uid, _ := priorTree["uid"].(string)
		if err := client.DeleteDashboard(ctx, uid); err != nil {
			resp.NewState = req.PriorState
			resp.Diagnostics = errorDiag("Unable to delete dashboard", err)
		}
		return resp, nil
	}

	saved, err := save(ctx, client, plannedTree, prior.IsNull())
	if err != nil {
		resp.NewState = req.PriorState
		resp.Diagnostics = errorDiag("Unable to save dashboard", err)
		return resp, nil
	}
	state, err := withAttributes(planned, map[string]tftypes.Value{
		"uid":     tftypes.NewValue(tftypes.String, saved.UID),
		"url":     tftypes.NewValue(tftypes.String, dashboardURL(client.BaseURL(), saved.URL)),
		"version": tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(saved.Version)),
	})
	if err == nil {
		resp.NewState, err = marshal(tfschema.Resource, state)
	}
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to record state", err)
	}
	return resp, nil
}

func save(ctx context.Context, client *grafana.Client, t map[string]any, create bool) (*grafana.SaveResponse, error) {
	body := dashboardBody(t)
	uid, _ := body["uid"].(string)
	if uid == "" {
		delete(body, "uid")
	}
	model, err := dashboard.Encode(body)
	if err != nil {
		return nil, err
	}
	overwrite, _ := t["overwrite"].(bool)
	folder, _ := t["folder_uid"].(string)
	message, _ := t["message"].(string)
	saved, err := client.SaveDashboard(ctx, grafana.SaveRequest{
		Dashboard: model,
		FolderUID: folder,
		Overwrite: overwrite || !create,
		Message:   message,
	})
	var apiErr *grafana.APIError
	if create && errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusConflict || apiErr.StatusCode == http.StatusPreconditionFailed) {
		return nil, fmt.Errorf("%w\n\nA dashboard with this UID or title already exists in the folder. "+
			"Import the existing dashboard (import ID: its UID), choose another uid/title, or set overwrite = true", err)
	}
	return saved, err
}

func (p *Provider) ReadResource(ctx context.Context, req *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
	resp := &tfprotov6.ReadResourceResponse{NewState: req.CurrentState}
	prior, priorTree, err := decodeResource(req.CurrentState)
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to read state", err)
		return resp, nil
	}
	if prior.IsNull() {
		return resp, nil
	}
	client, diags := p.requireClient()
	if diags != nil {
		resp.Diagnostics = diags
		return resp, nil
	}
	uid, _ := priorTree["uid"].(string)
	remote, err := client.GetDashboard(ctx, uid)
	if errors.Is(err, grafana.ErrNotFound) {
		resp.NewState, err = marshal(tfschema.Resource, tftypes.NewValue(tfschema.Resource.Type(), nil))
		if err != nil {
			resp.Diagnostics = errorDiag("Unable to record state", err)
		}
		return resp, nil
	}
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to read dashboard", err)
		return resp, nil
	}

	state, err := refreshedState(prior, priorTree, remote, client.BaseURL())
	if err == nil {
		resp.NewState, err = marshal(tfschema.Resource, state)
	}
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to record state", err)
	}
	return resp, nil
}

func refreshedState(prior tftypes.Value, priorTree map[string]any, remote *grafana.Dashboard, baseURL string) (tftypes.Value, error) {
	folder := folderValue(priorTree["folder_uid"], remote.Meta.FolderUID)
	link := dashboardURL(baseURL, remote.Meta.URL)
	body := dashboardBody(priorTree)
	importing := priorTree["title"] == nil

	if !importing && dashboard.Matches(body, remote.Model) {
		return withAttributes(prior, map[string]tftypes.Value{
			"folder_uid": tftypes.NewValue(tftypes.String, folder),
			"url":        tftypes.NewValue(tftypes.String, link),
			"version":    tftypes.NewValue(tftypes.Number, new(big.Float).SetInt64(remote.Meta.Version)),
		})
	}
	t := dashboard.Decode(remote.Model)
	if !importing {
		t = dashboard.Reconcile(body, remote.Model)
	}
	t["folder_uid"] = folder
	t["overwrite"] = priorTree["overwrite"]
	t["message"] = priorTree["message"]
	t["url"] = link
	t["version"] = json.Number(fmt.Sprint(remote.Meta.Version))
	return schema.ToValue(tfschema.Resource, t)
}

func (p *Provider) ImportResourceState(_ context.Context, req *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error) {
	resp := &tfprotov6.ImportResourceStateResponse{}
	if !dashboard.ValidUID(req.ID) {
		resp.Diagnostics = errorDiag("Invalid import ID", fmt.Errorf("expected a dashboard UID, got %q", req.ID))
		return resp, nil
	}
	v, err := schema.ToValue(tfschema.Resource, map[string]any{"uid": req.ID})
	var state *tfprotov6.DynamicValue
	if err == nil {
		state, err = marshal(tfschema.Resource, v)
	}
	if err != nil {
		resp.Diagnostics = errorDiag("Unable to import", err)
		return resp, nil
	}
	resp.ImportedResources = []*tfprotov6.ImportedResource{{TypeName: req.TypeName, State: state}}
	return resp, nil
}

func needsSave(prior, planned map[string]any) bool {
	if schema.HasUnknown(dashboardBody(planned)) || prior["folder_uid"] != planned["folder_uid"] {
		return true
	}
	a, errA := dashboard.Encode(dashboardBody(prior))
	b, errB := dashboard.Encode(dashboardBody(planned))
	return errA != nil || errB != nil || !dashboard.Equal(a, b)
}

func dashboardBody(t map[string]any) map[string]any {
	out := map[string]any{}
	for _, a := range dashboard.Body.Attributes {
		out[a.Name] = t[a.Name]
	}
	for _, b := range dashboard.Body.Blocks {
		out[b.Name] = t[b.Name]
	}
	return out
}

func folderValue(prior any, remote string) any {
	if s, _ := prior.(string); s == remote {
		return prior
	}
	if remote == "" {
		return nil
	}
	return remote
}

func dashboardURL(base, path string) string {
	u, err := url.Parse(base)
	if err != nil || !strings.HasPrefix(path, "/") {
		return base + path
	}
	if strings.HasPrefix(path, u.Path+"/") {
		u.Path = ""
	}
	return strings.TrimRight(u.String(), "/") + path
}
