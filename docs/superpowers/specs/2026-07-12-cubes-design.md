# Cubes — Sub-project 3 Design

**Date:** 2026-07-12
**Status:** Approved. Implements master design §7.3
(`2026-07-10-cube-planner-master-design.md`).

## 1. Scope

Cube CRUD, a batched-commit cube editor built on card autocomplete,
append-only changelog versioning with a history view and past-state
reconstruction, public/private visibility, a CubeCobra-style grouped
display page, and a public cube browser.

**Decisions made during brainstorming:**

| Decision | Choice |
|---|---|
| Save model | Batched commit: editor accumulates a pending diff; explicit save creates ONE changelog entry with an optional note |
| Duplicates | Quantities allowed — `quantity` column, one row per oracle card |
| History depth | Changelog page AND "view cube as of version N" reconstruction |
| Display grouping | Color-grouped default (CubeCobra-style) + switcher for type / CMC |
| Cube browser | Paginated public list + name search, newest-updated first |
| Printings | Adds use the autocomplete's representative printing; per-entry printing picker is future work |
| Metadata | Name + description + visibility only (cover card and tags/format labels logged as future work) |
| Save API | Diff commit + optimistic version check (approaches considered: full-list replace, server-side drafts — rejected for inferred-intent/payload size and schema+API weight respectively) |
| Cross-feature composition | Promote `CardAutocomplete` to `shared/cards/` per structure.md's "promote, don't copy" rule |

**Non-goals:** collaborative editing, comments/likes, cube forks/clones,
draft simulation. Collections diff and wantlists are sub-project 4;
event pinning to a `cube_change` is sub-project 5 (this spec provides the
reconstruction logic it will use).

## 2. Data model & versioning

Migration `backend/migrations/00004_cubes.sql` (+ goose down).

### Tables

**`cubes`**
- `id` uuid PK (generated)
- `owner_id` uuid FK → `users` ON DELETE CASCADE
- `name` text, 1–100 chars (CHECK)
- `description` text NOT NULL DEFAULT ''
- `visibility` text CHECK IN ('public', 'private')
- `version` int NOT NULL DEFAULT 0 — optimistic-concurrency token; bumps
  by exactly 1 per committed change
- `created_at`, `updated_at` timestamptz
- Indexes: `(visibility, updated_at)` for the browser listing; pg_trgm
  GIN index on `name` for browser search (same `<%` operator setup as
  cards, migration 00003's GUC applies database-wide)

**`cube_cards`** — materialized current list (fast reads)
- `cube_id` uuid FK → `cubes` ON DELETE CASCADE
- `oracle_id` uuid — denormalized from `cards` at write time
- `scryfall_id` uuid FK → `cards(scryfall_id)` — the chosen printing
- `quantity` int CHECK (quantity >= 1)
- `added_at` timestamptz
- **PK `(cube_id, oracle_id)`** — one row per oracle card. "2× Lightning
  Bolt" is one row with quantity 2. Two different printings of the same
  oracle card cannot coexist in a cube (consistent with the
  printing-picker-later decision).

**`cube_changes`** — append-only
- `id` uuid PK
- `cube_id` uuid FK → `cubes` ON DELETE CASCADE
- `version` int — the cube version this change produced;
  UNIQUE `(cube_id, version)`
- `author_id` uuid FK → `users`
- `note` text NOT NULL DEFAULT '', ≤500 chars (CHECK)
- `created_at` timestamptz

**`cube_change_items`**
- `change_id` uuid FK → `cube_changes` ON DELETE CASCADE
- `kind` text CHECK IN ('add', 'remove')
- `oracle_id` uuid, `scryfall_id` uuid FK → `cards(scryfall_id)`
- `quantity` int CHECK (quantity >= 1)
- One row per (change, kind, oracle card).
- No denormalized card name: history joins to `cards`. The mirror's sync
  CAN delete cards that vanish from the Scryfall bulk file
  (`DeleteCardsMissingFromStaging`), which would orphan these joins and
  violate the FKs — so that query gains a guard: never delete cards
  referenced by `cube_cards` or `cube_change_items`. (Correction found
  during planning; the original spec wrongly claimed the mirror never
  deletes.)

### Save semantics

One transaction, cube row locked with `SELECT … FOR UPDATE`:

1. Check `cubes.version = expectedVersion`, else the domain error that
   maps to 409.
2. Apply deltas to `cube_cards`:
   - add: insert row, or increment `quantity` if the oracle card is
     already present (the incoming `scryfall_id` is ignored on increment —
     the existing printing stays)
   - remove: decrement `quantity`; delete the row at 0; removing more
     than present → domain error mapping to 422
3. Insert one `cube_changes` row with `version = expectedVersion + 1`
   plus its items.
4. Bump `cubes.version` and `updated_at`.

### Past-state reconstruction

Pure Go (no DB): start from current `cube_cards`, walk `cube_changes`
backwards from latest down to (but not including) the target version,
inverting each change — subtract its adds, re-add its removes (using the
item's `scryfall_id` as the printing). `version 0` = empty cube.
Requesting a version above current → 404.

## 3. API surface

huma handlers in `backend/internal/platform/httpapi/cubes.go`.
Exported wire types drive generated TS names (same pattern as cards).

### Mutations (session required, owner-only)

- `POST /cubes` `{name, description, visibility}` → 201 `CubeDetail`
- `PATCH /cubes/{cubeId}` `{name?, description?, visibility?}` →
  `CubeDetail`
- `DELETE /cubes/{cubeId}` → 204
- `POST /cubes/{cubeId}/changes`
  `{expectedVersion, note, adds: [{scryfallId, quantity}], removes:
  [{oracleId, quantity}]}` → 201 `{change: ChangelogEntry, version}`
  - 409 problem type `cube-version-conflict` when `expectedVersion` is
    stale
  - 422 problem type `invalid-cube-change` for: remove > present,
    unknown card ids, empty diff, same oracle id appearing in both adds
    and removes

### Reads (public cubes: anonymous OK; private cubes: owner only)

- `GET /cubes?q=&limit=&offset=` → `{cubes: CubeSummary[], total}` —
  public cubes only; pg_trgm name search when `q` ≥ 2 chars; ordered by
  `updated_at` DESC
- `GET /me/cubes` → own cubes, both visibilities (session required)
- `GET /cubes/{cubeId}` → `CubeDetail` (metadata + version, no cards)
- `GET /cubes/{cubeId}/cards?atVersion=-1` → `{cards: CubeCardEntry[],
  version}` — `atVersion=-1` (default) serves current state straight from
  `cube_cards`; `atVersion=N` (N ≥ 0) serves the reconstructed past
  state. Value-typed sentinel because huma v2.38 panics on pointer query
  params. `N` > current version → 404.
- `GET /cubes/{cubeId}/changes?limit=&offset=` →
  `{changes: ChangelogEntry[], total}` — newest first, items joined to
  `cards` for names

### Visibility & auth semantics

- Private cube requested by non-owner or anonymous → **404** (no
  existence leak), for reads and mutations alike.
- Public cube mutated by a logged-in non-owner → **403**.
- Ownership checks live in the service layer, not handlers.

### Wire types

- `CubeSummary`: id, name, description, ownerName, cardCount (sum of
  quantities), visibility, updatedAt
- `CubeDetail`: CubeSummary fields + ownerId, version, createdAt
- `CubeCardEntry`: scryfallId, oracleId, name, manaCost, typeLine, cmc,
  colors, colorIdentity, rarity, imageSmall, imageNormal, quantity —
  everything the grouped display and hover preview need in one fetch
- `ChangelogEntry`: id, version, authorName, note, createdAt,
  adds/removes as `[{oracleId, name, quantity}]`

After the backend lands, `make api-generate` refreshes
`frontend/src/shared/api/` (CI enforces freshness).

## 4. Backend structure

New package `backend/internal/cubes/` (mirrors `internal/cards` layout):

- `service.go` — `Service` over `db.Queries` + pgx pool: Create, Get,
  Update, Delete, ListPublic (trgm search), ListMine, GetCards,
  ApplyChange (the diff-commit transaction), ListChanges,
  GetCardsAtVersion
- `replay.go` — **pure functions, no DB**:
  - `validateDiff(current, adds, removes)` → `ErrInvalidChange` with
    reason (`ErrVersionConflict` is raised by the service's in-tx
    version check, not here)
  - `applyDiff(current, adds, removes)` → next state
  - `replayBackwards(current, changes, targetVersion)` → past state
- `replay_test.go` — table-driven: empty diff, add existing card
  (increment), remove to zero (row disappears), remove more than
  present, same oracle in adds+removes rejected, replay to version 0 =
  empty, replay to mid-history version, replay across quantity churn
- `service_test.go` — unit tests where mocking is sensible

sqlc: `backend/internal/db/queries/cubes.sql` → generated
`cubes.sql.go`. `ApplyChange` runs in a pgx transaction as described
in §2.

HTTP: `platform/httpapi/cubes.go` registers the nine operations and maps
domain errors → problem+json. Session middleware exists (auth package):
reads use optional-session, mutations require it. Handlers stay thin:
parse → service → map.

No background work in this sub-project — plain request/response.

## 5. Frontend structure & UX

### Prep refactor (own commit, no behavior change)

Promote `CardAutocomplete`, `useCardAutocomplete`, and the `CardSummary`
type alias from `features/cards` to a new `shared/cards/`;
`features/cards` keeps the search-page pieces. Move their tests along.
Amend `docs/architecture/structure.md`: `shared/` may hold promoted
domain widgets once ≥2 features need them (the "promote, don't copy"
rule in practice; `shared/api` is already domain-typed). Collections
(sub-project 4) needs the same picker, so promotion is inevitable.

### Routes (thin, per structure.md rule 2)

- `/cubes` — public browser: debounced search box (≥2 chars), paginated
  `CubeSummary` list (name, owner, card count, last updated), "New cube"
  button when logged in
- `/cubes/mine` — own cubes (auth-guarded like `/account`)
- `/cubes/new` — create form (name, description, visibility)
- `/cubes/$cubeId` — display page; `?atVersion=N` search param
  (`validateSearch`) renders that past state read-only with a
  "viewing version N" banner and a link back to current
- `/cubes/$cubeId/edit` — editor; owner only, others redirected
- `/cubes/$cubeId/history` — changelog

### features/cubes/

`api.ts` — TanStack Query hooks over the generated client: `useCube`,
`useCubeCards(atVersion)`, `useCubeList`, `useMyCubes`,
`useCubeChanges`, `useCreateCube`, `useUpdateCube`, `useDeleteCube`,
`useCommitChange`. Plus `components/` and `lib/`.

### Display page

`lib/grouping.ts` — pure functions over `CubeCardEntry[]`: group by
color (W/U/B/R/G, multicolor, colorless, land — CMC-sorted within), by
type, by CMC. Unit tested. Group switcher defaults to color. Card rows
show name + mana cost, quantity badge when >1. Hover or keyboard focus
shows `imageNormal` in a floating panel
(`components/CardHoverPreview`) — keyboard-accessible; on touch, tap
opens the preview instead.

### Editor

`CardAutocomplete` on top; grouped current list below with per-row
quantity stepper and remove. Stepper increments on an existing row
enter the pending diff as adds carrying that row's existing
`scryfallId`. The pending diff lives in a `useReducer`:
net deltas keyed by `oracleId`, so an add followed by a remove cancels
to a no-op. A sidebar panel lists pending +N/−N lines with per-line
undo, the note input, Save, and Discard. Save calls `useCommitChange`
with `expectedVersion` = the fetched version. On 409: toast "cube
changed elsewhere", refetch, keep the local diff, re-validate it
client-side against the fresh list. Unsaved-changes guard via TanStack
Router navigation block + `beforeunload`.

### Changelog page

Newest-first entries — author, timestamp, note, +adds/−removes chips —
each linking to `/cubes/$cubeId?atVersion=N`. Paginated like the
browser.

### Conventions

All strings via `m.*()` (en + pl), semantic color tokens only,
`<Label htmlFor>` + `role="alert"` + `aria-describedby` per
structure.md, axe smoke test per screen (`// @vitest-environment
jsdom`).

## 6. Testing & error handling

**Backend**
- Table-driven unit tests for `replay.go` — the versioning core is pure
  logic.
- testcontainers integration tests: CRUD lifecycle; visibility matrix
  (owner/other/anonymous × public/private × read/mutate); commit happy
  path; concurrent-save 409; the 422 family (remove > present, unknown
  ids, empty diff, add+remove same oracle); `atVersion` reconstruction
  against seeded history; pagination.

**Frontend**
- vitest unit: `grouping.ts`; pending-diff reducer (cancel-out, net
  deltas, post-409 revalidation).
- RTL: editor add→pending→save flow (mocked hooks), browser list,
  changelog rendering, version banner.
- axe smoke per screen; `CardAutocomplete` tests move with the
  promotion.

**Error handling** (RFC 7807, master design §5)
- 409 `cube-version-conflict` → editor keeps local diff and refetches
- 422 `invalid-cube-change` → toast with problem `detail`
- 404/403 → route-level error components

## 7. Order of work

1. Promote `CardAutocomplete` → `shared/cards` (+ structure.md
   amendment). Own commit.
2. Migration 00004 + sqlc queries + `internal/cubes` (replay.go first,
   TDD) + httpapi + integration tests.
3. `make api-generate` → committed client.
4. `features/cubes`: browser → create/mine → display → editor →
   history/at-version.
5. Manual acceptance in the browser (Mateusz), as in sub-project 2.

## 8. Future work (logged, not in scope)

- Cover-card banner for cubes (owner picks a card whose art becomes the
  browser thumbnail) — explicitly requested as an eventual feature.
- Tags / format labels on cubes, filterable in the browser — explicitly
  requested as an eventual feature.
- Per-entry printing picker in the editor (`/cards/{oracleId}/printings`
  API is ready).
- Mana-symbol SVGs and other card-display niceties (existing backlog).
- Autocomplete popularity signal ("bolt" → Lightning Bolt), carried over
  from sub-project 2.
