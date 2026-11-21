# Tenantry — single entry point for dev/test/deploy (IMPLEMENTATION-PLAN §10).
# Targets grow with the milestones; today it covers the backend only.
SHELL := /bin/bash
ROOT  := $(shell pwd)

BACKEND := $(ROOT)/backend
COMPOSE := $(ROOT)/deploy/compose/docker-compose.dev.yml

DATABASE_URL ?= postgres://tenantry:tenantry@localhost:5432/tenantry?sslmode=disable
export DATABASE_URL
# Tests create and drop their own database on this server; they never touch DATABASE_URL's data.
TEST_DATABASE_URL ?= $(DATABASE_URL)
export TEST_DATABASE_URL
# Logs are JSON by default (for the cluster); text reads better in a terminal.
TENANTRY_LOG_FORMAT ?= text
export TENANTRY_LOG_FORMAT

.PHONY: help
help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

.PHONY: dev-up dev-down
dev-up: ## Start local PostgreSQL
	docker compose -f $(COMPOSE) up -d
dev-down: ## Stop local PostgreSQL and delete its volume
	docker compose -f $(COMPOSE) down -v

.PHONY: migrate-up migrate-down migrate-status
migrate-up: ## Apply every pending migration
	cd $(BACKEND) && go run ./cmd/tenantry-migrate up
migrate-down: ## Roll back the last migration
	cd $(BACKEND) && go run ./cmd/tenantry-migrate down
migrate-status: ## Show which migrations are applied
	cd $(BACKEND) && go run ./cmd/tenantry-migrate status

.PHONY: run-api run-worker
run-api: ## Run the Platform API locally
	cd $(BACKEND) && go run ./cmd/tenantry-api
run-worker: ## Run the outbox worker locally (logs events; no cluster yet)
	cd $(BACKEND) && go run ./cmd/tenantry-worker

.PHONY: cluster-up cluster-verify
cluster-up: ## Build the cluster foundation on the lab (deploy/cluster); safe to re-run
	deploy/cluster/up.sh
cluster-verify: ## Check the cluster foundation
	deploy/cluster/verify.sh

.PHONY: deploy
deploy: ## Install or upgrade the Tenantry charts on the lab (values in deploy/cluster/values)
	deploy/cluster/deploy.sh

.PHONY: image chart-lint
IMAGE ?= tenantry/backend:dev
image: ## Build the backend image (every binary, one image)
	docker build -f deploy/Dockerfile.backend -t $(IMAGE) $(BACKEND)
chart-lint: ## Lint and render the tenantry-backend chart
	helm lint deploy/charts/tenantry-backend --set database.existingSecret=lint
	helm template tenantry deploy/charts/tenantry-backend --set database.existingSecret=lint >/dev/null

.PHONY: generate build fmt fmt-check vet lint test
generate: ## Regenerate code from openapi/tenantry.v1.yaml (commit the result)
	cd $(BACKEND) && go generate ./...
build: ## Build every backend binary into bin/
	cd $(BACKEND) && go build -o $(ROOT)/bin/ ./cmd/...
fmt: ## gofmt
	cd $(BACKEND) && gofmt -l -w .
vet: ## go vet
	cd $(BACKEND) && go vet ./...
fmt-check: ## Fail if any Go file is not gofmt-formatted
	@cd $(BACKEND) && files=$$(gofmt -l .) && if [ -n "$$files" ]; then echo "not gofmt-formatted (run make fmt):"; echo "$$files"; exit 1; fi
lint: fmt-check vet ## gofmt check, vet, then golangci-lint if installed
	@if command -v golangci-lint >/dev/null; then cd $(BACKEND) && golangci-lint run ./...; else echo "golangci-lint not installed; ran go vet only"; fi
test: ## Tests (migration tests need PostgreSQL: make dev-up)
	cd $(BACKEND) && go test -race -count=1 ./...
