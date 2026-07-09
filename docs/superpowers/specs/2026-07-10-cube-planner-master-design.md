# Cube Planner — Master Design

**Date:** 2026-07-10
**Status:** Approved architecture overview. Each sub-project (§7) gets its own detailed spec before implementation.

## 1. What this is

A web app for a local Magic: The Gathering community. Users build and share
custom card lists (cubes), track their card collections, generate wantlists of
missing cards for cardmarket import, and join organized play events with
upfront paid entry.

**Audience & scale:** one local community, tens to hundreds of users, one main
event organizer (the site owner). Event fees go directly to the owner's Stripe
account — no multi-organizer payouts, no Stripe Connect.

## 2. Stack

| Concern | Choice |
|---|---|
| Backend | Go, modular monolith, `huma` on `chi`, `sqlc`, `goose` |
| Frontend | Vite + React + TanStack Router/Query, static SPA |
| API contract | OpenAPI generated from Go (huma); typed TS client generated from spec, checked in, freshness enforced in CI |
| Database | PostgreSQL (app data, sessions, card cache — everything) |
| Card data | Scryfall bulk "default cards" file, imported daily by an in-process background job |
| Payments | Stripe Checkout (hosted page) + webhook |
| Auth | Self-built: email+password (argon2id) and Discord/Google OAuth2, server-side sessions, first-party cookies |
| Frontend tooling | pnpm workspaces, oxlint, oxfmt, lefthook, strictest tsconfig (`strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `verbatimModuleSyntax`) |
| Backend tooling | golangci-lint, gofumpt |
| CI/CD | GitHub Actions; deploy via SSH + `docker compose up -d` |
| Production | Single VPS, Docker Compose: Caddy (static assets + reverse proxy `/api` → Go), Go binary, Postgres |

Single origin in production (Caddy fronts both SPA and API), so auth cookies
are plain first-party cookies — no CORS, no tokens in JS.

## 3. Monorepo layout

```
cube_planner/
├── backend/                  # Go module
│   ├── cmd/server/           # main entrypoint
│   ├── internal/
│   │   ├── auth/             # sessions, oauth, password
│   │   ├── cards/            # scryfall sync + card queries
│   │   ├── cubes/            # cubes, versioning
│   │   ├── collections/      # collection + wantlist
│   │   ├── events/           # events, payments, pairings
│   │   └── platform/         # db, config, mail, http middleware
│   └── migrations/           # goose SQL migrations
├── frontend/                 # Vite + React + TanStack (pnpm workspace pkg)
│   └── src/
│       ├── api/              # generated OpenAPI client (checked in)
│       ├── features/         # cubes/, collection/, events/, auth/
│       └── ...
├── packages/                 # future shared TS packages if needed
├── deploy/                   # docker-compose.yml, Caddyfile
└── .github/workflows/        # ci.yml, deploy.yml
```

One Go service, one package per domain, no microservices. The Scryfall sync
runs inside the server process; no separate worker at this scale.

## 4. Core data model

### Cards & identity

- `cards`: mirrors Scryfall default-cards bulk data. One row per **printing**,
  `scryfall_id` PK, `oracle_id` and name indexed.
- Cube entries and collection items reference a specific printing (correct
  art/set in the UI).
- Diffs and wantlists compare at **oracle level**: "do I own Lightning Bolt",
  not "that exact printing". This is what makes wantlists useful for
  cardmarket.

### Users & auth

- `users`: email, display name, optional password hash.
- `oauth_identities`: (provider, provider_user_id) → user. One account can
  link email, Discord, and Google simultaneously.
- `sessions`: server-side, cookie holds only an opaque token.

### Cubes

- `cubes`: owner, name, description, `visibility: public | private`.
- `cube_cards`: current list (fast reads).
- `cube_changes` + change items (`add | remove`, card, who, when): append-only
  changelog. **Auto-history versioning** — every save records a diff; any
  past state is reconstructable by replaying the log backwards from current
  state; the changelog renders as a CubeCobra-style history page. No manual
  version management, no full snapshots.

### Collections & wantlists

- `collection_items`: user, printing, quantity.
- Wantlist = cube minus collection, computed on demand at oracle level,
  exported as cardmarket-format txt. **Never stored.**

### Events

- `events`: organizer, name, date, fee + currency, `max_participants`,
  status `draft → published → started → finished`.
- `event_cubes`: links cubes to an event, optionally pinned to a specific
  `cube_change` ("play the cube as of this point in history").
- `registrations`: Stripe checkout session id, waitlist position, and a
  status machine: registering on an open spot → `pending_payment` → `paid`;
  registering on a full event → `waitlisted` (no payment taken). When a paid
  spot frees, the first waitlisted registration moves to `pending_payment`
  and the user is prompted to pay. Terminal states: `cancelled`, `refunded`.
  Capacity is enforced transactionally at registration time.
- `rounds` + `matches`: swiss pairings per round, results (player wins/losses/
  draws), standings computed from match points with standard tiebreakers
  (OMW% etc.).

## 5. Error handling conventions

- API errors are RFC 7807 `application/problem+json` (huma native). The
  generated client surfaces them as typed errors; frontend maps them to
  toasts / form field errors.
- Domain rules ("event full", "not cube owner") are distinct problem types
  from the domain layer, not generic 400s.
- Payments: the **webhook is the single source of truth** for "paid". The
  redirect back from Stripe shows only an optimistic "confirming…" state.
  Webhook handlers are idempotent (keyed on Stripe event id) because Stripe
  retries.

## 6. Testing strategy

- **Go:** table-driven unit tests for domain logic (pairings, standings,
  diff/wantlist computation, changelog replay — all pure logic). Integration
  tests for handlers against real Postgres via `testcontainers-go`. Stripe
  webhooks tested against recorded fixtures.
- **Frontend:** Vitest + React Testing Library for components with logic.
  The generated client + strict TS does contract enforcement statically.
- **E2E:** thin Playwright smoke suite (register → create cube → add cards →
  export wantlist) run in CI against docker-compose.

## 7. Sub-projects

Each gets its own spec → plan → implementation cycle, in this order:

1. **Foundation & auth.** Monorepo scaffolding, all tooling and lint/format
   configs, CI pipeline, Docker/Caddy deploy setup, Postgres + goose + sqlc
   wiring, huma + OpenAPI → TS client generation, full auth (email+password
   with verification, Discord/Google OAuth, account linking, sessions,
   password reset). Done when: register, log in, deploy to VPS.
2. **Card data.** Scryfall bulk import + daily sync, `cards` table, local
   card search/autocomplete API (fuzzy name search, cube-editor filters).
   Done when: fast card search served from own Postgres.
3. **Cubes.** CRUD, cube editor (add/remove via search), changelog versioning
   with history view, public/private visibility, CubeCobra-style display
   (grouped by color/type/cmc, card image hovers), public cube browser.
4. **Collections & wantlists.** Collection management UI (cards, quantities,
   printings), cube-vs-collection diff, cardmarket txt export.
5. **Events.** Event CRUD + organizer panel, publishing, Stripe Checkout
   registration, webhook handling, capacity + waitlist promotion, event
   start, swiss pairings, result entry, standings.

## 8. Decisions log

| Decision | Choice | Why |
|---|---|---|
| Scope model | Decompose into 5 sub-projects + this overview | Too large for one spec |
| Audience | Local community, single organizer | Simplifies payments, SEO, moderation |
| Hosting | VPS + Docker Compose | Cheap, simple, sufficient |
| Frontend | Vite + React + TanStack SPA | App-like logged-in UX, no Node server, SEO irrelevant at this scale |
| Card data | Bulk import + daily sync | Local search, no rate limits |
| Auth | Self-built in Go | No per-user cost, no external dependency |
| Cube versioning | Auto-history changelog | Zero user friction, CubeCobra-familiar |
| Build order | Cubes before collections/events | Cubes are the heart; events depend on them |
| API contract | OpenAPI from huma + generated TS client | End-to-end type safety enforced in CI |
