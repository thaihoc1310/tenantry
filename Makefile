# Tenantry — single entry point for dev/test/deploy (IMPLEMENTATION-PLAN §10).
# Targets grow with the milestones; today it covers the backend only.
SHELL := /bin/bash
ROOT  := $(shell pwd)

BACKEND := $(ROOT)/backend
COMPOSE := $(ROOT)/deploy/compose/docker-compose.dev.yml

DATABASE_URL ?= postgres://tenantry:tenantry@localhost:5432/tenantry?sslmode=disable
export DATABASE_URL

.PHONY: help
help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-12s %s\n", $$1, $$2}'

.PHONY: dev-up dev-down
dev-up: ## Start local PostgreSQL
	docker compose -f $(COMPOSE) up -d
dev-down: ## Stop local PostgreSQL and delete its volume
	docker compose -f $(COMPOSE) down -v

.PHONY: run-api
run-api: ## Run the Platform API locally
	cd $(BACKEND) && go run ./cmd/tenantry-api

.PHONY: build fmt vet lint test
build: ## Build every backend binary into bin/
	cd $(BACKEND) && go build -o $(ROOT)/bin/ ./cmd/...
fmt: ## gofmt
	cd $(BACKEND) && gofmt -l -w .
vet: ## go vet
	cd $(BACKEND) && go vet ./...
lint: vet ## golangci-lint if installed, else just vet
	@if command -v golangci-lint >/dev/null; then cd $(BACKEND) && golangci-lint run ./...; else echo "golangci-lint not installed; ran go vet only"; fi
test: ## Unit tests
	cd $(BACKEND) && go test -race -count=1 ./...
