#!/usr/bin/env bash
# End-to-end test against a real Grafana using real terraform/tofu binaries.
#   GRAFANA_URL=http://localhost:3000 GRAFANA_AUTH=admin:admin ./scripts/e2e.sh [terraform|tofu ...]
set -euo pipefail

: "${GRAFANA_URL:?set GRAFANA_URL}"
: "${GRAFANA_AUTH:?set GRAFANA_AUTH}"
export GRAFANA_URL GRAFANA_AUTH

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

binaries=("$@")
if [ ${#binaries[@]} -eq 0 ]; then
  for b in terraform tofu; do command -v "$b" >/dev/null && binaries+=("$b"); done
fi
[ ${#binaries[@]} -gt 0 ] || { echo "no terraform or tofu binary found" >&2; exit 1; }

step() { printf '\n==> %s\n' "$*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

grafana() {
  local method=$1 path=$2; shift 2
  if [[ $GRAFANA_AUTH == *:* ]]; then
    curl -fsS -u "$GRAFANA_AUTH" -X "$method" -H 'Content-Type: application/json' "$GRAFANA_URL$path" "$@"
  else
    curl -fsS -H "Authorization: Bearer $GRAFANA_AUTH" -X "$method" -H 'Content-Type: application/json' "$GRAFANA_URL$path" "$@"
  fi
}

expect_no_changes() {
  local code=0
  "$tf" plan -detailed-exitcode -input=false -no-color >"$work/plan.log" 2>&1 || code=$?
  [ "$code" -eq 0 ] || { cat "$work/plan.log"; fail "$1: expected no changes (exit $code)"; }
}

expect_changes() {
  local code=0
  "$tf" plan -detailed-exitcode -input=false -no-color >"$work/plan.log" 2>&1 || code=$?
  [ "$code" -eq 2 ] || { cat "$work/plan.log"; fail "$1: expected changes (exit $code)"; }
}

write_tfvars() {
  grep -ho '^variable "[a-z0-9_]*"' ./*.tf 2>/dev/null | cut -d'"' -f2 | sed 's/$/ = "testdata"/' >terraform.tfvars || true
}

write_provider() {
  cat >provider.tf <<'EOF'
terraform {
  required_providers {
    terragraph = {
      source = "alexmchughdev/terragraph"
    }
  }
}

provider "terragraph" {}
EOF
}

write_legacy() {
  cat >main.tf <<'EOF'
terraform {
  required_providers {
    grafana = {
      source = "grafana/grafana"
    }
  }
}

provider "grafana" {}

resource "grafana_folder" "e2e" {
  title = "terragraph e2e migrate"
}

resource "grafana_dashboard" "simple" {
  folder      = grafana_folder.e2e.uid
  config_json = file("${path.module}/dashboards/simple.json")
}

resource "grafana_dashboard" "inline" {
  config_json = jsonencode({ uid = "e2e-inline", title = "e2e inline", schemaVersion = 41 })
}
EOF
}

step "build"
mkdir -p "$work/bin"
(cd "$root" && go build -o "$work/bin/terraform-provider-terragraph" . && go build -o "$work/bin/terragraph" ./cmd/terragraph)
cat >"$work/tfrc" <<EOF
provider_installation {
  dev_overrides {
    "registry.terraform.io/alexmchughdev/terragraph" = "$work/bin"
    "registry.opentofu.org/alexmchughdev/terragraph" = "$work/bin"
  }
  direct {}
}
EOF
export TF_CLI_CONFIG_FILE="$work/tfrc" TF_IN_AUTOMATION=1
terragraph="$work/bin/terragraph"

for bin in "${binaries[@]}"; do
  tf=$(command -v "$bin")
  echo
  echo "################ $("$tf" version | head -1)"

  step "example: fmt, validate, apply, no-op plan"
  dir="$work/$bin-example"
  cp -r "$root/examples/service-overview" "$dir"
  cd "$dir"
  "$tf" fmt -check -recursive
  "$tf" validate -no-color >/dev/null
  "$tf" apply -auto-approve -input=false -no-color >/dev/null
  expect_no_changes "example"

  step "drift: edit through the API is detected and reverted"
  uid=service-overview
  grafana GET "/api/dashboards/uid/$uid" |
    jq '{dashboard: (.dashboard | .panels[0].title = "edited outside terraform"), folderUid: .meta.folderUid, overwrite: true}' |
    grafana POST /api/dashboards/db -d @- >/dev/null
  expect_changes "drift"
  grep -q 'edited outside terraform' "$work/plan.log" || fail "drift diff does not show the edited title"
  "$tf" apply -auto-approve -input=false -no-color >/dev/null
  expect_no_changes "after drift"

  step "deleted outside terraform is recreated"
  grafana DELETE "/api/dashboards/uid/$uid" >/dev/null
  expect_changes "deleted"
  "$tf" apply -auto-approve -input=false -no-color >/dev/null
  expect_no_changes "after recreate"

  step "convert JSON fixtures, fmt, validate, apply, no-op plan"
  dir="$work/$bin-converted"
  mkdir -p "$dir"
  cd "$dir"
  "$terragraph" convert -o . "$root/internal/dashboard/testdata"
  "$tf" fmt -check
  write_provider
  write_tfvars
  "$tf" validate -no-color >/dev/null
  "$tf" apply -auto-approve -input=false -no-color >/dev/null
  expect_no_changes "converted"
  mapfile -t uids < <("$tf" show -json | jq -r '.values.root_module.resources[] | select(.type == "terragraph_dashboard") | .values.uid')

  step "import with generated config"
  dir="$work/$bin-imported"
  mkdir -p "$dir"
  cd "$dir"
  write_provider
  for i in "${!uids[@]}"; do
    printf 'import {\n  to = terragraph_dashboard.d%d\n  id = "%s"\n}\n\n' "$i" "${uids[$i]}" >>imports.tf
  done
  "$tf" plan -generate-config-out=generated.tf -input=false -no-color >"$work/plan.log" 2>&1 || { cat "$work/plan.log"; fail "generate config"; }
  grep -q "${#uids[@]} to import, 0 to add, 0 to change, 0 to destroy" "$work/plan.log" || { cat "$work/plan.log"; fail "import plan"; }
  "$tf" fmt -check generated.tf
  "$tf" apply -auto-approve -input=false -no-color >/dev/null
  expect_no_changes "imported"
  rm -f terraform.tfstate*

  step "pull with import blocks"
  dir="$work/$bin-pulled"
  mkdir -p "$dir"
  cd "$dir"
  write_provider
  "$terragraph" pull -o . "${uids[@]}"
  "$tf" fmt -check
  write_tfvars
  "$tf" plan -input=false -no-color >"$work/plan.log" 2>&1 || { cat "$work/plan.log"; fail "pull plan"; }
  grep -q "${#uids[@]} to import, 0 to add, 0 to change, 0 to destroy" "$work/plan.log" || { cat "$work/plan.log"; fail "pull import plan"; }

  step "destroy"
  cd "$work/$bin-converted" && "$tf" destroy -auto-approve -input=false -no-color >/dev/null
  cd "$work/$bin-example" && "$tf" destroy -auto-approve -input=false -no-color >/dev/null
  for uid in service-overview "${uids[@]}"; do
    if grafana GET "/api/dashboards/uid/$uid" >/dev/null 2>&1; then fail "$uid still exists after destroy"; fi
  done

  step "migrate from the grafana provider without recreating dashboards"
  dir="$work/$bin-migrate"
  mkdir -p "$dir/dashboards"
  cd "$dir"
  cp "$root/internal/dashboard/testdata/simple.json" dashboards/simple.json
  migrate_uid=$(jq -r .uid dashboards/simple.json)
  write_legacy
  "$tf" init -input=false -no-color >/dev/null
  "$tf" apply -auto-approve -input=false -no-color >/dev/null
  version=$(grafana GET "/api/dashboards/uid/$migrate_uid" | jq .meta.version)
  "$terragraph" migrate -o terragraph.tf -remove .
  perl -0pi -e 's|(source = "grafana/grafana"\n    \})|$1\n    terragraph = {\n      source = "alexmchughdev/terragraph"\n    }|' main.tf
  printf '\nprovider "terragraph" {}\n' >>main.tf
  "$tf" fmt -check
  if grep -q grafana_dashboard main.tf; then fail "migrate -remove left grafana_dashboard blocks"; fi
  "$tf" plan -input=false -no-color >"$work/plan.log" 2>&1 || { cat "$work/plan.log"; fail "migrate plan"; }
  grep -q "2 to import, 0 to add, 0 to change, 0 to destroy" "$work/plan.log" || { cat "$work/plan.log"; fail "migrate plan summary"; }
  "$tf" apply -auto-approve -input=false -no-color >/dev/null
  expect_no_changes "migrated"
  [ "$(grafana GET "/api/dashboards/uid/$migrate_uid" | jq .meta.version)" = "$version" ] || fail "migration saved the dashboard"
  "$tf" destroy -auto-approve -input=false -no-color >/dev/null
done

echo
echo "e2e passed for: ${binaries[*]}"
