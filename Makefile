# Cube Planner — local development tasks.
#
# Infra (Postgres + Mailpit) runs in Docker via the root compose.yml.
# The Go backend and the Vite frontend run on the host for fast iteration.

COMPOSE := docker compose

# Values from .env (gitignored) override these defaults; the defaults match
# compose.yml so a fresh checkout works with no .env at all.
-include .env
DATABASE_URL ?= postgres://cube:cube@localhost:5432/cube?sslmode=disable
SMTP_HOST ?= localhost
SMTP_PORT ?= 1025
export PORT ENV DATABASE_URL BASE_URL SMTP_HOST SMTP_PORT SMTP_USER SMTP_PASS SMTP_FROM DISCORD_CLIENT_ID DISCORD_CLIENT_SECRET GOOGLE_CLIENT_ID GOOGLE_CLIENT_SECRET CARDS_SYNC_ENABLED SCRYFALL_BASE_URL

GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: up
up: ## Start infra (Docker) + backend + frontend dev servers
	$(COMPOSE) up -d --wait
	$(MAKE) -j2 backend-dev frontend-dev

.PHONY: down
down: ## Stop the Docker services
	$(COMPOSE) down

.PHONY: ps
ps: ## Show compose service status
	$(COMPOSE) ps

.PHONY: backend-dev
backend-dev: ## Run the Go backend on the host (env from .env, defaults match compose.yml)
	cd backend && go run ./cmd/server

.PHONY: frontend-dev
frontend-dev: ## Run the Vite dev server with HMR
	pnpm --filter @cube-planner/frontend dev

.PHONY: db-psql
db-psql: ## Open a psql shell in the Postgres container
	$(COMPOSE) exec postgres psql -U cube -d cube

.PHONY: db-logs
db-logs: ## Tail Postgres logs
	$(COMPOSE) logs -f postgres

.PHONY: db-reset
db-reset: ## DESTROY the database volume and recreate it (backend re-migrates on next boot)
	$(COMPOSE) rm -sf postgres
	-docker volume rm cube-planner_pgdata
	$(COMPOSE) up -d --wait postgres

.PHONY: backend-test
backend-test: ## Backend tests (unit + testcontainers integration; needs Docker)
	cd backend && go test ./...

.PHONY: backend-lint
backend-lint: ## Run golangci-lint on the backend
	cd backend && golangci-lint run

.PHONY: backend-image
backend-image: ## Build the backend Docker image with version metadata
	docker build \
		--build-arg VERSION=local \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t cube-planner-api:local backend

.PHONY: frontend-test
frontend-test: ## Frontend tests (vitest)
	pnpm --filter @cube-planner/frontend test

.PHONY: frontend-typecheck
frontend-typecheck: ## Type-check the frontend (tsc, no emit)
	pnpm --filter @cube-planner/frontend typecheck

.PHONY: frontend-lint
frontend-lint: ## Run oxlint on the frontend
	pnpm --filter @cube-planner/frontend lint

.PHONY: test
test: backend-test frontend-test ## All tests

.PHONY: api-generate
api-generate: ## Regenerate the TS client from the Go OpenAPI spec
	pnpm gen:api

.PHONY: api-check
api-check: ## Verify the generated TS client is fresh
	pnpm check:api
