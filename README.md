# terradash

Terraform/OpenTofu provider (`alexmchughdev/terradash`) for Grafana dashboards in native HCL, plus a `terradash` CLI that converts dashboard JSON/YAML or a live Grafana into HCL.

- Typed snake_case blocks (`variable`, `annotation`, `link`, `panel`, `row`); plugin payloads use Grafana's JSON keys verbatim.
- `extra` carries keys without a typed attribute.
- Auto layout and ids; drift ignores layout noise; `overwrite = false` on create.
- `terradash_dashboard_json` renders JSON without deploying.

## Example

```terraform
resource "terradash_dashboard" "service_overview" {
  uid   = "service-overview"
  title = "Service overview"

  panel {
    type     = "stat"
    title    = "Availability"
    grid_pos = { w = 6, h = 5 }
    targets  = [{ refId = "A", scenarioId = "random_walk" }]
  }
}
```

Full: [`examples/service-overview`](examples/service-overview/main.tf).

## Install

Provider: from the Terraform Registry on `init` (below). CLI and provider as a package:

```sh
# Debian/Ubuntu
curl -fsSL https://alexmchughdev.github.io/terraform-provider-terradash/key.gpg | sudo gpg --dearmor -o /etc/apt/keyrings/terradash.gpg
echo "deb [signed-by=/etc/apt/keyrings/terradash.gpg] https://alexmchughdev.github.io/terraform-provider-terradash stable main" | sudo tee /etc/apt/sources.list.d/terradash.list
sudo apt update && sudo apt install terradash
```

rpm, apk and Arch packages and standalone CLI binaries (macOS, Windows, FreeBSD, Linux) are attached to each [release](https://github.com/alexmchughdev/terraform-provider-terradash/releases). Packages also install the provider into Terraform's local plugin directory, so `init` works offline.

## Configure

```terraform
terraform {
  required_providers {
    terradash = { source = "alexmchughdev/terradash" }
  }
}

provider "terradash" {
  url  = "https://grafana.example.com" # or GRAFANA_URL
  auth = var.grafana_token             # or GRAFANA_AUTH
}
```

OpenTofu: until terradash is listed on registry.opentofu.org, use `source = "registry.terraform.io/alexmchughdev/terradash"`.

- `auth`: service account token, `user:password` or `anonymous`.
- Also: `org_id`, `ca_cert`, `insecure_skip_verify` (`GRAFANA_ORG_ID`, `GRAFANA_CA_CERT`, `GRAFANA_INSECURE_SKIP_VERIFY`), `http_headers`, `retries`, `timeout`.

Local build (Go required): `make build` (`bin/terraform-provider-terradash`, `bin/terradash`), `make install`.

Dev override (`TF_CLI_CONFIG_FILE`, `~/.terraformrc` or `~/.tofurc`); skip `init`:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/alexmchughdev/terradash" = "/path/to/terradash/bin"
    "registry.opentofu.org/alexmchughdev/terradash" = "/path/to/terradash/bin"
  }
  direct {}
}
```

## Convert existing dashboards

```sh
terradash convert -o dashboards/ -import exported/*.{json,yaml}   # offline
terradash pull -o dashboards/ UID [UID ...]                       # or -all, -folder UID1,UID2
```

`pull` needs `GRAFANA_URL` and `GRAFANA_AUTH`. Existing files need `-force`. See `terradash convert -h`, `terradash pull -h`.

`terradash migrate -o terradash.tf -remove .` converts `grafana_dashboard` resources from the `grafana/grafana` provider ([guide](docs/guides/migrating-from-grafana-provider.md)).

Provider only: add an `import` block (ID = dashboard UID), then `terraform plan -generate-config-out=dashboards.tf`.

## Compatibility

- Classic dashboard JSON only; v2 schema (`layout` / `elements`) is unsupported.
- Tested: Grafana 13.2.3, Terraform 1.16, OpenTofu 1.13. Older Grafana with `/api/dashboards` untested.
- `schemaVersion` defaults to 41.

## Docs

Generated into [`docs/`](docs/) from [`templates/`](templates/) and [`examples/`](examples/) (`make docs`).

## Development

`make build|install|test|lint|fmt|cover|clean|docs`. `make testacc` and `make e2e` (`scripts/e2e.sh [terraform|tofu ...]`) need Grafana:

```sh
export GRAFANA_URL=http://localhost:3000 GRAFANA_AUTH=admin:admin
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

## License

[MPL-2.0](LICENSE).
