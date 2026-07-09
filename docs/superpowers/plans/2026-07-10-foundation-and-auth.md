# Foundation & Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Working monorepo where a user can register (email+password with verification, or Discord/Google OAuth), log in with a session cookie, and the whole stack deploys to a VPS via Docker Compose — with CI enforcing lint, tests, and generated-API-client freshness.

**Architecture:** Go modular monolith (`huma` on `chi`) serving `/api/*`, OpenAPI generated from Go and used to generate a typed TS client. Vite+React+TanStack SPA served by Caddy in prod (single origin, first-party session cookies). Postgres via `pgx`/`sqlc`, migrations via `goose` embedded and run at startup.

**Tech Stack:** Go 1.25, huma v2, chi v5, pgx v5, sqlc, goose v3, testcontainers-go, golang.org/x/oauth2, golang.org/x/crypto/argon2, wneessen/go-mail; pnpm 10, Vite 7, React 19, TypeScript ≥5.9, TanStack Router + Query, openapi-typescript + openapi-fetch, oxlint, oxfmt, lefthook, Vitest + Testing Library; GitHub Actions; Docker Compose + Caddy 2.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-10-cube-planner-master-design.md`. This plan implements sub-project 1 only (§7.1). No card/cube/event code.
- Go module path: `github.com/mjabloniec/cube-planner/backend`. Go `1.25`.
- All API operations live under path prefix `/api/` (the prefix is part of the huma operation `Path`).
- API errors are RFC 7807 `application/problem+json` (huma default — never hand-roll error JSON).
- tsconfig must include: `"strict": true`, `"noUncheckedIndexedAccess": true`, `"exactOptionalPropertyTypes": true`, `"verbatimModuleSyntax": true`, `"noImplicitOverride": true`, `"noUnusedLocals": true`, `"noUnusedParameters": true`, `"noFallthroughCasesInSwitch": true`, `"isolatedModules": true`.
- Session cookie name: `cp_session`. OAuth state cookie: `cp_oauth_state`. Cookies are `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` when `ENV=prod`.
- Session TTL: 30 days. Email-verification token TTL: 24h. Password-reset token TTL: 1h. Tokens/session secrets: 32 random bytes, base64url, only SHA-256 hashes stored in DB.
- Argon2id parameters: time=2, memory=19456 KiB, threads=1, keyLen=32, saltLen=16 (OWASP recommendation).
- Env vars (all config comes from env): `PORT` (default 8080), `ENV` (`dev`|`prod`, default `dev`), `DATABASE_URL`, `BASE_URL` (default `http://localhost:5173`), `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`, `DISCORD_CLIENT_ID`, `DISCORD_CLIENT_SECRET`, `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`.
- Emails are stored lowercased; lowercase on every insert/lookup.
- Go tests that need Postgres use the `testdb` helper (testcontainers) — they require Docker and must be normal `go test ./...` tests (no build tags).
- Commit after every task (steps say when). Conventional-commit style messages (`feat:`, `chore:`, `test:`, `ci:`).

---

### Task 1: Monorepo skeleton

**Files:**
- Create: `.gitignore`, `pnpm-workspace.yaml`, `package.json`, `lefthook.yml`, `README.md`, `backend/go.mod`, `scripts/` (dir)

**Interfaces:**
- Produces: pnpm workspace with packages `frontend` (Task 2); Go module `github.com/mjabloniec/cube-planner/backend`; root scripts `gen:api` / `check:api` (implemented Task 4).

- [ ] **Step 1: Create root files**

`.gitignore`:
```gitignore
node_modules/
dist/
*.local
.env
backend/bin/
coverage/
```

`pnpm-workspace.yaml`:
```yaml
packages:
  - frontend
  - packages/*
```

`package.json`:
```json
{
  "name": "cube-planner",
  "private": true,
  "packageManager": "pnpm@10.13.1",
  "scripts": {
    "dev": "pnpm --filter @cube-planner/frontend dev",
    "gen:api": "bash scripts/gen-api.sh",
    "check:api": "bash scripts/check-api-fresh.sh",
    "prepare": "lefthook install"
  },
  "devDependencies": {
    "lefthook": "^2.0.0"
  }
}
```

`lefthook.yml`:
```yaml
pre-commit:
  parallel: true
  commands:
    oxlint:
      root: frontend/
      glob: "*.{ts,tsx}"
      run: pnpm exec oxlint {staged_files}
    oxfmt:
      root: frontend/
      glob: "*.{ts,tsx,css,json}"
      run: pnpm exec oxfmt {staged_files} && git add {staged_files}
    gofumpt:
      glob: "backend/**/*.go"
      run: cd backend && go run mvdan.cc/gofumpt@latest -l -w . && git add -u
```

`README.md`:
```markdown
# Cube Planner

MTG cube management + events for a local community. See
`docs/superpowers/specs/2026-07-10-cube-planner-master-design.md`.

## Dev quickstart

- `pnpm install`
- `docker compose -f deploy/docker-compose.dev.yml up -d` (Postgres)
- `cd backend && DATABASE_URL=postgres://cube:cube@localhost:5432/cube?sslmode=disable go run ./cmd/server`
- `pnpm dev` (Vite on :5173, proxies /api to :8080)
```

- [ ] **Step 2: Initialize Go module**

Run:
```bash
mkdir -p backend scripts && cd backend && go mod init github.com/mjabloniec/cube-planner/backend && go mod edit -go=1.25
```

- [ ] **Step 3: Verify**

Run: `pnpm install` → Expected: succeeds, creates `pnpm-lock.yaml`, lefthook installs git hooks.
Run: `cd backend && go build ./...` → Expected: no output, exit 0 (empty module).

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "chore: monorepo skeleton (pnpm workspace, go module, lefthook)"
```

---

### Task 2: Frontend scaffold — Vite + React + TanStack, strict TS, oxlint/oxfmt, Vitest

**Files:**
- Create: `frontend/package.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`, `frontend/.oxlintrc.json`, `frontend/index.html`, `frontend/src/main.tsx`, `frontend/src/routes/__root.tsx`, `frontend/src/routes/index.tsx`, `frontend/src/app.test.tsx`, `frontend/src/vitest-setup.ts`

**Interfaces:**
- Produces: pnpm package `@cube-planner/frontend` with scripts `dev`, `build`, `test`, `lint`, `fmt`, `fmt:check`, `typecheck`. File-based TanStack routes in `src/routes/` (later tasks add `login.tsx` etc.). Dev proxy `/api` → `http://localhost:8080`.

- [ ] **Step 1: Create package and install deps**

`frontend/package.json`:
```json
{
  "name": "@cube-planner/frontend",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc --noEmit && vite build",
    "typecheck": "tsc --noEmit",
    "test": "vitest run",
    "lint": "oxlint .",
    "fmt": "oxfmt .",
    "fmt:check": "oxfmt --check ."
  }
}
```

Run:
```bash
cd frontend
pnpm add react react-dom @tanstack/react-router @tanstack/react-query openapi-fetch
pnpm add -D typescript vite @vitejs/plugin-react @tanstack/router-plugin @types/react @types/react-dom oxlint oxfmt vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom openapi-typescript
```

- [ ] **Step 2: Write configs**

`frontend/tsconfig.json`:
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "types": ["vite/client", "@testing-library/jest-dom"],
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "verbatimModuleSyntax": true,
    "noImplicitOverride": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "isolatedModules": true,
    "skipLibCheck": true,
    "noEmit": true
  },
  "include": ["src"]
}
```

`frontend/vite.config.ts`:
```ts
/// <reference types="vitest/config" />
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [tanstackRouter({ target: "react", autoCodeSplitting: true }), react()],
  server: {
    proxy: { "/api": "http://localhost:8080" },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/vitest-setup.ts"],
  },
});
```

`frontend/.oxlintrc.json`:
```json
{
  "plugins": ["typescript", "react", "import", "oxc"],
  "categories": { "correctness": "error", "suspicious": "error", "perf": "warn" },
  "env": { "browser": true },
  "ignorePatterns": ["dist", "src/routeTree.gen.ts", "src/api/schema.d.ts"]
}
```

`frontend/src/vitest-setup.ts`:
```ts
import "@testing-library/jest-dom/vitest";
```

- [ ] **Step 3: Write app shell**

`frontend/index.html`:
```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Cube Planner</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

`frontend/src/routes/__root.tsx`:
```tsx
import { createRootRoute, Link, Outlet } from "@tanstack/react-router";

export const Route = createRootRoute({
  component: () => (
    <>
      <nav>
        <Link to="/">Cube Planner</Link>
      </nav>
      <Outlet />
    </>
  ),
});
```

`frontend/src/routes/index.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/")({
  component: () => <h1>Cube Planner</h1>,
});
```

`frontend/src/main.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createRouter, RouterProvider } from "@tanstack/react-router";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { routeTree } from "./routeTree.gen";

const queryClient = new QueryClient();
const router = createRouter({ routeTree });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

const rootEl = document.getElementById("root");
if (!rootEl) throw new Error("missing #root");
createRoot(rootEl).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
```

- [ ] **Step 4: Write a smoke test**

`frontend/src/app.test.tsx`:
```tsx
import { render, screen } from "@testing-library/react";
import { expect, test } from "vitest";

test("renders heading", () => {
  render(<h1>Cube Planner</h1>);
  expect(screen.getByRole("heading", { name: "Cube Planner" })).toBeInTheDocument();
});
```

- [ ] **Step 5: Verify everything passes**

Run (in `frontend/`): `pnpm dev` briefly (generates `src/routeTree.gen.ts`), then:
```bash
pnpm lint && pnpm fmt && pnpm test && pnpm build
```
Expected: all pass. If `oxfmt` reformatted files, that's fine — it defines the canonical style from now on.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: frontend scaffold with strict TS, TanStack, oxlint/oxfmt, vitest"
```

---

### Task 3: Backend HTTP skeleton — huma + chi, healthz, config, golangci-lint

**Files:**
- Create: `backend/internal/platform/config/config.go`, `backend/internal/platform/httpapi/api.go`, `backend/internal/platform/httpapi/health.go`, `backend/internal/platform/httpapi/health_test.go`, `backend/cmd/server/main.go`, `backend/.golangci.yml`

**Interfaces:**
- Produces:
  - `config.Load() Config` — `Config{Port int; Env string; DatabaseURL, BaseURL string; SMTP SMTPConfig; Discord, Google OAuthCredentials}` with `SMTPConfig{Host string; Port int; User, Pass, From string}` and `OAuthCredentials{ClientID, ClientSecret string}`; `Config.Secure() bool` returns `Env == "prod"`.
  - `httpapi.Build(deps Deps) (huma.API, http.Handler)` — `Deps` is an empty struct for now; later tasks add fields. All routes registered here.
  - `GET /api/healthz` → 200 `{"status":"ok"}`.

- [ ] **Step 1: Write the failing test**

`backend/internal/platform/httpapi/health_test.go`:
```go
package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
)

func TestHealthz(t *testing.T) {
	_, handler := httpapi.Build(httpapi.Deps{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("body = %q, want to contain \"ok\"", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/httpapi/`
Expected: FAIL — package `httpapi` does not exist.

- [ ] **Step 3: Implement config, Build, healthz, main**

Run: `cd backend && go get github.com/danielgtaylor/huma/v2 github.com/go-chi/chi/v5`

`backend/internal/platform/config/config.go`:
```go
package config

import (
	"os"
	"strconv"
)

type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

type OAuthCredentials struct {
	ClientID     string
	ClientSecret string
}

type Config struct {
	Port        int
	Env         string
	DatabaseURL string
	BaseURL     string
	SMTP        SMTPConfig
	Discord     OAuthCredentials
	Google      OAuthCredentials
}

func (c Config) Secure() bool { return c.Env == "prod" }

func Load() Config {
	return Config{
		Port:        envInt("PORT", 8080),
		Env:         env("ENV", "dev"),
		DatabaseURL: env("DATABASE_URL", ""),
		BaseURL:     env("BASE_URL", "http://localhost:5173"),
		SMTP: SMTPConfig{
			Host: env("SMTP_HOST", ""),
			Port: envInt("SMTP_PORT", 587),
			User: env("SMTP_USER", ""),
			Pass: env("SMTP_PASS", ""),
			From: env("SMTP_FROM", "cube-planner@localhost"),
		},
		Discord: OAuthCredentials{ClientID: env("DISCORD_CLIENT_ID", ""), ClientSecret: env("DISCORD_CLIENT_SECRET", "")},
		Google:  OAuthCredentials{ClientID: env("GOOGLE_CLIENT_ID", ""), ClientSecret: env("GOOGLE_CLIENT_SECRET", "")},
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
```

`backend/internal/platform/httpapi/api.go`:
```go
// Package httpapi wires all HTTP routes: huma-managed /api operations and
// plain chi routes for browser redirect flows.
package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
)

// Deps carries everything handlers need. Fields are added by later tasks;
// Build(Deps{}) must always be safe for OpenAPI generation (no I/O at
// registration time).
type Deps struct{}

func Build(deps Deps) (huma.API, http.Handler) {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Cube Planner API", "0.1.0"))
	registerHealth(api)
	return api, router
}
```

`backend/internal/platform/httpapi/health.go`:
```go
package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type healthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok"`
	}
}

func registerHealth(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "healthz",
		Method:      http.MethodGet,
		Path:        "/api/healthz",
		Summary:     "Health check",
		Tags:        []string{"meta"},
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}
```

`backend/cmd/server/main.go`:
```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/spf13/cobra"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
)

type options struct{}

func main() {
	cli := humacli.New(func(hooks humacli.Hooks, _ *options) {
		cfg := config.Load()
		_, handler := httpapi.Build(httpapi.Deps{})
		hooks.OnStart(func() {
			log.Printf("listening on :%d", cfg.Port)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), handler); err != nil {
				log.Fatal(err)
			}
		})
	})

	cli.Root().AddCommand(&cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec as YAML",
		Run: func(_ *cobra.Command, _ []string) {
			api, _ := httpapi.Build(httpapi.Deps{})
			b, err := api.OpenAPI().YAML()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(string(b))
		},
	})

	cli.Run()
}
```

`backend/.golangci.yml`:
```yaml
version: "2"
linters:
  default: standard
  enable:
    - gocritic
    - misspell
formatters:
  enable:
    - gofumpt
```

Run: `cd backend && go mod tidy`

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./...`
Expected: PASS.
Also run: `go run ./cmd/server openapi | head -5` → Expected: YAML starting with `openapi: 3.1.0` (or 3.1.x) and `info:`.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: backend http skeleton with huma/chi, healthz, env config"
```

---

### Task 4: OpenAPI → generated TS client, freshness check

**Files:**
- Create: `scripts/gen-api.sh`, `scripts/check-api-fresh.sh`, `frontend/src/api/client.ts`
- Generated (checked in): `frontend/src/api/openapi.yaml`, `frontend/src/api/schema.d.ts`

**Interfaces:**
- Consumes: `go run ./cmd/server openapi` (Task 3).
- Produces: `client` from `frontend/src/api/client.ts`: `import { client } from "../api/client"` — an `openapi-fetch` client typed by `paths` from `schema.d.ts`; usage `client.GET("/api/healthz")`, `client.POST("/api/auth/login", { body: {...} })`. Root scripts `pnpm gen:api` and `pnpm check:api`.

- [ ] **Step 1: Write the generation scripts**

`scripts/gen-api.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p frontend/src/api
(cd backend && go run ./cmd/server openapi) > frontend/src/api/openapi.yaml
pnpm --filter @cube-planner/frontend exec openapi-typescript src/api/openapi.yaml -o src/api/schema.d.ts
```

`scripts/check-api-fresh.sh`:
```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
bash scripts/gen-api.sh
if ! git diff --exit-code -- frontend/src/api; then
  echo "ERROR: generated API client is stale. Run 'pnpm gen:api' and commit." >&2
  exit 1
fi
```

- [ ] **Step 2: Generate and write the client wrapper**

Run: `pnpm gen:api`
Expected: creates `frontend/src/api/openapi.yaml` and `frontend/src/api/schema.d.ts` (contains `"/api/healthz"` in `paths`).

`frontend/src/api/client.ts`:
```ts
import createClient from "openapi-fetch";
import type { paths } from "./schema";

export const client = createClient<paths>();
```

- [ ] **Step 3: Verify types flow end-to-end**

Add to `frontend/src/app.test.tsx` (new test in same file):
```tsx
import { client } from "./api/client";

test("api client is typed", () => {
  // Compile-time check: healthz path exists on the typed client.
  expect(typeof client.GET).toBe("function");
});
```
Run: `cd frontend && pnpm typecheck && pnpm test` → Expected: PASS.
Run: `pnpm check:api` (from root) → Expected: exits 0.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: openapi export and generated typed TS client with freshness check"
```

---

### Task 5: Postgres — dev compose, goose migrations, sqlc, testdb harness

**Files:**
- Create: `deploy/docker-compose.dev.yml`, `backend/migrations/migrations.go`, `backend/migrations/00001_auth.sql`, `backend/sqlc.yaml`, `backend/internal/db/queries/auth.sql`, `backend/internal/platform/db/db.go`, `backend/internal/platform/testdb/testdb.go`, `backend/internal/db/db_test.go`
- Generated (checked in): `backend/internal/db/*.go` (sqlc output: `db.go`, `models.go`, `auth.sql.go`)

**Interfaces:**
- Produces:
  - `db.Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)` (package `platform/db`) — connects AND runs goose migrations.
  - sqlc package `internal/db`: `db.New(pool) *db.Queries` with methods listed in Step 3's SQL (`CreateUser`, `GetUserByEmail`, `GetUserByID`, `MarkEmailVerified`, `SetPasswordHash`, `CreateSession`, `GetSessionUserID`, `DeleteSession`, `DeleteSessionsForUser`, `CreateAuthToken`, `ConsumeAuthToken`, `GetOAuthIdentity`, `CreateOAuthIdentity`, `ListProvidersForUser`). Model `db.User{ID uuid.UUID; Email, DisplayName string; PasswordHash *string; EmailVerifiedAt *time.Time; CreatedAt time.Time}`.
  - `testdb.New(t *testing.T) *pgxpool.Pool` — starts a disposable Postgres container, migrated, cleaned up via `t.Cleanup`.

- [ ] **Step 1: Dev compose + migration**

`deploy/docker-compose.dev.yml`:
```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: cube
      POSTGRES_PASSWORD: cube
      POSTGRES_DB: cube
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
volumes:
  pgdata:
```

`backend/migrations/migrations.go`:
```go
// Package migrations embeds SQL migrations for goose.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

`backend/migrations/00001_auth.sql`:
```sql
-- +goose Up
create table users (
    id uuid primary key default gen_random_uuid(),
    email text not null unique,
    display_name text not null,
    password_hash text,
    email_verified_at timestamptz,
    created_at timestamptz not null default now()
);

create table oauth_identities (
    provider text not null,
    provider_user_id text not null,
    user_id uuid not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    primary key (provider, provider_user_id)
);

create table sessions (
    token_hash bytea primary key,
    user_id uuid not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);

create table auth_tokens (
    token_hash bytea primary key,
    user_id uuid not null references users (id) on delete cascade,
    purpose text not null check (purpose in ('email_verification', 'password_reset')),
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);

-- +goose Down
drop table auth_tokens;
drop table sessions;
drop table oauth_identities;
drop table users;
```

- [ ] **Step 2: sqlc config**

`backend/sqlc.yaml`:
```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "internal/db/queries"
    schema: "migrations"
    gen:
      go:
        package: "db"
        out: "internal/db"
        sql_package: "pgx/v5"
        emit_pointers_for_null_types: true
        overrides:
          - db_type: "uuid"
            go_type: "github.com/google/uuid.UUID"
          - db_type: "timestamptz"
            go_type: "time.Time"
          - db_type: "timestamptz"
            nullable: true
            go_type:
              type: "time.Time"
              pointer: true
```

- [ ] **Step 3: Queries**

`backend/internal/db/queries/auth.sql`:
```sql
-- name: CreateUser :one
insert into users (email, display_name, password_hash, email_verified_at)
values (lower(sqlc.arg(email)), sqlc.arg(display_name), sqlc.narg(password_hash), sqlc.narg(email_verified_at))
returning *;

-- name: GetUserByEmail :one
select * from users where email = lower(sqlc.arg(email));

-- name: GetUserByID :one
select * from users where id = sqlc.arg(id);

-- name: MarkEmailVerified :exec
update users set email_verified_at = now() where id = sqlc.arg(id) and email_verified_at is null;

-- name: SetPasswordHash :exec
update users set password_hash = sqlc.arg(password_hash) where id = sqlc.arg(id);

-- name: CreateSession :exec
insert into sessions (token_hash, user_id, expires_at)
values (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(expires_at));

-- name: GetSessionUserID :one
select user_id from sessions where token_hash = sqlc.arg(token_hash) and expires_at > now();

-- name: DeleteSession :exec
delete from sessions where token_hash = sqlc.arg(token_hash);

-- name: DeleteSessionsForUser :exec
delete from sessions where user_id = sqlc.arg(user_id);

-- name: CreateAuthToken :exec
insert into auth_tokens (token_hash, user_id, purpose, expires_at)
values (sqlc.arg(token_hash), sqlc.arg(user_id), sqlc.arg(purpose), sqlc.arg(expires_at));

-- name: ConsumeAuthToken :one
delete from auth_tokens
where token_hash = sqlc.arg(token_hash) and purpose = sqlc.arg(purpose) and expires_at > now()
returning user_id;

-- name: GetOAuthIdentity :one
select * from oauth_identities where provider = sqlc.arg(provider) and provider_user_id = sqlc.arg(provider_user_id);

-- name: CreateOAuthIdentity :exec
insert into oauth_identities (provider, provider_user_id, user_id)
values (sqlc.arg(provider), sqlc.arg(provider_user_id), sqlc.arg(user_id));

-- name: ListProvidersForUser :many
select provider from oauth_identities where user_id = sqlc.arg(user_id) order by provider;
```

Run:
```bash
cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate
```
Expected: creates `internal/db/db.go`, `internal/db/models.go`, `internal/db/auth.sql.go`.

- [ ] **Step 4: Write the failing integration test**

`backend/internal/db/db_test.go`:
```go
package db_test

import (
	"context"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestCreateAndGetUser(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	hash := "fakehash"
	u, err := q.CreateUser(ctx, db.CreateUserParams{
		Email:        "Alice@Example.com",
		DisplayName:  "Alice",
		PasswordHash: &hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email = %q, want lowercased", u.Email)
	}

	got, err := q.GetUserByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Fatalf("got ID %v, want %v", got.ID, u.ID)
	}
	if got.EmailVerifiedAt != nil {
		t.Fatal("new user must not be verified")
	}
}
```

Run: `cd backend && go test ./internal/db/` → Expected: FAIL — `testdb` package does not exist.

- [ ] **Step 5: Implement db.Connect and testdb**

Run:
```bash
cd backend && go get github.com/jackc/pgx/v5 github.com/google/uuid github.com/pressly/goose/v3 github.com/testcontainers/testcontainers-go/modules/postgres
```

`backend/internal/platform/db/db.go`:
```go
// Package db owns the connection pool and schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/mjabloniec/cube-planner/backend/migrations"
)

// Connect opens a pool and runs pending goose migrations.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if err := migrate(databaseURL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func migrate(databaseURL string) error {
	sqldb, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return err
	}
	defer sqldb.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(sqldb, ".")
}
```

`backend/internal/platform/testdb/testdb.go`:
```go
// Package testdb spins up a disposable, migrated Postgres for tests.
// Requires Docker.
package testdb

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/db"
)

func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	pgc, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)))
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(pgc) })

	url, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect+migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
```

Run: `cd backend && go mod tidy`

- [ ] **Step 6: Run test to verify it passes**

Run: `cd backend && go test ./internal/db/ -v`
Expected: PASS (pulls postgres:17-alpine on first run; needs Docker).

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: postgres wiring with goose migrations, sqlc queries, testcontainers harness"
```

---

### Task 6: Argon2id password hashing

**Files:**
- Create: `backend/internal/auth/password.go`, `backend/internal/auth/password_test.go`

**Interfaces:**
- Produces (package `auth`):
  - `HashPassword(password string) (string, error)` — returns `$argon2id$v=19$m=19456,t=2,p=1$<b64salt>$<b64hash>`.
  - `VerifyPassword(password, encoded string) bool` — constant-time; false on any parse error.

- [ ] **Step 1: Write the failing test**

`backend/internal/auth/password_test.go`:
```go
package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := HashPassword("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected format: %s", encoded)
	}
	if !VerifyPassword("hunter22", encoded) {
		t.Fatal("correct password must verify")
	}
	if VerifyPassword("wrong", encoded) {
		t.Fatal("wrong password must not verify")
	}
	if VerifyPassword("hunter22", "garbage") {
		t.Fatal("garbage hash must not verify")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/`
Expected: FAIL — `HashPassword` undefined.

- [ ] **Step 3: Implement**

Run: `cd backend && go get golang.org/x/crypto/argon2`

`backend/internal/auth/password.go`:
```go
// Package auth implements password, session, token, and OAuth logic.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP-recommended argon2id parameters.
const (
	argonTime    = 2
	argonMemory  = 19456 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var mem, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &time, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/` → Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: argon2id password hashing"
```

---

### Task 7: Mailer — interface, log mailer, SMTP mailer

**Files:**
- Create: `backend/internal/platform/mail/mail.go`, `backend/internal/platform/mail/mail_test.go`

**Interfaces:**
- Produces (package `mail`):
  - `type Mailer interface { Send(ctx context.Context, to, subject, textBody string) error }`
  - `NewLogMailer(logger *slog.Logger) Mailer` — logs instead of sending (dev).
  - `NewSMTPMailer(cfg config.SMTPConfig) Mailer` — real SMTP via `github.com/wneessen/go-mail`.
  - `FromConfig(cfg config.Config) Mailer` — SMTP if `cfg.SMTP.Host != ""`, else log mailer.

- [ ] **Step 1: Write the failing test**

`backend/internal/platform/mail/mail_test.go`:
```go
package mail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
)

func TestLogMailerLogsRecipientAndBody(t *testing.T) {
	var buf bytes.Buffer
	m := NewLogMailer(slog.New(slog.NewTextHandler(&buf, nil)))
	if err := m.Send(context.Background(), "a@b.c", "Verify", "http://x/verify?token=t"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "a@b.c") || !strings.Contains(out, "token=t") {
		t.Fatalf("log output missing fields: %s", out)
	}
}

func TestFromConfigPicksLogMailerWithoutSMTPHost(t *testing.T) {
	m := FromConfig(config.Config{})
	if _, ok := m.(*logMailer); !ok {
		t.Fatalf("want *logMailer, got %T", m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/mail/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

Run: `cd backend && go get github.com/wneessen/go-mail`

`backend/internal/platform/mail/mail.go`:
```go
// Package mail sends transactional email. Dev uses the log mailer; prod
// uses SMTP (config-gated by SMTP_HOST).
package mail

import (
	"context"
	"log/slog"

	gomail "github.com/wneessen/go-mail"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
)

type Mailer interface {
	Send(ctx context.Context, to, subject, textBody string) error
}

func FromConfig(cfg config.Config) Mailer {
	if cfg.SMTP.Host != "" {
		return NewSMTPMailer(cfg.SMTP)
	}
	return NewLogMailer(slog.Default())
}

type logMailer struct{ log *slog.Logger }

func NewLogMailer(logger *slog.Logger) Mailer { return &logMailer{log: logger} }

func (m *logMailer) Send(_ context.Context, to, subject, textBody string) error {
	m.log.Info("mail (not sent, log mailer)", "to", to, "subject", subject, "body", textBody)
	return nil
}

type smtpMailer struct{ cfg config.SMTPConfig }

func NewSMTPMailer(cfg config.SMTPConfig) Mailer { return &smtpMailer{cfg: cfg} }

func (m *smtpMailer) Send(ctx context.Context, to, subject, textBody string) error {
	msg := gomail.NewMsg()
	if err := msg.From(m.cfg.From); err != nil {
		return err
	}
	if err := msg.To(to); err != nil {
		return err
	}
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextPlain, textBody)

	client, err := gomail.NewClient(m.cfg.Host,
		gomail.WithPort(m.cfg.Port),
		gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
		gomail.WithUsername(m.cfg.User),
		gomail.WithPassword(m.cfg.Pass))
	if err != nil {
		return err
	}
	return client.DialAndSendWithContext(ctx, msg)
}
```

Run: `cd backend && go mod tidy`

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/mail/` → Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: mailer with log and smtp implementations"
```

---

### Task 8: Registration + email verification (service + endpoints)

**Files:**
- Create: `backend/internal/auth/token.go`, `backend/internal/auth/service.go`, `backend/internal/auth/service_test.go`, `backend/internal/platform/httpapi/auth.go`
- Modify: `backend/internal/platform/httpapi/api.go` (add Deps fields, call `registerAuth`), `backend/cmd/server/main.go` (build real deps)

**Interfaces:**
- Consumes: `db.Queries` (Task 5), `HashPassword`/`VerifyPassword` (Task 6), `mail.Mailer` (Task 7).
- Produces (package `auth`):
  - `NewService(q *db.Queries, mailer mail.Mailer, baseURL string) *Service`
  - `(*Service) Register(ctx, email, displayName, password string) error` — errors: `ErrEmailTaken`.
  - `(*Service) VerifyEmail(ctx, token string) error` — errors: `ErrInvalidToken`.
  - `(*Service) Login(ctx, email, password string) (db.User, error)` — errors: `ErrInvalidCredentials`, `ErrEmailNotVerified` (Login used by Task 9's endpoint).
  - `(*Service) RequestPasswordReset(ctx, email string) error` (always nil for unknown email), `(*Service) ResetPassword(ctx, token, newPassword string) error` (used by Task 10).
  - `newToken() (raw string, hash []byte, err error)` and `hashToken(raw string) []byte` (SHA-256) in `token.go`.
  - HTTP: `POST /api/auth/register` `{email, displayName, password}` → 204 (409 on taken email); `POST /api/auth/verify-email` `{token}` → 204 (422 on invalid token).
  - `httpapi.Deps` gains fields: `Auth *auth.Service` (later tasks add more).

- [ ] **Step 1: Write the failing service test**

`backend/internal/auth/service_test.go`:
```go
package auth_test

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// capturingMailer records sent mail for assertions.
type capturingMailer struct {
	mu   sync.Mutex
	last string
}

func (m *capturingMailer) Send(_ context.Context, _, _, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last = body
	return nil
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func TestRegisterVerifyLogin(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	mailer := &capturingMailer{}
	svc := auth.NewService(q, mailer, "http://localhost:5173")
	ctx := context.Background()

	if err := svc.Register(ctx, "Bob@Example.com", "Bob", "password123"); err != nil {
		t.Fatal(err)
	}

	// Duplicate email is rejected.
	if err := svc.Register(ctx, "bob@example.com", "Bob2", "password123"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}

	// Login before verification is rejected.
	if _, err := svc.Login(ctx, "bob@example.com", "password123"); !errors.Is(err, auth.ErrEmailNotVerified) {
		t.Fatalf("err = %v, want ErrEmailNotVerified", err)
	}

	// Verify with the token from the email.
	m := tokenRe.FindStringSubmatch(mailer.last)
	if m == nil {
		t.Fatalf("no token in mail body: %q", mailer.last)
	}
	if err := svc.VerifyEmail(ctx, m[1]); err != nil {
		t.Fatal(err)
	}
	// Token is single-use.
	if err := svc.VerifyEmail(ctx, m[1]); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken on reuse", err)
	}

	// Login now works; wrong password still fails.
	u, err := svc.Login(ctx, "bob@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "bob@example.com" {
		t.Fatalf("email = %q", u.Email)
	}
	if _, err := svc.Login(ctx, "bob@example.com", "nope"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/`
Expected: FAIL — `NewService` undefined.

- [ ] **Step 3: Implement token helpers and service**

`backend/internal/auth/token.go`:
```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// newToken returns a random secret (given to the user) and its SHA-256
// hash (stored in the DB). Applies to sessions and one-time auth tokens.
func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	return tok, hashToken(tok), nil
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
```

`backend/internal/auth/service.go`:
```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailNotVerified   = errors.New("email not verified")
)

const (
	purposeEmailVerification = "email_verification"
	purposePasswordReset     = "password_reset"

	verificationTTL = 24 * time.Hour
	resetTTL        = time.Hour
)

type Service struct {
	q       *db.Queries
	mailer  mail.Mailer
	baseURL string
}

func NewService(q *db.Queries, mailer mail.Mailer, baseURL string) *Service {
	return &Service{q: q, mailer: mailer, baseURL: baseURL}
}

func (s *Service) Register(ctx context.Context, email, displayName, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	u, err := s.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &hash,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return ErrEmailTaken
		}
		return err
	}
	return s.sendVerification(ctx, u)
}

func (s *Service) sendVerification(ctx context.Context, u db.User) error {
	tok, hash, err := newToken()
	if err != nil {
		return err
	}
	err = s.q.CreateAuthToken(ctx, db.CreateAuthTokenParams{
		TokenHash: hash,
		UserID:    u.ID,
		Purpose:   purposeEmailVerification,
		ExpiresAt: time.Now().Add(verificationTTL),
	})
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Hi %s,\n\nVerify your Cube Planner account:\n%s/verify-email?token=%s\n\nThe link expires in 24 hours.",
		u.DisplayName, s.baseURL, tok)
	return s.mailer.Send(ctx, u.Email, "Verify your Cube Planner account", body)
}

func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	userID, err := s.q.ConsumeAuthToken(ctx, db.ConsumeAuthTokenParams{
		TokenHash: hashToken(token),
		Purpose:   purposeEmailVerification,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		return err
	}
	return s.q.MarkEmailVerified(ctx, userID)
}

func (s *Service) Login(ctx context.Context, email, password string) (db.User, error) {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, ErrInvalidCredentials
		}
		return db.User{}, err
	}
	if u.PasswordHash == nil || !VerifyPassword(password, *u.PasswordHash) {
		return db.User{}, ErrInvalidCredentials
	}
	if u.EmailVerifiedAt == nil {
		return db.User{}, ErrEmailNotVerified
	}
	return u, nil
}

func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // do not reveal whether the email exists
		}
		return err
	}
	tok, hash, err := newToken()
	if err != nil {
		return err
	}
	err = s.q.CreateAuthToken(ctx, db.CreateAuthTokenParams{
		TokenHash: hash,
		UserID:    u.ID,
		Purpose:   purposePasswordReset,
		ExpiresAt: time.Now().Add(resetTTL),
	})
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Hi %s,\n\nReset your Cube Planner password:\n%s/reset-password?token=%s\n\nThe link expires in 1 hour. If you didn't request this, ignore this mail.",
		u.DisplayName, s.baseURL, tok)
	return s.mailer.Send(ctx, u.Email, "Reset your Cube Planner password", body)
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	userID, err := s.q.ConsumeAuthToken(ctx, db.ConsumeAuthTokenParams{
		TokenHash: hashToken(token),
		Purpose:   purposePasswordReset,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.q.SetPasswordHash(ctx, db.SetPasswordHashParams{PasswordHash: &hash, ID: userID}); err != nil {
		return err
	}
	// Changing the password invalidates all existing sessions.
	return s.q.DeleteSessionsForUser(ctx, userID)
}
```

- [ ] **Step 4: Run service test to verify it passes**

Run: `cd backend && go test ./internal/auth/` → Expected: PASS.

- [ ] **Step 5: Wire HTTP endpoints**

`backend/internal/platform/httpapi/auth.go`:
```go
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
)

type registerInput struct {
	Body struct {
		Email       string `json:"email" format:"email" maxLength:"254"`
		DisplayName string `json:"displayName" minLength:"1" maxLength:"50"`
		Password    string `json:"password" minLength:"8" maxLength:"200"`
	}
}

type verifyEmailInput struct {
	Body struct {
		Token string `json:"token" minLength:"1"`
	}
}

func registerAuth(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID:   "register",
		Method:        http.MethodPost,
		Path:          "/api/auth/register",
		Summary:       "Register with email and password",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *registerInput) (*struct{}, error) {
		err := deps.Auth.Register(ctx, in.Body.Email, in.Body.DisplayName, in.Body.Password)
		if errors.Is(err, auth.ErrEmailTaken) {
			return nil, huma.Error409Conflict("email already registered")
		}
		return nil, err
	})

	huma.Register(api, huma.Operation{
		OperationID:   "verify-email",
		Method:        http.MethodPost,
		Path:          "/api/auth/verify-email",
		Summary:       "Verify email address with a token",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *verifyEmailInput) (*struct{}, error) {
		err := deps.Auth.VerifyEmail(ctx, in.Body.Token)
		if errors.Is(err, auth.ErrInvalidToken) {
			return nil, huma.Error422UnprocessableEntity("invalid or expired token")
		}
		return nil, err
	})
}
```

Modify `backend/internal/platform/httpapi/api.go` — replace `Deps` and `Build`:
```go
// Deps carries everything handlers need. Build(Deps{}) must stay safe for
// OpenAPI generation (no I/O at registration time).
type Deps struct {
	Auth *auth.Service
}

func Build(deps Deps) (huma.API, http.Handler) {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Cube Planner API", "0.1.0"))
	registerHealth(api)
	registerAuth(api, deps)
	return api, router
}
```
(add import `"github.com/mjabloniec/cube-planner/backend/internal/auth"`)

Modify `backend/cmd/server/main.go` — replace the humacli builder function body:
```go
	cli := humacli.New(func(hooks humacli.Hooks, _ *options) {
		cfg := config.Load()
		ctx := context.Background()
		pool, err := db.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("database: %v", err)
		}
		queries := dbgen.New(pool)
		deps := httpapi.Deps{
			Auth: auth.NewService(queries, mail.FromConfig(cfg), cfg.BaseURL),
		}
		_, handler := httpapi.Build(deps)
		hooks.OnStart(func() {
			log.Printf("listening on :%d", cfg.Port)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.Port), handler); err != nil {
				log.Fatal(err)
			}
		})
	})
```
with imports:
```go
	"context"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	dbgen "github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
```

- [ ] **Step 6: Verify build + regenerate API client**

Run:
```bash
cd backend && go build ./... && go test ./... && cd .. && pnpm gen:api && cd frontend && pnpm typecheck
```
Expected: all pass; `schema.d.ts` now contains `/api/auth/register` and `/api/auth/verify-email`.

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat: registration and email verification"
```

---

### Task 9: Sessions — manager, middleware, login/logout/me endpoints

**Files:**
- Create: `backend/internal/auth/session.go`, `backend/internal/auth/session_test.go`, `backend/internal/platform/httpapi/session.go`, `backend/internal/platform/httpapi/session_endpoints_test.go`
- Modify: `backend/internal/platform/httpapi/api.go` (Deps gains `Sessions *auth.Sessions`; install middleware; call `registerSession`), `backend/cmd/server/main.go` (construct `auth.NewSessions`)

**Interfaces:**
- Consumes: `db.Queries` session queries (Task 5), `newToken`/`hashToken` (Task 8), `auth.Service.Login` (Task 8).
- Produces (package `auth`):
  - `NewSessions(q *db.Queries, secure bool) *Sessions`
  - `(*Sessions) Create(ctx, userID uuid.UUID) (*http.Cookie, error)` — creates DB session, returns ready-to-set cookie (name `cp_session`, 30-day expiry, HttpOnly, Lax, Path=/, Secure per flag).
  - `(*Sessions) UserID(ctx, rawToken string) (uuid.UUID, error)` — `ErrNoSession` if missing/expired.
  - `(*Sessions) Revoke(ctx, rawToken string) error`
  - `(*Sessions) ClearCookie() *http.Cookie` — expired cookie for logout.
  - `var ErrNoSession = errors.New("no valid session")`
- Produces (package `httpapi`):
  - huma middleware that resolves `cp_session` → sets user ID in context.
  - `CurrentUserID(ctx context.Context) (uuid.UUID, bool)` — for all future protected handlers.
  - HTTP: `POST /api/auth/login` `{email,password}` → 200 `{id,email,displayName,providers}` + Set-Cookie (401 bad creds, 403 unverified); `POST /api/auth/logout` → 204 + clearing Set-Cookie; `GET /api/me` → 200 same body as login (401 when anonymous).

- [ ] **Step 1: Write the failing tests**

`backend/internal/auth/session_test.go`:
```go
package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestSessionLifecycle(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	sessions := auth.NewSessions(q, false)
	ctx := context.Background()

	u, err := q.CreateUser(ctx, db.CreateUserParams{Email: "s@x.y", DisplayName: "S"})
	if err != nil {
		t.Fatal(err)
	}

	cookie, err := sessions.Create(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cookie.Name != "cp_session" || !cookie.HttpOnly {
		t.Fatalf("bad cookie: %+v", cookie)
	}

	uid, err := sessions.UserID(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if uid != u.ID {
		t.Fatalf("uid = %v, want %v", uid, u.ID)
	}

	if err := sessions.Revoke(ctx, cookie.Value); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.UserID(ctx, cookie.Value); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("err = %v, want ErrNoSession after revoke", err)
	}
}
```

`backend/internal/platform/httpapi/session_endpoints_test.go`:
```go
package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

type noopMailer struct{}

func (noopMailer) Send(context.Context, string, string, string) error { return nil }

func newTestServer(t *testing.T) (*httptest.Server, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:     auth.NewService(q, noopMailer{}, "http://test"),
		Sessions: auth.NewSessions(q, false),
		Queries:  q,
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, q
}

func TestLoginMeLogout(t *testing.T) {
	srv, q := newTestServer(t)
	ctx := context.Background()

	// Seed a verified user directly.
	hash, _ := auth.HashPassword("password123")
	u, err := q.CreateUser(ctx, db.CreateUserParams{Email: "eve@x.y", DisplayName: "Eve", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.MarkEmailVerified(ctx, u.ID); err != nil {
		t.Fatal(err)
	}

	jar := newCookieClient(t, srv)

	// Anonymous /api/me → 401.
	resp := jar.do(t, "GET", "/api/me", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me anonymous: status %d, want 401", resp.StatusCode)
	}

	// Login sets the cookie.
	resp = jar.do(t, "POST", "/api/auth/login", `{"email":"eve@x.y","password":"password123"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d, want 200", resp.StatusCode)
	}

	// /api/me now works.
	resp = jar.do(t, "GET", "/api/me", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: status %d, want 200", resp.StatusCode)
	}

	// Logout clears the session.
	resp = jar.do(t, "POST", "/api/auth/logout", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: status %d, want 204", resp.StatusCode)
	}
	resp = jar.do(t, "GET", "/api/me", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout: status %d, want 401", resp.StatusCode)
	}
}

// cookieClient is a tiny helper wrapping http.Client with a cookie jar.
type cookieClient struct {
	base   string
	client *http.Client
}

func newCookieClient(t *testing.T, srv *httptest.Server) *cookieClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &cookieClient{base: srv.URL, client: &http.Client{Jar: jar}}
}

func (c *cookieClient) do(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/auth/ ./internal/platform/httpapi/`
Expected: FAIL — `NewSessions` undefined; `Deps` has no field `Sessions`.

- [ ] **Step 3: Implement the session manager**

`backend/internal/auth/session.go`:
```go
package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

var ErrNoSession = errors.New("no valid session")

const (
	SessionCookieName = "cp_session"
	sessionTTL        = 30 * 24 * time.Hour
)

type Sessions struct {
	q      *db.Queries
	secure bool
}

func NewSessions(q *db.Queries, secure bool) *Sessions {
	return &Sessions{q: q, secure: secure}
}

func (s *Sessions) Create(ctx context.Context, userID uuid.UUID) (*http.Cookie, error) {
	tok, hash, err := newToken()
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(sessionTTL)
	err = s.q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hash,
		UserID:    userID,
		ExpiresAt: expires,
	})
	if err != nil {
		return nil, err
	}
	return s.cookie(tok, expires), nil
}

func (s *Sessions) UserID(ctx context.Context, rawToken string) (uuid.UUID, error) {
	uid, err := s.q.GetSessionUserID(ctx, hashToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoSession
	}
	return uid, err
}

func (s *Sessions) Revoke(ctx context.Context, rawToken string) error {
	return s.q.DeleteSession(ctx, hashToken(rawToken))
}

func (s *Sessions) ClearCookie() *http.Cookie {
	return s.cookie("", time.Unix(0, 0))
}

func (s *Sessions) cookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	}
}
```

- [ ] **Step 4: Implement middleware + endpoints**

`backend/internal/platform/httpapi/session.go`:
```go
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

type userIDKey struct{}

// sessionMiddleware resolves the session cookie into a user ID in context.
// It never rejects requests: handlers decide whether auth is required.
func sessionMiddleware(sessions *auth.Sessions) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if cookie, err := huma.ReadCookie(ctx, auth.SessionCookieName); err == nil && cookie.Value != "" {
			if uid, err := sessions.UserID(ctx.Context(), cookie.Value); err == nil {
				ctx = huma.WithValue(ctx, userIDKey{}, uid)
			}
		}
		next(ctx)
	}
}

// CurrentUserID returns the authenticated user's ID, if any.
func CurrentUserID(ctx context.Context) (uuid.UUID, bool) {
	uid, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return uid, ok
}

// UserBody is exported: huma derives the OpenAPI schema name (and thus the
// generated TS type name) from the Go type name.
type UserBody struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Providers   []string  `json:"providers"`
}

type loginInput struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"1"`
	}
}

type loginOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      UserBody
}

type meOutput struct {
	Body UserBody
}

type logoutInput struct {
	Cookie string `cookie:"cp_session"`
}

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func userBodyFor(ctx context.Context, deps Deps, u db.User) (UserBody, error) {
	providers, err := deps.Queries.ListProvidersForUser(ctx, u.ID)
	if err != nil {
		return UserBody{}, err
	}
	if providers == nil {
		providers = []string{}
	}
	return UserBody{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Providers: providers}, nil
}

func registerSession(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/auth/login",
		Summary:     "Log in with email and password",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, in *loginInput) (*loginOutput, error) {
		u, err := deps.Auth.Login(ctx, in.Body.Email, in.Body.Password)
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			return nil, huma.Error401Unauthorized("invalid email or password")
		case errors.Is(err, auth.ErrEmailNotVerified):
			return nil, huma.Error403Forbidden("email not verified")
		case err != nil:
			return nil, err
		}
		cookie, err := deps.Sessions.Create(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		body, err := userBodyFor(ctx, deps, u)
		if err != nil {
			return nil, err
		}
		return &loginOutput{SetCookie: *cookie, Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/api/auth/logout",
		Summary:       "Log out",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *logoutInput) (*logoutOutput, error) {
		if in.Cookie != "" {
			if err := deps.Sessions.Revoke(ctx, in.Cookie); err != nil {
				return nil, err
			}
		}
		return &logoutOutput{SetCookie: *deps.Sessions.ClearCookie()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/api/me",
		Summary:     "Current user",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		u, err := deps.Queries.GetUserByID(ctx, uid)
		if err != nil {
			return nil, err
		}
		body, err := userBodyFor(ctx, deps, u)
		if err != nil {
			return nil, err
		}
		return &meOutput{Body: body}, nil
	})
}
```

Modify `backend/internal/platform/httpapi/api.go`:
```go
type Deps struct {
	Auth     *auth.Service
	Sessions *auth.Sessions
	Queries  *db.Queries
}

func Build(deps Deps) (huma.API, http.Handler) {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("Cube Planner API", "0.1.0"))
	if deps.Sessions != nil {
		api.UseMiddleware(sessionMiddleware(deps.Sessions))
	}
	registerHealth(api)
	registerAuth(api, deps)
	registerSession(api, deps)
	return api, router
}
```
(add import for `internal/db`; also add `Queries: queries` and `Sessions: auth.NewSessions(queries, cfg.Secure())` to the deps construction in `cmd/server/main.go`)

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./...` → Expected: PASS.

- [ ] **Step 6: Regenerate API client, commit**

```bash
pnpm gen:api && cd frontend && pnpm typecheck && cd ..
git add -A && git commit -m "feat: sessions with login, logout, and me endpoints"
```

---

### Task 10: Password reset endpoints

**Files:**
- Create: `backend/internal/platform/httpapi/password_reset.go`
- Modify: `backend/internal/platform/httpapi/api.go` (call `registerPasswordReset`), `backend/internal/auth/service_test.go` (add reset test)

**Interfaces:**
- Consumes: `Service.RequestPasswordReset` / `Service.ResetPassword` (Task 8).
- Produces: `POST /api/auth/forgot-password` `{email}` → always 204; `POST /api/auth/reset-password` `{token,newPassword}` → 204 (422 invalid token).

- [ ] **Step 1: Write the failing service test**

Append to `backend/internal/auth/service_test.go`:
```go
func TestPasswordReset(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	mailer := &capturingMailer{}
	svc := auth.NewService(q, mailer, "http://localhost:5173")
	ctx := context.Background()

	if err := svc.Register(ctx, "carol@x.y", "Carol", "oldpassword"); err != nil {
		t.Fatal(err)
	}
	m := tokenRe.FindStringSubmatch(mailer.last)
	if err := svc.VerifyEmail(ctx, m[1]); err != nil {
		t.Fatal(err)
	}

	// Unknown email: no error, no mail.
	mailer.last = ""
	if err := svc.RequestPasswordReset(ctx, "nobody@x.y"); err != nil {
		t.Fatal(err)
	}
	if mailer.last != "" {
		t.Fatal("mail must not be sent for unknown email")
	}

	if err := svc.RequestPasswordReset(ctx, "carol@x.y"); err != nil {
		t.Fatal(err)
	}
	m = tokenRe.FindStringSubmatch(mailer.last)
	if m == nil {
		t.Fatalf("no token in mail: %q", mailer.last)
	}
	if err := svc.ResetPassword(ctx, m[1], "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "carol@x.y", "oldpassword"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatal("old password must stop working")
	}
	if _, err := svc.Login(ctx, "carol@x.y", "newpassword1"); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test — service methods already exist (Task 8), so it should PASS**

Run: `cd backend && go test ./internal/auth/ -run TestPasswordReset -v`
Expected: PASS (the service logic landed in Task 8; this test locks in behavior).

- [ ] **Step 3: Add the endpoints**

`backend/internal/platform/httpapi/password_reset.go`:
```go
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
)

type forgotPasswordInput struct {
	Body struct {
		Email string `json:"email" format:"email"`
	}
}

type resetPasswordInput struct {
	Body struct {
		Token       string `json:"token" minLength:"1"`
		NewPassword string `json:"newPassword" minLength:"8" maxLength:"200"`
	}
}

func registerPasswordReset(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID:   "forgot-password",
		Method:        http.MethodPost,
		Path:          "/api/auth/forgot-password",
		Summary:       "Request a password reset email",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *forgotPasswordInput) (*struct{}, error) {
		// Always 204: do not reveal whether the email exists.
		return nil, deps.Auth.RequestPasswordReset(ctx, in.Body.Email)
	})

	huma.Register(api, huma.Operation{
		OperationID:   "reset-password",
		Method:        http.MethodPost,
		Path:          "/api/auth/reset-password",
		Summary:       "Reset password with a token",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *resetPasswordInput) (*struct{}, error) {
		err := deps.Auth.ResetPassword(ctx, in.Body.Token, in.Body.NewPassword)
		if errors.Is(err, auth.ErrInvalidToken) {
			return nil, huma.Error422UnprocessableEntity("invalid or expired token")
		}
		return nil, err
	})
}
```

Add `registerPasswordReset(api, deps)` to `Build` in `api.go` after `registerSession(api, deps)`.

- [ ] **Step 4: Verify and regenerate**

Run: `cd backend && go test ./... && cd .. && pnpm gen:api && cd frontend && pnpm typecheck`
Expected: PASS; schema contains the two new paths.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: password reset flow"
```

---

### Task 11: OAuth — Discord & Google login + account linking

**Files:**
- Create: `backend/internal/auth/oauth.go`, `backend/internal/auth/oauth_test.go`
- Modify: `backend/internal/platform/httpapi/api.go` (mount OAuth chi routes), `backend/cmd/server/main.go` (construct `auth.NewOAuth`)

**Interfaces:**
- Consumes: `db.Queries` (Task 5), `Sessions` (Task 9), `config.OAuthCredentials` (Task 3).
- Produces (package `auth`):
  - `NewOAuth(q *db.Queries, sessions *Sessions, baseURL string, secure bool, providers map[string]*ProviderConfig) *OAuth`
  - `type ProviderConfig struct { OAuth2 *oauth2.Config; UserInfoURL string; ParseUser func(body []byte) (ProviderUser, error) }`
  - `type ProviderUser struct { ID, Email, DisplayName string; EmailVerified bool }`
  - `DiscordProvider(creds config.OAuthCredentials, redirectURL string) *ProviderConfig`, `GoogleProvider(creds config.OAuthCredentials, redirectURL string) *ProviderConfig`
  - `(*OAuth) Routes() http.Handler` — chi router with `GET /{provider}/start` and `GET /{provider}/callback`. Mounted at `/auth/oauth` (NOT under `/api` — these are browser redirects, intentionally outside OpenAPI).
  - `(*OAuth) CompleteLogin(ctx, provider string, pu ProviderUser, linkTo uuid.UUID) (uuid.UUID, error)` — exported for tests. Rules: existing identity → that user; `linkTo != uuid.Nil` → link to that user; else match by verified email → link; else create user (verified if provider verified). Error `ErrIdentityTaken` if identity already linked to a different user when linking.
- Redirect targets: success → `BASE_URL/account?linked={provider}` (link) or `BASE_URL/` (login); failure → `BASE_URL/login?error=oauth`.
- httpapi: `Deps` gains `OAuth http.Handler`; `Build` mounts it via `router.Mount("/auth/oauth", deps.OAuth)` when non-nil.

- [ ] **Step 1: Write the failing test for CompleteLogin rules**

`backend/internal/auth/oauth_test.go`:
```go
package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestCompleteLogin(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	sessions := auth.NewSessions(q, false)
	o := auth.NewOAuth(q, sessions, "http://test", false, nil)
	ctx := context.Background()

	pu := auth.ProviderUser{ID: "d123", Email: "Dana@X.Y", DisplayName: "Dana", EmailVerified: true}

	// 1. New identity, no matching user → creates a verified user.
	uid, err := o.CompleteLogin(ctx, "discord", pu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := q.GetUserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "dana@x.y" || u.EmailVerifiedAt == nil {
		t.Fatalf("user = %+v, want lowercased verified email", u)
	}

	// 2. Same identity again → same user (login path).
	uid2, err := o.CompleteLogin(ctx, "discord", pu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if uid2 != uid {
		t.Fatalf("second login uid = %v, want %v", uid2, uid)
	}

	// 3. Different provider, same verified email → links to same user.
	gu := auth.ProviderUser{ID: "g999", Email: "dana@x.y", DisplayName: "Dana G", EmailVerified: true}
	uid3, err := o.CompleteLogin(ctx, "google", gu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if uid3 != uid {
		t.Fatalf("google login uid = %v, want %v", uid3, uid)
	}
	providers, err := q.ListProvidersForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 2 {
		t.Fatalf("providers = %v, want discord+google", providers)
	}

	// 4. Explicit linking of an identity already owned by another user fails.
	other, err := q.CreateUser(ctx, db.CreateUserParams{Email: "other@x.y", DisplayName: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = o.CompleteLogin(ctx, "discord", pu, other.ID)
	if !errors.Is(err, auth.ErrIdentityTaken) {
		t.Fatalf("err = %v, want ErrIdentityTaken", err)
	}

	// 5. Unverified provider email must NOT auto-link to an existing account.
	eu := auth.ProviderUser{ID: "d777", Email: "other@x.y", DisplayName: "Evil", EmailVerified: false}
	uid5, err := o.CompleteLogin(ctx, "discord", eu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if uid5 == other.ID {
		t.Fatal("unverified email must not attach to existing account")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/auth/ -run TestCompleteLogin`
Expected: FAIL — `NewOAuth` undefined.

- [ ] **Step 3: Implement OAuth**

Run: `cd backend && go get golang.org/x/oauth2`

`backend/internal/auth/oauth.go`:
```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
)

var ErrIdentityTaken = errors.New("identity already linked to another user")

const stateCookieName = "cp_oauth_state"

type ProviderUser struct {
	ID            string
	Email         string
	DisplayName   string
	EmailVerified bool
}

type ProviderConfig struct {
	OAuth2      *oauth2.Config
	UserInfoURL string
	ParseUser   func(body []byte) (ProviderUser, error)
}

func DiscordProvider(creds config.OAuthCredentials, redirectURL string) *ProviderConfig {
	return &ProviderConfig{
		OAuth2: &oauth2.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			Endpoint:     endpoints.Discord,
			RedirectURL:  redirectURL,
			Scopes:       []string{"identify", "email"},
		},
		UserInfoURL: "https://discord.com/api/users/@me",
		ParseUser: func(body []byte) (ProviderUser, error) {
			var v struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Email    string `json:"email"`
				Verified bool   `json:"verified"`
			}
			if err := json.Unmarshal(body, &v); err != nil {
				return ProviderUser{}, err
			}
			return ProviderUser{ID: v.ID, Email: v.Email, DisplayName: v.Username, EmailVerified: v.Verified}, nil
		},
	}
}

func GoogleProvider(creds config.OAuthCredentials, redirectURL string) *ProviderConfig {
	return &ProviderConfig{
		OAuth2: &oauth2.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			Endpoint:     endpoints.Google,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
		},
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		ParseUser: func(body []byte) (ProviderUser, error) {
			var v struct {
				Sub           string `json:"sub"`
				Email         string `json:"email"`
				EmailVerified bool   `json:"email_verified"`
				Name          string `json:"name"`
			}
			if err := json.Unmarshal(body, &v); err != nil {
				return ProviderUser{}, err
			}
			return ProviderUser{ID: v.Sub, Email: v.Email, DisplayName: v.Name, EmailVerified: v.EmailVerified}, nil
		},
	}
}

type OAuth struct {
	q         *db.Queries
	sessions  *Sessions
	baseURL   string
	secure    bool
	providers map[string]*ProviderConfig
}

func NewOAuth(q *db.Queries, sessions *Sessions, baseURL string, secure bool, providers map[string]*ProviderConfig) *OAuth {
	return &OAuth{q: q, sessions: sessions, baseURL: baseURL, secure: secure, providers: providers}
}

// CompleteLogin resolves a provider identity to a local user, creating or
// linking as needed. linkTo is the logged-in user during explicit linking,
// uuid.Nil otherwise.
func (o *OAuth) CompleteLogin(ctx context.Context, provider string, pu ProviderUser, linkTo uuid.UUID) (uuid.UUID, error) {
	ident, err := o.q.GetOAuthIdentity(ctx, db.GetOAuthIdentityParams{Provider: provider, ProviderUserID: pu.ID})
	switch {
	case err == nil:
		if linkTo != uuid.Nil && ident.UserID != linkTo {
			return uuid.Nil, ErrIdentityTaken
		}
		return ident.UserID, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, err
	}

	userID := linkTo
	if userID == uuid.Nil {
		userID, err = o.matchOrCreateUser(ctx, pu)
		if err != nil {
			return uuid.Nil, err
		}
	}
	err = o.q.CreateOAuthIdentity(ctx, db.CreateOAuthIdentityParams{
		Provider:       provider,
		ProviderUserID: pu.ID,
		UserID:         userID,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

func (o *OAuth) matchOrCreateUser(ctx context.Context, pu ProviderUser) (uuid.UUID, error) {
	if pu.EmailVerified {
		if u, err := o.q.GetUserByEmail(ctx, pu.Email); err == nil {
			return u.ID, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, err
		}
	}
	var verifiedAt *time.Time
	if pu.EmailVerified {
		now := time.Now()
		verifiedAt = &now
	}
	u, err := o.q.CreateUser(ctx, db.CreateUserParams{
		Email:           pu.Email,
		DisplayName:     pu.DisplayName,
		EmailVerifiedAt: verifiedAt,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

// Routes returns browser-redirect endpoints, mounted outside /api.
func (o *OAuth) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/{provider}/start", o.handleStart)
	r.Get("/{provider}/callback", o.handleCallback)
	return r
}

func (o *OAuth) handleStart(w http.ResponseWriter, r *http.Request) {
	p, ok := o.providers[chi.URLParam(r, "provider")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	state, _, err := newToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	link := "0"
	if r.URL.Query().Get("link") == "1" {
		link = "1"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state + ":" + link,
		Path:     "/auth/oauth",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   o.secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, p.OAuth2.AuthCodeURL(state), http.StatusFound)
}

func (o *OAuth) handleCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	p, ok := o.providers[provider]
	if !ok {
		http.NotFound(w, r)
		return
	}
	fail := func() { http.Redirect(w, r, o.baseURL+"/login?error=oauth", http.StatusFound) }

	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		fail()
		return
	}
	state, link, ok := strings.Cut(stateCookie.Value, ":")
	if !ok || state == "" {
		fail()
		return
	}
	if r.URL.Query().Get("state") != state {
		fail()
		return
	}

	token, err := p.OAuth2.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		fail()
		return
	}
	pu, err := o.fetchUser(r.Context(), p, token)
	if err != nil {
		fail()
		return
	}

	linkTo := uuid.Nil
	if link == "1" {
		if c, err := r.Cookie(SessionCookieName); err == nil {
			if uid, err := o.sessions.UserID(r.Context(), c.Value); err == nil {
				linkTo = uid
			}
		}
		if linkTo == uuid.Nil {
			fail()
			return
		}
	}

	userID, err := o.CompleteLogin(r.Context(), provider, pu, linkTo)
	if err != nil {
		fail()
		return
	}
	cookie, err := o.sessions.Create(r.Context(), userID)
	if err != nil {
		fail()
		return
	}
	http.SetCookie(w, cookie)
	target := o.baseURL + "/"
	if link == "1" {
		target = o.baseURL + "/account?linked=" + provider
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (o *OAuth) fetchUser(ctx context.Context, p *ProviderConfig, token *oauth2.Token) (ProviderUser, error) {
	client := p.OAuth2.Client(ctx, token)
	resp, err := client.Get(p.UserInfoURL)
	if err != nil {
		return ProviderUser{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProviderUser{}, fmt.Errorf("userinfo status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ProviderUser{}, err
	}
	return p.ParseUser(body)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/auth/ -run TestCompleteLogin -v` → Expected: PASS.

- [ ] **Step 5: Mount routes and wire main**

In `backend/internal/platform/httpapi/api.go`, add to `Deps`:
```go
	OAuth http.Handler
```
and in `Build`, after huma registration:
```go
	if deps.OAuth != nil {
		router.Mount("/auth/oauth", deps.OAuth)
	}
```
(add `"net/http"` import if missing)

In `backend/cmd/server/main.go`, extend deps construction:
```go
		oauthProviders := map[string]*auth.ProviderConfig{}
		if cfg.Discord.ClientID != "" {
			oauthProviders["discord"] = auth.DiscordProvider(cfg.Discord, cfg.BaseURL+"/auth/oauth/discord/callback")
		}
		if cfg.Google.ClientID != "" {
			oauthProviders["google"] = auth.GoogleProvider(cfg.Google, cfg.BaseURL+"/auth/oauth/google/callback")
		}
		sessions := auth.NewSessions(queries, cfg.Secure())
		deps := httpapi.Deps{
			Auth:     auth.NewService(queries, mail.FromConfig(cfg), cfg.BaseURL),
			Sessions: sessions,
			Queries:  queries,
			OAuth:    auth.NewOAuth(queries, sessions, cfg.BaseURL, cfg.Secure(), oauthProviders).Routes(),
		}
```

- [ ] **Step 6: Verify full suite, commit**

Run: `cd backend && go build ./... && go test ./...` → Expected: PASS.

```bash
git add -A && git commit -m "feat: discord and google oauth login with account linking"
```

---

### Task 12: Frontend auth UI

**Files:**
- Create: `frontend/src/api/auth.ts`, `frontend/src/routes/login.tsx`, `frontend/src/routes/register.tsx`, `frontend/src/routes/verify-email.tsx`, `frontend/src/routes/forgot-password.tsx`, `frontend/src/routes/reset-password.tsx`, `frontend/src/routes/account.tsx`, `frontend/src/api/auth.test.tsx`
- Modify: `frontend/src/routes/__root.tsx` (nav shows login state)

**Interfaces:**
- Consumes: typed `client` (Task 4) with paths from Tasks 8–10; browser URLs `/auth/oauth/{provider}/start` (Task 11; plain `<a href>` links — dev note: OAuth redirect flows only work through the prod Caddy origin or with backend BASE_URL pointing at Vite, acceptable for now).
- Produces: hooks `useMe()`, `useLogin()`, `useLogout()` in `src/api/auth.ts`; routes `/login`, `/register`, `/verify-email?token=`, `/forgot-password`, `/reset-password?token=`, `/account`.

- [ ] **Step 1: Write the auth hooks**

`frontend/src/api/auth.ts`:
```ts
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { client } from "./client";
import type { components } from "./schema";

export type User = components["schemas"]["UserBody"];

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    retry: false,
    queryFn: async (): Promise<User | null> => {
      const { data, response } = await client.GET("/api/me");
      if (response.status === 401) return null;
      if (!data) throw new Error("failed to load current user");
      return data;
    },
  });
}

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: { email: string; password: string }) => {
      const { data, error } = await client.POST("/api/auth/login", { body });
      if (error) throw new Error(error.detail ?? "login failed");
      return data;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["me"] }),
  });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await client.POST("/api/auth/logout");
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["me"] }),
  });
}
```
**Note:** the exact schema type name (`UserBody`) comes from the generated `schema.d.ts` — huma names it after the Go struct. After `pnpm gen:api`, check `schema.d.ts` for the actual name under `components["schemas"]` and use that.

- [ ] **Step 2: Write a hook test (failing until pages exist is fine — this tests the hook)**

`frontend/src/api/auth.test.tsx`:
```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { expect, test, vi } from "vitest";
import { useMe } from "./auth";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

test("useMe returns null on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ title: "Unauthorized", status: 401 }), {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
      }),
    ),
  );
  const { result } = renderHook(() => useMe(), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toBeNull();
  vi.unstubAllGlobals();
});
```

Run: `cd frontend && pnpm test` → Expected: PASS (hook + stubbed fetch).

- [ ] **Step 3: Write the pages**

`frontend/src/routes/login.tsx`:
```tsx
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useLogin } from "../api/auth";

export const Route = createFileRoute("/login")({
  validateSearch: (s: Record<string, unknown>) => ({
    error: typeof s["error"] === "string" ? s["error"] : undefined,
  }),
  component: LoginPage,
});

function LoginPage() {
  const { error } = Route.useSearch();
  const login = useLogin();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  return (
    <main>
      <h1>Log in</h1>
      {error === "oauth" && <p role="alert">Social login failed. Try again.</p>}
      {login.isError && <p role="alert">{login.error.message}</p>}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          login.mutate({ email, password }, { onSuccess: () => void navigate({ to: "/" }) });
        }}
      >
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          Password
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </label>
        <button type="submit" disabled={login.isPending}>
          Log in
        </button>
      </form>
      <p>
        <a href="/auth/oauth/discord/start">Log in with Discord</a> ·{" "}
        <a href="/auth/oauth/google/start">Log in with Google</a>
      </p>
      <p>
        <Link to="/register">Register</Link> · <Link to="/forgot-password">Forgot password?</Link>
      </p>
    </main>
  );
}
```

`frontend/src/routes/register.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { client } from "../api/client";

export const Route = createFileRoute("/register")({ component: RegisterPage });

function RegisterPage() {
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [status, setStatus] = useState<"idle" | "sent" | "error">("idle");
  const [message, setMessage] = useState("");

  if (status === "sent") {
    return (
      <main>
        <h1>Check your inbox</h1>
        <p>We sent a verification link to {email}.</p>
      </main>
    );
  }

  return (
    <main>
      <h1>Register</h1>
      {status === "error" && <p role="alert">{message}</p>}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void (async () => {
            const { error } = await client.POST("/api/auth/register", {
              body: { email, displayName, password },
            });
            if (error) {
              setStatus("error");
              setMessage(error.detail ?? "registration failed");
            } else {
              setStatus("sent");
            }
          })();
        }}
      >
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <label>
          Display name
          <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} required />
        </label>
        <label>
          Password (min 8 characters)
          <input
            type="password"
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        <button type="submit">Register</button>
      </form>
    </main>
  );
}
```

`frontend/src/routes/verify-email.tsx`:
```tsx
import { useMutation } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect } from "react";
import { client } from "../api/client";

export const Route = createFileRoute("/verify-email")({
  validateSearch: (s: Record<string, unknown>) => ({
    token: typeof s["token"] === "string" ? s["token"] : "",
  }),
  component: VerifyEmailPage,
});

function VerifyEmailPage() {
  const { token } = Route.useSearch();
  const verify = useMutation({
    mutationFn: async (t: string) => {
      const { error } = await client.POST("/api/auth/verify-email", { body: { token: t } });
      if (error) throw new Error(error.detail ?? "verification failed");
    },
  });
  const mutate = verify.mutate;

  useEffect(() => {
    if (token) mutate(token);
  }, [token, mutate]);

  if (!token) return <p role="alert">Missing verification token.</p>;
  if (verify.isPending || verify.isIdle) return <p>Verifying…</p>;
  if (verify.isError) return <p role="alert">{verify.error.message}</p>;
  return (
    <main>
      <h1>Email verified</h1>
      <p>
        You can now <Link to="/login">log in</Link>.
      </p>
    </main>
  );
}
```

`frontend/src/routes/forgot-password.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { client } from "../api/client";

export const Route = createFileRoute("/forgot-password")({ component: ForgotPasswordPage });

function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);

  if (sent) {
    return (
      <main>
        <p>If an account exists for {email}, a reset link is on its way.</p>
      </main>
    );
  }
  return (
    <main>
      <h1>Forgot password</h1>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void client.POST("/api/auth/forgot-password", { body: { email } }).then(() => setSent(true));
        }}
      >
        <label>
          Email
          <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </label>
        <button type="submit">Send reset link</button>
      </form>
    </main>
  );
}
```

`frontend/src/routes/reset-password.tsx`:
```tsx
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { client } from "../api/client";

export const Route = createFileRoute("/reset-password")({
  validateSearch: (s: Record<string, unknown>) => ({
    token: typeof s["token"] === "string" ? s["token"] : "",
  }),
  component: ResetPasswordPage,
});

function ResetPasswordPage() {
  const { token } = Route.useSearch();
  const [password, setPassword] = useState("");
  const [state, setState] = useState<"idle" | "done" | "error">("idle");
  const [message, setMessage] = useState("");

  if (!token) return <p role="alert">Missing reset token.</p>;
  if (state === "done") {
    return (
      <main>
        <p>
          Password updated. <Link to="/login">Log in</Link>.
        </p>
      </main>
    );
  }
  return (
    <main>
      <h1>Reset password</h1>
      {state === "error" && <p role="alert">{message}</p>}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void (async () => {
            const { error } = await client.POST("/api/auth/reset-password", {
              body: { token, newPassword: password },
            });
            if (error) {
              setState("error");
              setMessage(error.detail ?? "reset failed");
            } else {
              setState("done");
            }
          })();
        }}
      >
        <label>
          New password (min 8 characters)
          <input
            type="password"
            minLength={8}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>
        <button type="submit">Set new password</button>
      </form>
    </main>
  );
}
```

`frontend/src/routes/account.tsx`:
```tsx
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMe } from "../api/auth";

export const Route = createFileRoute("/account")({ component: AccountPage });

const ALL_PROVIDERS = ["discord", "google"] as const;

function AccountPage() {
  const me = useMe();

  if (me.isPending) return <p>Loading…</p>;
  if (!me.data) {
    return (
      <p>
        You are not logged in. <Link to="/login">Log in</Link>
      </p>
    );
  }
  const linked = new Set(me.data.providers);
  return (
    <main>
      <h1>Account</h1>
      <p>
        {me.data.displayName} — {me.data.email}
      </p>
      <h2>Linked logins</h2>
      <ul>
        {ALL_PROVIDERS.map((p) => (
          <li key={p}>
            {p}: {linked.has(p) ? "linked" : <a href={`/auth/oauth/${p}/start?link=1`}>link now</a>}
          </li>
        ))}
      </ul>
    </main>
  );
}
```

Modify `frontend/src/routes/__root.tsx`:
```tsx
import { createRootRoute, Link, Outlet } from "@tanstack/react-router";
import { useLogout, useMe } from "../api/auth";

export const Route = createRootRoute({ component: RootLayout });

function RootLayout() {
  const me = useMe();
  const logout = useLogout();
  return (
    <>
      <nav>
        <Link to="/">Cube Planner</Link>{" "}
        {me.data ? (
          <>
            <Link to="/account">{me.data.displayName}</Link>{" "}
            <button type="button" onClick={() => logout.mutate()}>
              Log out
            </button>
          </>
        ) : (
          <Link to="/login">Log in</Link>
        )}
      </nav>
      <Outlet />
    </>
  );
}
```

- [ ] **Step 4: Verify**

Run: `cd frontend && pnpm lint && pnpm fmt && pnpm typecheck && pnpm test && pnpm build`
Expected: all pass.

Manual smoke (optional but recommended): start Postgres (`docker compose -f deploy/docker-compose.dev.yml up -d`), run backend with `DATABASE_URL=postgres://cube:cube@localhost:5432/cube?sslmode=disable BASE_URL=http://localhost:5173 go run ./cmd/server`, run `pnpm dev`, register at `http://localhost:5173/register`, copy the verification link from backend logs (log mailer), verify, log in — nav shows your display name.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: frontend auth pages and session-aware nav"
```

---

### Task 13: GitHub Actions CI

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: all package scripts and Go tests from prior tasks; `scripts/check-api-fresh.sh` (Task 4).

- [ ] **Step 1: Write the workflow**

`.github/workflows/ci.yml`:
```yaml
name: CI
on:
  push:
    branches: [main, master]
  pull_request:

jobs:
  backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
      - uses: golangci/golangci-lint-action@v8
        with:
          working-directory: backend
      - name: Test
        working-directory: backend
        run: go test ./...

  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: pnpm
      - run: pnpm install --frozen-lockfile
      - name: Lint
        working-directory: frontend
        run: pnpm lint
      - name: Format check
        working-directory: frontend
        run: pnpm fmt:check
      - name: Test
        working-directory: frontend
        run: pnpm test
      - name: Build (includes typecheck)
        working-directory: frontend
        run: pnpm build

  api-client-fresh:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
      - uses: pnpm/action-setup@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: pnpm
      - run: pnpm install --frozen-lockfile
      - name: Check generated client freshness
        run: pnpm check:api
```

- [ ] **Step 2: Verify locally what can be verified**

Run: `bash scripts/check-api-fresh.sh && cd backend && go test ./... && cd ../frontend && pnpm lint && pnpm fmt:check && pnpm test && pnpm build`
Expected: all pass (the workflow runs these same commands).

- [ ] **Step 3: Commit and push, watch CI**

```bash
git add -A && git commit -m "ci: lint, test, build, and api-client freshness on github actions"
```
If the repo has a GitHub remote, push and confirm all three jobs are green before proceeding (`gh run watch`). If it has no remote yet, note that in the task report and move on.

---

### Task 14: Production Docker + Caddy + deploy workflow

**Files:**
- Create: `backend/Dockerfile`, `frontend/Dockerfile`, `deploy/Caddyfile`, `deploy/docker-compose.prod.yml`, `deploy/.env.example`, `.github/workflows/deploy.yml`, `.dockerignore`

**Interfaces:**
- Consumes: everything.
- Produces: images `ghcr.io/<owner>/cube-planner-api` and `ghcr.io/<owner>/cube-planner-web`; a compose file the VPS runs. GitHub secrets required (documented in `.env.example` header): `VPS_HOST`, `VPS_USER`, `VPS_SSH_KEY`.

- [ ] **Step 1: Write Dockerfiles**

`.dockerignore`:
```
node_modules
dist
.git
```

`backend/Dockerfile`:
```dockerfile
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

`frontend/Dockerfile` (build context is the repo root — it needs the workspace):
```dockerfile
FROM node:22-alpine AS build
RUN corepack enable
WORKDIR /repo
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml ./
COPY frontend/package.json frontend/
RUN pnpm install --frozen-lockfile --filter @cube-planner/frontend
COPY frontend frontend
RUN pnpm --filter @cube-planner/frontend build

FROM caddy:2-alpine
COPY deploy/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /repo/frontend/dist /srv
```

- [ ] **Step 2: Caddyfile + prod compose**

`deploy/Caddyfile`:
```
{$DOMAIN:localhost}

handle /api/* {
	reverse_proxy api:8080
}

handle /auth/oauth/* {
	reverse_proxy api:8080
}

handle {
	root * /srv
	try_files {path} /index.html
	file_server
}
```

`deploy/docker-compose.prod.yml`:
```yaml
services:
  web:
    image: ghcr.io/${GHCR_OWNER}/cube-planner-web:latest
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    environment:
      DOMAIN: ${DOMAIN}
    volumes:
      - caddy_data:/data
    depends_on:
      - api

  api:
    image: ghcr.io/${GHCR_OWNER}/cube-planner-api:latest
    restart: unless-stopped
    environment:
      ENV: prod
      DATABASE_URL: postgres://cube:${POSTGRES_PASSWORD}@postgres:5432/cube?sslmode=disable
      BASE_URL: https://${DOMAIN}
      SMTP_HOST: ${SMTP_HOST}
      SMTP_PORT: ${SMTP_PORT}
      SMTP_USER: ${SMTP_USER}
      SMTP_PASS: ${SMTP_PASS}
      SMTP_FROM: ${SMTP_FROM}
      DISCORD_CLIENT_ID: ${DISCORD_CLIENT_ID}
      DISCORD_CLIENT_SECRET: ${DISCORD_CLIENT_SECRET}
      GOOGLE_CLIENT_ID: ${GOOGLE_CLIENT_ID}
      GOOGLE_CLIENT_SECRET: ${GOOGLE_CLIENT_SECRET}
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: cube
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: cube
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U cube"]
      interval: 5s
      timeout: 3s
      retries: 10

volumes:
  caddy_data:
  pgdata:
```

`deploy/.env.example`:
```bash
# Copy to /opt/cube-planner/.env on the VPS and fill in.
# GitHub Actions secrets needed for deploy.yml: VPS_HOST, VPS_USER, VPS_SSH_KEY.
GHCR_OWNER=mjabloniec
DOMAIN=cube.example.com
POSTGRES_PASSWORD=change-me
SMTP_HOST=
SMTP_PORT=587
SMTP_USER=
SMTP_PASS=
SMTP_FROM=cube-planner@example.com
DISCORD_CLIENT_ID=
DISCORD_CLIENT_SECRET=
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
```

- [ ] **Step 3: Deploy workflow**

`.github/workflows/deploy.yml`:
```yaml
name: Deploy
on:
  push:
    branches: [main, master]
  workflow_dispatch:

permissions:
  contents: read
  packages: write

jobs:
  build-push:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/build-push-action@v6
        with:
          context: backend
          push: true
          tags: ghcr.io/${{ github.repository_owner }}/cube-planner-api:latest
      - uses: docker/build-push-action@v6
        with:
          context: .
          file: frontend/Dockerfile
          push: true
          tags: ghcr.io/${{ github.repository_owner }}/cube-planner-web:latest

  deploy:
    needs: build-push
    runs-on: ubuntu-latest
    steps:
      - uses: appleboy/ssh-action@v1
        with:
          host: ${{ secrets.VPS_HOST }}
          username: ${{ secrets.VPS_USER }}
          key: ${{ secrets.VPS_SSH_KEY }}
          script: |
            cd /opt/cube-planner
            docker compose -f docker-compose.prod.yml pull
            docker compose -f docker-compose.prod.yml up -d
```

- [ ] **Step 4: Verify the stack locally**

Run (from repo root):
```bash
docker build -t cube-planner-api backend
docker build -t cube-planner-web -f frontend/Dockerfile .
GHCR_OWNER=local POSTGRES_PASSWORD=devpass DOMAIN=localhost docker compose -f deploy/docker-compose.prod.yml up -d --no-build 2>/dev/null || true
```
For the local check, temporarily point the two images at the locally built tags:
```bash
cd deploy && GHCR_OWNER=x POSTGRES_PASSWORD=devpass DOMAIN=localhost \
  docker compose -f docker-compose.prod.yml config > /dev/null && cd ..
docker network create cube-test 2>/dev/null || true
docker run -d --name cube-pg --network cube-test -e POSTGRES_USER=cube -e POSTGRES_PASSWORD=devpass -e POSTGRES_DB=cube postgres:17-alpine
sleep 5
docker run -d --name cube-api --network cube-test --network-alias api \
  -e ENV=prod -e DATABASE_URL='postgres://cube:devpass@cube-pg:5432/cube?sslmode=disable' -e BASE_URL='http://localhost:8088' \
  cube-planner-api
docker run -d --name cube-web --network cube-test -p 8088:80 -e DOMAIN=:80 cube-planner-web
sleep 2
curl -sf http://localhost:8088/api/healthz
docker rm -f cube-web cube-api cube-pg && docker network rm cube-test
```
Expected: `{"status":"ok"}` (the `$schema` field huma adds is also fine) before cleanup.

- [ ] **Step 5: Commit**

```bash
git add -A && git commit -m "feat: production docker images, caddy, compose, and deploy workflow"
```

---

## Final acceptance checklist (run after Task 14)

- [ ] `go test ./...` green in `backend/`
- [ ] `pnpm lint && pnpm fmt:check && pnpm typecheck && pnpm test && pnpm build` green in `frontend/`
- [ ] `pnpm check:api` green at root
- [ ] Dev flow works end-to-end: register → verification link in server log → verify → login → `/account` shows user
- [ ] Local prod-image smoke (Task 14 Step 4) returns healthz through Caddy
- [ ] CI green on GitHub (if remote configured)
