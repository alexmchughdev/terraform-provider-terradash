resource "terragraph_dashboard" "api" {
  uid        = "api-overview"
  folder_uid = "platform"
  title      = "API overview"
  tags       = ["api"]
  time       = { from = "now-6h", to = "now" }

  variable {
    name        = "instance"
    type        = "query"
    datasource  = { type = "prometheus", uid = "prometheus" }
    query       = "label_values(up{job=\"api\"}, instance)"
    refresh     = 1
    multi       = true
    include_all = true
  }

  panel {
    type       = "timeseries"
    title      = "Requests"
    datasource = { type = "prometheus", uid = "prometheus" }
    targets = [{
      refId        = "A"
      expr         = "sum by (code) (rate(http_requests_total{instance=~\"$instance\"}[$__rate_interval]))"
      legendFormat = "{{code}}"
    }]
    field_config = {
      defaults  = { unit = "reqps" }
      overrides = []
    }
  }

  panel {
    type       = "stat"
    title      = "Error ratio"
    datasource = { type = "prometheus", uid = "prometheus" }
    targets = [{
      refId = "A"
      expr  = "sum(rate(http_requests_total{code=~\"5..\"}[5m])) / sum(rate(http_requests_total[5m]))"
    }]
    field_config = {
      defaults  = { unit = "percentunit" }
      overrides = []
    }
  }
}
