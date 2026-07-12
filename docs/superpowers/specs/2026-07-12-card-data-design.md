# Card Data — Design (Sub-project 2)

**Date:** 2026-07-12
**Status:** Approved design. Parent: `2026-07-10-cube-planner-master-design.md` §7.2.

## 1. Goal & scope

Mirror Scryfall's "default cards" bulk data into our own Postgres and serve
fast, typo-tolerant card search from it — no per-request Scryfall calls, no
rate limits. Done when: fast card search (autocomplete + filtered search) is
served from our own database, with a reusable frontend autocomplete component
proven on a minimal `/cards` demo page.

**In scope**

- `cards` table (one row per printing, lean extracted columns) + goose
  migration, `pg_trgm` extension.
- Streaming bulk import + daily sync as an in-process background job in a new
  `internal/cards` package.
- Public search API: autocomplete, filtered search, printings-by-oracle.
- Frontend: domain-blind `shared/ui` combobox primitive, `features/cards`
  slice (query hooks, `CardAutocomplete`, minimal search page), `/cards`
  route, en + pl messages, a11y per structure.md.

**Out of scope (deliberately)**

- Scryfall-syntax query parser (`c:red t:instant`), oracle-text full-text
  search, card detail route, price data, sync admin UI/endpoint. All can be
  added later without schema breakage (new columns backfill on next sync).

**Decided during brainstorm** (2026-07-12, with Mateusz):

| Decision | Choice |
|---|---|
| Frontend scope | Backend + reusable autocomplete component + minimal demo page |
| Data shape | Extracted columns only — no raw JSONB blob |
| Card pool | Paper-playable only, filtered at import |
| API surface | Autocomplete + basic filters (+ printings endpoint) |
| Sync trigger | Auto on startup when empty + periodic in-process check |
| Import mechanics | Staging table + `pgx.CopyFrom`, atomic swap transaction |
| Fuzzy search | `pg_trgm` (GIN) with prefix boost + `word_similarity` |

## 2. Data model

One goose migration (`00002_cards.sql`):

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

`pg_trgm` ships in Postgres contrib and is present in the official image used
both in production compose and testcontainers.

### `cards` — one row per printing

| Column | Type | Notes |
|---|---|---|
| `scryfall_id` | `uuid` PK | Scryfall card object `id` |
| `oracle_id` | `uuid` NOT NULL | indexed (btree); diff/wantlist identity level |
| `name` | `text` NOT NULL | full combined name, e.g. `Fire // Ice` |
| `normalized_name` | `text` NOT NULL | lowercased, diacritics folded **in Go at import time** (avoids the non-immutable `unaccent()` index problem); trgm GIN index |
| `released_at` | `date` NOT NULL | |
| `set_code` | `text` NOT NULL | e.g. `neo` |
| `set_name` | `text` NOT NULL | |
| `collector_number` | `text` NOT NULL | text — contains `123a`, `★` etc. |
| `rarity` | `text` NOT NULL | `common\|uncommon\|rare\|mythic\|special\|bonus` |
| `layout` | `text` NOT NULL | `normal`, `transform`, `split`, … |
| `mana_cost` | `text` NOT NULL DEFAULT `''` | top-level when Scryfall provides it, else front face |
| `cmc` | `numeric` NOT NULL | |
| `type_line` | `text` NOT NULL | combined, e.g. `Instant // Instant` |
| `oracle_text` | `text` NOT NULL DEFAULT `''` | faces joined with `\n//\n` |
| `colors` | `text[]` NOT NULL | `{W,U,B,R,G}` subset; faces' union when only per-face |
| `color_identity` | `text[]` NOT NULL | filter axis for cube semantics |
| `promo` | `boolean` NOT NULL | representative-printing tiebreaker |
| `image_small` | `text` NULL | front face; null when Scryfall has no image yet |
| `image_normal` | `text` NULL | |
| `back_image_small` | `text` NULL | only for two-sided layouts |
| `back_image_normal` | `text` NULL | |
| `updated_at` | `timestamptz` NOT NULL | set on upsert |

Indexes: btree on `oracle_id`; `GIN (normalized_name gin_trgm_ops)`. ~90k rows
after filtering — filtered non-name queries may seq-scan; no speculative
indexes until a real query hurts.

A `cards_staging` table with the identical shape (minus indexes) exists for
the import path; truncated at the start of each sync.

### `card_sync_runs`

`id bigserial PK`, `started_at`, `finished_at NULL`, `status`
(`running|succeeded|failed`), `bulk_updated_at timestamptz NULL` (Scryfall's
`updated_at` for the imported file), `cards_count int NULL`, `error text NULL`.

Drives the "is there a newer bulk file?" check and gives observability
(`SELECT * FROM card_sync_runs ORDER BY id DESC LIMIT 5`).

### Multi-face cards

Combined `name`/`type_line` as Scryfall provides at top level. `mana_cost` and
image URIs fall back to `card_faces[0]` when absent at top level (transform,
modal_dfc); back-face images from `card_faces[1]`. `oracle_text` is all faces'
text joined. `colors` is the union of face colors when top-level is absent.

## 3. Import & sync (`internal/cards`)

### Filter — paper-playable only

Keep a card iff **all** of:

- `games` contains `"paper"`
- `layout` ∉ {`token`, `double_faced_token`, `emblem`, `art_series`}
- `oversized` is false
- `set_type` ≠ `memorabilia`

Loosening the filter later = code change + next sync imports the rest.

### Sync flow

1. `GET {SCRYFALL_BASE_URL}/bulk-data/default-cards` (metadata; proper
   `User-Agent: cube-planner/<version>` and `Accept: application/json` per
   Scryfall API guidelines).
2. If `updated_at` ≤ `bulk_updated_at` of the last **succeeded** run → skip.
3. Insert a `running` sync row. Stream the `download_uri` body through
   `json.Decoder` token-by-token — the ~450MB file is never fully in memory.
4. Transform + filter each card object; `pgx.CopyFrom` batches into truncated
   `cards_staging`.
5. One transaction: upsert `cards_staging` → `cards`
   (`INSERT … ON CONFLICT (scryfall_id) DO UPDATE`), then
   `DELETE FROM cards WHERE scryfall_id NOT IN (SELECT … FROM cards_staging)`
   (handles Scryfall deletions/re-IDs). Mark the run `succeeded` with counts.
6. Any error: mark the run `failed` with the error text; `cards` is untouched
   — the app keeps serving the previous data. Next scheduled check retries.

### Scheduling

- On server start: if no `succeeded` run exists, launch sync immediately in a
  goroutine — the server serves all other routes meanwhile; search endpoints
  simply return empty results until the first import lands.
- A ticker checks metadata every 6h (cheap GET; Scryfall refreshes the file
  daily). Downloads only when `updated_at` is newer.
- Config (`platform/config`): `CARDS_SYNC_ENABLED` (default `true`; `false`
  in tests), `SCRYFALL_BASE_URL` (default `https://api.scryfall.com`;
  overridden with an `httptest` URL in integration tests).
- Server shutdown cancels the sync context; an aborted run is marked `failed`.

## 4. Search API

huma handlers in `internal/platform/httpapi`, logic in `internal/cards`
`Service` (`Autocomplete`, `Search`, `Printings`), sqlc queries in
`internal/db`. Composition in `cmd/server/main.go`, same pattern as auth.
`make api-generate` refreshes the TS client; CI freshness check applies.

All endpoints are **public** (card data isn't sensitive). Search results are
**oracle-level with a representative printing**: `DISTINCT ON (oracle_id)`
ordered by non-promo first, then latest `released_at`, then has-image.
Errors are huma-native RFC 7807. Empty result = empty array, not an error.

### `GET /api/cards/autocomplete?q=`

- `q` required, min 2 chars (huma validation → 422). Returns top 15.
- Ranking: exact normalized-prefix matches first, then `word_similarity`
  against `normalized_name` descending — "lighntin bol" finds Lightning Bolt.
- Item: `scryfall_id`, `oracle_id`, `name`, `mana_cost`, `type_line`,
  `image_small`.

### `GET /api/cards/search`

- Params, all optional, AND-combined:
  - `name` — fuzzy, same trgm ranking as autocomplete
  - `colors` — cube semantics: card `color_identity` ⊆ selected colors;
    selecting colorless-only matches `color_identity = {}`
  - `type` — case-insensitive substring of `type_line`
  - `cmc_min`, `cmc_max`
  - `rarity`, `set` (set code)
- `limit` (default 20, max 100) + `offset`; response carries `total`.
- Sort: `name` asc; relevance first when `name` is given.
- Item: autocomplete fields + `set_code`, `set_name`, `collector_number`,
  `rarity`, `cmc`, `colors`, `color_identity`, `oracle_text`, `image_normal`,
  `back_image_normal`.

### `GET /api/cards/{oracle_id}/printings`

All printings of one oracle card, `released_at` desc, same item shape as
search results — feeds the future "pick your printing" picker (cube editor,
collections). Unknown `oracle_id` → 404 problem.

## 5. Frontend

Per structure.md dependency rules (`routes` → `features` → `shared`):

- **`shared/ui/combobox.tsx`** — new domain-blind primitive: accessible
  autocomplete (ARIA combobox pattern — `role="combobox"`, `aria-expanded`,
  `aria-activedescendant`, Arrow/Enter/Escape), cva-styled with semantic
  tokens, controlled input, async options + render-option via props.
  Hand-rolled; no cmdk dependency.
- **`features/cards/`** vertical slice:
  - `api.ts` — TanStack Query hooks over the generated client:
    `useCardAutocomplete(q)` (debounced ~250ms, enabled at ≥2 chars),
    `useCardSearch(filters)`.
  - `components/CardAutocomplete.tsx` — wraps the shared combobox; options
    show name, mana cost, type line, thumbnail; emits selection via
    `onSelect(card)`. This is the surface the cube editor consumes in
    sub-project 3 via route-level composition (promote to shared only if a
    second feature ever needs it — per structure.md rule 1).
  - `components/CardSearchPage.tsx` — minimal demo page: the autocomplete
    plus a selected-card panel (image, set, oracle text). Exercises the whole
    pipeline end to end.
- Route `/cards` — thin, public. All strings via Paraglide `m.*()` (en + pl).
  `<Label htmlFor>` on the input; loading/empty/error states via existing
  primitives.

## 6. Testing

- **Go unit (table-driven):** bulk-JSON → card transform, including filter
  decisions. Fixtures: normal card, transform DFC, split card, token
  (excluded), digital-only (excluded), art series (excluded), memorabilia
  (excluded), diacritics normalization (e.g. `Lim-Dûl's Vault`).
- **Go integration (testcontainers):** an `httptest` server serves a small
  fixture bulk file (metadata + download endpoints). Run `Sync` → assert
  inserted rows; mutate the fixture → second sync → assert upsert,
  delete-missing, and skip-when-not-newer. Endpoint tests: ranking ("bolt" →
  Lightning Bolt first; typo tolerance), each search filter, printings order,
  422/404 problems.
- **Frontend:** RTL (happy-dom) for combobox behavior — debounce, results
  render, keyboard navigation, selection callback. Axe smoke test for the
  `/cards` screen (`// @vitest-environment jsdom`). API mocking consistent
  with existing auth feature tests.
- **CI:** nothing new — `make test` and the generated-client freshness check
  cover it.

## 7. Operational notes

- First deploy after this ships: the VPS server start triggers the initial
  import (~1–2 min download + insert); search is empty until it completes.
- `make db-reset` in dev wipes cards; next server start re-imports.
- Scryfall image URLs are hotlinked (allowed by Scryfall's guidelines for
  card images); we store URLs only, never image bytes.
