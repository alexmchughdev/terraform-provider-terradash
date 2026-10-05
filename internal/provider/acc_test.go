package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/alexmchughdev/terraform-provider-terragraph/internal/grafana"
)

var protoV6Factories = map[string]func() (tfprotov6.ProviderServer, error){
	"terragraph": func() (tfprotov6.ProviderServer, error) { return New("test"), nil },
}

func accClient(t *testing.T) *grafana.Client {
	t.Helper()
	client, err := grafana.New(grafana.Config{URL: os.Getenv("GRAFANA_URL"), Auth: os.Getenv("GRAFANA_AUTH")})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func accPreCheck(t *testing.T) {
	if os.Getenv("GRAFANA_URL") == "" || os.Getenv("GRAFANA_AUTH") == "" {
		t.Fatal("GRAFANA_URL and GRAFANA_AUTH must be set for acceptance tests")
	}
}

func checkDestroyed(t *testing.T, uid string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		_, err := accClient(t).GetDashboard(context.Background(), uid)
		if errors.Is(err, grafana.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("dashboard %s still exists (err: %w)", uid, err)
	}
}

func lifecycleConfig(title, folder string) string {
	folderAttr := ""
	if folder != "" {
		folderAttr = fmt.Sprintf("folder_uid = %q", folder)
	}
	return fmt.Sprintf(`
resource "terragraph_dashboard" "test" {
  uid         = "acc-lifecycle"
  title       = %q
  description = "managed by terraform"
  tags        = ["acc", "terragraph"]
  timezone    = "utc"
  refresh     = "30s"
  editable    = true
  schema_version = 41
  %s

  time = {
    from = "now-6h"
    to   = "now"
  }

  variable {
    name  = "env"
    type  = "custom"
    label = "Environment"
    query = "prod,staging"
  }

  annotation {
    name       = "Deploys"
    enable     = true
    icon_color = "red"
  }

  link {
    title = "Docs"
    type  = "link"
    url   = "https://grafana.com/docs"
  }

  panel {
    id       = 1
    type     = "timeseries"
    title    = "Requests"
    grid_pos = { x = 0, y = 0, w = 12, h = 8 }
    targets = [{
      refId = "A"
      expr  = "sum(rate(http_requests_total[5m]))"
    }]
    options = {
      legend = { showLegend = true, placement = "bottom" }
    }
    field_config = {
      defaults = { unit = "reqps" }
    }
  }

  panel {
    id       = 2
    type     = "text"
    title    = "Notes"
    grid_pos = { x = 12, y = 0, w = 12, h = 8 }
    options = { mode = "markdown", content = "# Hello <world>" }
  }

  row {
    id       = 3
    title    = "Open row"
    grid_pos = { x = 0, y = 8, w = 24, h = 1 }
    panel {
      id       = 4
      type     = "stat"
      title    = "Errors"
      grid_pos = { x = 0, y = 9, w = 12, h = 8 }
    }
  }

  row {
    id        = 5
    title     = "Collapsed row"
    collapsed = true
    grid_pos  = { x = 0, y = 17, w = 24, h = 1 }
    panel {
      id       = 6
      type     = "gauge"
      title    = "Saturation"
      grid_pos = { x = 0, y = 18, w = 12, h = 8 }
      options = {
        reduceOptions = { calcs = ["lastNotNull"] }
      }
    }
    panel {
      id       = 7
      type     = "stat"
      title    = "Nested"
      grid_pos = { x = 12, y = 18, w = 12, h = 8 }
    }
  }
}
`, title, folderAttr)
}

func TestAccDashboard_lifecycle(t *testing.T) {
	const name = "terragraph_dashboard.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             checkDestroyed(t, "acc-lifecycle"),
		Steps: []resource.TestStep{
			{
				Config: lifecycleConfig("Acc Lifecycle", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "uid", "acc-lifecycle"),
					resource.TestCheckResourceAttr(name, "title", "Acc Lifecycle"),
					resource.TestCheckResourceAttr(name, "tags.#", "2"),
					resource.TestCheckResourceAttr(name, "variable.#", "1"),
					resource.TestCheckResourceAttr(name, "variable.0.name", "env"),
					resource.TestCheckResourceAttr(name, "annotation.0.name", "Deploys"),
					resource.TestCheckResourceAttr(name, "link.0.url", "https://grafana.com/docs"),
					resource.TestCheckResourceAttr(name, "panel.#", "2"),
					resource.TestCheckResourceAttr(name, "panel.0.options.legend.placement", "bottom"),
					resource.TestCheckResourceAttr(name, "panel.1.options.content", "# Hello <world>"),
					resource.TestCheckResourceAttr(name, "row.#", "2"),
					resource.TestCheckResourceAttr(name, "row.1.collapsed", "true"),
					resource.TestCheckResourceAttr(name, "row.1.panel.#", "2"),
					resource.TestCheckResourceAttr(name, "version", "1"),
					resource.TestCheckResourceAttrSet(name, "url"),
				),
			},
			{
				Config: lifecycleConfig("Acc Lifecycle Renamed", "team-a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "title", "Acc Lifecycle Renamed"),
					resource.TestCheckResourceAttr(name, "folder_uid", "team-a"),
					resource.TestCheckResourceAttr(name, "version", "2"),
				),
			},
			{
				ResourceName:                         name,
				ImportState:                          true,
				ImportStateId:                        "acc-lifecycle",
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "uid",
				ImportStateVerifyIgnore:              []string{"overwrite", "message"},
			},
			{
				Config:   lifecycleConfig("Acc Lifecycle Renamed", "team-a"),
				PlanOnly: true,
			},
		},
	})
}

func driftConfig(title string) string {
	return fmt.Sprintf(`
resource "terragraph_dashboard" "test" {
  uid   = "acc-drift"
  title = %q
  panel {
    type  = "stat"
    title = "Original"
  }
}
`, title)
}

// modifyRemote rewrites a stored dashboard through the Grafana API.
func modifyRemote(t *testing.T, uid string, edit func(map[string]any)) {
	t.Helper()
	ctx := context.Background()
	client := accClient(t)
	remote, err := client.GetDashboard(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	edit(remote.Model)
	_, err = client.SaveDashboard(ctx, grafana.SaveRequest{Dashboard: remote.Model, FolderUID: remote.Meta.FolderUID, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAccDashboard_drift(t *testing.T) {
	const name = "terragraph_dashboard.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             checkDestroyed(t, "acc-drift"),
		Steps: []resource.TestStep{
			{Config: driftConfig("Acc Drift")},
			{
				PreConfig: func() {
					modifyRemote(t, "acc-drift", func(model map[string]any) {
						model["panels"].([]any)[0].(map[string]any)["title"] = "Changed in the UI"
					})
				},
				Config:             driftConfig("Acc Drift"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			{
				Config: driftConfig("Acc Drift"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr(name, "panel.0.title", "Original"),
			},
			{Config: driftConfig("Acc Drift"), PlanOnly: true},
		},
	})
}

func TestAccDashboard_deletedOutside(t *testing.T) {
	const name = "terragraph_dashboard.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { accPreCheck(t) },
		ProtoV6ProviderFactories: protoV6Factories,
		CheckDestroy:             checkDestroyed(t, "acc-deleted"),
		Steps: []resource.TestStep{
			{Config: deletedConfig},
			{
				PreConfig: func() {
					if err := accClient(t).DeleteDashboard(context.Background(), "acc-deleted"); err != nil {
						t.Fatal(err)
					}
				},
				Config: deletedConfig,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionCreate)},
				},
				Check: resource.TestCheckResourceAttr(name, "version", "1"),
			},
		},
	})
}

const deletedConfig = `
resource "terragraph_dashboard" "test" {
  uid   = "acc-deleted"
  title = "Acc Deleted"
}
`

func TestAccDashboardJSONDataSource(t *testing.T) {
	const name = "data.terragraph_dashboard_json.test"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: protoV6Factories,
		Steps: []resource.TestStep{{
			Config: `
provider "terragraph" {
  url = "http://127.0.0.1:1"
}

data "terragraph_dashboard_json" "test" {
  title = "Offline"
  panel {
    type    = "text"
    title   = "a < b"
    options = { content = "x" }
  }
}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestMatchResourceAttr(name, "json", regexpTitle),
				resource.TestMatchResourceAttr(name, "json", regexpPanelTitle),
				resource.TestMatchResourceAttr(name, "json", regexpSchemaVersion),
			),
		}},
	})
}

var (
	regexpTitle         = regexp.MustCompile(`"title": "Offline"`)
	regexpPanelTitle    = regexp.MustCompile(`"title": "a < b"`)
	regexpSchemaVersion = regexp.MustCompile(`"schemaVersion": 41`)
)
