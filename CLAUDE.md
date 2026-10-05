# terragraph

Terraform/OpenTofu provider (`alexmchughdev/terragraph`) for Grafana dashboards written in native HCL, plus a `terragraph` CLI that converts existing dashboard JSON (files or a live Grafana) into HCL.

## Working rules

- Go, idiomatic and boring. Small functions, no clever abstractions; prefer clarity over fewer lines.
- Bare-minimum code comments. Design notes live here, not in code.
- Commits: no AI attribution lines. Never push; everything stays local until the owner says otherwise.
- Every change must keep `make test`, `make lint` and `scripts/e2e.sh` green.

## Layout

| Path | Purpose |
| --- | --- |
| `main.go` | Provider entry point (`tf6server.Serve`). |
| `cmd/terragraph` | CLI: `convert` (JSON/YAML files, dirs, stdin to HCL), `pull` (live Grafana to HCL with `import` blocks), `migrate` (grafana provider resources to terragraph). |
| `internal/schema` | Tiny schema DSL: produces the protocol schema, Terraform's implied types, and converts `tftypes.Value` to and from plain Go trees. |
| `internal/dashboard` | The core. Field tables (`fields.go`), `Encode` (tree to Grafana JSON), `Decode` (JSON to tree), `Reconcile` (drift), `Validate`, `Equal`. No Terraform or HTTP knowledge. |
| `internal/hclgen` | Writes trees as formatted HCL (heredocs, escaping, `${DS_*}` placeholders to `var.*`). |
| `internal/tfschema` | Provider, resource and data source schemas (shared by the provider and the converter so the converter never imports the protocol server). |
| `internal/convert` | Loading dashboard JSON in its various shapes and producing HCL files. Shared by the CLI. |
| `internal/migrate` | Statically evaluates `grafana_dashboard` blocks (`file`, `templatefile`, `fileset`, `jsonencode`, `for_each`) and emits `removed` + `import` + `terragraph_dashboard` per instance. |
| `internal/grafana` | Minimal HTTP client (dashboards API only). |
| `internal/provider` | The tfprotov6 server: provider config, `terragraph_dashboard` resource, `terragraph_dashboard_json` data source. |
| `examples/` | Real, deployable configurations; used by the e2e script. |
| `scripts/e2e.sh` | End-to-end test against a real Grafana with real terraform/tofu binaries. |

## Key design decisions

**terraform-plugin-go instead of the plugin framework.** Panels need list blocks with dynamic attributes (`panel { options = { ... } }`). The framework rejects dynamic attributes inside list/set blocks; Terraform core supports them (it types such blocks as tuples). So the provider implements tfprotov6 directly, like `kubernetes_manifest`. `schema.Block.ListType` mirrors core: a list block containing dynamic types is `DynamicPseudoType`, and values are tuples.

**Tree representation.** Everything internal works on plain Go values: `map[string]any`, `[]any`, `string`, `json.Number`, `bool`, `nil`, and `schema.Unknown`. Numbers stay `json.Number` end to end to avoid float drift. Dynamic values are inferred the way HCL types literals (objects and tuples, nulls as `DynamicPseudoType`), so generated config and imported state have identical types and never produce phantom diffs.

**Typed where it matters, verbatim elsewhere.** Structural parts (dashboard settings, panels, rows, variables, annotations, links, grid positions) are typed snake_case attributes so `validate` catches mistakes. Plugin-specific payloads (`options`, `field_config`, `targets`, `transformations`, `datasource`, `query`, ...) are dynamic and use Grafana's JSON keys verbatim, so Grafana docs and exported JSON map one-to-one. Every block has an `extra` attribute: unknown or oddly-typed keys land there on decode, so conversion is lossless.

**Rows.** Grafana stores a flat panel list where panels following an expanded row belong to it and collapsed rows nest their panels. HCL models this as top-level `panel` blocks followed by `row` blocks containing `panel` blocks. Anything unrepresentable (panels after a collapsed row) falls back to `extra.panels` verbatim.

**Auto layout and ids.** `grid_pos` and `id` are optional. `layout.go` flows panels left to right (default 12x8), wraps at 24 columns, places rows at the bottom, and assigns ids above the highest explicit id. Rendering is deterministic, so the rendered JSON can be compared with what Grafana returns.

**Drift and reconciliation.** State holds the config tree, not JSON. On read the provider renders state and compares it with Grafana's model using `dashboard.Equal` (semantic: numeric canonicalisation; null, `""`, `[]` and `{}` inside objects count as absent). Equal means state is kept untouched. Otherwise `Reconcile` decodes the remote model but reuses the prior tree for every element/field that still renders identically (panels matched by id, variables by name). Drift plans therefore show only real changes, not auto-layout noise.

**Empty strings.** Terraform's `-generate-config-out` writes `""` as `null` (OpenTofu keeps `""`). Decode drops empty optional strings, and `Equal` treats them as absent, so both tools import cleanly.

**Defaults sent to Grafana.** `schemaVersion` defaults to 41 when unset; without it Grafana runs every migration on load and mangles modern panels.

**Plan/apply.** Plan marks `url`/`version` unknown only when the rendered JSON or folder changes; otherwise apply skips the API. `uid` changes force replacement. Creates send `overwrite = false` (configurable) so an existing dashboard is never clobbered silently; Grafana 13 answers 409 (older versions 412) and the error suggests importing.

**Import.** Two paths: `import {}` + `plan -generate-config-out` (works on Terraform and OpenTofu), or `terragraph convert`/`pull`, which produce nicer HCL (schema order, heredocs, no null noise, `__inputs` turned into variables) and work offline from JSON files. `Load` accepts raw models, `GET /api/dashboards/uid` responses and `dashboard.grafana.app` v0/v1 resources; v2 (layout/elements) dashboards are rejected with a clear error.

**YAML.** Inputs not starting with `{` are parsed as YAML 1.2 (`go.yaml.in/yaml/v3`). YAML 1.1 parsers turn keys like `y` into booleans and corrupt `gridPos`.

**Migration from the grafana provider.** Different resource types cannot use `moved`, so `migrate` emits `removed { lifecycle { destroy = false } }` for the old resource and an `import` for the new one on the same UID: the plan is N imports, nothing changed, nothing destroyed (verified in e2e; needs Terraform/OpenTofu >= 1.7). Expressions for `folder`, `overwrite`, `message` are copied as raw tokens (`hclwrite.Tokens` in a tree is written verbatim). Anything not statically evaluable is skipped with a warning and never removed.

**Packaging.** GoReleaser nfpm builds deb/rpm/apk/archlinux. Binaries live in `/usr/lib/terragraph`; relative symlinks expose `/usr/bin/terragraph` and the provider in Terraform's implied local mirror (`/usr/share/terraform/plugins/registry.{terraform.io,opentofu.org}/alexmchughdev/terragraph/<version>/linux_<arch>/`), so `init` works offline for both tools (verified with `XDG_DATA_DIRS` on an unpacked package). OpenTofu `init` queries the registry even for dev-overridden providers, so unpublished local testing with tofu needs the mirror, not `dev_overrides`, when other providers must be installed.

**Publishing.** The repo must be `alexmchughdev/terraform-provider-terragraph` (Terraform Registry naming); the Go module path matches, the provider address stays `alexmchughdev/terragraph`. Releases are non-draft so the registry webhook ingests them; signing needs an RSA key. The release workflow's `apt` job runs `scripts/apt-repo.sh` (apt-ftparchive + gpg) over the existing `gh-pages` pool plus new `.deb`s and republishes it; verified with a real `apt install` in a Debian 13 chroot. Owner steps are in RELEASING.md.

**Scope.** v2 dashboards (`elements`/`layout`) are rejected; a native v2 resource would be separate. SigNoz would be a sibling resource/provider reusing `schema`, `hclgen` and the tree approach.

## Security notes

- Credentials only via provider config (sensitive) or env (`GRAFANA_AUTH`); the CLI never takes them as flags.
- HTTP client: TLS >= 1.2, optional CA bundle, refuses redirects to other hosts (custom headers and auth are never forwarded off-host), 64 MiB response cap, credentials never included in errors.
- Warning when credentials would be sent over plain HTTP to a non-loopback host.
- CLI writes files with `O_EXCL` unless `-force`, checks every target before writing any, and only uses sanitised identifiers (or a validated `-name`) as file names. `variables` is reserved for `variables.tf`.
- Provider limits: `retries` <= 10, `timeout` <= 3600s; `Retry-After` is clamped to 30s; TLS verification failures are never retried.
- Numbers outside float64 range are rejected: decimal formatting of e.g. `1e100000000` would hang, and Grafana cannot represent them anyway.

## Development

Go, terraform, tofu and Grafana are not assumed to be installed system-wide.

```sh
make build        # bin/terraform-provider-terragraph, bin/terragraph
make test         # unit tests (race)
make lint         # golangci-lint
GRAFANA_URL=http://localhost:3000 GRAFANA_AUTH=admin:admin make testacc   # terraform-plugin-testing against Grafana
GRAFANA_URL=http://localhost:3000 GRAFANA_AUTH=admin:admin make e2e       # real terraform + tofu: fmt, validate, apply, drift, import
```

Local Grafana without Docker: download the release tarball and run `GF_SECURITY_ADMIN_PASSWORD=admin GF_PATHS_DATA=/tmp/gf bin/grafana server`.

Use a dev override to try the provider from a local build:

```hcl
provider_installation {
  dev_overrides {
    "registry.terraform.io/alexmchughdev/terragraph" = "/path/to/terragraph/bin"
  }
  direct {}
}
```

## Verified behaviour (Grafana 13.2.3, Terraform 1.16.5, OpenTofu 1.13.1)

- 37 real dashboards (Grafana bundled plugin dashboards and popular grafana.com dashboards, incl. Node Exporter Full) convert, validate, `fmt -check` clean, deploy, and re-plan with no changes; stored JSON matches the source semantically.
- Import via generated config and via `terragraph pull`: N to import, 0 to change, on both tools.
- Original vs HCL-deployed dashboards render pixel-identically where data is deterministic.
