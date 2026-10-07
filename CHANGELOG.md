# Changelog

Format: [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning: [SemVer](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-10-08

### Changed

- Renamed from terragraph to terradash (the name was taken). Provider `alexmchughdev/terradash`, resources `terradash_dashboard` and `terradash_dashboard_json`, CLI and packages `terradash`, repository `alexmchughdev/terraform-provider-terradash`, apt repository at `https://alexmchughdev.github.io/terraform-provider-terradash`.
- Upgrade from 0.1.x: replace `terragraph` with `terradash` in configuration, `terraform state rm` the old `terragraph_dashboard` addresses, and import the new ones by UID (`import` blocks). Dashboards are not touched.

## [0.1.1] - 2026-10-06

### Security

- Update dependencies: fixes GO-2026-5026 (`golang.org/x/net`), `golang.org/x/text` and `google.golang.org/grpc` (GO-2026-6443) advisories reported by govulncheck.

### Fixed

- CI and CodeQL now run on pushes to `master`.

## [0.1.0] - 2026-10-06

### Added

- `terradash_dashboard` resource: Grafana dashboards in native HCL (typed `variable`, `annotation`, `link`, `panel`, `row` blocks; verbatim plugin payloads; `extra` for untyped keys).
- `terradash_dashboard_json` data source: render dashboard JSON without deploying.
- Auto layout and panel ids; `schemaVersion` defaults to 41.
- Import support: `import` blocks with `-generate-config-out` on Terraform and OpenTofu.
- Drift reconciliation: semantic comparison, layout noise ignored, plans show only real changes.
- Validation of dashboard structure at `validate` time.
- `overwrite = false` on create; existing dashboards are never clobbered silently.
- `terradash convert`: dashboard JSON (files, dirs, stdin) to HCL, with optional `import` blocks.
- `terradash convert` accepts YAML (YAML 1.2; `.yaml`/`.yml` in directories), including Git Sync `dashboard.grafana.app` resources.
- `terradash migrate`: `grafana_dashboard` resources (JSON) to `terradash_dashboard`, with `removed` and `import` blocks; `-remove` deletes the old blocks.
- `terradash pull`: live Grafana to HCL with `import` blocks.
- Provider options: `url`, `auth`, `org_id`, `ca_cert`, `insecure_skip_verify`, `http_headers`, `retries`, `timeout`; env fallbacks.
- Hardened HTTP client: TLS >= 1.2, same-host redirects only, response size cap.
- Packaging: archives, deb, rpm, apk and archlinux packages; signed apt repository on GitHub Pages; Terraform Registry-ready releases.

[Unreleased]: https://github.com/alexmchughdev/terraform-provider-terradash/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/alexmchughdev/terraform-provider-terradash/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/alexmchughdev/terraform-provider-terradash/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/alexmchughdev/terraform-provider-terradash/releases/tag/v0.1.0
