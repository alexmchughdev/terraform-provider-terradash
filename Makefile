VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOOS    ?= $(shell go env GOOS)
GOARCH  ?= $(shell go env GOARCH)
LDFLAGS := -s -w -X main.version=$(VERSION)
PLUGIN_DIR ?= $(HOME)/.terraform.d/plugins/registry.terraform.io/alexmchughdev/terradash/$(VERSION)/$(GOOS)_$(GOARCH)

.DEFAULT_GOAL := help

.PHONY: help build install test testacc e2e lint fmt docs cover clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-10s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build provider and CLI into ./bin
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/terraform-provider-terradash .
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/terradash ./cmd/terradash

install: ## Install provider into the local Terraform plugin directory
	mkdir -p $(PLUGIN_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(PLUGIN_DIR)/terraform-provider-terradash_v$(VERSION) .

test: ## Run unit tests with the race detector
	go test -race ./...

testacc: ## Run acceptance tests (needs Grafana; see GRAFANA_URL, GRAFANA_AUTH)
	TF_ACC=1 go test ./internal/provider/ -run TestAcc -v -timeout 30m

e2e: build ## Run end-to-end script
	./scripts/e2e.sh

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format code and tidy modules
	gofmt -s -w .
	go mod tidy

docs: ## Regenerate registry docs from templates/ and examples/
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name terradash

cover: ## Generate coverage report
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -n 1

clean: ## Remove build artifacts
	rm -rf bin dist coverage.out
