provider "terragraph" {
  url  = "https://grafana.example.com"
  auth = var.grafana_token
}

variable "grafana_token" {
  type      = string
  sensitive = true
}
