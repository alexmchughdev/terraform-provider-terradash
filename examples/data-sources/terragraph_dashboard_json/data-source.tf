data "terragraph_dashboard_json" "preview" {
  title = "Rendered without deploying"

  panel {
    type    = "text"
    options = { mode = "markdown", content = "Hello from HCL" }
  }
}

output "dashboard_json" {
  value = data.terragraph_dashboard_json.preview.json
}
