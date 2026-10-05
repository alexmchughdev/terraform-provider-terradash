# Contributing

## Prerequisites

- Go 1.27.1 (see `go.mod`).
- `golangci-lint`.
- Terraform and/or OpenTofu (acceptance, e2e, `make docs`).
- A Grafana for `make testacc` and `make e2e`; local, no Docker:

  ```sh
  GF_SECURITY_ADMIN_PASSWORD=admin GF_PATHS_DATA=/tmp/gf bin/grafana server
  export GRAFANA_URL=http://localhost:3000 GRAFANA_AUTH=admin:admin
  ```

  Download the Grafana release tarball first. CI uses `grafana/grafana:13.2.3`.
- `jq` and `curl` for `scripts/e2e.sh`.

## Dev loop

```sh
make build test lint   # bin/, unit tests (race), golangci-lint
make testacc           # acceptance tests; needs Grafana
make e2e               # real terraform/tofu; needs Grafana
make docs              # after schema, template or example changes
make fmt               # gofmt -s, go mod tidy
```

- Keep `make test`, `make lint` and `scripts/e2e.sh` green.
- Commit regenerated `docs/`.
- Architecture and design decisions: `CLAUDE.md`.

## Code style

- Idiomatic, boring Go. Small functions, no clever abstractions.
- Minimal comments; design notes go in `CLAUDE.md`.
- Tests required for new behaviour and bug fixes.
- `gofmt` and `golangci-lint` clean.
- Credentials only via provider config or env; never CLI flags.

## Commits

- Imperative, concise subject ("Add X", "Fix Y"); no conventional-commit prefixes.
- No AI or co-author attribution lines.

## Pull requests

- One logical change per PR.
- Tests added or updated.
- `make build test lint` pass; `make e2e` for provider or converter changes.
- `make docs` run if schema, templates or examples changed.
- `CHANGELOG.md` updated under `Unreleased`.
- CI green (lint, test, acceptance, generated, vuln, e2e, goreleaser-check).

## Releases

See [RELEASING.md](RELEASING.md).

## Conduct and security

- [Code of Conduct](CODE_OF_CONDUCT.md).
- Vulnerabilities: see [SECURITY.md](SECURITY.md); do not open public issues.
