package provider

import (
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/alexmchughdev/terraform-provider-terradash/internal/schema"
	"github.com/alexmchughdev/terraform-provider-terradash/internal/tfschema"
)

func TestGetProviderSchema(t *testing.T) {
	resp, err := New("test").GetProviderSchema(t.Context(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Provider == nil {
		t.Error("missing provider schema")
	}
	if resp.ResourceSchemas[tfschema.ResourceType] == nil {
		t.Errorf("missing resource schema %s", tfschema.ResourceType)
	}
	if resp.DataSourceSchemas[tfschema.DataSourceType] == nil {
		t.Errorf("missing data source schema %s", tfschema.DataSourceType)
	}
	var blocks []string
	for _, b := range resp.ResourceSchemas[tfschema.ResourceType].Block.BlockTypes {
		blocks = append(blocks, b.TypeName)
	}
	if got := strings.Join(blocks, ","); got != "variable,annotation,link,panel,row" {
		t.Errorf("resource blocks = %s", got)
	}
}

func TestGetMetadata(t *testing.T) {
	resp, err := New("test").GetMetadata(t.Context(), &tfprotov6.GetMetadataRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Resources) != 1 || resp.Resources[0].TypeName != tfschema.ResourceType {
		t.Errorf("resources = %v", resp.Resources)
	}
	if len(resp.DataSources) != 1 || resp.DataSources[0].TypeName != tfschema.DataSourceType {
		t.Errorf("data sources = %v", resp.DataSources)
	}
}

func TestValidateProviderConfig(t *testing.T) {
	tests := []struct {
		name string
		url  string
		bad  bool
	}{
		{"valid", "https://grafana.example.com", false},
		{"bad scheme", "ftp://grafana.example.com", true},
		{"credentials in url", "https://user:pass@grafana.example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := New("test").ValidateProviderConfig(t.Context(), &tfprotov6.ValidateProviderConfigRequest{
				Config: dynamicValue(t, tfschema.Provider, map[string]any{"url": tt.url}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(resp.Diagnostics) > 0; got != tt.bad {
				t.Errorf("diagnostics = %q, want error: %v", errorSummaries(resp.Diagnostics), tt.bad)
			}
		})
	}
}

func clearEnv(t *testing.T) {
	for _, k := range []string{"GRAFANA_URL", "GRAFANA_AUTH", "GRAFANA_ORG_ID", "GRAFANA_CA_CERT", "GRAFANA_INSECURE_SKIP_VERIFY"} {
		t.Setenv(k, "")
	}
}

func TestConfigureProvider(t *testing.T) {
	clearEnv(t)
	srv := httptest.NewUnstartedServer(nil)
	srv.StartTLS()
	defer srv.Close()
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	certFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(certFile, []byte(certPEM), 0o600); err != nil {
		t.Fatal(err)
	}
	garbageFile := filepath.Join(t.TempDir(), "garbage.pem")
	if err := os.WriteFile(garbageFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		cfg        map[string]any
		env        map[string]string
		wantErr    string
		wantWarn   bool
		wantClient bool
	}{
		{name: "https url", cfg: map[string]any{"url": "https://grafana.example.com", "auth": "token"}, wantClient: true},
		{name: "url from env", env: map[string]string{"GRAFANA_URL": "http://localhost:3000"}, wantClient: true},
		{name: "plain http with credentials", cfg: map[string]any{"url": "http://grafana.example.com", "auth": "admin:admin"}, wantWarn: true, wantClient: true},
		{name: "plain http without credentials", cfg: map[string]any{"url": "http://grafana.example.com"}, wantClient: true},
		{name: "plain http anonymous", cfg: map[string]any{"url": "http://grafana.example.com", "auth": "anonymous"}, wantClient: true},
		{name: "localhost", cfg: map[string]any{"url": "http://localhost:3000", "auth": "admin:admin"}, wantClient: true},
		{name: "loopback ip", cfg: map[string]any{"url": "http://127.0.0.1:3000", "auth": "admin:admin"}, wantClient: true},
		{name: "ipv6 loopback", cfg: map[string]any{"url": "http://[::1]:3000", "auth": "admin:admin"}, wantClient: true},
		{name: "org id", cfg: map[string]any{"url": "https://g.example.com", "org_id": num("2")}, wantClient: true},
		{name: "negative org id", cfg: map[string]any{"url": "https://g.example.com", "org_id": num("-1")}, wantErr: "org_id"},
		{name: "invalid org id env", cfg: map[string]any{"url": "https://g.example.com"}, env: map[string]string{"GRAFANA_ORG_ID": "two"}, wantErr: "org_id"},
		{name: "invalid bool env", cfg: map[string]any{"url": "https://g.example.com"}, env: map[string]string{"GRAFANA_INSECURE_SKIP_VERIFY": "maybe"}, wantErr: "insecure_skip_verify"},
		{name: "inline ca cert", cfg: map[string]any{"url": srv.URL, "ca_cert": certPEM}, wantClient: true},
		{name: "ca cert path", cfg: map[string]any{"url": srv.URL, "ca_cert": certFile}, wantClient: true},
		{name: "ca cert missing file", cfg: map[string]any{"url": srv.URL, "ca_cert": filepath.Join(t.TempDir(), "missing.pem")}, wantErr: "ca_cert"},
		{name: "ca cert not pem", cfg: map[string]any{"url": srv.URL, "ca_cert": garbageFile}, wantErr: "no valid certificates"},
		{name: "invalid url", cfg: map[string]any{"url": "grafana.example.com"}, wantErr: "scheme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			p := New("test").(*Provider)
			resp, err := p.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{
				Config: dynamicValue(t, tfschema.Provider, tt.cfg),
			})
			if err != nil {
				t.Fatal(err)
			}
			got := errorSummaries(resp.Diagnostics)
			if tt.wantErr != "" {
				if !strings.Contains(got, tt.wantErr) {
					t.Fatalf("diagnostics = %q, want %q", got, tt.wantErr)
				}
				return
			}
			warned := len(resp.Diagnostics) == 1 && resp.Diagnostics[0].Severity == tfprotov6.DiagnosticSeverityWarning
			if tt.wantWarn != warned || (!tt.wantWarn && len(resp.Diagnostics) > 0) {
				t.Errorf("diagnostics = %q, want warning: %v", got, tt.wantWarn)
			}
			if (p.client != nil) != tt.wantClient {
				t.Errorf("client configured = %v, want %v", p.client != nil, tt.wantClient)
			}
		})
	}
}

func TestUnconfiguredProviderErrors(t *testing.T) {
	clearEnv(t)
	tests := []struct {
		name string
		cfg  func(*testing.T) *tfprotov6.DynamicValue
		want string
	}{
		{"url missing", func(t *testing.T) *tfprotov6.DynamicValue { return dynamicValue(t, tfschema.Provider, nil) }, "url is not set"},
		{"url unknown", func(t *testing.T) *tfprotov6.DynamicValue {
			return dynamicValue(t, tfschema.Provider, map[string]any{"url": schema.Unknown})
		}, "not known until apply"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New("test").(*Provider)
			resp, err := p.ConfigureProvider(t.Context(), &tfprotov6.ConfigureProviderRequest{Config: tt.cfg(t)})
			if err != nil || len(resp.Diagnostics) > 0 {
				t.Fatalf("configure should defer the error: %v %q", err, errorSummaries(resp.Diagnostics))
			}
			if p.client != nil {
				t.Fatal("client should be nil")
			}

			read, _ := p.ReadResource(t.Context(), &tfprotov6.ReadResourceRequest{
				TypeName:     tfschema.ResourceType,
				CurrentState: dynamicValue(t, tfschema.Resource, stored()),
			})
			if got := errorSummaries(read.Diagnostics); !strings.Contains(got, tt.want) {
				t.Errorf("read diagnostics = %q, want %q", got, tt.want)
			}

			apply, _ := p.ApplyResourceChange(t.Context(), &tfprotov6.ApplyResourceChangeRequest{
				TypeName:     tfschema.ResourceType,
				PriorState:   nullState(t),
				PlannedState: dynamicValue(t, tfschema.Resource, testDashboard()),
			})
			if got := errorSummaries(apply.Diagnostics); !strings.Contains(got, tt.want) {
				t.Errorf("apply diagnostics = %q, want %q", got, tt.want)
			}
		})
	}
}
