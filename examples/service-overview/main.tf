terraform {
  required_providers {
    terragraph = {
      source = "alexmchughdev/terragraph"
    }
  }
}

provider "terragraph" {}

locals {
  datasource = { type = "grafana-testdata-datasource", uid = "testdata" }

  services = {
    api      = "API"
    checkout = "Checkout"
    search   = "Search"
  }

  latency_thresholds = {
    mode = "absolute"
    steps = [
      { color = "green", value = null },
      { color = "orange", value = 250 },
      { color = "red", value = 500 },
    ]
  }
}

resource "terragraph_dashboard" "service_overview" {
  uid           = "service-overview"
  title         = "Service overview"
  description   = "Golden signals for customer-facing services."
  tags          = ["services", "slo"]
  refresh       = "30s"
  graph_tooltip = 1
  time          = { from = "now-6h", to = "now" }

  variable {
    name    = "env"
    label   = "Environment"
    type    = "custom"
    query   = "production,staging"
    current = { text = "production", value = "production" }
  }

  link {
    title        = "Runbooks"
    type         = "link"
    url          = "https://example.com/runbooks"
    icon         = "doc"
    target_blank = true
  }

  panel {
    type       = "stat"
    title      = "Availability"
    datasource = local.datasource
    grid_pos   = { w = 6, h = 5 }
    targets    = [{ refId = "A", scenarioId = "random_walk", min = 99, max = 100, startValue = 99.9 }]
    options = {
      colorMode     = "background"
      graphMode     = "area"
      reduceOptions = { calcs = ["lastNotNull"], fields = "", values = false }
    }
    field_config = {
      defaults = {
        unit     = "percent"
        decimals = 2
        thresholds = {
          mode  = "absolute"
          steps = [{ color = "red", value = null }, { color = "green", value = 99.5 }]
        }
      }
      overrides = []
    }
  }

  panel {
    type       = "gauge"
    title      = "p95 latency"
    datasource = local.datasource
    grid_pos   = { w = 6, h = 5 }
    targets    = [{ refId = "A", scenarioId = "random_walk", min = 100, max = 600 }]
    field_config = {
      defaults  = { unit = "ms", min = 0, max = 800, thresholds = local.latency_thresholds }
      overrides = []
    }
  }

  panel {
    type       = "bargauge"
    title      = "Error budget remaining"
    datasource = local.datasource
    grid_pos   = { w = 12, h = 5 }
    targets = [for i, name in values(local.services) : {
      refId      = "S${i}"
      scenarioId = "random_walk"
      min        = 0
      max        = 100
      alias      = name
    }]
    options = { displayMode = "gradient", orientation = "horizontal" }
    field_config = {
      defaults  = { unit = "percent", min = 0, max = 100 }
      overrides = []
    }
  }

  row {
    title = "Per service"

    dynamic "panel" {
      for_each = local.services
      content {
        type       = "timeseries"
        title      = "${panel.value} requests ($env)"
        datasource = local.datasource
        grid_pos   = { w = 8, h = 8 }
        targets = [
          { refId = "A", scenarioId = "random_walk", alias = "2xx" },
          { refId = "B", scenarioId = "random_walk", alias = "5xx", min = 0, max = 5 },
        ]
        options = {
          legend  = { displayMode = "list", placement = "bottom", showLegend = true }
          tooltip = { mode = "multi", sort = "desc" }
        }
        field_config = {
          defaults  = { unit = "reqps", custom = { fillOpacity = 15, lineWidth = 2 } }
          overrides = []
        }
      }
    }
  }

  row {
    title     = "Details"
    collapsed = true

    panel {
      type       = "table"
      title      = "Recent deployments"
      datasource = local.datasource
      grid_pos   = { w = 16, h = 8 }
      targets = [{
        refId      = "A"
        scenarioId = "csv_content"
        csvContent = <<-CSV
          service,version,deployed_by,status
          api,1.42.0,ci,success
          checkout,3.1.7,ci,success
          search,0.9.3,alex,rolled back
        CSV
      }]
    }

    panel {
      type     = "text"
      title    = "Notes"
      grid_pos = { w = 8, h = 8 }
      options = {
        mode    = "markdown"
        content = <<-MD
          ### On call
          Page **#oncall-services** for anything red.
          Latency targets: p95 < 250ms.
        MD
      }
    }
  }
}

output "url" {
  value = terragraph_dashboard.service_overview.url
}
