package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/grafana"
	"github.com/alexmchughdev/terraform-provider-terragraph/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terragraph/internal/tfschema"
)

const (
	maxRetries        = 10
	maxTimeoutSeconds = 3600
)

type Provider struct {
	version   string
	client    *grafana.Client
	configErr error
}

func New(version string) tfprotov6.ProviderServer {
	return &Provider{version: version, configErr: errors.New("the provider has not been configured")}
}

func (p *Provider) GetMetadata(context.Context, *tfprotov6.GetMetadataRequest) (*tfprotov6.GetMetadataResponse, error) {
	return &tfprotov6.GetMetadataResponse{
		ServerCapabilities: &tfprotov6.ServerCapabilities{GetProviderSchemaOptional: true},
		Resources:          []tfprotov6.ResourceMetadata{{TypeName: tfschema.ResourceType}},
		DataSources:        []tfprotov6.DataSourceMetadata{{TypeName: tfschema.DataSourceType}},
	}, nil
}

func (p *Provider) GetProviderSchema(context.Context, *tfprotov6.GetProviderSchemaRequest) (*tfprotov6.GetProviderSchemaResponse, error) {
	return &tfprotov6.GetProviderSchemaResponse{
		ServerCapabilities: &tfprotov6.ServerCapabilities{GetProviderSchemaOptional: true},
		Provider:           &tfprotov6.Schema{Block: tfschema.Provider.Proto()},
		ResourceSchemas:    map[string]*tfprotov6.Schema{tfschema.ResourceType: {Block: tfschema.Resource.Proto()}},
		DataSourceSchemas:  map[string]*tfprotov6.Schema{tfschema.DataSourceType: {Block: tfschema.DataSource.Proto()}},
	}, nil
}

func (p *Provider) ValidateProviderConfig(_ context.Context, req *tfprotov6.ValidateProviderConfigRequest) (*tfprotov6.ValidateProviderConfigResponse, error) {
	resp := &tfprotov6.ValidateProviderConfigResponse{PreparedConfig: req.Config}
	cfg, err := decode(req.Config, tfschema.Provider)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid provider configuration", err)
		return resp, nil
	}
	if s, ok := cfg["url"].(string); ok {
		if _, err := grafana.ParseBaseURL(s); err != nil {
			resp.Diagnostics = errorDiag("Invalid url", err)
		}
	}
	return resp, nil
}

func (p *Provider) ConfigureProvider(_ context.Context, req *tfprotov6.ConfigureProviderRequest) (*tfprotov6.ConfigureProviderResponse, error) {
	resp := &tfprotov6.ConfigureProviderResponse{}
	cfg, err := decode(req.Config, tfschema.Provider)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid provider configuration", err)
		return resp, nil
	}
	if schema.HasUnknown(cfg) {
		p.configErr = errors.New("the provider configuration depends on values that are not known until apply")
		return resp, nil
	}
	clientCfg, err := p.clientConfig(cfg)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid provider configuration", err)
		return resp, nil
	}
	if clientCfg.URL == "" {
		p.configErr = errors.New("url is not set; configure the provider's url argument or GRAFANA_URL")
		return resp, nil
	}
	client, err := grafana.New(clientCfg)
	if err != nil {
		resp.Diagnostics = errorDiag("Invalid provider configuration", err)
		return resp, nil
	}
	p.client, p.configErr = client, nil
	if warning := plaintextWarning(clientCfg); warning != nil {
		resp.Diagnostics = append(resp.Diagnostics, warning)
	}
	return resp, nil
}

func (p *Provider) clientConfig(cfg map[string]any) (grafana.Config, error) {
	c := grafana.Config{
		URL:       stringOr(cfg["url"], os.Getenv("GRAFANA_URL")),
		Auth:      stringOr(cfg["auth"], os.Getenv("GRAFANA_AUTH")),
		Retries:   grafana.DefaultRetries,
		UserAgent: "terraform-provider-terragraph/" + p.version,
	}
	orgID, err := int64Setting(cfg["org_id"], "GRAFANA_ORG_ID")
	if err != nil {
		return c, fmt.Errorf("org_id: %w", err)
	}
	c.OrgID = orgID
	retries, err := int64Setting(cfg["retries"], "")
	if err != nil {
		return c, fmt.Errorf("retries: %w", err)
	}
	if retries > maxRetries {
		return c, fmt.Errorf("retries must be at most %d", maxRetries)
	}
	if cfg["retries"] != nil {
		c.Retries = int(retries)
	}
	timeout, err := int64Setting(cfg["timeout"], "")
	if err != nil {
		return c, fmt.Errorf("timeout: %w", err)
	}
	if timeout > maxTimeoutSeconds {
		return c, fmt.Errorf("timeout must be at most %d seconds", maxTimeoutSeconds)
	}
	c.Timeout = time.Duration(timeout) * time.Second
	if c.InsecureSkipVerify, err = boolSetting(cfg["insecure_skip_verify"], "GRAFANA_INSECURE_SKIP_VERIFY"); err != nil {
		return c, fmt.Errorf("insecure_skip_verify: %w", err)
	}
	if c.CACertPEM, err = caCert(stringOr(cfg["ca_cert"], os.Getenv("GRAFANA_CA_CERT"))); err != nil {
		return c, fmt.Errorf("ca_cert: %w", err)
	}
	if headers, ok := cfg["http_headers"].(map[string]any); ok {
		c.Headers = make(map[string]string, len(headers))
		for k, v := range headers {
			if s, ok := v.(string); ok {
				c.Headers[k] = s
			}
		}
	}
	return c, nil
}

func stringOr(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

func int64Setting(v any, env string) (int64, error) {
	var s string
	if n, ok := v.(json.Number); ok {
		s = string(n)
	} else if env != "" {
		s = os.Getenv(env)
	}
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a non-negative integer", s)
	}
	return n, nil
}

func boolSetting(v any, env string) (bool, error) {
	if b, ok := v.(bool); ok {
		return b, nil
	}
	s := os.Getenv(env)
	if s == "" {
		return false, nil
	}
	return strconv.ParseBool(s)
}

func caCert(v string) ([]byte, error) {
	if v == "" || strings.Contains(v, "-----BEGIN") {
		return []byte(v), nil
	}
	return os.ReadFile(v)
}

func plaintextWarning(c grafana.Config) *tfprotov6.Diagnostic {
	if !c.SendsPlaintextCredentials() {
		return nil
	}
	return &tfprotov6.Diagnostic{
		Severity: tfprotov6.DiagnosticSeverityWarning,
		Summary:  "Credentials sent over plain HTTP",
		Detail:   "The Grafana url uses http://, so credentials are sent unencrypted. Use https:// outside local development.",
	}
}

func (p *Provider) requireClient() (*grafana.Client, []*tfprotov6.Diagnostic) {
	if p.client == nil {
		return nil, errorDiag("Grafana client not configured", p.configErr)
	}
	return p.client, nil
}

func (p *Provider) StopProvider(context.Context, *tfprotov6.StopProviderRequest) (*tfprotov6.StopProviderResponse, error) {
	return &tfprotov6.StopProviderResponse{}, nil
}
