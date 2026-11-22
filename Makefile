# Tenantry — single entry point for dev/test/deploy (IMPLEMENTATION-PLAN §10).
# Targets grow with the milestones: backend, web portal, cluster.
SHELL := /bin/bash
ROOT  := $(shell pwd)

BACKEND := $(ROOT)/backend
WEB     := $(ROOT)/web
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

.PHONY: seed
# The lab's endpoints (deploy/cluster/env.sh); override for another cluster.
SEED_FLAGS ?= --proxy-endpoint https://kube-a.tenantry.site --console-url https://console-a.tenantry.site --apps-domain mvp.tenantry.site
seed: ## Seed cluster-a, its pools and the onboarding policy; safe to re-run
	cd $(BACKEND) && go run ./cmd/tenantry-seed $(SEED_FLAGS)

.PHONY: run-api run-worker run-web web-install web-check
web-install: ## Install the portal's npm packages, exactly as locked
	cd $(WEB) && npm ci
run-web: ## Run the portal on http://localhost:5173 (proxies /api and /auth to make run-api)
	cd $(WEB) && npm run dev
web-check: ## Type check, lint, unit tests and production build of the portal
	cd $(WEB) && npm run typecheck && npm run lint && npm test && npx vite build
# Local logins go through the lab's Keycloak and land on the Vite dev server
# (IMPLEMENTATION-PLAN §10). The portal client secret and a session key stay
# outside the repo, in DEV_SECRETS:
#   TENANTRY_OIDC_CLIENT_SECRET=<secret of the portal client>
#   TENANTRY_SESSION_KEY=<openssl rand -base64 32>
DEV_SECRETS ?= $(HOME)/.config/tenantry/dev.env
TENANTRY_OIDC_ISSUER ?= https://auth.tenantry.site/realms/tenantry
TENANTRY_PUBLIC_URL ?= http://localhost:5173
run-api: ## Run the Platform API locally (secrets from DEV_SECRETS)
	@test -f $(DEV_SECRETS) || { echo "missing $(DEV_SECRETS), see the comment above run-api in the Makefile"; exit 1; }
	cd $(BACKEND) && set -a && . $(DEV_SECRETS) && set +a && \
		TENANTRY_OIDC_ISSUER=$(TENANTRY_OIDC_ISSUER) TENANTRY_PUBLIC_URL=$(TENANTRY_PUBLIC_URL) go run ./cmd/tenantry-api
run-worker: ## Run every worker loop locally (outbox events are only logged; no cluster yet)
	cd $(BACKEND) && go run ./cmd/tenantry-worker --loops=all

.PHONY: cluster-up cluster-verify
cluster-up: ## Build the cluster foundation on the lab (deploy/cluster); safe to re-run
	deploy/cluster/up.sh
cluster-verify: ## Check the cluster foundation
	deploy/cluster/verify.sh

.PHONY: deploy
deploy: ## Install or upgrade the Tenantry charts on the lab (values in deploy/cluster/values)
	deploy/cluster/deploy.sh

.PHONY: image chart-lint
# The chart's required values, with placeholders.
CHART_LINT_SET := --set database.existingSecret=lint --set api.auth.existingSecret=lint \
	--set api.auth.issuer=https://auth.example.com/realms/tenantry --set api.auth.publicURL=https://portal.example.com
IMAGE ?= tenantry/backend:dev
WEB_LINT_SET := --set ingress.host=portal.example.com --set ingress.clusterIssuer=lint
image: ## Build the backend and portal images (make deploy builds and loads them itself)
	docker build -f deploy/Dockerfile.backend -t $(IMAGE) $(BACKEND)
	docker build -f deploy/Dockerfile.web -t tenantry/web:dev $(WEB)
chart-lint: ## Lint and render the charts
	helm lint deploy/charts/tenantry-backend $(CHART_LINT_SET)
	helm template tenantry deploy/charts/tenantry-backend $(CHART_LINT_SET) >/dev/null
	helm template tenantry deploy/charts/tenantry-backend $(CHART_LINT_SET) --set seed.enabled=true \
		--set seed.proxyEndpoint=https://kube.example.com --set seed.consoleURL=https://console.example.com --set seed.appsDomain=apps.example.com >/dev/null
	helm lint deploy/charts/tenantry-web $(WEB_LINT_SET)
	helm template tenantry-web deploy/charts/tenantry-web $(WEB_LINT_SET) >/dev/null

.PHONY: generate build fmt fmt-check vet lint test
generate: ## Regenerate code from openapi/tenantry.v1.yaml, Go and TypeScript (commit the result)
	cd $(BACKEND) && go generate ./...
	cd $(WEB) && npm run --silent generate
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
