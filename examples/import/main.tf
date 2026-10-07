# Generate HCL for existing dashboards with either:
#   terraform plan -generate-config-out=dashboards.tf
#   terradash pull -o . <uid>...
import {
  to = terradash_dashboard.home
  id = "home-dashboard-uid"
}
