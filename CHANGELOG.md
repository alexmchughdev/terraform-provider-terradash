# Changelog

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning: [SemVer](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-10-06

### Added

- `terragraph_dashboard` resource: Grafana dashboards in native HCL (typed `variable`, `annotation`, `link`, `panel`, `row` blocks; verbatim plugin payloads; `extra` for untyped keys).
- `terragraph_dashboard_json` data source: render dashboard JSON without deploying.
- Auto layout and panel ids; `schemaVersion` defaults to 41.
- Import support: `import` blocks with `-generate-config-out` on Terraform and OpenTofu.
- Drift reconciliation: semantic comparison, layout noise ignored, plans show only real changes.
- Validation of dashboard structure at `validate` time.
- `overwrite = false` on create; existing dashboards are never clobbered silently.
- `terragraph convert`: dashboard JSON (files, dirs, stdin) to HCL, with optional `import` blocks.
- `terragraph convert` accepts YAML (YAML 1.2; `.yaml`/`.yml` in directories), including Git Sync `dashboard.grafana.app` resources.
- `terragraph migrate`: `grafana_dashboard` resources (JSON) to `terragraph_dashboard`, with `removed` and `import` blocks; `-remove` deletes the old blocks.
- `terragraph pull`: live Grafana to HCL with `import` blocks.
- Provider options: `url`, `auth`, `org_id`, `ca_cert`, `insecure_skip_verify`, `http_headers`, `retries`, `timeout`; env fallbacks.
- Hardened HTTP client: TLS >= 1.2, same-host redirects only, response size cap.
- Packaging: archives, deb, rpm, apk and archlinux packages; signed apt repository on GitHub Pages; Terraform Registry-ready releases.

[Unreleased]: https://github.com/alexmchughdev/terraform-provider-terragraph/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/alexmchughdev/terraform-provider-terragraph/releases/tag/v0.1.0
