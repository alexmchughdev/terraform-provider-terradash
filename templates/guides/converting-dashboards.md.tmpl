---
page_title: "Converting existing dashboards"
subcategory: "Guides"
description: |-
  Turn existing Grafana dashboards or dashboard JSON files into terragraph HCL.
---

# Converting existing dashboards

- `terragraph convert`: JSON or YAML files, offline.
- `terragraph migrate`: `grafana_dashboard` resources to terragraph ([guide](migrating-from-grafana-provider)).
- `terragraph pull`: live Grafana.
- No CLI: `import` block + `-generate-config-out`.

Build the CLI with `make build` (`bin/terragraph`).

## `terragraph convert`

```shell
terragraph convert [flags] FILE|DIR|- ...
```

Inputs: files, directories (`*.json`, `*.yaml`, `*.yml` directly inside), `-` for stdin (once). JSON or YAML is detected from content.

| Flag | Description |
| --- | --- |
| `-o PATH` | Output; `.tf` path is one file, otherwise a directory with `<resource_name>.tf` each. Default stdout. |
| `-import` | Emit `import` blocks for dashboards with a `uid`. |
| `-folder-uid UID` | Set `folder_uid` on all dashboards. |
| `-name NAME` | Resource name; one input only; valid identifier. |
| `-force` | Overwrite existing files. |

- Resource names come from titles (lower-case, underscores, unique).
- Accepted: raw model, `GET /api/dashboards/uid/<uid>` response (`meta.folderUid` becomes `folder_uid`), `dashboard.grafana.app` v0/v1 `Dashboard` (JSON, or YAML as in Git Sync).
- YAML is parsed as YAML 1.2 (`y`, `no` stay strings).
- v2 schema (`layout` / `elements`) is rejected.
- Nothing is overwritten without `-force`; directory output checks all targets first.

```shell
terragraph convert -o dashboards/ -folder-uid platform exported/*.json
terraform fmt -check dashboards/ && terraform validate
```

### `__inputs` (export for sharing)

- `__inputs`, `__requires`, `__elements` are removed.
- Each input becomes a Terraform `variable`; `${DS_X}` becomes `var.<name>`; constants keep their value as default.
- Variables go to `variables.tf` (directory mode) or the top of the output.

## `terragraph pull`

```shell
export GRAFANA_URL=https://grafana.example.com GRAFANA_AUTH=glsa_xxxxxxxx
terragraph pull -o dashboards/ UID [UID ...]
terragraph pull -o dashboards/ -folder platform,sre
terragraph pull -o dashboards/ -all
```

Credentials come only from `GRAFANA_AUTH` (no flag).

| Flag | Description |
| --- | --- |
| `-url URL` | Default `$GRAFANA_URL`. |
| `-org-id ID` | Default `$GRAFANA_ORG_ID`. |
| `-ca-cert PATH` | PEM CA bundle. Default `$GRAFANA_CA_CERT`. |
| `-insecure-skip-verify` | Skip TLS verification. |
| `-all` | Every dashboard. |
| `-folder UIDS` | Comma-separated folder UIDs. |
| `-o PATH` | Output file or directory. Default stdout. |
| `-import` | `import` blocks; default `true`. |
| `-force` | Overwrite existing files. |

- UIDs combine with `-all` / `-folder` without duplicates.
- File-provisioned dashboards produce a warning (API changes are rejected).
- `folder_uid` is written from the dashboard; the folder itself is not managed.

## Workflow

1. `terragraph pull -o dashboards/ -folder platform`
2. `terraform plan` shows `N to import, 0 to change`; `terraform apply`.
3. Remove the `import` blocks.

## Without the CLI

Write an `import` block ([syntax](../resources/dashboard#import)), then `terraform plan -generate-config-out=dashboards.tf`.

- Output is noisier (explicit `null`s, no heredocs) and `__inputs` are not converted.
