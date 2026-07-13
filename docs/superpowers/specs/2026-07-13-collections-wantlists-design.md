# Collections & Wantlists — Sub-project 4 Design

**Date:** 2026-07-13
**Status:** Approved. Implements master design §7.4
(`2026-07-10-cube-planner-master-design.md`).

## 1. Scope

Per-user card collection management (search-add with a printing picker,
quantity editing, bulk text import with a review step), the
cube-vs-collection wantlist computed on demand at oracle level, and a
client-side Cardmarket txt export.

**Decisions made during brainstorming:**

| Decision | Choice |
|---|---|
| Entry granularity | Printing-level — PK `(user_id, scryfall_id)`; owning 2× Beta Bolt and 1× M10 Bolt is two rows. Wantlist math aggregates to oracle level (master design §4) |
| Entry methods | Search/autocomplete add AND bulk text import (paste `4 Lightning Bolt` lines) with a server-side resolve + review step |
| Wantlist UX | Action on the cube display page (own or public cubes) → wantlist subpage + txt download; no standalone tool page |
| Visibility | Collections are strictly private — every endpoint operates on the session user; no visibility column |
| Printing picker | Built this sub-project for collections only; the cube editor keeps its representative-printing behavior (backlog) |
| Mutation model | Plain CRUD upserts, no changelog/versioning (approaches considered: cube-style changelog — rejected, no user story needs collection history; client-driven import via N autocomplete calls — rejected for request volume and ambiguity handling) |
| Export | Generated client-side from the wantlist JSON; no export endpoint |

**Non-goals:** collection sharing/browsing, CSV import
(deckbox/moxfield migration), foil/condition/language tracking, prices
and cardmarket deep links (niceties backlog), per-entry printing picker
in the cube editor, collection history.

## 2. Data model

Migration `backend/migrations/00005_collections.sql` (+ goose down).

**`collection_items`**
- `user_id` uuid FK → `users` ON DELETE CASCADE
- `scryfall_id` uuid FK → `cards(scryfall_id)` — the owned printing
- `oracle_id` uuid — denormalized from `cards` at write time (same
  convention as `cube_cards`)
- `quantity` int CHECK (quantity >= 1) — unlike `cube_cards` there is
  no two-statement depletion dance: setting quantity to 0 is a plain
  DELETE in the same handler, so no intermediate 0 state ever exists
- `created_at`, `updated_at` timestamptz
- **PK `(user_id, scryfall_id)`**
- Indexes: `(user_id, oracle_id)` for wantlist aggregation;
  `(scryfall_id)` mirroring the cubes convention for FK-side lookups

**Sync guard extension (data integrity):** the card mirror's
`DeleteCardsMissingFromStaging` currently refuses to delete cards
referenced by `cube_cards` or `cube_change_items`. It must additionally
refuse cards referenced by `collection_items`, or a Scryfall bulk-file
removal would violate the new FK mid-sync.

## 3. API surface

huma handlers in `backend/internal/platform/httpapi/collections.go`.
Exported wire types drive generated TS names. **All six operations
require a session** (collections are private; the wantlist compares
against the caller's own collection).

### Collection

- `GET /collection?search=&limit=&offset=` →
  `{items: CollectionItemEntry[], total, totalCopies}` — the session
  user's items sorted by name; `search` filters by name within the
  collection (plain case-insensitive substring `ILIKE` — no trigram
  machinery needed at collection scale); `total` counts distinct rows
  matching the filter, `totalCopies` sums their quantities.
- `PUT /collection/cards/{scryfallId}` `{quantity}` (0–999) —
  idempotent set-quantity upsert; `0` deletes the row (204); unknown
  `scryfallId` → 422 problem type `invalid-collection-item`.
- `POST /collection/cards/{scryfallId}/change-printing`
  `{newScryfallId}` → `CollectionItemEntry` — one transaction: verify
  the source row exists (else 422), verify `newScryfallId` differs from
  the source (else 422 — a naive add-then-delete on the same row would
  lose the row), verify the target printing exists and shares the
  source's `oracle_id` (else 422), add the source quantity onto the
  target row (upsert, clamped at 999), delete the source row.
  Exists so the "change printing" UI action is atomic — a client-side
  two-call composition could tear and leave a duplicated quantity.

### Import (two steps: resolve is a pure read, import commits)

- `POST /collection/import/resolve` `{text}` (≤ 64 KB, ≤ 500 non-blank
  lines, else 422) → `{lines: ImportResolveLine[]}`. Line grammar:
  optional quantity prefix (`4` or `4x`/`4X`, default 1, 1–999), then a
  card name; whitespace-trimmed; blank lines skipped. Each line
  resolves to one of:
  - `matched` — exact case-insensitive name match → that oracle card's
    representative printing (the same rule the autocomplete uses); if
    several oracle cards share the name, fall through to `ambiguous`
  - `ambiguous` — no exact match but fuzzy candidates exist (same
    trigram search as autocomplete, top 5 suggestions with images)
  - `unmatched` — nothing found, or an unparsable quantity
- `POST /collection/import` `{items: [{scryfallId, quantity}]}`
  (≤ 500 items, quantities 1–999) → `{addedRows, updatedRows}` —
  **adds** quantities onto existing rows (upsert-add); duplicate
  `scryfallId`s within one request are summed first; resulting
  quantities are clamped at 999 so 999 stays the per-printing maximum
  everywhere (PUT enforces the same bound); any unknown `scryfallId`
  fails the whole batch with 422 (the review step should never produce
  one).

### Wantlist

- `GET /cubes/{cubeId}/wantlist` → `{cubeName, items: WantlistEntry[],
  totalMissing}` — session required; cube visibility exactly as
  sub-project 3 (private cube for a non-owner → 404, no existence
  leak). One SQL query: per `cube_cards` row (already oracle-level),
  `missing = cube.quantity − Σ(caller's collection quantity for that
  oracle_id across all printings)`, keep rows with `missing > 0`, sort
  by name. Never stored (master design §4).

### Wire types

- `CollectionItemEntry`: scryfallId, oracleId, name, manaCost,
  typeLine, setCode, setName, collectorNumber, imageSmall, imageNormal,
  quantity
- `ImportResolveLine`: lineNumber, raw, quantity, status
  (`matched | ambiguous | unmatched`), match (CollectionItemEntry-shaped
  card fields, no quantity) when matched, suggestions (same shape,
  ≤ 5) when ambiguous
- `WantlistEntry`: oracleId, scryfallId (the cube's chosen printing,
  for imagery), name, manaCost, imageSmall, imageNormal,
  missingQuantity, cubeQuantity, ownedQuantity

After the backend lands, `make api-generate` refreshes
`frontend/src/shared/api/` (CI enforces freshness).

## 4. Backend structure

New package `backend/internal/collections/` (mirrors `internal/cubes`):

- `service.go` — `Service` over `db.Queries` + pgx pool: List,
  SetQuantity, ChangePrinting (tx), ResolveImport, ApplyImport (tx),
  Wantlist. Cube visibility for the wantlist is enforced inside the
  wantlist query/service (owner or public, else not-found) — no
  dependency on the cubes package.
- `parse.go` — **pure functions, no DB**: `parseImportText(text)` →
  `[]importLine` + per-line parse errors (quantity grammar, caps,
  blank-line skipping).
- `parse_test.go` — table-driven: bare name, `4 Name`, `4x Name`,
  `4X Name`, quantity 0 / 1000 / garbage → unparsable, surrounding
  whitespace, blank and whitespace-only lines skipped, 500-line cap,
  names containing digits (`Borrowing 100,000 Arrows`).
- `service_test.go` — unit tests where mocking is sensible.

sqlc: `backend/internal/db/queries/collections.sql` → generated
`collections.sql.go`. The change-printing and import transactions run
on pgx like `ApplyChange` does. The resolve step batches: one query for
exact-name matches across all lines, one fuzzy query per unresolved
line (≤ 500, and only misses pay it).

`internal/cards`: extend the staging-swap delete guard per §2.

HTTP: `platform/httpapi/collections.go` registers the six operations,
maps domain errors → problem+json. Handlers stay thin: parse → service
→ map.

No background work — plain request/response.

## 5. Frontend structure & UX

### Routes (thin, per structure.md)

- `/collection` — auth-guarded like `/cubes/mine`
- `/cubes/$cubeId/wantlist` — auth-guarded; renders for public cubes
  and own private cubes (404 component otherwise, same as display page)

### features/collection/

`api.ts` — TanStack Query hooks over the generated client:
`useCollection(search, page)`, `useSetQuantity`, `useChangePrinting`,
`useResolveImport`, `useImport`, `useWantlist(cubeId)`. Mutations
invalidate the collection list query on success — plain
mutate-and-invalidate, no optimistic reducer machinery (nothing here
has versions or conflicts). Plus `components/` and `lib/`.

### Collection page

- Header: distinct-card count + total copies (from the list response).
- `CardAutocomplete` (already in `shared/cards/`) at the top: selecting
  a card immediately calls import-add with 1× its representative
  printing (merges onto an existing row if present).
- Search-within-collection input (debounced) + paginated table: name,
  printing (set name + collector number), quantity stepper, change
  printing, remove (✕). Row hover/focus shows `imageNormal` via the
  existing hover-preview pattern.
- Quantity steppers debounce so rapid clicks land as ONE final
  set-quantity call (PUT is a set, so the final value wins; no
  lost-update risk against oneself).
- **Change printing** opens `PrintingPickerDialog` fed by the existing
  `GET /cards/{oracleId}/printings`: set/collector-number list with art
  thumbnails; picking one calls the atomic change-printing endpoint.
  The dialog lives in `shared/cards/` — the cube editor is a known
  future consumer (sub-project 3 backlog), same promotion argument as
  `CardAutocomplete`.
- **Import list** button → dialog: textarea → resolve → review table in
  three groups: matched (kept as-is), ambiguous (per-line suggestion
  select, defaulting to the top suggestion, with a "skip line" option),
  unmatched (shown, always skipped). Confirm commits matched +
  resolved-ambiguous lines via one import call → summary toast
  (`N added, M lines skipped`). Cancel discards everything.

### Wantlist page

Entry point: a "Compare with my collection" action on the cube display
page, visible when logged in. The page shows the cube name, a
name-sorted table — card (with hover preview), missing / in cube /
owned columns — `totalMissing` in the header, and a **Download for
Cardmarket** button. The txt is generated client-side
(`lib/cardmarket.ts`, pure function): one `<missingQuantity> <name>`
line per entry — the format Cardmarket's Wants import accepts —
downloaded as a Blob named `<cube-name>-wantlist.txt`. Empty state:
"you own everything in this cube".

### Conventions

All strings via `m.*()` (en + pl), semantic color tokens only, cva
variants, `<Label htmlFor>` + `role="alert"` + `aria-describedby` per
structure.md, axe smoke test per screen (`// @vitest-environment
jsdom`).

## 6. Testing & error handling

**Backend**
- Table-driven unit tests for `parse.go` (see §4 list).
- testcontainers integration tests: set-quantity matrix (insert /
  update / delete-at-0 / unknown printing 422 / quantity 1000 422);
  change-printing (merge onto existing target with 999 clamp, oracle
  mismatch 422, same-printing 422, missing source 422); import (adds
  onto existing rows, in-request duplicates summed and clamped at 999,
  unknown id fails batch, caps); resolve (exact
  match, ambiguous with suggestions, unmatched, duplicate-name
  fall-through); wantlist (aggregation across multiple owned printings
  of one oracle, quantity math, owner vs non-owner vs private 404,
  empty collection = whole cube, fully-owned = empty list); every
  endpoint 401 without a session; list pagination + search filter.
- `internal/cards`: sync guard keeps cards referenced only by
  `collection_items`.

**Frontend**
- vitest unit: `lib/cardmarket.ts`; import-review selection logic.
- RTL: import dialog flow (paste → resolve (mocked) → groups render →
  suggestion select → confirm payload), stepper debounce (one call,
  final value), collection table rendering, wantlist page with download
  button, empty states.
- axe smoke per screen.

**Error handling** (RFC 7807, master design §5)
- 422 `invalid-collection-item` / `invalid-import` → toast with problem
  `detail`
- 404 (private cube wantlist) → route-level error component
- 401 → existing auth redirect behavior

## 7. Order of work

1. Migration 00005 + sqlc queries + `internal/collections` (`parse.go`
   first, TDD) + sync-guard extension + httpapi + integration tests.
2. `make api-generate` → committed client.
3. `features/collection`: collection page (list → steppers → add →
   printing picker) → import dialog → wantlist page + cube-page entry
   point.
4. Manual acceptance in the browser (Mateusz), as in sub-projects 2–3.

## 8. Future work (logged, not in scope)

- Cube-editor printing picker: wire the now-shared
  `PrintingPickerDialog` into editor rows (needs a decision on whether
  a printing change writes a changelog entry).
- CSV import for deckbox/moxfield migrations.
- Collection sharing (public/private toggle + browse surface).
- Prices / cardmarket deep links, mana-symbol SVGs (niceties backlog).
- Popularity signal for autocomplete and resolve suggestions (carried
  from sub-project 2).
