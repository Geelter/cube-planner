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
