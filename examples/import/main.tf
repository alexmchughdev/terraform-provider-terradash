# Generate HCL for existing dashboards with either:
#   terraform plan -generate-config-out=dashboards.tf
#   terragraph pull -o . <uid>...
import {
  to = terragraph_dashboard.home
  id = "home-dashboard-uid"
}
