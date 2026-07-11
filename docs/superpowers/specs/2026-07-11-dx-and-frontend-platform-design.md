# DX & Frontend Platform — Design

**Date:** 2026-07-11
**Status:** Approved design. Interstitial sub-project between sub-project 1
(Foundation & auth) and sub-project 2 (Card data) of the master design
(`2026-07-10-cube-planner-master-design.md`).

## 1. What this is

Sub-project 1 shipped a working but bare stack: unstyled frontend, no i18n,
no single-command local dev, no codified directory structure. Before the
codebase grows, this sub-project establishes the platform every later
feature builds on:

1. Single-command local development (Makefile + root `compose.yml` + Mailpit)
2. Styling: Tailwind v4 + shadcn/ui copy-in components
3. i18n: Paraglide JS, Polish + English, runtime-switchable
4. Accessibility as a standing default (lint rules, primitives, test assertions)
5. `routeTree.gen.ts` out of version control
6. Directory-structure guidelines doc as source of truth + project `CLAUDE.md`
7. TanStack Router/Query devtools
8. jsdom → happy-dom
9. Lefthook: fast pre-commit + CI-mirroring pre-push

**Validation slice:** the existing auth screens (login, register, account,
forgot/reset password, verify email) get restyled with the new UI
primitives, translated to PL/EN, and moved into the new directory
structure. That proves every piece of the platform end to end.

## 2. Decisions

| Decision | Choice | Why |
|---|---|---|
| Component layer | shadcn/ui copy-in (Radix + Tailwind) | Accessible primitives we own in-repo, freely restylable |
| Styling engine | Tailwind v4, CSS-first config, `@tailwindcss/vite` | Utility CSS without hand-writing styles; v4 is current |
| Component variants | `cva` (class-variance-authority) | Variants as typed props in one file; shadcn/ui already uses it |
| Class sorting | oxfmt built-in `sortTailwindcss` | First-party since Jan 2026 — no Prettier plugin, no hack |
| Dark mode | Ship now; two-tier semantic tokens + `data-theme`, light/dark/system | Dark-correct by construction, not per-component `dark:` overrides |
| i18n | Paraglide JS (inlang) | Typed message functions — missing keys fail the build; matches our generated-artifact ethos |
| Locale model | Runtime-switchable PL/EN, persisted, browser-detected initial, `en` fallback | Community is bilingual; no per-market builds needed |
| Local dev | Hybrid: Postgres + Mailpit in Docker; Go + Vite on host; Makefile orchestrates | Fast iteration and debugger access; one command via `make up` |
| Dev compose file | Root `compose.yml`, hardcoded local-only creds | Bare `docker compose up` works; prod compose stays in `deploy/` |
| routeTree.gen.ts | Gitignored; `tsr generate` before bare `tsc` | Fully derived; Vite plugin regenerates on dev/build/test; kills merge churn |
| Test DOM | happy-dom default; jsdom kept as per-file escape hatch | 2.5–10× faster; RTL unaffected; `// @vitest-environment jsdom` when needed |
| Devtools | `@tanstack/react-devtools` + Router/Query panels, dev-only | Unified official shell; lazy-imported, absent from prod bundle |
| Git hooks | Staged-file pre-commit (unchanged) + new pre-push mirroring CI | Instant commits; pushes don't bounce off CI |
| Structure doc | `docs/architecture/structure.md` + new root `CLAUDE.md` | One source of truth; CLAUDE.md makes every future agent inherit it |

## 3. Local development stack

### Root `compose.yml` (local only)

Replaces `deploy/docker-compose.dev.yml`. Services, with intentionally
hardcoded local-only credentials and a header comment saying so:

- `postgres`: `postgres:17-alpine`, user/password/db `cube`, port 5432,
  named volume, `pg_isready` healthcheck.
- `mailpit`: `axllent/mailpit`, SMTP on 1025, web UI on 8025. All local
  transactional mail (verification, reset) becomes readable at
  `http://localhost:8025`.

`deploy/docker-compose.prod.yml` and the Caddy setup are untouched.

### Makefile (repo root, primary task runner)

Self-documenting (`make help` greps `##` comments, default goal). Targets:

- `up` — `docker compose up -d` (postgres + mailpit) then `make -j2
  backend-dev frontend-dev` (both dev servers in parallel, interleaved output)
- `down` — stop compose services
- `backend-dev` — `go run ./cmd/server` from `backend/` with local env
  (reads `.env` if present; sane defaults match `compose.yml`)
- `frontend-dev` — `pnpm --filter @cube-planner/frontend dev`
- `db-psql` — psql shell in the postgres container
- `db-reset` — drop the volume, recreate, restart backend (goose re-migrates on boot)
- `db-logs`, `ps` — logs / service status
- `backend-test`, `backend-lint` — `go test ./...`, `golangci-lint run`
- `frontend-test`, `frontend-typecheck`, `frontend-lint` — vitest / tsc / oxlint
- `api-generate`, `api-check` — existing `gen:api` / `check:api` scripts
- `test` — backend + frontend tests

### `.env.example` (repo root)

Documents every variable the backend reads (`PORT`, `ENV`, `DATABASE_URL`,
`BASE_URL`, `SMTP_*`, `DISCORD_*`, `GOOGLE_*`), with local-dev values
pointing at the compose services (SMTP → localhost:1025). `.env` stays
gitignored; `make backend-dev` loads it.

### Backend Dockerfile improvements

- BuildKit cache mounts for `/go/pkg/mod` and `/root/.cache/go-build`
- `-trimpath` and version metadata via ldflags (`VERSION`, `GIT_COMMIT`,
  `BUILD_TIME` build args, wired from the Makefile and deploy workflow)

Base image stays `distroless/static-debian12:nonroot` (adjudicated in
sub-project 1 — do not change).

## 4. Styling: Tailwind v4 + shadcn/ui

- `@tailwindcss/vite` plugin; single global CSS entry with `@import
  "tailwindcss"` and design tokens in `@theme` (colors, radii, fonts).
  No `tailwind.config.js`.
- shadcn/ui initialized with `components.json` targeting
  `frontend/src/shared/ui/`. Components are **copied in as needed** —
  start with only what the auth screens require (button, input, label,
  form, card, alert/toast) plus a minimal app shell (header with
  language switcher). Add more per-feature later; never import a
  component we don't use.
- Copied components are ours: adapt them to oxfmt formatting and the
  strict tsconfig; they are subject to normal review like hand-written code.
- **Class sorting:** oxfmt's built-in `sortTailwindcss` (v4 mode, pointed
  at the global stylesheet) with `functions: ["cva", "cn", "clsx"]` so
  sorting also applies inside variant definitions. Beta-fresh feature; if a
  sorter bug bites, disable the option rather than work around it.
- **Variants are props, implemented with `cva`** (class-variance-authority,
  which shadcn/ui components already use): one component file per
  component, variants as typed cva config — never `ButtonPrimary.tsx` /
  `Button2.tsx` files.

### Dark mode (shipped now, token-driven)

Dark mode is not per-component `dark:` overrides — it falls out of a
two-tier token architecture:

- **Tier 1 — primitive palette:** raw scales (e.g. `--zinc-*`, brand
  scale) defined once, theme-independent.
- **Tier 2 — semantic role tokens:** `--color-surface`, `--color-surface-raised`,
  `--color-text`, `--color-text-muted`, `--color-border`, `--color-accent`,
  `--color-danger`, etc. Each theme assigns primitives to roles:
  defaults on `:root`, overrides under `[data-theme="dark"]`. Exposed to
  Tailwind via `@theme inline` so utilities like `bg-surface` /
  `text-muted` exist.
- **Components consume only semantic utilities** — never raw palette
  colors, and `dark:` variants are a rare escape hatch (registered via
  `@custom-variant dark (&:where([data-theme=dark], [data-theme=dark] *))`),
  not the mechanism. A component written with role tokens is dark-mode
  correct by construction. This rule goes in the structure doc.
- **Theme switching:** three-way user setting (light / dark / system)
  persisted in `localStorage`, `system` resolving via
  `prefers-color-scheme` (and reacting to OS changes). A tiny inline
  script in `index.html` sets `data-theme` before first paint (no flash).
  `color-scheme` is set per theme so native form controls and scrollbars
  match. Theme toggle lives in the app shell next to the language switcher.
- shadcn/ui's stock variables are remapped onto these role tokens when
  components are copied in, so primitives and hand-written code share one
  vocabulary.

## 5. i18n: Paraglide JS

- `@inlang/paraglide-js` with its Vite plugin; inlang project settings at
  the frontend root; messages in `frontend/messages/{en,pl}.json`.
- Compiled output (typed message functions) is **generated, gitignored**
  build output — same policy as `routeTree.gen.ts`; generation runs via the
  Vite plugin on dev/build/test and via CLI before bare `tsc`.
- Runtime locale switching: locale stored in `localStorage`, initial value
  from `navigator.language` (`pl*` → pl, else en), `en` is the fallback for
  missing messages. A visible PL/EN switcher lives in the app shell.
- `<html lang>` is kept in sync with the active locale (a11y requirement).
- **Rule (goes in structure doc + CLAUDE.md):** no hardcoded user-facing
  strings in components — every display string is a message call. English
  is the source-of-truth locale; Polish must have the same key set (the
  Paraglide compiler enforces key parity).
- Backend API error messages stay English-only for now (RFC 7807 problem
  details are developer-facing); frontend maps known problem types to
  translated toasts/field errors. Localizing backend text is out of scope.

## 6. Accessibility

- Enable oxlint's `jsx-a11y` rule category in the oxlint config (error level).
- Radix primitives (via shadcn/ui) carry keyboard/focus/ARIA behavior for
  interactive widgets.
- Component tests assert accessible behavior: query by role/label (already
  RTL idiom), plus `vitest-axe` smoke checks on each screen-level component.
- Conventions (recorded in the structure doc): every form input has a
  label; errors are associated via `aria-describedby`; route changes move
  focus to the main heading; `<html lang>` synced to locale.
- Playwright + axe E2E audits: deferred to the E2E suite planned in the
  master design, not this sub-project.

## 7. routeTree.gen.ts out of git

- Add `frontend/src/routeTree.gen.ts` to `.gitignore`, `git rm --cached` it.
- Add a `gen:routes` script (`tsr generate` from `@tanstack/router-cli`)
  and run it before every bare `tsc --noEmit` (the `build` and `typecheck`
  scripts, and CI's typecheck step). Vite dev/build and vitest already
  regenerate it via the router plugin.

## 8. Directory structure guidelines

New file `docs/architecture/structure.md` — the source of truth, written
from industry consensus (feature-sliced / vertical-slice frontend
organization à la bulletproof-react; Go modular monolith). Binding rules:

**Frontend (`frontend/src/`):**

```
src/
├── app/                  # bootstrap: main.tsx, providers, app shell, global CSS
├── shared/               # domain-blind, feature-blind
│   ├── ui/               # shadcn/ui primitives + hand-written primitives
│   ├── api/              # generated OpenAPI client + fetch wrapper
│   ├── i18n/             # paraglide runtime glue, locale switch hook
│   └── lib/              # generic utilities
├── features/<feature>/   # vertical slices: components/, hooks/, api.ts, …
└── routes/               # TanStack file routes — thin: compose feature components
```

- Dependencies point inward only: `routes → features → shared`.
  **Never feature → feature; never shared → feature.**
- Variants are props implemented with `cva`, never new component files.
- Colors come from semantic role tokens (`bg-surface`, `text-muted`, …),
  never raw palette utilities; `dark:` is a rare escape hatch (see §4).
- No hardcoded user-facing strings (see §5).
- Test files live next to what they test (`-` prefix inside `routes/`).

**Backend (`backend/`):** codifies what sub-project 1 already does — one
domain package per bounded context under `internal/`, `platform/` for
infra with no business logic, `cmd/server` as the sole composition root,
migrations embedded, sqlc for queries. Domains never import each other;
cross-domain needs go through interfaces wired in `main.go`.

**Migration in this sub-project:** existing frontend files move into this
shape (auth screens → `features/auth/`, generated client → `shared/api/`,
`main.tsx` + providers → `app/`); routes stay as thin files. Backend
already conforms — no moves.

**New root `CLAUDE.md`:** short agent guide — layout map, pointer to
`structure.md` as binding, the Makefile as the task runner, the
non-negotiable rules (dependency direction, no hardcoded strings, a11y
conventions, adjudicated auth decisions left alone).

## 9. Devtools, happy-dom, lefthook

- **Devtools:** `@tanstack/react-devtools` with the Router and Query
  panels, mounted only in dev (lazy `import.meta.env.DEV` gate) so it never
  reaches the production bundle.
- **happy-dom:** vitest `environment: "happy-dom"`; jsdom stays in
  devDependencies as the documented per-file escape hatch
  (`// @vitest-environment jsdom`). Existing tests must pass unchanged.
- **Lefthook:** pre-commit keeps the current staged-file lint/format
  commands, each gaining a `fail_text` hint. New `pre-push` block mirroring
  CI: frontend `typecheck` + `vitest run`, backend `go vet ./...` +
  `go test ./...`, and the API-client freshness check (`check:api`).

## 10. Testing strategy

- Every migrated/restyled auth screen keeps its existing tests (adapted to
  new paths) and gains: role/label-based queries where missing and a
  `vitest-axe` smoke assertion.
- i18n: one test asserting the language switcher flips visible copy and
  `<html lang>`; key parity is enforced by the Paraglide compiler, not tests.
- Theming: one test asserting the theme toggle cycles light/dark/system,
  persists, and sets `data-theme` (system resolution via matchMedia mock).
- Makefile/compose: verified by running the stack (`make up`, register a
  user, read the verification mail in Mailpit) — documented as the manual
  acceptance check, not automated.
- CI must stay green with routeTree + paraglide output gitignored — the CI
  workflow gains the generation steps before typecheck.

## 11. Out of scope

- Any card-data / sub-project-2 functionality
- Themes beyond light + dark (the token architecture permits more later)
- Localizing backend-generated text
- Playwright E2E and axe audits (come with the master design's E2E suite)
- Restructuring the backend (already conforms)

## 12. Done when

`make up` brings the whole stack up from a clean checkout; the auth flow
works end to end with mail visible in Mailpit; all auth screens are styled
with shared/ui components, correct in both light and dark themes
(switchable, persisted, system-aware), fully translated (PL/EN switchable
at runtime), and pass axe smoke checks; `structure.md` and `CLAUDE.md` exist and the
frontend tree matches them; CI is green with `routeTree.gen.ts` and
Paraglide output untracked; pre-push hook blocks a push that would fail CI.
