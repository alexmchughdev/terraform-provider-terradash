---
page_title: "Migrating from the grafana provider"
subcategory: "Guides"
description: |-
  Move grafana_dashboard resources (JSON) from the grafana/grafana provider to terragraph_dashboard without recreating dashboards.
---

# Migrating from the grafana provider

- Converts `grafana_dashboard` (`config_json`) to `terragraph_dashboard`.
- Generates `removed` blocks (old resources leave state, dashboards are not destroyed) and `import` blocks (dashboards enter state).
- Needs Terraform/OpenTofu >= 1.7 for `removed` blocks. Older: `terraform state rm grafana_dashboard.x` per resource.
- Build the CLI with `make build` (`bin/terragraph`).

## Steps

1. Add terragraph next to the existing provider. `grafana_folder` etc. stay on the grafana provider.

   ```terraform
   terraform {
     required_providers {
       grafana = {
         source = "grafana/grafana"
       }
       terragraph = {
         source = "alexmchughdev/terragraph"
       }
     }
   }

   provider "grafana" {}

   provider "terragraph" {} # shares GRAFANA_URL / GRAFANA_AUTH
   ```

2. Migrate:

   ```shell
   terragraph migrate -o terragraph.tf -remove .
   ```

3. `terraform plan`. Expect `N to import, 0 to add, 0 to change, 0 to destroy` and `will no longer be managed ... will not be destroyed` for each old resource.
4. `terraform apply`.
5. Delete the old JSON files and the `removed` / `import` blocks.

## Before / after

```terraform
resource "grafana_dashboard" "simple" {
  folder      = grafana_folder.team.uid
  config_json = file("${path.module}/dashboards/simple.json")
}
```

```terraform
removed {
  from = grafana_dashboard.simple

  lifecycle {
    destroy = false
  }
}

import {
  to = terragraph_dashboard.simple
  id = "simple-demo"
}

resource "terragraph_dashboard" "simple" {
  uid        = "simple-demo"
  folder_uid = grafana_folder.team.uid
  title      = "Simple service overview"
  # ... remaining attributes and panel blocks
}
```

## Coverage

| Migrated | Notes |
| --- | --- |
| `config_json` | `file(...)`, `templatefile(...)`, `jsonencode({...})`, literal strings; evaluated statically. |
| `for_each = fileset(...)` | One resource per instance, named `<name>_<file stem>`; one `removed` block per source resource. |
| `folder` | Copied verbatim to `folder_uid`. |
| `overwrite`, `message` | Copied verbatim. |
| `__inputs` | Become Terraform `variable`s. |

| Not migrated | Behavior |
| --- | --- |
| `count`, `org_id`, `provider`, `depends_on` | Warning; review the generated resource. |
| `config_json` from variables or data sources | Warning; block skipped and left in place. |
| `fileset` patterns with `**` | Warning; block skipped and left in place. |
| `folder` using `each.*` | Warning; set `folder_uid` by hand. |
| `folder` ending in `.id` | Warning; `folder_uid` needs the folder UID. |
| Dashboard JSON without `uid` | Warning; add the `import` block by hand. |

Warnings go to stderr.

## `terragraph migrate`

```shell
terragraph migrate [flags] DIR
```

Reads `*.tf` in `DIR`.

| Flag | Description |
| --- | --- |
| `-o PATH` | Output; `.tf` path is one file, otherwise a directory. Default stdout. |
| `-remove` | Delete migrated `grafana_dashboard` blocks from `DIR`. |
| `-force` | Overwrite existing files. |

- Without `-remove`, delete the old blocks by hand; both resources otherwise manage the same dashboard.
- Skipped blocks are never removed.
