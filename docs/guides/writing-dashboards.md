---
page_title: "Writing dashboards in HCL"
subcategory: "Guides"
description: |-
  Patterns for writing maintainable Grafana dashboards with terradash.
---

# Writing dashboards in HCL

## `locals`, `for` and `dynamic`

```terraform
locals {
  prometheus = { type = "prometheus", uid = "prometheus" }
  services   = { api = "API", checkout = "Checkout" }
}

resource "terradash_dashboard" "per_service" {
  title = "Per service"

  row {
    title = "Requests"

    dynamic "panel" {
      for_each = local.services
      content {
        type       = "timeseries"
        title      = "${panel.value} requests"
        datasource = local.prometheus
        targets    = [{ refId = "A", expr = "sum(rate(http_requests_total{service=\"${panel.key}\"}[5m]))" }]
      }
    }
  }
}
```

- `dynamic "panel"` works at top level and inside `row`.
- Prefer `for_each` over `count`; order follows key order.
- Panels without `grid_pos` are auto-laid out ([notes](../resources/dashboard#notes)).

## Heredocs

```terraform
resource "terradash_dashboard" "notes" {
  title = "Notes"

  panel {
    type  = "table"
    title = "Orders"
    targets = [{
      refId  = "A"
      format = "table"
      rawSql = chomp(<<-SQL
        SELECT id, total
        FROM orders
        WHERE created_at > $__timeFrom()
      SQL
      )
    }]
  }
}
```

- Heredocs end with a newline; `chomp(...)` removes it.

## Escaping `$` and `%`

| Grafana sees | Write |
| --- | --- |
| `$instance`, `$__rate_interval` | `"$instance"`, `"$__rate_interval"` |
| `${instance}` | `"$${instance}"` |
| `${instance:csv}` | `"$${instance:csv}"` |
| `%{` | `"%%{"` |

- Same rules in heredocs.
- The converter already escapes; `${DS_*}` from `__inputs` become `var.*`.

## Preview JSON

```terraform
data "terradash_dashboard_json" "preview" {
  title = "Preview"

  panel {
    type    = "text"
    options = { mode = "markdown", content = "Hello" }
  }
}

output "json" {
  value = data.terradash_dashboard_json.preview.json
}
```

`terraform apply -target=data.terradash_dashboard_json.preview && terraform output -raw json`, or `terraform console`.

## Unmodelled keys

```terraform
panel {
  type  = "text"
  extra = { someFutureKey = true }
}
```

`extra` merges verbatim into the JSON.
