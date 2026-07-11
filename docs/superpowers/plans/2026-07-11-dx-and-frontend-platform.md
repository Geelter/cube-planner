# DX & Frontend Platform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Single-command local dev, Tailwind v4 + shadcn-style UI with token-driven dark mode, Paraglide PL/EN i18n, a11y defaults, codified directory structure — validated by restyling and translating the existing auth screens.

**Architecture:** Infra (Postgres + Mailpit) moves to a root `compose.yml` orchestrated by a Makefile; backend and frontend dev servers run on the host. The frontend is restructured into `app/ | shared/ | features/ | routes/` slices, gains Tailwind v4 with a two-tier semantic-token system (`data-theme` switching), copy-in UI primitives (cva + cn), and Paraglide-compiled typed messages. Generated artifacts (`routeTree.gen.ts`, `src/paraglide/`) leave version control.

**Tech Stack:** Go 1.25 / chi / huma (unchanged), Vite 8, React 19, TanStack Router/Query, Tailwind v4 (`@tailwindcss/vite`), shadcn-style components (cva, clsx, tailwind-merge, @radix-ui/react-slot, lucide-react), @inlang/paraglide-js, @tanstack/react-devtools, happy-dom, vitest-axe, oxlint/oxfmt, lefthook.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-11-dx-and-frontend-platform-design.md`. All work in `/Users/mateusz/projects/cube_planner`.
- **Do not change backend auth behavior.** The four adjudicated decisions (unverified-email overwrite on register; distinct 401/403 login errors; OAuth link+wipe; ErrEmailCollision rejection) and the OAuth empty-identity guard are user-decided spec.
- Backend Docker base image stays `gcr.io/distroless/static-debian12:nonroot`. The deploy workflow's `workflow_run.event == 'push'` guard and `DEPLOY_SHA` pattern must not be weakened.
- Frontend tsconfig strictness is untouchable: `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `verbatimModuleSyntax` (use `import type` where required).
- Tooling is oxlint + oxfmt only — never introduce eslint or prettier.
- Dependency direction: `app`/`routes` → `features` → `shared`. Never feature → feature, never shared → feature.
- Colors in components come only from semantic token utilities (`bg-surface`, `text-fg`, `text-fg-muted`, `border-border`, `bg-accent`, `text-accent-fg`, `bg-danger`, `text-danger-fg`, `bg-surface-raised`); raw palette utilities (`bg-zinc-*` etc.) are forbidden outside `src/app/styles.css`.
- Component variants are cva configs; conditional/merged classes always go through `cn()` from `@/shared/lib/cn`.
- After Task 8, no hardcoded user-facing strings in components — every display string is an `m.*()` call. Exception: backend RFC 7807 `detail` strings are displayed verbatim (spec §5).
- Generated files are never hand-edited or committed: `frontend/src/routeTree.gen.ts`, `frontend/src/paraglide/`. The generated API client under `frontend/src/shared/api/` (after Task 5) **stays tracked**.
- Test files inside `frontend/src/routes/` need a `-` filename prefix (route generation would otherwise pick them up). Outside `routes/`, no prefix.
- Commits: conventional style (`feat:`, `chore:`, `refactor:`, `docs:`, `test:`). Run the full affected suite once before each commit.
- Frontend commands run from `frontend/` (or via `pnpm --filter @cube-planner/frontend`); backend commands from `backend/`.

---

## File structure (end state)

```
cube_planner/
├── Makefile                        # NEW — primary task runner
├── compose.yml                     # NEW — local-only Postgres + Mailpit
├── .env.example                    # NEW — documents backend env vars
├── CLAUDE.md                       # NEW — agent guide
├── lefthook.yml                    # MODIFIED — fail_text + pre-push
├── docs/architecture/structure.md  # NEW — structure source of truth
├── deploy/                         # docker-compose.dev.yml DELETED; prod untouched
├── backend/
│   ├── Dockerfile                  # MODIFIED — cache mounts, ldflags
│   └── cmd/server/main.go          # MODIFIED — version vars + startup log
└── frontend/
    ├── .oxfmtrc.json               # NEW — ignorePatterns + sortTailwindcss
    ├── .oxlintrc.json              # NEW — jsx-a11y + ignorePatterns
    ├── components.json             # NEW — shadcn CLI config (future adds)
    ├── tsr.config.json             # NEW — tsr generate CLI config
    ├── project.inlang/settings.json# NEW — inlang project
    ├── messages/{en,pl}.json       # NEW — message catalogs
    ├── index.html                  # MODIFIED — theme pre-paint script, main path
    ├── vite.config.ts              # MODIFIED — tailwind, paraglide, alias, happy-dom
    ├── tsconfig.json               # MODIFIED — @/* paths
    └── src/
        ├── routeTree.gen.ts        # gitignored (generated)
        ├── paraglide/              # gitignored (generated)
        ├── app/                    # main.tsx, styles.css, app.test.tsx
        ├── shared/
        │   ├── api/                # client.ts, schema.d.ts, openapi.yaml (moved)
        │   ├── i18n/               # LanguageSwitcher.tsx (+ test)
        │   ├── lib/                # cn.ts, theme.ts (+ tests)
        │   └── ui/                 # button, input, label, card, alert, theme-toggle
        ├── features/auth/          # api.ts + components/ (6 screens + tests)
        └── routes/                 # thin route files only
```

---

### Task 1: Root compose.yml, Makefile, .env.example

**Files:**
- Create: `compose.yml`, `Makefile`, `.env.example`
- Delete: `deploy/docker-compose.dev.yml`

**Interfaces:**
- Produces: compose services `postgres` (cube/cube/cube on 5432, volume `cube-planner_pgdata`) and `mailpit` (SMTP 1025, UI 8025); make targets `up`, `down`, `ps`, `backend-dev`, `frontend-dev`, `db-psql`, `db-logs`, `db-reset`, `backend-test`, `backend-lint`, `frontend-test`, `frontend-typecheck`, `frontend-lint`, `test`, `api-generate`, `api-check`, `backend-image`. Task 2's Dockerfile build args are passed by `backend-image`.

- [ ] **Step 1: Create `compose.yml`** at the repo root:

```yaml
# LOCAL DEVELOPMENT ONLY. Credentials are intentionally hardcoded for
# convenience and MUST NOT be reused in any shared or production environment.
name: cube-planner
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
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U cube -d cube"]
      interval: 5s
      timeout: 5s
      retries: 5
  mailpit:
    image: axllent/mailpit:latest
    ports:
      - "1025:1025" # SMTP (backend -> mailpit)
      - "8025:8025" # Web UI: http://localhost:8025
volumes:
  pgdata:
```

- [ ] **Step 2: Verify compose file parses**

Run: `docker compose config -q`
Expected: exit 0, no output.

- [ ] **Step 3: Create `Makefile`** at the repo root (tabs for recipe indentation, not spaces):

```make
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
export DATABASE_URL SMTP_HOST SMTP_PORT

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
```

- [ ] **Step 4: Create `.env.example`** at the repo root:

```bash
# Backend configuration for local development.
#
# The Makefile already exports sane defaults matching compose.yml, so a fresh
# checkout needs no .env at all. Copy this to .env (gitignored) only to
# override something. `make backend-dev` picks it up automatically.

# Port the HTTP server listens on (default 8080).
# PORT=8080

# "dev" (default) or "prod". prod marks the session cookie Secure.
# ENV=dev

# Postgres connection string. Default matches the compose.yml postgres service.
# DATABASE_URL=postgres://cube:cube@localhost:5432/cube?sslmode=disable

# Public URL of the frontend, used in emailed links (default http://localhost:5173).
# BASE_URL=http://localhost:5173

# --- Mail ---------------------------------------------------------------
# Defaults point at the Mailpit compose service; read captured mail at
# http://localhost:8025. Mailpit needs no credentials.
# SMTP_HOST=localhost
# SMTP_PORT=1025
# SMTP_USER=
# SMTP_PASS=
# SMTP_FROM=cube-planner@localhost

# --- OAuth (optional locally; social login buttons fail without them) ----
# DISCORD_CLIENT_ID=
# DISCORD_CLIENT_SECRET=
# GOOGLE_CLIENT_ID=
# GOOGLE_CLIENT_SECRET=
```

- [ ] **Step 5: Delete the old dev compose file**

Run: `git rm deploy/docker-compose.dev.yml`

- [ ] **Step 6: Verify the Makefile works**

Run: `make help`
Expected: aligned list of all targets with descriptions.

Run: `make up` — wait for both dev servers to boot (backend logs on 8080, Vite on 5173), then Ctrl-C, then `make down`.
Expected: postgres and mailpit start healthy, backend connects and migrates, Vite serves. If Docker isn't available in your environment, verify instead with `make -n up` (prints the recipe) and note it in your report.

- [ ] **Step 7: Commit**

```bash
git add compose.yml Makefile .env.example
git commit -m "feat: single-command local dev stack (Makefile, root compose, Mailpit)"
```

---

### Task 2: Backend image — version metadata, cache mounts; deploy build args

**Files:**
- Modify: `backend/cmd/server/main.go` (add package-level version vars + startup log)
- Modify: `backend/Dockerfile` (full rewrite below)
- Modify: `.github/workflows/deploy.yml` (add `build-args` to the backend build step only)

**Interfaces:**
- Consumes: `make backend-image` from Task 1 passes `VERSION`, `GIT_COMMIT`, `BUILD_TIME` build args.
- Produces: ldflags-injectable `main.version`, `main.commit`, `main.buildTime`.

- [ ] **Step 1: Add version vars to `backend/cmd/server/main.go`**

Add at package level (near the existing package-level declarations):

```go
// Injected at build time via -ldflags (see backend/Dockerfile and Makefile).
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)
```

At the top of `main()`, before any existing logic, add (import `log/slog` if not already imported):

```go
slog.Info("cube-planner api", "version", version, "commit", commit, "buildTime", buildTime)
```

- [ ] **Step 2: Verify the backend still builds and tests pass**

Run: `cd backend && go build ./... && go vet ./... && go test ./internal/platform/...`
Expected: PASS (run the full `go test ./...` before committing).

- [ ] **Step 3: Rewrite `backend/Dockerfile`**:

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${GIT_COMMIT} -X main.buildTime=${BUILD_TIME}" \
      -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
```

- [ ] **Step 4: Verify the image builds with metadata**

Run: `make backend-image` (from repo root)
Expected: build succeeds. If Docker is unavailable, note it and rely on Step 2.

- [ ] **Step 5: Add build args to the backend build step in `.github/workflows/deploy.yml`**

Only the **first** `docker/build-push-action@v6` step (context: backend) gains:

```yaml
          build-args: |
            VERSION=${{ env.DEPLOY_SHA }}
            GIT_COMMIT=${{ env.DEPLOY_SHA }}
```

Do not touch the job `if:` guards, `DEPLOY_SHA` env, tags, or the frontend build step. (`BUILD_TIME` stays "unknown" in CI — not worth an extra step.)

- [ ] **Step 6: Validate the workflow**

Run: `actionlint .github/workflows/deploy.yml` (if actionlint is unavailable: `docker compose config -q`-style YAML sanity via `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/deploy.yml'))"`)
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add backend/cmd/server/main.go backend/Dockerfile .github/workflows/deploy.yml
git commit -m "feat: backend version metadata + Docker build cache mounts"
```

---

### Task 3: Untrack routeTree.gen.ts; generation before bare tsc

**Files:**
- Modify: `.gitignore`, `frontend/package.json`
- Create: `frontend/tsr.config.json`, `frontend/.oxfmtrc.json`
- Untrack: `frontend/src/routeTree.gen.ts`

**Interfaces:**
- Produces: `pnpm gen` script (Task 8 appends Paraglide compilation to it); `.oxfmtrc.json` (Task 6 adds `sortTailwindcss` to it).

- [ ] **Step 1: Add the router CLI and bump oxfmt**

Run: `pnpm --filter @cube-planner/frontend add -D @tanstack/router-cli oxfmt@latest`

(oxfmt must be recent enough for `.oxfmtrc.json` support — the config file + Tailwind sorting shipped ahead of the Feb 2026 beta.)

- [ ] **Step 2: Create `frontend/tsr.config.json`** (makes the CLI match the Vite plugin's defaults explicitly):

```json
{
  "routesDirectory": "./src/routes",
  "generatedRouteTree": "./src/routeTree.gen.ts"
}
```

- [ ] **Step 3: Update `frontend/package.json` scripts** — replace the `scripts` block with:

```json
  "scripts": {
    "dev": "vite",
    "gen": "tsr generate",
    "build": "pnpm gen && tsc --noEmit && vite build",
    "typecheck": "pnpm gen && tsc --noEmit",
    "test": "vitest run",
    "lint": "oxlint .",
    "fmt": "oxfmt .",
    "fmt:check": "oxfmt --check ."
  },
```

- [ ] **Step 4: Create `frontend/.oxfmtrc.json`** so oxfmt never touches generated output:

```json
{
  "ignorePatterns": ["src/routeTree.gen.ts", "src/paraglide/**"]
}
```

- [ ] **Step 5: Untrack the generated file**

Append to the repo-root `.gitignore`:

```
frontend/src/routeTree.gen.ts
```

Run: `git rm --cached frontend/src/routeTree.gen.ts`

- [ ] **Step 6: Verify regeneration + formatting hygiene**

Run (from `frontend/`): `rm src/routeTree.gen.ts && pnpm typecheck`
Expected: `tsr generate` recreates the file; tsc passes.

Run: `pnpm fmt:check`
Expected: passes even though the regenerated file is in the generator's own style (it's ignored).

Run: `pnpm test`
Expected: all existing tests pass (vitest's router plugin also regenerates the file).

If the oxfmt bump changed formatting of tracked files, run `pnpm fmt` and include those files in the commit.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "chore: untrack routeTree.gen.ts, generate before typecheck"
```

(The `git rm --cached` deletion of the tracked routeTree must be staged; `git add -A` covers it.)

---

### Task 4: happy-dom default + TanStack devtools

**Files:**
- Modify: `frontend/vite.config.ts`, `frontend/src/routes/__root.tsx`, `frontend/package.json` (deps)

**Interfaces:**
- Produces: vitest runs on happy-dom; `// @vitest-environment jsdom` is the documented per-file escape hatch (jsdom stays installed). Devtools render only in dev.

- [ ] **Step 1: Install**

Run: `pnpm --filter @cube-planner/frontend add -D happy-dom @tanstack/react-devtools @tanstack/react-router-devtools @tanstack/react-query-devtools`

- [ ] **Step 2: Switch the vitest environment** in `frontend/vite.config.ts`:

```ts
  test: {
    environment: "happy-dom",
    setupFiles: ["./src/vitest-setup.ts"],
  },
```

- [ ] **Step 3: Run the suite to prove happy-dom compatibility**

Run: `pnpm --filter @cube-planner/frontend test`
Expected: all 4 existing tests pass. If a test hits a happy-dom gap, add `// @vitest-environment jsdom` as the first line of that file and report it.

- [ ] **Step 4: Mount devtools in `frontend/src/routes/__root.tsx`**

Add imports and render the devtools after `<Outlet />`:

```tsx
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
```

```tsx
      <Outlet />
      {import.meta.env.DEV && (
        <TanStackDevtools
          plugins={[
            { name: "TanStack Router", render: <TanStackRouterDevtoolsPanel /> },
            { name: "TanStack Query", render: <ReactQueryDevtoolsPanel /> },
          ]}
        />
      )}
```

(If the `plugins` prop shape differs in the installed version, follow the package's README — keep the `import.meta.env.DEV` gate regardless.)

- [ ] **Step 5: Verify dev-only**

Run (from `frontend/`): `pnpm build && ! grep -rl "TanStackDevtools" dist/assets`
Expected: build succeeds and grep finds nothing (exit 0 overall). Then `pnpm test` still green.

- [ ] **Step 6: Commit**

```bash
git add -A frontend pnpm-lock.yaml
git commit -m "feat: happy-dom test env + TanStack devtools (dev-only)"
```

---

### Task 5: Restructure frontend into app/ shared/ features/ + @/ alias

**Files:**
- Modify: `frontend/tsconfig.json`, `frontend/vite.config.ts`, `frontend/index.html`, `scripts/gen-api.sh`, `scripts/check-api-fresh.sh` (path only), all files under `frontend/src/`
- Moves (git mv): `src/main.tsx → src/app/main.tsx`; `src/app.test.tsx → src/app/app.test.tsx`; `src/api/{client.ts,schema.d.ts,openapi.yaml} → src/shared/api/`; `src/api/auth.ts → src/features/auth/api.ts`; `src/api/auth.test.tsx → src/features/auth/api.test.tsx`; `src/routes/-verify-email.test.tsx → src/features/auth/components/VerifyEmailPage.test.tsx`
- Create: `src/features/auth/components/{LoginPage,RegisterPage,ForgotPasswordPage,ResetPasswordPage,VerifyEmailPage,AccountPage}.tsx` (extracted from route files)

**Interfaces:**
- Produces: `@/*` alias → `frontend/src/*`; page components exported by name (`LoginPage` etc.) from `@/features/auth/components/*`; auth hooks (`useMe`, `useLogin`, `useLogout`, `User` type) from `@/features/auth/api`; API client from `@/shared/api/client`. Route files stay thin. Later tasks import via these paths.

- [ ] **Step 1: Add the alias.** In `frontend/tsconfig.json` `compilerOptions`, add:

```json
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] },
```

In `frontend/vite.config.ts`, add a resolve alias (top-level in the config object):

```ts
import { fileURLToPath, URL } from "node:url";
// ...
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
```

- [ ] **Step 2: Move files with `git mv`** per the list above (create directories as needed). Update `frontend/index.html`'s script tag to `/src/app/main.tsx`. In `scripts/gen-api.sh` replace both `frontend/src/api` and `src/api` occurrences with the `shared/api` equivalents (`mkdir -p frontend/src/shared/api`, output paths `src/shared/api/openapi.yaml`, `src/shared/api/schema.d.ts`). In `scripts/check-api-fresh.sh` change the diff path to `frontend/src/shared/api`.

- [ ] **Step 3: Extract page components.** For each route file, move the page component function (unchanged markup/logic) into `src/features/auth/components/<Name>.tsx`, exporting it by name, and convert `Route.useSearch()` to the router-agnostic API. Example — `src/features/auth/components/LoginPage.tsx` starts:

```tsx
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useLogin } from "../api";

const route = getRouteApi("/login");

export function LoginPage() {
  const { error } = route.useSearch();
  // ... body unchanged from the current routes/login.tsx LoginPage ...
}
```

Same pattern: `ResetPasswordPage` and `VerifyEmailPage` use `getRouteApi("/reset-password")` / `getRouteApi("/verify-email")` for their `token` search param. `RegisterPage`, `ForgotPasswordPage` import `{ client } from "@/shared/api/client"`. `AccountPage` imports `{ useMe } from "../api"`. `VerifyEmailPage` keeps the `fired` ref guard exactly as-is.

- [ ] **Step 4: Rewrite the route files as thin wrappers.** Full contents:

`src/routes/login.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { LoginPage } from "@/features/auth/components/LoginPage";

export const Route = createFileRoute("/login")({
  validateSearch: (s: Record<string, unknown>): { error?: string } =>
    typeof s["error"] === "string" ? { error: s["error"] } : {},
  component: LoginPage,
});
```

`src/routes/register.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { RegisterPage } from "@/features/auth/components/RegisterPage";

export const Route = createFileRoute("/register")({ component: RegisterPage });
```

`src/routes/forgot-password.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { ForgotPasswordPage } from "@/features/auth/components/ForgotPasswordPage";

export const Route = createFileRoute("/forgot-password")({ component: ForgotPasswordPage });
```

`src/routes/reset-password.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { ResetPasswordPage } from "@/features/auth/components/ResetPasswordPage";

export const Route = createFileRoute("/reset-password")({
  validateSearch: (s: Record<string, unknown>) => ({
    token: typeof s["token"] === "string" ? s["token"] : "",
  }),
  component: ResetPasswordPage,
});
```

`src/routes/verify-email.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { VerifyEmailPage } from "@/features/auth/components/VerifyEmailPage";

export const Route = createFileRoute("/verify-email")({
  validateSearch: (s: Record<string, unknown>) => ({
    token: typeof s["token"] === "string" ? s["token"] : "",
  }),
  component: VerifyEmailPage,
});
```

`src/routes/account.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { AccountPage } from "@/features/auth/components/AccountPage";

export const Route = createFileRoute("/account")({ component: AccountPage });
```

`src/routes/index.tsx` and `src/routes/__root.tsx`: unchanged in this task except `__root.tsx`'s import `../api/auth` → `@/features/auth/api`.

- [ ] **Step 5: Fix remaining imports.** `src/features/auth/api.ts`: `./client` → `@/shared/api/client`, `./schema` → `@/shared/api/schema`. `src/app/main.tsx`: `./routeTree.gen` → `@/routeTree.gen`. Moved test files: update relative imports to `@/`-based ones (`api.test.tsx` → `./api` and `@/shared/api/client`; `app.test.tsx` → `@/routeTree.gen` etc.; `VerifyEmailPage.test.tsx` — its probe component mirrors the effect now living in `VerifyEmailPage.tsx`; update any imports/comments referencing the old path).

- [ ] **Step 6: Verify everything**

Run (from `frontend/`): `pnpm typecheck && pnpm test && pnpm lint && pnpm build`
Expected: all green.
Run (repo root): `pnpm check:api`
Expected: passes — the moved generated client is fresh at its new path.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: feature-sliced frontend layout (app/, shared/, features/auth/)"
```

---

### Task 6: Tailwind v4, semantic tokens, dark mode runtime

**Files:**
- Modify: `frontend/vite.config.ts`, `frontend/index.html`, `frontend/src/app/main.tsx`, `frontend/.oxfmtrc.json`
- Create: `frontend/src/app/styles.css`, `frontend/src/shared/lib/theme.ts`, `frontend/src/shared/lib/theme.test.ts`, `frontend/.oxlintrc.json`

**Interfaces:**
- Produces: semantic utilities `bg-surface`, `bg-surface-raised`, `text-fg`, `text-fg-muted`, `border-border`, `bg-accent`, `text-accent`, `text-accent-fg`, `bg-danger`, `text-danger`, `text-danger-fg`; `ThemeSetting = "light" | "dark" | "system"`; `getThemeSetting(): ThemeSetting`, `setThemeSetting(s: ThemeSetting): void`, `initTheme(): void` from `@/shared/lib/theme`. Task 9's ThemeToggle consumes these.

- [ ] **Step 1: Install**

Run: `pnpm --filter @cube-planner/frontend add -D tailwindcss @tailwindcss/vite`

- [ ] **Step 2: Register the Vite plugin** in `frontend/vite.config.ts`:

```ts
import tailwindcss from "@tailwindcss/vite";
// plugins: [tanstackRouter(...), react(), tailwindcss()],
```

- [ ] **Step 3: Create `frontend/src/app/styles.css`** — the only file allowed to reference raw palette colors:

```css
@import "tailwindcss";

/* dark: variant is an escape hatch — components should use semantic tokens. */
@custom-variant dark (&:where([data-theme="dark"], [data-theme="dark"] *));

/* Tier 2: semantic role tokens. Tier 1 is Tailwind's built-in palette
   (--color-zinc-*, --color-violet-*, --color-red-*). Themes assign
   primitives to roles; components only ever use the roles. */
:root {
  color-scheme: light;
  --surface: var(--color-zinc-50);
  --surface-raised: var(--color-white);
  --fg: var(--color-zinc-900);
  --fg-muted: var(--color-zinc-600);
  --border-color: var(--color-zinc-200);
  --accent: var(--color-violet-600);
  --accent-fg: var(--color-white);
  --danger: var(--color-red-600);
  --danger-fg: var(--color-white);
}

[data-theme="dark"] {
  color-scheme: dark;
  --surface: var(--color-zinc-950);
  --surface-raised: var(--color-zinc-900);
  --fg: var(--color-zinc-100);
  --fg-muted: var(--color-zinc-400);
  --border-color: var(--color-zinc-800);
  --accent: var(--color-violet-500);
  --accent-fg: var(--color-zinc-950);
  --danger: var(--color-red-500);
  --danger-fg: var(--color-zinc-950);
}

@theme inline {
  --color-surface: var(--surface);
  --color-surface-raised: var(--surface-raised);
  --color-fg: var(--fg);
  --color-fg-muted: var(--fg-muted);
  --color-border: var(--border-color);
  --color-accent: var(--accent);
  --color-accent-fg: var(--accent-fg);
  --color-danger: var(--danger);
  --color-danger-fg: var(--danger-fg);
}

body {
  @apply bg-surface text-fg antialiased;
}
```

- [ ] **Step 4: Write the failing theme test** — `frontend/src/shared/lib/theme.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getThemeSetting, initTheme, setThemeSetting } from "./theme";

type Listener = (e: { matches: boolean }) => void;

function stubMatchMedia(matches: boolean) {
  const listeners: Listener[] = [];
  const mql = {
    matches,
    addEventListener: (_: string, cb: Listener) => listeners.push(cb),
    removeEventListener: () => undefined,
  };
  vi.stubGlobal("matchMedia", () => mql);
  return {
    fireChange(next: boolean) {
      mql.matches = next;
      for (const cb of listeners) cb({ matches: next });
    },
  };
}

describe("theme", () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.unstubAllGlobals());

  it("defaults to system", () => {
    stubMatchMedia(false);
    expect(getThemeSetting()).toBe("system");
  });

  it("applies and persists an explicit setting", () => {
    stubMatchMedia(false);
    setThemeSetting("dark");
    expect(localStorage.getItem("theme")).toBe("dark");
    expect(document.documentElement.dataset["theme"]).toBe("dark");
    setThemeSetting("light");
    expect(document.documentElement.dataset["theme"]).toBe("light");
  });

  it("system resolves via prefers-color-scheme", () => {
    stubMatchMedia(true);
    setThemeSetting("system");
    expect(document.documentElement.dataset["theme"]).toBe("dark");
  });

  it("reacts to OS changes while in system mode", () => {
    const media = stubMatchMedia(false);
    initTheme();
    expect(document.documentElement.dataset["theme"]).toBe("light");
    media.fireChange(true);
    expect(document.documentElement.dataset["theme"]).toBe("dark");
  });

  it("ignores OS changes when set explicitly", () => {
    const media = stubMatchMedia(false);
    initTheme();
    setThemeSetting("light");
    media.fireChange(true);
    expect(document.documentElement.dataset["theme"]).toBe("light");
  });
});
```

- [ ] **Step 5: Run it to verify it fails**

Run: `pnpm --filter @cube-planner/frontend exec vitest run src/shared/lib/theme.test.ts`
Expected: FAIL — module `./theme` not found.

- [ ] **Step 6: Implement `frontend/src/shared/lib/theme.ts`**:

```ts
export type ThemeSetting = "light" | "dark" | "system";

const STORAGE_KEY = "theme";
const DARK_QUERY = "(prefers-color-scheme: dark)";

export function getThemeSetting(): ThemeSetting {
  const v = localStorage.getItem(STORAGE_KEY);
  return v === "light" || v === "dark" ? v : "system";
}

function apply(setting: ThemeSetting): void {
  const resolved =
    setting === "system" ? (matchMedia(DARK_QUERY).matches ? "dark" : "light") : setting;
  document.documentElement.dataset["theme"] = resolved;
}

export function setThemeSetting(setting: ThemeSetting): void {
  localStorage.setItem(STORAGE_KEY, setting);
  apply(setting);
}

export function initTheme(): void {
  apply(getThemeSetting());
  matchMedia(DARK_QUERY).addEventListener("change", () => {
    if (getThemeSetting() === "system") apply("system");
  });
}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `pnpm --filter @cube-planner/frontend exec vitest run src/shared/lib/theme.test.ts`
Expected: 5/5 PASS.

- [ ] **Step 8: Wire bootstrap.** In `frontend/src/app/main.tsx`, add at the top:

```tsx
import "./styles.css";
import { initTheme } from "@/shared/lib/theme";

initTheme();
```

In `frontend/index.html`, add as the **first child of `<head>`** (before any stylesheet) the pre-paint script:

```html
    <script>
      (function () {
        var t = localStorage.getItem("theme");
        var dark =
          t === "dark" ||
          ((t === null || t === "system") &&
            matchMedia("(prefers-color-scheme: dark)").matches);
        document.documentElement.dataset.theme = dark ? "dark" : "light";
      })();
    </script>
```

- [ ] **Step 9: Enable class sorting and jsx-a11y.** `frontend/.oxfmtrc.json` becomes:

```json
{
  "ignorePatterns": ["src/routeTree.gen.ts", "src/paraglide/**"],
  "sortTailwindcss": {
    "stylesheet": "./src/app/styles.css",
    "functions": ["cva", "cn", "clsx"]
  }
}
```

Create `frontend/.oxlintrc.json` (plugins list must repeat the defaults — setting it replaces them):

```json
{
  "plugins": ["react", "unicorn", "typescript", "oxc", "jsx-a11y"],
  "ignorePatterns": ["src/routeTree.gen.ts", "src/paraglide/**"]
}
```

- [ ] **Step 10: Full verify**

Run (from `frontend/`): `pnpm fmt && pnpm fmt:check && pnpm lint && pnpm typecheck && pnpm test && pnpm build`
Expected: all green (commit any files `pnpm fmt` re-sorted). Visually: `pnpm dev`, page background/text follow OS theme with no flash on reload.

- [ ] **Step 11: Commit**

```bash
git add -A
git commit -m "feat: tailwind v4 with semantic tokens and system-aware dark mode"
```

---

### Task 7: cn() helper + shared/ui primitives

**Files:**
- Create: `frontend/src/shared/lib/cn.ts`, `frontend/src/shared/ui/{button,input,label,card,alert}.tsx`, `frontend/src/shared/ui/button.test.tsx`, `frontend/components.json`

**Interfaces:**
- Consumes: semantic token utilities (Task 6), `@/` alias (Task 5).
- Produces: `cn(...inputs: ClassValue[]): string`; `<Button variant="default|outline|ghost|danger|link" size="default|sm|icon" asChild?>`; `<Input>`; `<Label htmlFor>`; `<Card><CardHeader><CardTitle as="h1|h2|h3"><CardContent><CardFooter>`; `<Alert variant="default|danger">` (renders `role="alert"`). Tasks 9–10 consume all of these.

- [ ] **Step 1: Install**

Run: `pnpm --filter @cube-planner/frontend add class-variance-authority clsx tailwind-merge @radix-ui/react-slot lucide-react`

- [ ] **Step 2: Create `frontend/src/shared/lib/cn.ts`**:

```ts
import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
```

- [ ] **Step 3: Create `frontend/components.json`** (config for future `shadcn add` runs; this task hand-writes the primitives):

```json
{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "new-york",
  "rsc": false,
  "tsx": true,
  "tailwind": {
    "config": "",
    "css": "src/app/styles.css",
    "baseColor": "zinc",
    "cssVariables": true
  },
  "aliases": {
    "components": "@/shared/ui",
    "ui": "@/shared/ui",
    "utils": "@/shared/lib/cn",
    "lib": "@/shared/lib",
    "hooks": "@/shared/hooks"
  },
  "iconLibrary": "lucide"
}
```

- [ ] **Step 4: Write the failing component test** — `frontend/src/shared/ui/button.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Button } from "./button";

describe("Button", () => {
  it("renders an accessible button with variant classes", () => {
    render(<Button variant="danger">Delete</Button>);
    const btn = screen.getByRole("button", { name: "Delete" });
    expect(btn.className).toContain("bg-danger");
  });

  it("lets consumer className win over defaults via cn()", () => {
    render(<Button className="h-12">Tall</Button>);
    const btn = screen.getByRole("button", { name: "Tall" });
    expect(btn.className).toContain("h-12");
    expect(btn.className).not.toContain("h-9");
  });

  it("renders as the child element with asChild", () => {
    render(
      <Button asChild>
        <a href="/x">Go</a>
      </Button>,
    );
    expect(screen.getByRole("link", { name: "Go" })).toBeInTheDocument();
  });
});
```

- [ ] **Step 5: Run it to verify it fails**

Run: `pnpm --filter @cube-planner/frontend exec vitest run src/shared/ui/button.test.tsx`
Expected: FAIL — `./button` not found.

- [ ] **Step 6: Create the primitives.**

`frontend/src/shared/ui/button.tsx`:
```tsx
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-accent text-accent-fg hover:bg-accent/90",
        outline: "border border-border bg-surface-raised text-fg hover:bg-surface",
        ghost: "text-fg hover:bg-surface-raised",
        danger: "bg-danger text-danger-fg hover:bg-danger/90",
        link: "text-accent underline-offset-4 hover:underline",
      },
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 px-3 text-xs",
        icon: "size-9",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

type ButtonProps = ComponentProps<"button"> &
  VariantProps<typeof buttonVariants> & { asChild?: boolean };

export function Button({ className, variant, size, asChild = false, ...props }: ButtonProps) {
  const Comp = asChild ? Slot : "button";
  return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
```

`frontend/src/shared/ui/input.tsx`:
```tsx
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function Input({ className, ...props }: ComponentProps<"input">) {
  return (
    <input
      className={cn(
        "h-9 w-full rounded-md border border-border bg-surface-raised px-3 py-1 text-sm text-fg placeholder:text-fg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}
```

`frontend/src/shared/ui/label.tsx`:
```tsx
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function Label({ className, ...props }: ComponentProps<"label">) {
  return <label className={cn("text-sm font-medium text-fg", className)} {...props} />;
}
```

`frontend/src/shared/ui/card.tsx`:
```tsx
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

export function Card({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      className={cn("rounded-xl border border-border bg-surface-raised shadow-sm", className)}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex flex-col gap-1.5 p-6", className)} {...props} />;
}

type CardTitleProps = ComponentProps<"h2"> & { as?: "h1" | "h2" | "h3" };

export function CardTitle({ className, as: Comp = "h2", ...props }: CardTitleProps) {
  return <Comp className={cn("text-lg leading-none font-semibold", className)} {...props} />;
}

export function CardContent({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("p-6 pt-0", className)} {...props} />;
}

export function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return <div className={cn("flex items-center p-6 pt-0", className)} {...props} />;
}
```

`frontend/src/shared/ui/alert.tsx`:
```tsx
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/cn";

const alertVariants = cva("w-full rounded-lg border px-4 py-3 text-sm", {
  variants: {
    variant: {
      default: "border-border bg-surface-raised text-fg",
      danger: "border-danger/50 bg-danger/10 text-danger",
    },
  },
  defaultVariants: { variant: "default" },
});

type AlertProps = ComponentProps<"div"> & VariantProps<typeof alertVariants>;

export function Alert({ className, variant, ...props }: AlertProps) {
  return <div role="alert" className={cn(alertVariants({ variant }), className)} {...props} />;
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `pnpm --filter @cube-planner/frontend exec vitest run src/shared/ui/button.test.tsx`
Expected: 3/3 PASS.

- [ ] **Step 8: Full verify + commit**

Run (from `frontend/`): `pnpm fmt && pnpm lint && pnpm typecheck && pnpm test`

```bash
git add -A
git commit -m "feat: shared/ui primitives (button, input, label, card, alert) with cva + cn"
```

---

### Task 8: Paraglide i18n foundation

**Files:**
- Create: `frontend/project.inlang/settings.json`, `frontend/messages/en.json`, `frontend/messages/pl.json`
- Modify: `frontend/vite.config.ts`, `frontend/package.json` (dep + `gen` script), `.gitignore`, `frontend/src/app/main.tsx`

**Interfaces:**
- Produces: typed messages `m.<key>()` from `@/paraglide/messages`; runtime `getLocale()`, `setLocale(locale)`, `locales` from `@/paraglide/runtime`. Locale resolution: localStorage → browser language → `en`. `setLocale` persists and reloads the page (Paraglide default) — the whole app re-renders in the new locale; no reactive re-render plumbing needed. Tasks 9–10 consume `m` and the runtime.

- [ ] **Step 1: Install**

Run: `pnpm --filter @cube-planner/frontend add -D @inlang/paraglide-js`

- [ ] **Step 2: Create `frontend/project.inlang/settings.json`**:

```json
{
  "$schema": "https://inlang.com/schema/project-settings",
  "baseLocale": "en",
  "locales": ["en", "pl"],
  "modules": [
    "https://cdn.jsdelivr.net/npm/@inlang/plugin-message-format@latest/dist/index.js"
  ],
  "plugin.inlang.messageFormat": { "pathPattern": "./messages/{locale}.json" }
}
```

- [ ] **Step 3: Create the message catalogs.**

`frontend/messages/en.json`:
```json
{
  "$schema": "https://inlang.com/schema/inlang-message-format",
  "app_name": "Cube Planner",
  "loading": "Loading…",
  "error_generic": "Something went wrong. Try again.",
  "nav_login": "Log in",
  "nav_logout": "Log out",
  "lang_label": "Language",
  "theme_light": "Light theme",
  "theme_dark": "Dark theme",
  "theme_system": "System theme",
  "field_email": "Email",
  "field_password": "Password",
  "field_display_name": "Display name",
  "field_password_min": "Password (min 8 characters)",
  "field_new_password_min": "New password (min 8 characters)",
  "login_title": "Log in",
  "login_submit": "Log in",
  "login_error_oauth": "Social login failed. Try again.",
  "login_error_email_taken": "That email is already registered. Log in and link accounts from your account page.",
  "login_with_discord": "Log in with Discord",
  "login_with_google": "Log in with Google",
  "login_register_link": "Register",
  "login_forgot_link": "Forgot password?",
  "register_title": "Register",
  "register_submit": "Register",
  "register_error_fallback": "Registration failed.",
  "register_sent_title": "Check your inbox",
  "register_sent_body": "We sent a verification link to {email}.",
  "forgot_title": "Forgot password",
  "forgot_submit": "Send reset link",
  "forgot_sent": "If an account exists for {email}, a reset link is on its way.",
  "reset_title": "Reset password",
  "reset_submit": "Set new password",
  "reset_missing_token": "Missing reset token.",
  "reset_done": "Password updated.",
  "reset_error_fallback": "Reset failed.",
  "verify_missing_token": "Missing verification token.",
  "verify_pending": "Verifying…",
  "verify_done_title": "Email verified",
  "verify_done_body": "You can now log in.",
  "account_title": "Account",
  "account_not_logged_in": "You are not logged in.",
  "account_linked_title": "Linked logins",
  "account_linked": "linked",
  "account_link_now": "link now"
}
```

`frontend/messages/pl.json`:
```json
{
  "$schema": "https://inlang.com/schema/inlang-message-format",
  "app_name": "Cube Planner",
  "loading": "Wczytywanie…",
  "error_generic": "Coś poszło nie tak. Spróbuj ponownie.",
  "nav_login": "Zaloguj się",
  "nav_logout": "Wyloguj się",
  "lang_label": "Język",
  "theme_light": "Motyw jasny",
  "theme_dark": "Motyw ciemny",
  "theme_system": "Motyw systemowy",
  "field_email": "E-mail",
  "field_password": "Hasło",
  "field_display_name": "Nazwa wyświetlana",
  "field_password_min": "Hasło (min. 8 znaków)",
  "field_new_password_min": "Nowe hasło (min. 8 znaków)",
  "login_title": "Logowanie",
  "login_submit": "Zaloguj się",
  "login_error_oauth": "Logowanie przez konto zewnętrzne nie powiodło się. Spróbuj ponownie.",
  "login_error_email_taken": "Ten adres e-mail jest już zarejestrowany. Zaloguj się i połącz konta na stronie konta.",
  "login_with_discord": "Zaloguj się przez Discorda",
  "login_with_google": "Zaloguj się przez Google",
  "login_register_link": "Zarejestruj się",
  "login_forgot_link": "Nie pamiętasz hasła?",
  "register_title": "Rejestracja",
  "register_submit": "Zarejestruj się",
  "register_error_fallback": "Rejestracja nie powiodła się.",
  "register_sent_title": "Sprawdź skrzynkę",
  "register_sent_body": "Wysłaliśmy link weryfikacyjny na adres {email}.",
  "forgot_title": "Odzyskiwanie hasła",
  "forgot_submit": "Wyślij link do resetu",
  "forgot_sent": "Jeśli konto dla adresu {email} istnieje, link do resetu hasła jest w drodze.",
  "reset_title": "Ustaw nowe hasło",
  "reset_submit": "Ustaw nowe hasło",
  "reset_missing_token": "Brak tokenu resetu hasła.",
  "reset_done": "Hasło zostało zmienione.",
  "reset_error_fallback": "Reset hasła nie powiódł się.",
  "verify_missing_token": "Brak tokenu weryfikacyjnego.",
  "verify_pending": "Weryfikowanie…",
  "verify_done_title": "E-mail zweryfikowany",
  "verify_done_body": "Możesz się teraz zalogować.",
  "account_title": "Konto",
  "account_not_logged_in": "Nie jesteś zalogowany(-a).",
  "account_linked_title": "Połączone logowania",
  "account_linked": "połączone",
  "account_link_now": "połącz teraz"
}
```

- [ ] **Step 4: Wire the Vite plugin** in `frontend/vite.config.ts`:

```ts
import { paraglideVitePlugin } from "@inlang/paraglide-js";
// plugins: [ ...existing,
//   paraglideVitePlugin({
//     project: "./project.inlang",
//     outdir: "./src/paraglide",
//     strategy: ["localStorage", "preferredLanguage", "baseLocale"],
//   }),
// ],
```

(If the import path differs in the installed version — some versions export from `@inlang/paraglide-js/vite` — use what the package README says; the options are the same.)

- [ ] **Step 5: Generation + ignore wiring.**

- `frontend/package.json`: `"gen": "tsr generate && paraglide-js compile --project ./project.inlang --outdir ./src/paraglide"`
- Repo `.gitignore`: add `frontend/src/paraglide/`
- (`.oxfmtrc.json`/`.oxlintrc.json` already ignore `src/paraglide/**` from Tasks 3/6.)

- [ ] **Step 6: Sync `<html lang>`.** In `frontend/src/app/main.tsx` after `initTheme()`:

```tsx
import { getLocale } from "@/paraglide/runtime";

document.documentElement.lang = getLocale();
```

Also set `lang="en"` on the `<html>` tag in `frontend/index.html` as the pre-hydration default.

- [ ] **Step 7: Write the copy-flip test** — `frontend/src/shared/i18n/messages.test.ts` (proves locale switching changes rendered copy; `overwriteGetLocale` is exported by the generated runtime for tests):

```ts
import { afterEach, describe, expect, it } from "vitest";
import { m } from "@/paraglide/messages";
import { overwriteGetLocale } from "@/paraglide/runtime";

describe("message catalogs", () => {
  afterEach(() => overwriteGetLocale(() => "en"));

  it("renders per-locale copy", () => {
    expect(m.login_title()).toBe("Log in");
    overwriteGetLocale(() => "pl");
    expect(m.login_title()).toBe("Logowanie");
  });

  it("interpolates parameters", () => {
    overwriteGetLocale(() => "pl");
    expect(m.register_sent_body({ email: "a@b.c" })).toContain("a@b.c");
  });
});
```

- [ ] **Step 8: Verify compilation end to end**

Run (from `frontend/`): `pnpm gen`
Expected: `src/paraglide/` appears with `messages.js`/`runtime.js` (+ `.d.ts`); `git status` shows no new tracked files.

Run: `pnpm typecheck && pnpm test && pnpm build`
Expected: green, including the new messages test.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat: paraglide i18n foundation (en/pl catalogs, typed messages)"
```

---

### Task 9: App shell — header, LanguageSwitcher, ThemeToggle, focus management

**Files:**
- Create: `frontend/src/shared/i18n/LanguageSwitcher.tsx`, `frontend/src/shared/i18n/LanguageSwitcher.test.tsx`, `frontend/src/shared/ui/theme-toggle.tsx`, `frontend/src/shared/ui/theme-toggle.test.tsx`
- Modify: `frontend/src/routes/__root.tsx`

**Interfaces:**
- Consumes: `Button` (Task 7), theme API (Task 6), `m`/`getLocale`/`setLocale`/`locales` (Task 8), auth hooks (Task 5).
- Produces: the app shell all routes render inside: sticky header + `<main tabIndex={-1}>` that receives focus on route change.

- [ ] **Step 1: Write the failing LanguageSwitcher test** — `frontend/src/shared/i18n/LanguageSwitcher.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LanguageSwitcher } from "./LanguageSwitcher";

const setLocale = vi.fn();
vi.mock("@/paraglide/runtime", () => ({
  locales: ["en", "pl"],
  getLocale: () => "en",
  setLocale: (l: string) => setLocale(l),
}));

describe("LanguageSwitcher", () => {
  it("marks the active locale and switches on click", async () => {
    render(<LanguageSwitcher />);
    const en = screen.getByRole("button", { name: "EN" });
    const pl = screen.getByRole("button", { name: "PL" });
    expect(en).toHaveAttribute("aria-pressed", "true");
    expect(pl).toHaveAttribute("aria-pressed", "false");
    await userEvent.click(pl);
    expect(setLocale).toHaveBeenCalledWith("pl");
  });
});
```

- [ ] **Step 2: Run to verify it fails**, then create `frontend/src/shared/i18n/LanguageSwitcher.tsx`:

```tsx
import { m } from "@/paraglide/messages";
import { getLocale, locales, setLocale } from "@/paraglide/runtime";
import { Button } from "@/shared/ui/button";

export function LanguageSwitcher() {
  const current = getLocale();
  return (
    <div role="group" aria-label={m.lang_label()} className="flex gap-1">
      {locales.map((locale) => (
        <Button
          key={locale}
          variant={locale === current ? "outline" : "ghost"}
          size="sm"
          aria-pressed={locale === current}
          onClick={() => setLocale(locale)}
        >
          {locale.toUpperCase()}
        </Button>
      ))}
    </div>
  );
}
```

Run the test again. Expected: PASS. (`setLocale` reloads the page in the real app; the mock keeps the test hermetic.)

- [ ] **Step 3: Write the failing ThemeToggle test** — `frontend/src/shared/ui/theme-toggle.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ThemeToggle } from "./theme-toggle";

describe("ThemeToggle", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.stubGlobal("matchMedia", () => ({
      matches: false,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }));
  });

  it("cycles system -> light -> dark -> system and persists", async () => {
    render(<ThemeToggle />);
    const btn = screen.getByRole("button");
    await userEvent.click(btn); // system -> light
    expect(localStorage.getItem("theme")).toBe("light");
    expect(document.documentElement.dataset["theme"]).toBe("light");
    await userEvent.click(btn); // light -> dark
    expect(localStorage.getItem("theme")).toBe("dark");
    expect(document.documentElement.dataset["theme"]).toBe("dark");
    await userEvent.click(btn); // dark -> system
    expect(localStorage.getItem("theme")).toBe("system");
  });
});
```

- [ ] **Step 4: Run to verify it fails**, then create `frontend/src/shared/ui/theme-toggle.tsx`:

```tsx
import { Monitor, Moon, Sun } from "lucide-react";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { getThemeSetting, setThemeSetting, type ThemeSetting } from "@/shared/lib/theme";
import { Button } from "@/shared/ui/button";

const NEXT: Record<ThemeSetting, ThemeSetting> = {
  system: "light",
  light: "dark",
  dark: "system",
};

const LABEL: Record<ThemeSetting, () => string> = {
  light: m.theme_light,
  dark: m.theme_dark,
  system: m.theme_system,
};

export function ThemeToggle() {
  const [setting, setSetting] = useState<ThemeSetting>(getThemeSetting);
  const Icon = setting === "light" ? Sun : setting === "dark" ? Moon : Monitor;
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label={LABEL[setting]()}
      title={LABEL[setting]()}
      onClick={() => {
        const next = NEXT[setting];
        setThemeSetting(next);
        setSetting(next);
      }}
    >
      <Icon aria-hidden className="size-4" />
    </Button>
  );
}
```

Run the test again. Expected: PASS.

- [ ] **Step 5: Rewrite `frontend/src/routes/__root.tsx`** as the styled shell (devtools block from Task 4 stays):

```tsx
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { createRootRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { useEffect, useRef } from "react";
import { useLogout, useMe } from "@/features/auth/api";
import { m } from "@/paraglide/messages";
import { LanguageSwitcher } from "@/shared/i18n/LanguageSwitcher";
import { Button } from "@/shared/ui/button";
import { ThemeToggle } from "@/shared/ui/theme-toggle";

export const Route = createRootRoute({ component: RootLayout });

function RootLayout() {
  const me = useMe();
  const logout = useLogout();
  const mainRef = useRef<HTMLElement>(null);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const firstRender = useRef(true);

  // A11y: move focus to the page content on route change so screen readers
  // announce the new page instead of staying on the clicked link.
  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    mainRef.current?.focus();
  }, [pathname]);

  return (
    <div className="min-h-svh">
      <header className="border-b border-border bg-surface-raised">
        <div className="mx-auto flex h-14 max-w-4xl items-center justify-between gap-4 px-4">
          <Link to="/" className="font-semibold text-fg hover:text-accent">
            {m.app_name()}
          </Link>
          <div className="flex items-center gap-2">
            {me.data ? (
              <>
                <Button asChild variant="ghost" size="sm">
                  <Link to="/account">{me.data.displayName}</Link>
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => logout.mutate()}
                >
                  {m.nav_logout()}
                </Button>
              </>
            ) : (
              <Button asChild variant="outline" size="sm">
                <Link to="/login">{m.nav_login()}</Link>
              </Button>
            )}
            <LanguageSwitcher />
            <ThemeToggle />
          </div>
        </div>
      </header>
      <main ref={mainRef} tabIndex={-1} className="mx-auto max-w-4xl px-4 py-8 outline-none">
        <Outlet />
      </main>
      {import.meta.env.DEV && (
        <TanStackDevtools
          plugins={[
            { name: "TanStack Router", render: <TanStackRouterDevtoolsPanel /> },
            { name: "TanStack Query", render: <ReactQueryDevtoolsPanel /> },
          ]}
        />
      )}
    </div>
  );
}
```

Note: the page components still render their own `<main>` until Task 10 — that's a temporary invalid nesting fixed there; don't fix screens in this task.

- [ ] **Step 6: Verify**

Run (from `frontend/`): `pnpm fmt && pnpm lint && pnpm typecheck && pnpm test`
Expected: green (app.test may need its expectations updated if it asserted on the old nav markup — adapt assertions, keep the behavior it verifies).

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: app shell with language switcher, theme toggle, focus management"
```

---

### Task 10: Restyle + translate auth screens; axe smoke tests

**Files:**
- Modify: all 6 files in `frontend/src/features/auth/components/`, `frontend/src/routes/index.tsx`, `frontend/src/vitest-setup.ts`
- Create: `frontend/src/features/auth/components/a11y.test.tsx`

**Interfaces:**
- Consumes: everything from Tasks 5–9.
- Produces: fully styled, translated auth screens. Screens render a `<div>` wrapper (the shell owns `<main>`). Every input has `<Label htmlFor>` + `id`; errors use `<Alert variant="danger">`.

Shared layout idiom for the form screens (exact classes):
outer `<div className="mx-auto w-full max-w-sm">`, `<Card>` > `<CardHeader><CardTitle as="h1">…` + `<CardContent className="flex flex-col gap-4">`, forms `className="flex flex-col gap-4"`, each field `<div className="flex flex-col gap-1.5">`.

- [ ] **Step 1: Register vitest-axe.**

Run: `pnpm --filter @cube-planner/frontend add -D vitest-axe`

Append to `frontend/src/vitest-setup.ts`:

```ts
import "vitest-axe/extend-expect";
```

- [ ] **Step 2: Replace hardcoded error fallbacks in `src/features/auth/api.ts`.** In `useLogin`, change `new Error(error.detail ?? "login failed")` to `new Error(error.detail ?? m.error_generic())` (import `{ m } from "@/paraglide/messages"`). Backend `detail` strings stay verbatim per spec.

- [ ] **Step 3: Rewrite the screens.** Full contents:

`LoginPage.tsx`:
```tsx
import { getRouteApi, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { useLogin } from "../api";

const route = getRouteApi("/login");

export function LoginPage() {
  const { error } = route.useSearch();
  const login = useLogin();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");

  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.login_title()}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {error === "oauth" && <Alert variant="danger">{m.login_error_oauth()}</Alert>}
          {error === "email-taken" && (
            <Alert variant="danger">{m.login_error_email_taken()}</Alert>
          )}
          {error !== undefined && error !== "oauth" && error !== "email-taken" && (
            <Alert variant="danger">{m.error_generic()}</Alert>
          )}
          {login.isError && <Alert variant="danger">{login.error.message}</Alert>}
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              login.mutate({ email, password }, { onSuccess: () => void navigate({ to: "/" }) });
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="login-email">{m.field_email()}</Label>
              <Input
                id="login-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="login-password">{m.field_password()}</Label>
              <Input
                id="login-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <Button type="submit" disabled={login.isPending}>
              {m.login_submit()}
            </Button>
          </form>
          <p className="text-sm text-fg-muted">
            <a className="text-accent hover:underline" href="/auth/oauth/discord/start">
              {m.login_with_discord()}
            </a>
            {" · "}
            <a className="text-accent hover:underline" href="/auth/oauth/google/start">
              {m.login_with_google()}
            </a>
          </p>
          <p className="text-sm text-fg-muted">
            <Link className="text-accent hover:underline" to="/register">
              {m.login_register_link()}
            </Link>
            {" · "}
            <Link className="text-accent hover:underline" to="/forgot-password">
              {m.login_forgot_link()}
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
```

`RegisterPage.tsx`:
```tsx
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";

export function RegisterPage() {
  const [email, setEmail] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [status, setStatus] = useState<"idle" | "sent" | "error">("idle");
  const [message, setMessage] = useState("");

  if (status === "sent") {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Card>
          <CardHeader>
            <CardTitle as="h1">{m.register_sent_title()}</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-fg-muted">{m.register_sent_body({ email })}</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.register_title()}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {status === "error" && <Alert variant="danger">{message}</Alert>}
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              void (async () => {
                const { error } = await client.POST("/api/auth/register", {
                  body: { email, displayName, password },
                });
                if (error) {
                  setStatus("error");
                  setMessage(error.detail ?? m.register_error_fallback());
                } else {
                  setStatus("sent");
                }
              })();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="register-email">{m.field_email()}</Label>
              <Input
                id="register-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="register-display-name">{m.field_display_name()}</Label>
              <Input
                id="register-display-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                required
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="register-password">{m.field_password_min()}</Label>
              <Input
                id="register-password"
                type="password"
                minLength={8}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <Button type="submit">{m.register_submit()}</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
```

`ForgotPasswordPage.tsx`:
```tsx
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";

export function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);

  if (sent) {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm text-fg-muted">{m.forgot_sent({ email })}</p>
          </CardContent>
        </Card>
      </div>
    );
  }
  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.forgot_title()}</CardTitle>
        </CardHeader>
        <CardContent>
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              void client
                .POST("/api/auth/forgot-password", { body: { email } })
                .then(() => setSent(true));
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="forgot-email">{m.field_email()}</Label>
              <Input
                id="forgot-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <Button type="submit">{m.forgot_submit()}</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
```

`ResetPasswordPage.tsx`:
```tsx
import { getRouteApi, Link } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";

const route = getRouteApi("/reset-password");

export function ResetPasswordPage() {
  const { token } = route.useSearch();
  const [password, setPassword] = useState("");
  const [state, setState] = useState<"idle" | "done" | "error">("idle");
  const [message, setMessage] = useState("");

  if (!token) {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Alert variant="danger">{m.reset_missing_token()}</Alert>
      </div>
    );
  }
  if (state === "done") {
    return (
      <div className="mx-auto w-full max-w-sm">
        <Card>
          <CardContent className="pt-6">
            <p className="text-sm">
              {m.reset_done()}{" "}
              <Link className="text-accent hover:underline" to="/login">
                {m.nav_login()}
              </Link>
            </p>
          </CardContent>
        </Card>
      </div>
    );
  }
  return (
    <div className="mx-auto w-full max-w-sm">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.reset_title()}</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {state === "error" && <Alert variant="danger">{message}</Alert>}
          <form
            className="flex flex-col gap-4"
            onSubmit={(e) => {
              e.preventDefault();
              void (async () => {
                const { error } = await client.POST("/api/auth/reset-password", {
                  body: { token, newPassword: password },
                });
                if (error) {
                  setState("error");
                  setMessage(error.detail ?? m.reset_error_fallback());
                } else {
                  setState("done");
                }
              })();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="reset-password">{m.field_new_password_min()}</Label>
              <Input
                id="reset-password"
                type="password"
                minLength={8}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <Button type="submit">{m.reset_submit()}</Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
```

`VerifyEmailPage.tsx` (the effect + `fired` ref stay **exactly** as-is — that shape is regression-tested; only the error fallback string becomes a message call):
```tsx
import { useMutation } from "@tanstack/react-query";
import { getRouteApi, Link } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import { Alert } from "@/shared/ui/alert";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";

const route = getRouteApi("/verify-email");

export function VerifyEmailPage() {
  const { token } = route.useSearch();
  const verify = useMutation({
    mutationFn: async (t: string) => {
      const { error } = await client.POST("/api/auth/verify-email", { body: { token: t } });
      if (error) throw new Error(error.detail ?? m.error_generic());
    },
  });
  const mutate = verify.mutate;
  const fired = useRef(false);

  useEffect(() => {
    if (!token || fired.current) return;
    fired.current = true;
    mutate(token);
  }, [token, mutate]);

  const wrap = "mx-auto w-full max-w-sm";
  if (!token) {
    return (
      <div className={wrap}>
        <Alert variant="danger">{m.verify_missing_token()}</Alert>
      </div>
    );
  }
  if (verify.isPending || verify.isIdle) {
    return (
      <div className={wrap}>
        <p className="text-sm text-fg-muted">{m.verify_pending()}</p>
      </div>
    );
  }
  if (verify.isError) {
    return (
      <div className={wrap}>
        <Alert variant="danger">{verify.error.message}</Alert>
      </div>
    );
  }
  return (
    <div className={wrap}>
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.verify_done_title()}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm">
            {m.verify_done_body()}{" "}
            <Link className="text-accent hover:underline" to="/login">
              {m.nav_login()}
            </Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
}
```

`AccountPage.tsx`:
```tsx
import { Link } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { Card, CardContent, CardHeader, CardTitle } from "@/shared/ui/card";
import { useMe } from "../api";

const ALL_PROVIDERS = ["discord", "google"] as const;

export function AccountPage() {
  const me = useMe();

  if (me.isPending) return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  if (!me.data) {
    return (
      <p className="text-sm">
        {m.account_not_logged_in()}{" "}
        <Link className="text-accent hover:underline" to="/login">
          {m.nav_login()}
        </Link>
      </p>
    );
  }
  const linked = new Set(me.data.providers ?? []);
  return (
    <div className="mx-auto w-full max-w-md">
      <Card>
        <CardHeader>
          <CardTitle as="h1">{m.account_title()}</CardTitle>
          <p className="text-sm text-fg-muted">
            {me.data.displayName} — {me.data.email}
          </p>
        </CardHeader>
        <CardContent>
          <h2 className="mb-2 text-sm font-semibold">{m.account_linked_title()}</h2>
          <ul className="flex flex-col gap-1 text-sm">
            {ALL_PROVIDERS.map((p) => (
              <li key={p} className="capitalize">
                {p}:{" "}
                {linked.has(p) ? (
                  m.account_linked()
                ) : (
                  <a
                    className="text-accent normal-case hover:underline"
                    href={`/auth/oauth/${p}/start?link=1`}
                  >
                    {m.account_link_now()}
                  </a>
                )}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  );
}
```

`frontend/src/routes/index.tsx`:
```tsx
import { createFileRoute } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";

export const Route = createFileRoute("/")({
  component: () => <h1 className="text-2xl font-semibold">{m.app_name()}</h1>,
});
```

- [ ] **Step 4: Run the existing suite** — behavior must be preserved.

Run (from `frontend/`): `pnpm test`
Expected: green. If a test asserted on English strings now behind `m.*`, the base locale (en) still renders the same English copy under vitest — failures indicate a real regression, not the translation.

- [ ] **Step 5: Write the axe smoke test** — `frontend/src/features/auth/components/a11y.test.tsx`. First line MUST be the jsdom pragma (axe-core is incompatible with happy-dom):

```tsx
// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";
import { axe } from "vitest-axe";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

const PATHS = [
  "/",
  "/login",
  "/login?error=oauth",
  "/register",
  "/forgot-password",
  "/reset-password?token=t",
  "/verify-email?token=t",
  "/account",
];

describe("auth screens have no axe violations", () => {
  beforeEach(() => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("{}", { status: 401 })),
    );
  });

  for (const path of PATHS) {
    it(path, async () => {
      const router = createRouter({
        routeTree,
        history: createMemoryHistory({ initialEntries: [path] }),
      });
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      const { container } = render(
        <QueryClientProvider client={qc}>
          <RouterProvider router={router} />
        </QueryClientProvider>,
      );
      await router.load();
      expect(await axe(container)).toHaveNoViolations();
    });
  }
});
```

- [ ] **Step 6: Run it and fix violations**

Run: `pnpm --filter @cube-planner/frontend exec vitest run src/features/auth/components/a11y.test.tsx`
Expected: 8/8 PASS. If axe reports violations, fix the component markup (that is the point of the test) — do not filter rules. Document any fix in your report.

- [ ] **Step 7: Full verify**

Run (from `frontend/`): `pnpm fmt && pnpm lint && pnpm typecheck && pnpm test && pnpm build`
Expected: all green, no hardcoded display strings left (spot-check: `grep -rn '"Log in\|"Register\|"Email' src/features src/routes src/shared/ui` returns nothing).

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat: styled, translated auth screens with axe smoke coverage"
```

---

### Task 11: Lefthook — fail_text + pre-push

**Files:**
- Modify: `lefthook.yml` (full replacement below)

- [ ] **Step 1: Replace `lefthook.yml`**:

```yaml
pre-commit:
  parallel: true
  commands:
    oxlint:
      root: frontend/
      glob: "*.{ts,tsx}"
      fail_text: "Lint failed. Run: make frontend-lint"
      run: pnpm exec oxlint --no-error-on-unmatched-pattern {staged_files}
    oxfmt:
      root: frontend/
      glob: "*.{ts,tsx,css,json}"
      fail_text: "Formatting failed. Run: pnpm --filter @cube-planner/frontend fmt"
      run: pnpm exec oxfmt {staged_files} && git add {staged_files}
    gofumpt:
      root: backend/
      glob: "*.go"
      fail_text: "gofumpt failed."
      run: go run mvdan.cc/gofumpt@latest -l -w {staged_files} && git add {staged_files}

# Mirrors CI so pushes don't bounce. Heavy checks live here, not on commit.
pre-push:
  parallel: true
  commands:
    frontend-typecheck:
      root: frontend/
      fail_text: "TypeScript failed. Run: make frontend-typecheck"
      run: pnpm typecheck
    frontend-test:
      root: frontend/
      fail_text: "Frontend tests failed. Run: make frontend-test"
      run: pnpm test
    backend-vet:
      root: backend/
      fail_text: "go vet failed."
      run: go vet ./...
    backend-test:
      root: backend/
      fail_text: "Backend tests failed. Run: make backend-test"
      run: go test ./...
    api-fresh:
      fail_text: "Generated API client is stale. Run: make api-generate and commit."
      run: pnpm check:api
```

- [ ] **Step 2: Verify both hooks run**

Run: `lefthook run pre-commit && lefthook run pre-push`
Expected: pre-commit skips (nothing staged) or passes; pre-push runs all five commands green. (No git remote exists yet, so a real push can't be tested — `lefthook run` is the verification.)

- [ ] **Step 3: Commit**

```bash
git add lefthook.yml
git commit -m "chore: lefthook pre-push mirroring CI + fail_text hints"
```

---

### Task 12: structure.md, CLAUDE.md, README refresh

**Files:**
- Create: `docs/architecture/structure.md`, `CLAUDE.md`
- Modify: `README.md` (quickstart points at `make up`)

- [ ] **Step 1: Create `docs/architecture/structure.md`**:

````markdown
# Directory Structure & Code Organization

Source of truth for where code lives and what may depend on what. Reviews
enforce this document. Change the rules here first, then the code.

## Monorepo

```
cube_planner/
├── Makefile          # primary task runner — `make help`
├── compose.yml       # LOCAL-ONLY infra (Postgres + Mailpit)
├── backend/          # Go module
├── frontend/         # Vite + React SPA (pnpm workspace pkg)
├── deploy/           # production compose + Caddyfile
├── docs/             # specs, plans, architecture docs
└── .github/workflows # ci.yml, deploy.yml
```

## Backend (`backend/`)

Modular monolith. One package per bounded context under `internal/`
(`auth`, and per master design later: `cards`, `cubes`, `collections`,
`events`). `internal/platform/` holds shared infrastructure (db, config,
mail, httpapi) and contains **no business logic**.

- `cmd/server/main.go` is the sole composition root: config → db →
  services → router wiring happens there and nowhere else.
- Domain packages never import each other. Cross-domain needs are
  expressed as small interfaces, satisfied in `main.go`.
- Migrations: goose SQL files embedded in the binary, applied on startup.
  Queries: sqlc. Handlers: huma on chi.
- Tests: table-driven unit tests for pure logic; testcontainers
  (real Postgres) integration tests for handlers/repos.

## Frontend (`frontend/src/`)

Feature-sliced. Two axes that must not blur: `shared/` is domain-blind,
`features/<feature>/` are vertical slices.

```
src/
├── app/                  # bootstrap: main.tsx, styles.css, app-level tests
├── shared/               # domain-blind, feature-blind
│   ├── ui/               # primitives (button, input, card, …) — cva + cn
│   ├── api/              # GENERATED OpenAPI client + fetch wrapper (tracked)
│   ├── i18n/             # locale UI glue (LanguageSwitcher)
│   └── lib/              # generic utilities (cn, theme)
├── features/<feature>/   # vertical slices: components/, api.ts, hooks
├── routes/               # TanStack file routes — THIN: compose feature components
├── paraglide/            # GENERATED messages + runtime (gitignored)
└── routeTree.gen.ts      # GENERATED route tree (gitignored)
```

### The rules

1. **Dependency direction:** `app`/`routes` → `features` → `shared`.
   Never feature → feature. Never shared → feature. If two features need
   the same thing, it moves down into `shared/` (promote, don't copy).
2. **Routes are thin.** A route file declares path config
   (`validateSearch`, etc.) and points `component` at a feature component.
   No markup or logic in route files. Components read search params via
   `getRouteApi("/path")`, keeping them out of route files.
3. **Variants are props, implemented with cva.** `ButtonPrimary.tsx` or
   `Button2.tsx` are defects. One file per component; variants are typed
   cva config. All conditional/merged classes go through `cn()`
   (`@/shared/lib/cn` = clsx + tailwind-merge) so consumer `className`
   overrides win.
4. **Semantic tokens only.** Components use `bg-surface`,
   `bg-surface-raised`, `text-fg`, `text-fg-muted`, `border-border`,
   `bg-accent`, `text-accent-fg`, `bg-danger`, `text-danger-fg`. Raw
   palette utilities (`bg-zinc-800`) are allowed only inside
   `src/app/styles.css`, where themes map primitives to roles. The `dark:`
   variant is a rare escape hatch, not the theming mechanism.
5. **No hardcoded user-facing strings.** Every display string is a typed
   message call: `m.some_key()` from `@/paraglide/messages`. English
   (`messages/en.json`) is the source of truth; `pl.json` must carry the
   same keys (the Paraglide compiler enforces parity). Exception: backend
   RFC 7807 `detail` strings are shown verbatim.
6. **Accessibility conventions.** Every input has a `<Label htmlFor>` with
   a matching `id`. Errors render in `<Alert>` (`role="alert"`); when an
   error belongs to one field, associate it via `aria-describedby`. Route
   changes focus `<main>` (handled by the shell). `<html lang>` follows
   the active locale. Screen-level components get a vitest-axe smoke test
   (with `// @vitest-environment jsdom` — axe needs jsdom).
7. **Test placement.** Tests sit next to what they test. Inside
   `src/routes/` a test file needs a `-` filename prefix so route
   generation skips it; elsewhere use plain `*.test.ts(x)`.
8. **Generated artifacts.**

   | Artifact | Tracked? | Regenerate |
   |---|---|---|
   | `src/shared/api/` (client) | yes — CI checks freshness | `make api-generate` |
   | `src/routeTree.gen.ts` | no (gitignored) | `pnpm gen` / any vite run |
   | `src/paraglide/` | no (gitignored) | `pnpm gen` / any vite run |

   Never hand-edit generated files; oxlint/oxfmt ignore them.

### Adding shadcn/ui components

`frontend/components.json` targets `@/shared/ui`. After
`pnpm dlx shadcn@latest add <component>`: reformat with `pnpm fmt`, fix
strict-TS complaints, and remap shadcn's stock color variables to our
semantic tokens:

| shadcn variable | our token |
|---|---|
| background | surface |
| card / popover | surface-raised |
| foreground / card-foreground | fg |
| muted-foreground | fg-muted |
| border / input | border |
| primary | accent |
| primary-foreground | accent-fg |
| destructive | danger |
| ring | accent |

Copied components are ours: they are reviewed and maintained like
hand-written code.
````

- [ ] **Step 2: Create `CLAUDE.md`** at the repo root:

````markdown
# Cube Planner — Agent Guide

Monorepo web app for a local MTG community: Go backend (`backend/`,
huma + chi + sqlc + goose) and a React SPA (`frontend/`, Vite + TanStack
Router/Query + Tailwind v4 + Paraglide i18n). Master design:
`docs/superpowers/specs/2026-07-10-cube-planner-master-design.md`.

## Ground rules

1. **`docs/architecture/structure.md` is binding** — directory layout,
   dependency direction (`app`/`routes` → `features` → `shared`, never
   feature → feature), cva variants, semantic color tokens only, no
   hardcoded user-facing strings (Paraglide `m.*()` messages, en + pl),
   a11y conventions. Read it before writing frontend code.
2. **Use the Makefile** (`make help`) instead of ad-hoc commands:
   `make up` (whole dev stack: Postgres + Mailpit in Docker, Go + Vite on
   host), `make test`, `make db-reset`, `make api-generate`.
3. **The OpenAPI client is the contract.** Backend huma handlers generate
   the spec; `make api-generate` regenerates `frontend/src/shared/api/`
   (tracked; CI fails if stale). Never hand-edit it.
4. **Generated = gitignored:** `frontend/src/routeTree.gen.ts` and
   `frontend/src/paraglide/` are build output (`pnpm gen` recreates them).
   Never commit or hand-edit them.
5. **Auth decisions are settled.** Register on an unverified email
   overwrites; login returns distinct 401/403; OAuth verified-email
   collisions link + wipe password; unverified collisions are rejected.
   These were explicitly adjudicated — do not "fix" them.
6. **Tooling:** oxlint + oxfmt (never eslint/prettier), gofumpt +
   golangci-lint, lefthook (fast pre-commit, CI-mirroring pre-push),
   strict tsconfig (do not loosen).
7. **Tests:** frontend vitest + RTL on happy-dom (axe/a11y files opt into
   jsdom via `// @vitest-environment jsdom`); backend table-driven unit +
   testcontainers integration tests. Test files in `src/routes/` need a
   `-` prefix.

## Gotchas

- Local email lands in Mailpit: http://localhost:8025.
- The frontend Docker build needs `pnpm install --ignore-scripts`
  (root `prepare: lefthook install` fails without git).
- Deploy workflow (`workflow_run`) must keep its
  `github.event.workflow_run.event == 'push'` guard — it prevents
  fork-PR pwn requests.
- `deploy/docker-compose.prod.yml` + `.env` live at `/opt/cube-planner`
  on the VPS (placed manually).
````

- [ ] **Step 3: Update `README.md`** — replace any per-service run instructions in the quickstart with:

```markdown
## Development

Requires Docker, Go, Node 22 + pnpm.

    make up        # Postgres + Mailpit (Docker), Go API, Vite dev server
    make help      # everything else

Mail sent by the app (verification, password reset) is captured by
Mailpit: http://localhost:8025.
```

Keep the rest of the README's content (adjust anything the Makefile/compose change made stale, e.g. references to `deploy/docker-compose.dev.yml`).

- [ ] **Step 4: Verify docs match reality** — spot-check every path and command named in both docs (`ls` the paths, `make -n <target>` the targets). Fix discrepancies in the docs.

- [ ] **Step 5: Commit**

```bash
git add docs/architecture/structure.md CLAUDE.md README.md
git commit -m "docs: structure source of truth + agent guide + README quickstart"
```

---

## Final acceptance (whole branch)

From a clean checkout state:
1. `make up` → register a user at localhost:5173, read the verification mail at localhost:8025, verify, log in. Toggle theme (light/dark/system persists, follows OS in system mode, no flash on reload). Switch language to PL — all auth screens and the shell render Polish; `<html lang>` is `pl`.
2. `make test` green; `pnpm --filter @cube-planner/frontend build` green; `pnpm check:api` green; `lefthook run pre-push` green.
3. `git status` clean after a full dev/test cycle (no generated-file churn).
