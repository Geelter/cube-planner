# Collections & Wantlists (Sub-project 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-user printing-level collection (search-add, quantity steppers, printing picker, bulk text import with review) + on-demand cube-vs-collection wantlist with client-side Cardmarket txt export, per `docs/superpowers/specs/2026-07-13-collections-wantlists-design.md`.

**Architecture:** One new table `collection_items` PK `(user_id, scryfall_id)` with denormalized `oracle_id`; plain CRUD upserts (no versioning). New backend package `internal/collections` (pure `parse.go` + `Service`); six huma operations in `platform/httpapi/collections.go`. Wantlist is a single SQL join aggregating the caller's collection to oracle level against `cube_cards`. Frontend is a new `features/collection` slice; `CardHoverPreview` gets promoted to `shared/cards/` and a `Dialog` primitive lands in `shared/ui/`.

**Tech Stack:** Go + huma v2 + chi + sqlc (pgx/v5) + goose; React + TanStack Router/Query + Tailwind v4 + Paraglide; vitest + RTL + vitest-axe; testcontainers-go.

## Global Constraints

- Go module path: `github.com/mjabloniec/cube-planner/backend`. Frontend alias `@/` = `frontend/src/`.
- Format/lint: `gofumpt -w` on touched Go files; `pnpm --filter @cube-planner/frontend fmt` (oxfmt) and `lint` (oxlint). Never eslint/prettier. lefthook runs these pre-commit — if a commit fails on formatting, run the formatter and retry the commit.
- Backend tests: `cd backend && go test ./...` (integration tests need Docker running). Frontend: `pnpm --filter @cube-planner/frontend test`, typecheck via `pnpm --filter @cube-planner/frontend typecheck`.
- Every user-facing string is `m.key()` from `@/paraglide/messages`; `frontend/messages/en.json` and `frontend/messages/pl.json` must carry identical key sets. After editing messages run `pnpm --filter @cube-planner/frontend gen` if tests complain about missing message functions.
- Semantic color tokens only (`bg-surface`, `text-fg`, `text-fg-muted`, `border-border`, `bg-accent`, `text-danger`, …). No raw palette classes.
- huma v2.38 panics on pointer-typed query params — optional query params use value types (here: `search` empty string = no filter).
- Generated artifacts: `frontend/src/shared/api/` is regenerated ONLY via `make api-generate` and committed; `src/routeTree.gen.ts` + `src/paraglide/` are gitignored build output (`pnpm gen` recreates). `backend/internal/db/*.sql.go` regenerated via `cd backend && sqlc generate` and committed. Never hand-edit any of them.
- Per-printing quantity bounds: 0–999 on PUT (0 = delete); import/change-printing clamp results at 999 (`MaxItemQuantity`). Import caps: ≤ 500 lines / ≤ 500 items / text ≤ 64 KB.
- Commit messages: conventional prefixes (`feat(collections): …`), imperative mood, trailer:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- All tests in this plan sit in `features/`, `shared/`, or backend packages — no `src/routes/` test files (which would need a `-` prefix).
- Existing helpers you may use in `backend/internal/platform/httpapi/*_test.go` (same `httpapi_test` package, shared across files): `seedCard(t, pool, testCard{…})`, `loggedInClient(t, srv, q, email)`, `newCookieClient(t, srv)`, `c.do(t, method, path, body)`, `decode[T](t, resp)`, `getJSON(t, srv, path, out)`.

---

### Task 1: Migration 00005 + sync-guard extension

`collection_items` table, plus the card-mirror delete guard learns about it (a Scryfall bulk-file removal must never delete a card a collection references — it would violate the new FK mid-sync).

**Files:**
- Create: `backend/migrations/00005_collections.sql`
- Modify: `backend/internal/db/queries/cards.sql` (the `DeleteCardsMissingFromStaging` query)
- Modify: `backend/internal/cards/sync_test.go` (new guard test)
- Generated: `cd backend && sqlc generate` refreshes `backend/internal/db/` (schema-only change: models pick up `CollectionItem`)

**Interfaces:**
- Produces: table `collection_items (user_id uuid, scryfall_id uuid, oracle_id uuid, quantity int >= 1, created_at, updated_at, PK (user_id, scryfall_id))` — every later backend task builds on it.

- [ ] **Step 1: Write the migration** `backend/migrations/00005_collections.sql`:

```sql
-- +goose Up
-- Printing-level collection. quantity >= 1 (not >= 0 like cube_cards):
-- setting quantity to 0 is a plain DELETE in the same handler, so no
-- intermediate 0 state ever exists.
create table collection_items (
    user_id uuid not null references users (id) on delete cascade,
    scryfall_id uuid not null references cards (scryfall_id),
    oracle_id uuid not null,
    quantity int not null check (quantity >= 1),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (user_id, scryfall_id)
);

-- Wantlist aggregation groups a user's items by oracle card.
create index collection_items_user_oracle_idx on collection_items (user_id, oracle_id);
create index collection_items_scryfall_idx on collection_items (scryfall_id);

-- +goose Down
drop table collection_items;
```

- [ ] **Step 2: Regenerate sqlc models**

Run: `cd backend && sqlc generate && go build ./...`
Expected: clean build; `internal/db/models.go` now contains `CollectionItem`.

- [ ] **Step 3: Write the failing guard test.** Append to `backend/internal/cards/sync_test.go` (the file already imports `time`, `uuid`, `context`; `fixtureCard`, `idA`/`idB`, `oracleA`/`oracleB`, `newSyncTest` are package-level helpers in this file):

```go
func TestSyncKeepsCollectionReferencedCards(t *testing.T) {
	f, syncer, _, pool := newSyncTest(t)
	ctx := context.Background()

	f.cards = []scryfallCard{
		fixtureCard(idA, oracleA, "Alpha Strike"),
		fixtureCard(idB, oracleB, "Beta Blast"),
	}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// A user owns copies of B; B then vanishes from the bulk file.
	var userID uuid.UUID
	if err := pool.QueryRow(ctx,
		`insert into users (email, display_name) values ('owner@test', 'Owner') returning id`,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`insert into collection_items (user_id, scryfall_id, oracle_id, quantity)
		 values ($1, $2, $3, 2)`, userID, idB, oracleB); err != nil {
		t.Fatal(err)
	}

	f.updatedAt = f.updatedAt.Add(24 * time.Hour)
	f.cards = []scryfallCard{fixtureCard(idA, oracleA, "Alpha Strike")}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	var exists bool
	if err := pool.QueryRow(ctx,
		`select exists(select 1 from cards where scryfall_id = $1)`, idB).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("card B is in a collection and must survive the sync delete")
	}
}
```

- [ ] **Step 4: Run it to verify it fails**

Run: `cd backend && go test ./internal/cards/ -run TestSyncKeepsCollectionReferencedCards -v`
Expected: FAIL — either the delete violates the FK (sync errors) or B is gone.

- [ ] **Step 5: Extend the guard.** In `backend/internal/db/queries/cards.sql`, replace the `DeleteCardsMissingFromStaging` query (comment included) with:

```sql
-- Cards referenced by cubes (current lists or changelog history) or by a
-- user's collection are kept even if they vanish from the Scryfall bulk
-- file: those views join to cards, and the referencing tables have FKs
-- to cards.
-- name: DeleteCardsMissingFromStaging :execrows
delete from cards
where scryfall_id not in (select scryfall_id from cards_staging)
  and scryfall_id not in (select scryfall_id from cube_cards)
  and scryfall_id not in (select scryfall_id from cube_change_items)
  and scryfall_id not in (select scryfall_id from collection_items);
```

- [ ] **Step 6: Regenerate + run tests**

Run: `cd backend && sqlc generate && go test ./internal/cards/ -run 'TestSync' -v`
Expected: all sync tests PASS, including the new one.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/00005_collections.sql backend/internal/db backend/internal/cards/sync_test.go
git commit -m "feat(collections): collection_items table; sync guard keeps collection-referenced cards"
```

---

### Task 2: Collections sqlc queries

All SQL the collections service needs. No Go service code yet — this task's deliverable is generated, compiling query code.

**Files:**
- Create: `backend/internal/db/queries/collections.sql`
- Generated: `backend/internal/db/collections.sql.go` (via `sqlc generate`; committed)

**Interfaces:**
- Produces (generated on `*db.Queries`, used by Tasks 4–7):
  - `ListCollectionItems(ctx, ListCollectionItemsParams{UserID, Search *string, PageLimit, PageOffset int32}) ([]ListCollectionItemsRow, error)` — row carries display fields + `Total int64` + `TotalCopies int64` windows
  - `UpsertCollectionItem(ctx, UpsertCollectionItemParams{UserID, ScryfallID uuid.UUID, Quantity int32}) (CollectionItem, error)` — **set** semantics; `pgx.ErrNoRows` = unknown card
  - `AddCollectionItem(ctx, AddCollectionItemParams{UserID, ScryfallID uuid.UUID, Quantity int32}) error` — **add** semantics, result clamped at 999
  - `DeleteCollectionItem(ctx, DeleteCollectionItemParams{UserID, ScryfallID uuid.UUID}) (int64, error)`
  - `GetCollectionItemEntry(ctx, GetCollectionItemEntryParams{UserID, ScryfallID}) (GetCollectionItemEntryRow, error)`
  - `GetCollectionItemForUpdate(ctx, GetCollectionItemForUpdateParams{UserID, ScryfallID}) (CollectionItem, error)`
  - `GetOwnedScryfallIDs(ctx, GetOwnedScryfallIDsParams{UserID uuid.UUID, Ids []uuid.UUID}) ([]uuid.UUID, error)`
  - `GetCardsByNormalizedNames(ctx, names []string) ([]GetCardsByNormalizedNamesRow, error)`
  - `SuggestCardsByName(ctx, query string) ([]SuggestCardsByNameRow, error)`
  - `GetCubeWantlist(ctx, GetCubeWantlistParams{CubeID, UserID uuid.UUID}) ([]GetCubeWantlistRow, error)`

- [ ] **Step 1: Write `backend/internal/db/queries/collections.sql`**

```sql
-- The session user's collection, name-sorted, with display data and
-- window totals (distinct rows + summed copies) over the filtered set.
-- Plain ILIKE is enough at collection scale — no trigram machinery.
-- name: ListCollectionItems :many
select ci.scryfall_id, ci.oracle_id, ci.quantity,
    ca.name, ca.mana_cost, ca.type_line, ca.set_code, ca.set_name,
    ca.collector_number, ca.image_small, ca.image_normal,
    count(*) over () as total,
    sum(ci.quantity) over ()::bigint as total_copies
from collection_items ci
join cards ca on ca.scryfall_id = ci.scryfall_id
where ci.user_id = sqlc.arg(user_id)
  and (sqlc.narg(search)::text is null or ca.name ilike '%' || sqlc.narg(search) || '%')
order by ca.name, ca.set_code, ca.collector_number
limit sqlc.arg(page_limit)::int offset sqlc.arg(page_offset)::int;

-- Set-quantity upsert. Selecting from cards resolves oracle_id at write
-- time AND makes an unknown printing insert zero rows (pgx.ErrNoRows on
-- :one), which the service maps to the invalid-item error.
-- name: UpsertCollectionItem :one
insert into collection_items (user_id, scryfall_id, oracle_id, quantity)
select sqlc.arg(user_id), c.scryfall_id, c.oracle_id, sqlc.arg(quantity)
from cards c
where c.scryfall_id = sqlc.arg(scryfall_id)
on conflict (user_id, scryfall_id)
do update set quantity = excluded.quantity, updated_at = now()
returning *;

-- Add-quantity upsert (import + change-printing merge). 999 is the hard
-- per-printing maximum everywhere, so results clamp instead of erroring.
-- name: AddCollectionItem :exec
insert into collection_items (user_id, scryfall_id, oracle_id, quantity)
select sqlc.arg(user_id), c.scryfall_id, c.oracle_id, least(sqlc.arg(quantity)::int, 999)
from cards c
where c.scryfall_id = sqlc.arg(scryfall_id)
on conflict (user_id, scryfall_id)
do update set
    quantity = least(collection_items.quantity + excluded.quantity, 999),
    updated_at = now();

-- name: DeleteCollectionItem :execrows
delete from collection_items
where user_id = sqlc.arg(user_id) and scryfall_id = sqlc.arg(scryfall_id);

-- One item with display data (PUT / change-printing responses).
-- name: GetCollectionItemEntry :one
select ci.scryfall_id, ci.oracle_id, ci.quantity,
    ca.name, ca.mana_cost, ca.type_line, ca.set_code, ca.set_name,
    ca.collector_number, ca.image_small, ca.image_normal
from collection_items ci
join cards ca on ca.scryfall_id = ci.scryfall_id
where ci.user_id = sqlc.arg(user_id) and ci.scryfall_id = sqlc.arg(scryfall_id);

-- Locks the source row for the change-printing transaction.
-- name: GetCollectionItemForUpdate :one
select * from collection_items
where user_id = sqlc.arg(user_id) and scryfall_id = sqlc.arg(scryfall_id)
for update;

-- Which of these printings does the user already own? (import summary)
-- name: GetOwnedScryfallIDs :many
select scryfall_id from collection_items
where user_id = sqlc.arg(user_id) and scryfall_id = any(sqlc.arg(ids)::uuid[]);

-- Exact-name resolution for import: representative printing per oracle
-- card (same non-promo/newest/has-image rule as autocomplete). Several
-- oracle cards sharing one name all come back — the service treats that
-- name as ambiguous.
-- name: GetCardsByNormalizedNames :many
with matches as (
    select distinct on (oracle_id) *
    from cards
    where normalized_name = any(sqlc.arg(names)::text[])
    order by oracle_id, promo, released_at desc, (image_small is null)
)
select scryfall_id, oracle_id, name, normalized_name, mana_cost, type_line,
    set_code, set_name, collector_number, image_small, image_normal
from matches;

-- Fuzzy suggestions for one unresolved import line. Same <% + GUC
-- threshold setup as autocomplete (see cards.sql for why the operator
-- form matters); oracle-level with a representative printing.
-- name: SuggestCardsByName :many
with matches as (
    select distinct on (oracle_id) *
    from cards
    where sqlc.arg(query)::text <% normalized_name
    order by oracle_id, promo, released_at desc, (image_small is null)
)
select scryfall_id, oracle_id, name, mana_cost, type_line,
    set_code, set_name, collector_number, image_small, image_normal
from matches
order by
    word_similarity(sqlc.arg(query), normalized_name) desc,
    similarity(sqlc.arg(query), normalized_name) desc,
    name asc
limit 5;

-- Wantlist: per cube row (already oracle-level), missing = cube quantity
-- minus the user's copies of that oracle across all printings, rows with
-- missing > 0 only. Computed on demand, never stored.
-- name: GetCubeWantlist :many
select cc.oracle_id, cc.scryfall_id,
    cc.quantity as cube_quantity,
    coalesce(own.owned, 0)::int as owned_quantity,
    (cc.quantity - coalesce(own.owned, 0))::int as missing_quantity,
    ca.name, ca.mana_cost, ca.image_small, ca.image_normal
from cube_cards cc
join cards ca on ca.scryfall_id = cc.scryfall_id
left join (
    select oracle_id, sum(quantity)::int as owned
    from collection_items
    where user_id = sqlc.arg(user_id)
    group by oracle_id
) own on own.oracle_id = cc.oracle_id
where cc.cube_id = sqlc.arg(cube_id)
  and cc.quantity > coalesce(own.owned, 0)
order by ca.name;
```

- [ ] **Step 2: Generate and build**

Run: `cd backend && sqlc generate && go build ./...`
Expected: clean build, new `internal/db/collections.sql.go`.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/db
git commit -m "feat(collections): sqlc queries for collection CRUD, import resolution, wantlist"
```

---

### Task 3: Import line parser (pure Go, TDD)

**Files:**
- Create: `backend/internal/collections/parse.go`
- Test: `backend/internal/collections/parse_test.go`

**Interfaces:**
- Produces:
  - `const MaxImportLines = 500`, `const MaxItemQuantity = 999`
  - `var ErrTooManyLines = errors.New("import exceeds 500 lines")`
  - `type ParsedLine struct { LineNumber int32; Raw string; Quantity int32; Name string; OK bool }`
  - `func ParseImportText(text string) ([]ParsedLine, error)` — blank/whitespace-only lines skipped (but counted in `LineNumber`); `OK=false` lines carry `Raw` only.

- [ ] **Step 1: Write the failing tests** `backend/internal/collections/parse_test.go`:

```go
package collections

import (
	"errors"
	"strings"
	"testing"
)

func TestParseImportText(t *testing.T) {
	tests := []struct {
		name string
		line string
		want ParsedLine
	}{
		{"bare name", "Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "Lightning Bolt", Quantity: 1, Name: "Lightning Bolt", OK: true}},
		{"qty space name", "4 Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "4 Lightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"qty x suffix", "4x Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "4x Lightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"qty X suffix", "4X Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "4X Lightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"tab separator", "4\tLightning Bolt", ParsedLine{LineNumber: 1, Raw: "4\tLightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"surrounding whitespace", "  2 Sol Ring  ", ParsedLine{LineNumber: 1, Raw: "2 Sol Ring", Quantity: 2, Name: "Sol Ring", OK: true}},
		{"name starting with digits stays a name", "Borrowing 100,000 Arrows", ParsedLine{LineNumber: 1, Raw: "Borrowing 100,000 Arrows", Quantity: 1, Name: "Borrowing 100,000 Arrows", OK: true}},
		{"quantity zero unparsable", "0 Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "0 Lightning Bolt", OK: false}},
		{"quantity 1000 unparsable", "1000 Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "1000 Lightning Bolt", OK: false}},
		{"quantity without name unparsable", "4x", ParsedLine{LineNumber: 1, Raw: "4x", OK: false}},
		{"lone x is a name", "x Bolt", ParsedLine{LineNumber: 1, Raw: "x Bolt", Quantity: 1, Name: "x Bolt", OK: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseImportText(tt.line)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("lines = %d, want 1", len(got))
			}
			if got[0] != tt.want {
				t.Fatalf("got %+v, want %+v", got[0], tt.want)
			}
		})
	}
}

func TestParseImportTextSkipsBlankLinesButCountsThem(t *testing.T) {
	got, err := ParseImportText("Lightning Bolt\n\n   \n2 Sol Ring\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("lines = %d, want 2", len(got))
	}
	if got[0].LineNumber != 1 || got[1].LineNumber != 4 {
		t.Fatalf("line numbers = %d, %d; want 1, 4", got[0].LineNumber, got[1].LineNumber)
	}
	if got[1].Name != "Sol Ring" {
		t.Fatalf("CRLF line parsed as %q", got[1].Name)
	}
}

func TestParseImportTextLineCap(t *testing.T) {
	text := strings.Repeat("Lightning Bolt\n", MaxImportLines)
	if _, err := ParseImportText(text); err != nil {
		t.Fatalf("exactly %d lines must be fine: %v", MaxImportLines, err)
	}
	text += "One More\n"
	if _, err := ParseImportText(text); !errors.Is(err, ErrTooManyLines) {
		t.Fatalf("err = %v, want ErrTooManyLines", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/collections/ -v`
Expected: FAIL — package doesn't compile (`ParseImportText` undefined).

- [ ] **Step 3: Implement** `backend/internal/collections/parse.go`:

```go
// Package collections owns per-user card collections and the
// cube-vs-collection wantlist.
package collections

import (
	"errors"
	"strconv"
	"strings"
)

const (
	// MaxImportLines caps one pasted import.
	MaxImportLines = 500
	// MaxItemQuantity is the hard per-printing maximum everywhere:
	// the PUT bound, and the clamp for import / change-printing adds.
	MaxItemQuantity = 999
)

var ErrTooManyLines = errors.New("import exceeds 500 lines")

// ParsedLine is one non-blank line of a pasted import list.
// Grammar: optional quantity prefix ("4" or "4x"/"4X", 1–999), then a
// card name. A line whose first token is not numeric is a bare name
// with quantity 1. OK=false = unparsable (bad quantity or no name).
type ParsedLine struct {
	LineNumber int32 // 1-based position in the original text; blank lines count
	Raw        string
	Quantity   int32
	Name       string
	OK         bool
}

func ParseImportText(text string) ([]ParsedLine, error) {
	var out []ParsedLine
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if len(out) == MaxImportLines {
			return nil, ErrTooManyLines
		}
		out = append(out, parseLine(int32(i+1), line))
	}
	return out, nil
}

func parseLine(n int32, line string) ParsedLine {
	p := ParsedLine{LineNumber: n, Raw: line}
	first, rest := line, ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		first, rest = line[:i], strings.TrimSpace(line[i+1:])
	}
	qtyToken := first
	if len(qtyToken) > 1 && (strings.HasSuffix(qtyToken, "x") || strings.HasSuffix(qtyToken, "X")) {
		qtyToken = qtyToken[:len(qtyToken)-1]
	}
	qty, err := strconv.Atoi(qtyToken)
	if err != nil {
		// No leading quantity — the whole line is the name.
		p.Quantity, p.Name, p.OK = 1, line, true
		return p
	}
	if qty < 1 || qty > MaxItemQuantity || rest == "" {
		return p
	}
	p.Quantity, p.Name, p.OK = int32(qty), rest, true
	return p
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/collections/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/collections
git commit -m "feat(collections): import line parser"
```

---

### Task 4: Service + endpoints: list collection, set quantity (+ wiring)

Vertical slice: `Service` skeleton with `List`/`SetQuantity`, the `httpapi/collections.go` file with the first two operations, `Deps` wiring, `main.go` wiring, endpoint integration tests.

**Files:**
- Create: `backend/internal/collections/service.go`
- Create: `backend/internal/platform/httpapi/collections.go`
- Create: `backend/internal/platform/httpapi/collections_endpoints_test.go`
- Modify: `backend/internal/platform/httpapi/api.go` (Deps field + register call)
- Modify: `backend/cmd/server/main.go` (both `httpapi.Deps` literals — see Step 4)

**Interfaces:**
- Consumes: Task 2 queries; existing `CurrentUserID(ctx)`, `testdb.New`, `seedCard`, `loggedInClient`.
- Produces:
  - `collections.NewService(queries *db.Queries, pool *pgxpool.Pool) *Service`
  - `var ErrInvalidItem`, `ErrInvalidImport`, `ErrCubeNotFound` (Tasks 5–7 reuse)
  - `type ItemEntry struct { ScryfallID, OracleID uuid.UUID; Name, ManaCost, TypeLine, SetCode, SetName, CollectorNumber string; ImageSmall, ImageNormal *string; Quantity int32 }`
  - `(*Service).List(ctx, userID uuid.UUID, search string, limit, offset int32) ([]ItemEntry, int64, int64, error)` — items, total, totalCopies
  - `(*Service).SetQuantity(ctx, userID, scryfallID uuid.UUID, quantity int32) (*ItemEntry, error)` — nil entry when quantity 0 deleted the row
  - Wire type `CollectionItemEntry` + helper `collectionEntryFrom(e collections.ItemEntry) CollectionItemEntry` + `mapCollectionErr(err) error` + `parseScryfallID(raw string) (uuid.UUID, error)` in `httpapi/collections.go`
  - Operations `getCollection` (GET `/api/collection`), `setCollectionQuantity` (PUT `/api/collection/cards/{scryfallId}`)

- [ ] **Step 1: Write `backend/internal/collections/service.go`**

```go
package collections

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// Service owns collection CRUD, import resolution, and the wantlist.
// Collections are strictly private: every method operates on the
// session user's rows only.
type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewService(queries *db.Queries, pool *pgxpool.Pool) *Service {
	return &Service{queries: queries, pool: pool}
}

var (
	// ErrInvalidItem covers unknown printings, oracle mismatches, and
	// change-printing misuse — the whole 422 invalid-collection-item family.
	ErrInvalidItem = errors.New("invalid collection item")
	// ErrInvalidImport covers bad import batches (unknown ids, too many lines).
	ErrInvalidImport = errors.New("invalid import")
	// ErrCubeNotFound also covers "exists but private and you are not the
	// owner" — private cubes must not leak their existence (same rule as
	// the cubes package).
	ErrCubeNotFound = errors.New("cube not found")
)

// ItemEntry is one collection line with card display data.
type ItemEntry struct {
	ScryfallID      uuid.UUID
	OracleID        uuid.UUID
	Name            string
	ManaCost        string
	TypeLine        string
	SetCode         string
	SetName         string
	CollectorNumber string
	ImageSmall      *string
	ImageNormal     *string
	Quantity        int32
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, search string, limit, offset int32) ([]ItemEntry, int64, int64, error) {
	var q *string
	if search != "" {
		q = &search
	}
	rows, err := s.queries.ListCollectionItems(ctx, db.ListCollectionItemsParams{
		UserID: userID, Search: q, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, 0, 0, err
	}
	var total, copies int64
	if len(rows) > 0 {
		total, copies = rows[0].Total, rows[0].TotalCopies
	}
	entries := make([]ItemEntry, len(rows))
	for i, r := range rows {
		entries[i] = ItemEntry{
			ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
			ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
			SetName: r.SetName, CollectorNumber: r.CollectorNumber,
			ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			Quantity: r.Quantity,
		}
	}
	return entries, total, copies, nil
}

// SetQuantity is the idempotent set-quantity upsert; 0 deletes the row
// (nil entry, no error — deleting an absent row is a no-op by design).
func (s *Service) SetQuantity(ctx context.Context, userID, scryfallID uuid.UUID, quantity int32) (*ItemEntry, error) {
	if quantity == 0 {
		_, err := s.queries.DeleteCollectionItem(ctx, db.DeleteCollectionItemParams{
			UserID: userID, ScryfallID: scryfallID,
		})
		return nil, err
	}
	_, err := s.queries.UpsertCollectionItem(ctx, db.UpsertCollectionItemParams{
		UserID: userID, ScryfallID: scryfallID, Quantity: quantity,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidItem
	}
	if err != nil {
		return nil, err
	}
	return s.getEntry(ctx, userID, scryfallID)
}

func (s *Service) getEntry(ctx context.Context, userID, scryfallID uuid.UUID) (*ItemEntry, error) {
	r, err := s.queries.GetCollectionItemEntry(ctx, db.GetCollectionItemEntryParams{
		UserID: userID, ScryfallID: scryfallID,
	})
	if err != nil {
		return nil, err
	}
	return &ItemEntry{
		ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
		ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
		SetName: r.SetName, CollectorNumber: r.CollectorNumber,
		ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
		Quantity: r.Quantity,
	}, nil
}
```

- [ ] **Step 2: Write `backend/internal/platform/httpapi/collections.go`**

```go
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/collections"
)

// CollectionItemEntry is exported: huma derives OpenAPI schema names
// (and generated TS type names) from Go type names.
type CollectionItemEntry struct {
	ScryfallID      uuid.UUID `json:"scryfallId"`
	OracleID        uuid.UUID `json:"oracleId"`
	Name            string    `json:"name"`
	ManaCost        string    `json:"manaCost"`
	TypeLine        string    `json:"typeLine"`
	SetCode         string    `json:"setCode"`
	SetName         string    `json:"setName"`
	CollectorNumber string    `json:"collectorNumber"`
	ImageSmall      *string   `json:"imageSmall"`
	ImageNormal     *string   `json:"imageNormal"`
	Quantity        int32     `json:"quantity"`
}

func collectionEntryFrom(e collections.ItemEntry) CollectionItemEntry {
	return CollectionItemEntry{
		ScryfallID: e.ScryfallID, OracleID: e.OracleID, Name: e.Name,
		ManaCost: e.ManaCost, TypeLine: e.TypeLine, SetCode: e.SetCode,
		SetName: e.SetName, CollectorNumber: e.CollectorNumber,
		ImageSmall: e.ImageSmall, ImageNormal: e.ImageNormal,
		Quantity: e.Quantity,
	}
}

func mapCollectionErr(err error) error {
	switch {
	case errors.Is(err, collections.ErrCubeNotFound):
		return huma.Error404NotFound("cube not found")
	case errors.Is(err, collections.ErrInvalidItem):
		return &huma.ErrorModel{
			Status: http.StatusUnprocessableEntity, Type: "invalid-collection-item",
			Title: "Unprocessable Entity", Detail: err.Error(),
		}
	case errors.Is(err, collections.ErrInvalidImport):
		return &huma.ErrorModel{
			Status: http.StatusUnprocessableEntity, Type: "invalid-import",
			Title: "Unprocessable Entity", Detail: err.Error(),
		}
	default:
		return err
	}
}

// parseScryfallID: a malformed printing id is just an unknown printing —
// same 422 family as the rest of the invalid-collection-item errors.
func parseScryfallID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, mapCollectionErr(collections.ErrInvalidItem)
	}
	return id, nil
}

type getCollectionInput struct {
	Search string `query:"search" maxLength:"100" doc:"Name filter (substring, case-insensitive)"`
	Limit  int32  `query:"limit" minimum:"1" maximum:"100" default:"50"`
	Offset int32  `query:"offset" minimum:"0" default:"0"`
}

type getCollectionOutput struct {
	Body struct {
		Items       []CollectionItemEntry `json:"items"`
		Total       int64                 `json:"total" doc:"Distinct printings matching the filter"`
		TotalCopies int64                 `json:"totalCopies" doc:"Sum of their quantities"`
	}
}

type setCollectionQuantityInput struct {
	ScryfallID string `path:"scryfallId"`
	Body       struct {
		Quantity int32 `json:"quantity" minimum:"0" maximum:"999" doc:"0 deletes the row"`
	}
}

type collectionItemOutput struct {
	Body struct {
		Item *CollectionItemEntry `json:"item" doc:"null after a quantity-0 delete"`
	}
}

func registerCollections(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "getCollection",
		Method:      http.MethodGet,
		Path:        "/api/collection",
		Summary:     "The session user's collection",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *getCollectionInput) (*getCollectionOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		entries, total, copies, err := deps.Collections.List(ctx, uid, in.Search, in.Limit, in.Offset)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &getCollectionOutput{}
		out.Body.Total = total
		out.Body.TotalCopies = copies
		out.Body.Items = make([]CollectionItemEntry, len(entries))
		for i, e := range entries {
			out.Body.Items[i] = collectionEntryFrom(e)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "setCollectionQuantity",
		Method:      http.MethodPut,
		Path:        "/api/collection/cards/{scryfallId}",
		Summary:     "Set the owned quantity of a printing (0 deletes)",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *setCollectionQuantityInput) (*collectionItemOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseScryfallID(in.ScryfallID)
		if err != nil {
			return nil, err
		}
		entry, err := deps.Collections.SetQuantity(ctx, uid, id, in.Body.Quantity)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &collectionItemOutput{}
		if entry != nil {
			e := collectionEntryFrom(*entry)
			out.Body.Item = &e
		}
		return out, nil
	})
}
```

- [ ] **Step 3: Wire `Deps`.** In `backend/internal/platform/httpapi/api.go`:
  - add import `"github.com/mjabloniec/cube-planner/backend/internal/collections"`
  - add field `Collections *collections.Service` to `Deps` (after `Cubes`)
  - add `registerCollections(api, deps)` after `registerCubes(api, deps)`

- [ ] **Step 4: Wire `main.go`.** In `backend/cmd/server/main.go`, add import `"github.com/mjabloniec/cube-planner/backend/internal/collections"` and add `Collections: collections.NewService(queries, pool),` to the runtime `httpapi.Deps` literal (next to `Cubes:` around line 62). The OpenAPI-generation call `httpapi.Build(httpapi.Deps{})` (~line 77) needs no change — registration must stay I/O-free.

- [ ] **Step 5: Build**

Run: `cd backend && go build ./...`
Expected: clean.

- [ ] **Step 6: Write endpoint integration tests** `backend/internal/platform/httpapi/collections_endpoints_test.go`:

```go
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/cards"
	"github.com/mjabloniec/cube-planner/backend/internal/collections"
	"github.com/mjabloniec/cube-planner/backend/internal/cubes"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func newCollectionsServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:        auth.NewService(q, noopMailer{}, "http://test"),
		Sessions:    auth.NewSessions(q, false),
		Queries:     q,
		Cards:       cards.NewService(q),
		Cubes:       cubes.NewService(q, pool),
		Collections: collections.NewService(q, pool),
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q
}

type collectionItemBody struct {
	ScryfallID string `json:"scryfallId"`
	OracleID   string `json:"oracleId"`
	Name       string `json:"name"`
	SetName    string `json:"setName"`
	Quantity   int32  `json:"quantity"`
}

type collectionListBody struct {
	Items       []collectionItemBody `json:"items"`
	Total       int64                `json:"total"`
	TotalCopies int64                `json:"totalCopies"`
}

type collectionItemResp struct {
	Item *collectionItemBody `json:"item"`
}

func putQuantity(t *testing.T, c *cookieClient, scryfallID uuid.UUID, quantity int32) *http.Response {
	t.Helper()
	return c.do(t, "PUT", "/api/collection/cards/"+scryfallID.String(),
		fmt.Sprintf(`{"quantity":%d}`, quantity))
}

func TestCollectionRequiresAuth(t *testing.T) {
	srv, _, _ := newCollectionsServer(t)
	if code := getJSON(t, srv, "/api/collection", nil); code != http.StatusUnauthorized {
		t.Fatalf("GET /api/collection anonymous = %d, want 401", code)
	}
}

func TestSetQuantityLifecycle(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "col1@test.dev")
	boltS, boltO := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: boltO, name: "Lightning Bolt"})

	// Insert.
	resp := putQuantity(t, c, boltS, 3)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("insert = %d, want 200", resp.StatusCode)
	}
	body := decode[collectionItemResp](t, resp)
	if body.Item == nil || body.Item.Quantity != 3 || body.Item.Name != "Lightning Bolt" {
		t.Fatalf("insert body = %+v", body.Item)
	}

	// Idempotent set.
	resp = putQuantity(t, c, boltS, 7)
	if body := decode[collectionItemResp](t, resp); body.Item == nil || body.Item.Quantity != 7 {
		t.Fatalf("update body = %+v", body.Item)
	}

	// Delete at 0 → null item; row gone.
	resp = putQuantity(t, c, boltS, 0)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete = %d, want 200", resp.StatusCode)
	}
	if body := decode[collectionItemResp](t, resp); body.Item != nil {
		t.Fatalf("delete body = %+v, want null item", body.Item)
	}
	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Total != 0 {
		t.Fatalf("total after delete = %d, want 0", list.Total)
	}

	// Deleting again is a no-op, not an error.
	if resp = putQuantity(t, c, boltS, 0); resp.StatusCode != http.StatusOK {
		t.Fatalf("re-delete = %d, want 200", resp.StatusCode)
	}

	// Unknown printing → 422; quantity 1000 → huma 422.
	if resp = putQuantity(t, c, uuid.New(), 1); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unknown printing = %d, want 422", resp.StatusCode)
	}
	if resp = putQuantity(t, c, boltS, 1000); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("quantity 1000 = %d, want 422", resp.StatusCode)
	}
}

func TestCollectionListSearchAndPagination(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "col2@test.dev")
	other := loggedInClient(t, srv, q, "col2-other@test.dev")

	boltS, ringS := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: uuid.New(), name: "Lightning Bolt"})
	seedCard(t, pool, testCard{scryfallID: ringS, oracleID: uuid.New(), name: "Sol Ring", typeLine: "Artifact", colorIdentity: []string{}})

	putQuantity(t, c, boltS, 4)
	putQuantity(t, c, ringS, 1)
	putQuantity(t, other, boltS, 9) // must not leak into c's list

	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Total != 2 || list.TotalCopies != 5 {
		t.Fatalf("total=%d copies=%d, want 2/5", list.Total, list.TotalCopies)
	}
	// Name-sorted: Lightning Bolt before Sol Ring.
	if list.Items[0].Name != "Lightning Bolt" || list.Items[1].Name != "Sol Ring" {
		t.Fatalf("order = %q, %q", list.Items[0].Name, list.Items[1].Name)
	}

	filtered := decode[collectionListBody](t, c.do(t, "GET", "/api/collection?search=bolt", ""))
	if filtered.Total != 1 || filtered.Items[0].Name != "Lightning Bolt" {
		t.Fatalf("filtered = %+v", filtered)
	}

	page2 := decode[collectionListBody](t, c.do(t, "GET", "/api/collection?limit=1&offset=1", ""))
	if len(page2.Items) != 1 || page2.Items[0].Name != "Sol Ring" || page2.Total != 2 {
		t.Fatalf("page2 = %+v", page2)
	}
}
```

Add `"fmt"` to the import block (used by `putQuantity`).

- [ ] **Step 7: Run the tests**

Run: `cd backend && go test ./internal/platform/httpapi/ -run 'TestCollection|TestSetQuantity' -v`
Expected: PASS (Docker must be running).

- [ ] **Step 8: Commit**

```bash
git add backend/internal/collections backend/internal/platform/httpapi backend/cmd/server
git commit -m "feat(collections): collection list + set-quantity endpoints"
```

---

### Task 5: Change-printing endpoint (atomic re-key)

**Files:**
- Modify: `backend/internal/collections/service.go` (add `ChangePrinting`)
- Modify: `backend/internal/platform/httpapi/collections.go` (add operation)
- Modify: `backend/internal/platform/httpapi/collections_endpoints_test.go` (add tests)

**Interfaces:**
- Consumes: Task 2 queries (`GetCollectionItemForUpdate`, `AddCollectionItem`, `DeleteCollectionItem`), existing `GetCardsByScryfallIDs`.
- Produces: `(*Service).ChangePrinting(ctx, userID, fromScryfallID, toScryfallID uuid.UUID) (*ItemEntry, error)`; operation `changeCollectionPrinting` (POST `/api/collection/cards/{scryfallId}/change-printing`).

- [ ] **Step 1: Write the failing tests.** Append to `collections_endpoints_test.go`:

```go
func changePrinting(t *testing.T, c *cookieClient, from, to uuid.UUID) *http.Response {
	t.Helper()
	return c.do(t, "POST", "/api/collection/cards/"+from.String()+"/change-printing",
		fmt.Sprintf(`{"newScryfallId":%q}`, to))
}

func TestChangePrinting(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "col3@test.dev")

	boltO := uuid.New()
	alphaS, m10S := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: alphaS, oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	seedCard(t, pool, testCard{scryfallID: m10S, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	strikeS := uuid.New()
	seedCard(t, pool, testCard{scryfallID: strikeS, oracleID: uuid.New(), name: "Lightning Strike"})

	// Simple re-key: 3× alpha → 3× m10, alpha row gone.
	putQuantity(t, c, alphaS, 3)
	resp := changePrinting(t, c, alphaS, m10S)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change = %d, want 200", resp.StatusCode)
	}
	if body := decode[collectionItemResp](t, resp); body.Item == nil ||
		body.Item.ScryfallID != m10S.String() || body.Item.Quantity != 3 {
		t.Fatalf("changed item = %+v", body.Item)
	}
	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Total != 1 {
		t.Fatalf("total = %d, want 1 (source row re-keyed)", list.Total)
	}

	// Merge onto an existing target, clamped at 999.
	putQuantity(t, c, alphaS, 998)
	resp = changePrinting(t, c, alphaS, m10S) // 998 + 3, clamps to 999
	if body := decode[collectionItemResp](t, resp); body.Item == nil || body.Item.Quantity != 999 {
		t.Fatalf("merged item = %+v, want quantity 999", body.Item)
	}

	// 422 family: same printing, oracle mismatch, missing source, unknown target.
	putQuantity(t, c, alphaS, 1)
	for name, resp := range map[string]*http.Response{
		"same printing":   changePrinting(t, c, alphaS, alphaS),
		"oracle mismatch": changePrinting(t, c, alphaS, strikeS),
		"missing source":  changePrinting(t, c, uuid.New(), m10S),
		"unknown target":  changePrinting(t, c, alphaS, uuid.New()),
	} {
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s = %d, want 422", name, resp.StatusCode)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestChangePrinting -v`
Expected: FAIL — 404 (route not registered).

- [ ] **Step 3: Implement the service method.** Append to `backend/internal/collections/service.go` (add `"fmt"` to imports):

```go
// ChangePrinting re-keys an owned printing to another printing of the
// same oracle card in one transaction: add the source quantity onto the
// target row (clamped at 999), delete the source. Atomic so a torn pair
// can never leave a duplicated quantity.
func (s *Service) ChangePrinting(ctx context.Context, userID, fromID, toID uuid.UUID) (*ItemEntry, error) {
	if fromID == toID {
		// A naive add-then-delete on the same row would lose the row.
		return nil, fmt.Errorf("%w: target is the same printing", ErrInvalidItem)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.queries.WithTx(tx)

	src, err := qtx.GetCollectionItemForUpdate(ctx, db.GetCollectionItemForUpdateParams{
		UserID: userID, ScryfallID: fromID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: printing not in collection", ErrInvalidItem)
	}
	if err != nil {
		return nil, err
	}
	targets, err := qtx.GetCardsByScryfallIDs(ctx, []uuid.UUID{toID})
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: unknown target printing", ErrInvalidItem)
	}
	if targets[0].OracleID != src.OracleID {
		return nil, fmt.Errorf("%w: target is a different card", ErrInvalidItem)
	}
	if err := qtx.AddCollectionItem(ctx, db.AddCollectionItemParams{
		UserID: userID, ScryfallID: toID, Quantity: src.Quantity,
	}); err != nil {
		return nil, err
	}
	if _, err := qtx.DeleteCollectionItem(ctx, db.DeleteCollectionItemParams{
		UserID: userID, ScryfallID: fromID,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getEntry(ctx, userID, toID)
}
```

- [ ] **Step 4: Register the operation.** Append to `registerCollections` in `httpapi/collections.go`, with the input type added next to the others:

```go
type changePrintingInput struct {
	ScryfallID string `path:"scryfallId"`
	Body       struct {
		NewScryfallID uuid.UUID `json:"newScryfallId"`
	}
}
```

```go
	huma.Register(api, huma.Operation{
		OperationID: "changeCollectionPrinting",
		Method:      http.MethodPost,
		Path:        "/api/collection/cards/{scryfallId}/change-printing",
		Summary:     "Re-key an owned printing to another printing of the same card",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *changePrintingInput) (*collectionItemOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseScryfallID(in.ScryfallID)
		if err != nil {
			return nil, err
		}
		entry, err := deps.Collections.ChangePrinting(ctx, uid, id, in.Body.NewScryfallID)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &collectionItemOutput{}
		e := collectionEntryFrom(*entry)
		out.Body.Item = &e
		return out, nil
	})
```

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestChangePrinting -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/collections backend/internal/platform/httpapi
git commit -m "feat(collections): atomic change-printing endpoint"
```

---

### Task 6: Import endpoints (resolve + commit)

**Files:**
- Modify: `backend/internal/collections/service.go` (add `ResolveImport`, `ApplyImport`)
- Modify: `backend/internal/platform/httpapi/collections.go` (two operations + wire types)
- Modify: `backend/internal/platform/httpapi/collections_endpoints_test.go` (add tests)

**Interfaces:**
- Consumes: Task 3 `ParseImportText`; Task 2 queries; `cards.NormalizeName(name string) string` from `internal/cards`.
- Produces:
  - `type CardRef struct { ScryfallID, OracleID uuid.UUID; Name, ManaCost, TypeLine, SetCode, SetName, CollectorNumber string; ImageSmall, ImageNormal *string }`
  - `type ResolvedLine struct { LineNumber int32; Raw string; Quantity int32; Status string; Match *CardRef; Suggestions []CardRef }` — `Status ∈ matched|ambiguous|unmatched`
  - `(*Service).ResolveImport(ctx, text string) ([]ResolvedLine, error)` — pure read
  - `type ImportItem struct { ScryfallID uuid.UUID; Quantity int32 }`
  - `(*Service).ApplyImport(ctx, userID uuid.UUID, items []ImportItem) (added, updated int32, err error)`
  - Operations `resolveCollectionImport` (POST `/api/collection/import/resolve`), `importCollectionItems` (POST `/api/collection/import`); wire types `ImportCardMatch`, `ImportResolveLine`

- [ ] **Step 1: Write the failing tests.** Append to `collections_endpoints_test.go`:

```go
type importLineBody struct {
	LineNumber int32  `json:"lineNumber"`
	Raw        string `json:"raw"`
	Quantity   int32  `json:"quantity"`
	Status     string `json:"status"`
	Match      *struct {
		ScryfallID string `json:"scryfallId"`
		Name       string `json:"name"`
	} `json:"match"`
	Suggestions []struct {
		ScryfallID string `json:"scryfallId"`
		Name       string `json:"name"`
	} `json:"suggestions"`
}

type resolveBody struct {
	Lines []importLineBody `json:"lines"`
}

type importResultBody struct {
	AddedRows   int32 `json:"addedRows"`
	UpdatedRows int32 `json:"updatedRows"`
}

func TestResolveImport(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "imp1@test.dev")

	boltO := uuid.New()
	// Two printings of Bolt: representative = newer non-promo.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	newBolt := uuid.New()
	seedCard(t, pool, testCard{scryfallID: newBolt, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	// Duplicate name across two oracle ids → ambiguous even on exact match.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Twin Name"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Twin Name"})

	resp := c.do(t, "POST", "/api/collection/import/resolve",
		`{"text":"4 Lightning Bolt\nLihgtning Blot\nTwin Name\n17 Utter Gibberish Nonexistent\n0 Lightning Bolt"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve = %d, want 200", resp.StatusCode)
	}
	body := decode[resolveBody](t, resp)
	if len(body.Lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(body.Lines))
	}

	exact := body.Lines[0]
	if exact.Status != "matched" || exact.Quantity != 4 ||
		exact.Match == nil || exact.Match.ScryfallID != newBolt.String() {
		t.Fatalf("exact line = %+v (want matched, representative printing)", exact)
	}
	fuzzy := body.Lines[1]
	if fuzzy.Status != "ambiguous" || len(fuzzy.Suggestions) == 0 ||
		fuzzy.Suggestions[0].Name != "Lightning Bolt" {
		t.Fatalf("fuzzy line = %+v (want ambiguous with Bolt suggestion)", fuzzy)
	}
	twin := body.Lines[2]
	if twin.Status != "ambiguous" || len(twin.Suggestions) != 2 {
		t.Fatalf("duplicate-name line = %+v (want ambiguous, 2 suggestions)", twin)
	}
	if body.Lines[3].Status != "unmatched" {
		t.Fatalf("gibberish line = %+v, want unmatched", body.Lines[3])
	}
	if body.Lines[4].Status != "unmatched" {
		t.Fatalf("bad-quantity line = %+v, want unmatched", body.Lines[4])
	}
}

func TestApplyImport(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "imp2@test.dev")

	boltS, ringS := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: uuid.New(), name: "Lightning Bolt"})
	seedCard(t, pool, testCard{scryfallID: ringS, oracleID: uuid.New(), name: "Sol Ring", typeLine: "Artifact", colorIdentity: []string{}})
	putQuantity(t, c, ringS, 2) // pre-existing row → "updated"

	// In-request duplicates sum (2+2 bolt), adds stack on existing rows.
	resp := c.do(t, "POST", "/api/collection/import", fmt.Sprintf(
		`{"items":[{"scryfallId":%q,"quantity":2},{"scryfallId":%q,"quantity":2},{"scryfallId":%q,"quantity":1}]}`,
		boltS, boltS, ringS))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import = %d, want 200", resp.StatusCode)
	}
	res := decode[importResultBody](t, resp)
	if res.AddedRows != 1 || res.UpdatedRows != 1 {
		t.Fatalf("added=%d updated=%d, want 1/1", res.AddedRows, res.UpdatedRows)
	}
	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.TotalCopies != 7 { // 4 bolts + 3 rings
		t.Fatalf("copies = %d, want 7", list.TotalCopies)
	}

	// Clamp at 999.
	resp = c.do(t, "POST", "/api/collection/import", fmt.Sprintf(
		`{"items":[{"scryfallId":%q,"quantity":999}]}`, boltS))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clamp import = %d", resp.StatusCode)
	}
	list = decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Items[0].Quantity != 999 {
		t.Fatalf("bolt quantity = %d, want clamped 999", list.Items[0].Quantity)
	}

	// Unknown id fails the whole batch.
	resp = c.do(t, "POST", "/api/collection/import", fmt.Sprintf(
		`{"items":[{"scryfallId":%q,"quantity":1},{"scryfallId":%q,"quantity":1}]}`,
		boltS, uuid.New()))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unknown id = %d, want 422", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/platform/httpapi/ -run 'TestResolveImport|TestApplyImport' -v`
Expected: FAIL — 404s.

- [ ] **Step 3: Implement the service methods.** Append to `backend/internal/collections/service.go` (add import `"github.com/mjabloniec/cube-planner/backend/internal/cards"`):

```go
// CardRef is a resolved card reference for import review (a match or a
// suggestion) — display fields, no quantity.
type CardRef struct {
	ScryfallID      uuid.UUID
	OracleID        uuid.UUID
	Name            string
	ManaCost        string
	TypeLine        string
	SetCode         string
	SetName         string
	CollectorNumber string
	ImageSmall      *string
	ImageNormal     *string
}

// ResolvedLine statuses.
const (
	StatusMatched   = "matched"
	StatusAmbiguous = "ambiguous"
	StatusUnmatched = "unmatched"
)

type ResolvedLine struct {
	LineNumber  int32
	Raw         string
	Quantity    int32
	Status      string
	Match       *CardRef
	Suggestions []CardRef
}

// ResolveImport parses pasted text and resolves each line. Pure read:
// nothing is written. Exact (case-insensitive, normalized) name matches
// resolve to the oracle card's representative printing; a name shared by
// several oracle cards falls through to ambiguous; misses get fuzzy
// suggestions or unmatched.
func (s *Service) ResolveImport(ctx context.Context, text string) ([]ResolvedLine, error) {
	lines, err := ParseImportText(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidImport, err)
	}

	nameSet := make(map[string]struct{})
	var names []string
	for _, l := range lines {
		if !l.OK {
			continue
		}
		n := cards.NormalizeName(l.Name)
		if _, seen := nameSet[n]; !seen {
			nameSet[n] = struct{}{}
			names = append(names, n)
		}
	}
	exact := make(map[string][]CardRef)
	if len(names) > 0 {
		rows, err := s.queries.GetCardsByNormalizedNames(ctx, names)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			exact[r.NormalizedName] = append(exact[r.NormalizedName], CardRef{
				ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
				ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
				SetName: r.SetName, CollectorNumber: r.CollectorNumber,
				ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			})
		}
	}

	out := make([]ResolvedLine, len(lines))
	for i, l := range lines {
		rl := ResolvedLine{LineNumber: l.LineNumber, Raw: l.Raw, Quantity: l.Quantity}
		switch {
		case !l.OK:
			rl.Status = StatusUnmatched
		default:
			matches := exact[cards.NormalizeName(l.Name)]
			switch len(matches) {
			case 1:
				rl.Status = StatusMatched
				m := matches[0]
				rl.Match = &m
			case 0:
				// Only misses pay for a fuzzy query.
				suggestions, err := s.suggest(ctx, l.Name)
				if err != nil {
					return nil, err
				}
				if len(suggestions) > 0 {
					rl.Status = StatusAmbiguous
					rl.Suggestions = suggestions
				} else {
					rl.Status = StatusUnmatched
				}
			default:
				// One name, several oracle cards — the user must choose.
				rl.Status = StatusAmbiguous
				rl.Suggestions = matches
			}
		}
		out[i] = rl
	}
	return out, nil
}

func (s *Service) suggest(ctx context.Context, name string) ([]CardRef, error) {
	rows, err := s.queries.SuggestCardsByName(ctx, cards.NormalizeName(name))
	if err != nil {
		return nil, err
	}
	refs := make([]CardRef, len(rows))
	for i, r := range rows {
		refs[i] = CardRef{
			ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
			ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
			SetName: r.SetName, CollectorNumber: r.CollectorNumber,
			ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
		}
	}
	return refs, nil
}

type ImportItem struct {
	ScryfallID uuid.UUID
	Quantity   int32
}

// ApplyImport ADDS quantities onto existing rows. In-request duplicates
// are summed first; results clamp at 999; any unknown printing fails the
// whole batch (the review step should never produce one).
func (s *Service) ApplyImport(ctx context.Context, userID uuid.UUID, items []ImportItem) (added, updated int32, err error) {
	merged := make(map[uuid.UUID]int32, len(items))
	order := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		if _, seen := merged[it.ScryfallID]; !seen {
			order = append(order, it.ScryfallID)
		}
		q := merged[it.ScryfallID] + it.Quantity
		if q > MaxItemQuantity {
			q = MaxItemQuantity
		}
		merged[it.ScryfallID] = q
	}

	known, err := s.queries.GetCardsByScryfallIDs(ctx, order)
	if err != nil {
		return 0, 0, err
	}
	if len(known) != len(order) {
		return 0, 0, fmt.Errorf("%w: unknown printing in batch", ErrInvalidImport)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.queries.WithTx(tx)

	owned, err := qtx.GetOwnedScryfallIDs(ctx, db.GetOwnedScryfallIDsParams{
		UserID: userID, Ids: order,
	})
	if err != nil {
		return 0, 0, err
	}
	ownedSet := make(map[uuid.UUID]struct{}, len(owned))
	for _, id := range owned {
		ownedSet[id] = struct{}{}
	}
	for _, id := range order {
		if err := qtx.AddCollectionItem(ctx, db.AddCollectionItemParams{
			UserID: userID, ScryfallID: id, Quantity: merged[id],
		}); err != nil {
			return 0, 0, err
		}
		if _, ok := ownedSet[id]; ok {
			updated++
		} else {
			added++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return added, updated, nil
}
```

- [ ] **Step 4: Register the operations.** In `httpapi/collections.go`, add wire types next to `CollectionItemEntry`:

```go
// ImportCardMatch is a resolved card for import review (match or
// suggestion) — CollectionItemEntry shape without quantity.
type ImportCardMatch struct {
	ScryfallID      uuid.UUID `json:"scryfallId"`
	OracleID        uuid.UUID `json:"oracleId"`
	Name            string    `json:"name"`
	ManaCost        string    `json:"manaCost"`
	TypeLine        string    `json:"typeLine"`
	SetCode         string    `json:"setCode"`
	SetName         string    `json:"setName"`
	CollectorNumber string    `json:"collectorNumber"`
	ImageSmall      *string   `json:"imageSmall"`
	ImageNormal     *string   `json:"imageNormal"`
}

type ImportResolveLine struct {
	LineNumber  int32             `json:"lineNumber"`
	Raw         string            `json:"raw"`
	Quantity    int32             `json:"quantity"`
	Status      string            `json:"status" enum:"matched,ambiguous,unmatched"`
	Match       *ImportCardMatch  `json:"match,omitempty"`
	Suggestions []ImportCardMatch `json:"suggestions,omitempty"`
}

func importCardMatchFrom(r collections.CardRef) ImportCardMatch {
	return ImportCardMatch{
		ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
		ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
		SetName: r.SetName, CollectorNumber: r.CollectorNumber,
		ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
	}
}
```

and input/output types:

```go
type resolveImportInput struct {
	Body struct {
		Text string `json:"text" minLength:"1" maxLength:"65536"`
	}
}

type resolveImportOutput struct {
	Body struct {
		Lines []ImportResolveLine `json:"lines"`
	}
}

type importItemsInput struct {
	Body struct {
		Items []struct {
			ScryfallID uuid.UUID `json:"scryfallId"`
			Quantity   int32     `json:"quantity" minimum:"1" maximum:"999"`
		} `json:"items" minItems:"1" maxItems:"500"`
	}
}

type importItemsOutput struct {
	Body struct {
		AddedRows   int32 `json:"addedRows"`
		UpdatedRows int32 `json:"updatedRows"`
	}
}
```

then append to `registerCollections`:

```go
	huma.Register(api, huma.Operation{
		OperationID: "resolveCollectionImport",
		Method:      http.MethodPost,
		Path:        "/api/collection/import/resolve",
		Summary:     "Resolve a pasted card list (pure read, nothing is written)",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *resolveImportInput) (*resolveImportOutput, error) {
		if _, ok := CurrentUserID(ctx); !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		lines, err := deps.Collections.ResolveImport(ctx, in.Body.Text)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &resolveImportOutput{}
		out.Body.Lines = make([]ImportResolveLine, len(lines))
		for i, l := range lines {
			rl := ImportResolveLine{
				LineNumber: l.LineNumber, Raw: l.Raw, Quantity: l.Quantity, Status: l.Status,
			}
			if l.Match != nil {
				m := importCardMatchFrom(*l.Match)
				rl.Match = &m
			}
			for _, s := range l.Suggestions {
				rl.Suggestions = append(rl.Suggestions, importCardMatchFrom(s))
			}
			out.Body.Lines[i] = rl
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "importCollectionItems",
		Method:      http.MethodPost,
		Path:        "/api/collection/import",
		Summary:     "Add quantities onto the collection (bulk import commit)",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *importItemsInput) (*importItemsOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		items := make([]collections.ImportItem, len(in.Body.Items))
		for i, it := range in.Body.Items {
			items[i] = collections.ImportItem{ScryfallID: it.ScryfallID, Quantity: it.Quantity}
		}
		added, updated, err := deps.Collections.ApplyImport(ctx, uid, items)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &importItemsOutput{}
		out.Body.AddedRows = added
		out.Body.UpdatedRows = updated
		return out, nil
	})
```

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go test ./internal/platform/httpapi/ -run 'TestResolveImport|TestApplyImport' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/collections backend/internal/platform/httpapi
git commit -m "feat(collections): bulk import resolve + commit endpoints"
```

---

### Task 7: Wantlist endpoint

**Files:**
- Modify: `backend/internal/collections/service.go` (add `Wantlist`)
- Modify: `backend/internal/platform/httpapi/collections.go` (add operation + wire type)
- Modify: `backend/internal/platform/httpapi/collections_endpoints_test.go` (add tests)

**Interfaces:**
- Consumes: Task 2 `GetCubeWantlist`; existing `GetCube` query; existing cubes-test helpers `createCube(t, c, name, visibility) cubeDetailBody` and the commit-change endpoint for seeding cube cards.
- Produces:
  - `type WantlistItem struct { OracleID, ScryfallID uuid.UUID; Name, ManaCost string; ImageSmall, ImageNormal *string; MissingQuantity, CubeQuantity, OwnedQuantity int32 }`
  - `(*Service).Wantlist(ctx, cubeID, userID uuid.UUID) (cubeName string, items []WantlistItem, totalMissing int64, err error)`
  - Operation `getCubeWantlist` (GET `/api/cubes/{cubeId}/wantlist`); wire type `WantlistEntry`

- [ ] **Step 1: Write the failing tests.** Append to `collections_endpoints_test.go`:

```go
type wantlistEntryBody struct {
	OracleID        string `json:"oracleId"`
	Name            string `json:"name"`
	MissingQuantity int32  `json:"missingQuantity"`
	CubeQuantity    int32  `json:"cubeQuantity"`
	OwnedQuantity   int32  `json:"ownedQuantity"`
}

type wantlistBody struct {
	CubeName     string              `json:"cubeName"`
	Items        []wantlistEntryBody `json:"items"`
	TotalMissing int64               `json:"totalMissing"`
}

func TestCubeWantlist(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	owner := loggedInClient(t, srv, q, "want-owner@test.dev")
	viewer := loggedInClient(t, srv, q, "want-viewer@test.dev")

	boltO := uuid.New()
	boltAlpha, boltM10 := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltAlpha, oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	seedCard(t, pool, testCard{scryfallID: boltM10, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	ringS := uuid.New()
	seedCard(t, pool, testCard{scryfallID: ringS, oracleID: uuid.New(), name: "Sol Ring", typeLine: "Artifact", colorIdentity: []string{}})

	cube := createCube(t, owner, "Wantlist Cube", "public")
	resp := owner.do(t, "POST", "/api/cubes/"+cube.ID+"/changes", fmt.Sprintf(
		`{"expectedVersion":0,"adds":[{"scryfallId":%q,"quantity":4},{"scryfallId":%q,"quantity":1}]}`,
		boltAlpha, ringS))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed cube = %d", resp.StatusCode)
	}

	// Viewer owns 1 alpha + 2 m10 bolts: 3 of 4 → missing 1; Sol Ring fully missing.
	putQuantity(t, viewer, boltAlpha, 1)
	putQuantity(t, viewer, boltM10, 2)

	resp = viewer.do(t, "GET", "/api/cubes/"+cube.ID+"/wantlist", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wantlist = %d, want 200", resp.StatusCode)
	}
	body := decode[wantlistBody](t, resp)
	if body.CubeName != "Wantlist Cube" || body.TotalMissing != 2 || len(body.Items) != 2 {
		t.Fatalf("wantlist = %+v", body)
	}
	// Name-sorted: Bolt first.
	bolt, ring := body.Items[0], body.Items[1]
	if bolt.Name != "Lightning Bolt" || bolt.MissingQuantity != 1 ||
		bolt.CubeQuantity != 4 || bolt.OwnedQuantity != 3 {
		t.Fatalf("bolt entry = %+v (owned printings must aggregate)", bolt)
	}
	if ring.Name != "Sol Ring" || ring.MissingQuantity != 1 || ring.OwnedQuantity != 0 {
		t.Fatalf("ring entry = %+v", ring)
	}

	// Owning everything → empty list.
	putQuantity(t, viewer, boltM10, 3)
	putQuantity(t, viewer, ringS, 1)
	body = decode[wantlistBody](t, viewer.do(t, "GET", "/api/cubes/"+cube.ID+"/wantlist", ""))
	if len(body.Items) != 0 || body.TotalMissing != 0 {
		t.Fatalf("fully-owned wantlist = %+v, want empty", body)
	}
}

func TestCubeWantlistVisibilityAndAuth(t *testing.T) {
	srv, _, q := newCollectionsServer(t)
	owner := loggedInClient(t, srv, q, "want-priv@test.dev")
	stranger := loggedInClient(t, srv, q, "want-stranger@test.dev")

	priv := createCube(t, owner, "Secret Cube", "private")

	// Anonymous → 401 (session required even for public cubes).
	if code := getJSON(t, srv, "/api/cubes/"+priv.ID+"/wantlist", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", code)
	}
	// Private cube, non-owner → 404, no existence leak.
	if resp := stranger.do(t, "GET", "/api/cubes/"+priv.ID+"/wantlist", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stranger = %d, want 404", resp.StatusCode)
	}
	// Owner sees it (empty cube → empty wantlist).
	resp := owner.do(t, "GET", "/api/cubes/"+priv.ID+"/wantlist", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner = %d, want 200", resp.StatusCode)
	}
	if body := decode[wantlistBody](t, resp); body.TotalMissing != 0 {
		t.Fatalf("empty cube wantlist = %+v", body)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestCubeWantlist -v`
Expected: FAIL — 404s.

- [ ] **Step 3: Implement the service method.** Append to `backend/internal/collections/service.go`:

```go
type WantlistItem struct {
	OracleID        uuid.UUID
	ScryfallID      uuid.UUID // the cube's chosen printing, for imagery
	Name            string
	ManaCost        string
	ImageSmall      *string
	ImageNormal     *string
	MissingQuantity int32
	CubeQuantity    int32
	OwnedQuantity   int32
}

// Wantlist computes cube-minus-collection at oracle level, on demand,
// never stored. Cube visibility follows the cubes rule: private cubes
// 404 for non-owners (no existence leak). No dependency on the cubes
// package — the same GetCube query enforces it here.
func (s *Service) Wantlist(ctx context.Context, cubeID, userID uuid.UUID) (string, []WantlistItem, int64, error) {
	cube, err := s.queries.GetCube(ctx, cubeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, 0, ErrCubeNotFound
	}
	if err != nil {
		return "", nil, 0, err
	}
	if cube.Visibility == "private" && cube.OwnerID != userID {
		return "", nil, 0, ErrCubeNotFound
	}
	rows, err := s.queries.GetCubeWantlist(ctx, db.GetCubeWantlistParams{
		CubeID: cubeID, UserID: userID,
	})
	if err != nil {
		return "", nil, 0, err
	}
	items := make([]WantlistItem, len(rows))
	var totalMissing int64
	for i, r := range rows {
		items[i] = WantlistItem{
			OracleID: r.OracleID, ScryfallID: r.ScryfallID, Name: r.Name,
			ManaCost: r.ManaCost, ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			MissingQuantity: r.MissingQuantity, CubeQuantity: r.CubeQuantity,
			OwnedQuantity: r.OwnedQuantity,
		}
		totalMissing += int64(r.MissingQuantity)
	}
	return cube.Name, items, totalMissing, nil
}
```

- [ ] **Step 4: Register the operation.** In `httpapi/collections.go`, add:

```go
type WantlistEntry struct {
	OracleID        uuid.UUID `json:"oracleId"`
	ScryfallID      uuid.UUID `json:"scryfallId" doc:"The cube's chosen printing"`
	Name            string    `json:"name"`
	ManaCost        string    `json:"manaCost"`
	ImageSmall      *string   `json:"imageSmall"`
	ImageNormal     *string   `json:"imageNormal"`
	MissingQuantity int32     `json:"missingQuantity"`
	CubeQuantity    int32     `json:"cubeQuantity"`
	OwnedQuantity   int32     `json:"ownedQuantity"`
}

type getWantlistInput struct {
	CubeID string `path:"cubeId"`
}

type getWantlistOutput struct {
	Body struct {
		CubeName     string          `json:"cubeName"`
		Items        []WantlistEntry `json:"items"`
		TotalMissing int64           `json:"totalMissing"`
	}
}
```

then append to `registerCollections` (note `parseCubeID` already exists in `cubes.go`, same package):

```go
	huma.Register(api, huma.Operation{
		OperationID: "getCubeWantlist",
		Method:      http.MethodGet,
		Path:        "/api/cubes/{cubeId}/wantlist",
		Summary:     "Cards in the cube missing from the caller's collection",
		Tags:        []string{"collection"},
	}, func(ctx context.Context, in *getWantlistInput) (*getWantlistOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseCubeID(in.CubeID)
		if err != nil {
			return nil, err
		}
		name, items, totalMissing, err := deps.Collections.Wantlist(ctx, id, uid)
		if err != nil {
			return nil, mapCollectionErr(err)
		}
		out := &getWantlistOutput{}
		out.Body.CubeName = name
		out.Body.TotalMissing = totalMissing
		out.Body.Items = make([]WantlistEntry, len(items))
		for i, it := range items {
			out.Body.Items[i] = WantlistEntry{
				OracleID: it.OracleID, ScryfallID: it.ScryfallID, Name: it.Name,
				ManaCost: it.ManaCost, ImageSmall: it.ImageSmall, ImageNormal: it.ImageNormal,
				MissingQuantity: it.MissingQuantity, CubeQuantity: it.CubeQuantity,
				OwnedQuantity: it.OwnedQuantity,
			}
		}
		return out, nil
	})
```

- [ ] **Step 5: Run the whole backend suite**

Run: `cd backend && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/collections backend/internal/platform/httpapi
git commit -m "feat(collections): cube wantlist endpoint"
```

---

### Task 8: Regenerate the OpenAPI client

**Files:**
- Generated: `frontend/src/shared/api/openapi.yaml`, `frontend/src/shared/api/schema.d.ts` (committed; CI fails if stale)

- [ ] **Step 1: Regenerate**

Run: `make api-generate`
Expected: `frontend/src/shared/api/schema.d.ts` gains `CollectionItemEntry`, `ImportCardMatch`, `ImportResolveLine`, `WantlistEntry` schemas and the six new paths.

- [ ] **Step 2: Typecheck the frontend against the new client**

Run: `pnpm --filter @cube-planner/frontend typecheck`
Expected: clean (nothing consumes the new schemas yet).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/shared/api
git commit -m "feat(collections): regenerate API client for collection + wantlist endpoints"
```

---

### Task 9: Promote CardHoverPreview to shared/cards

`features/collection` needs the hover preview but must not import from `features/cubes` (structure.md rule 1: never feature → feature; promote, don't copy). Generalize the prop from `CubeCardEntry` to a structural type so both features pass their own entry types unchanged.

**Files:**
- Create: `frontend/src/shared/cards/CardHoverPreview.tsx` (moved + generalized)
- Delete: `frontend/src/features/cubes/components/CardHoverPreview.tsx`
- Modify: every current importer in `features/cubes` (find them with `grep -rn "CardHoverPreview" frontend/src --include='*.tsx' -l` — expected: `GroupedCardList.tsx`, possibly others)

**Interfaces:**
- Produces: `import { CardHoverPreview } from "@/shared/cards/CardHoverPreview"` with props `{ card: { imageNormal?: string | null }; children: ReactNode }` — any object with an `imageNormal` field satisfies it (`CubeCardEntry`, `CollectionItemEntry`, `WantlistEntry`).

- [ ] **Step 1: Move and generalize**

```bash
git mv frontend/src/features/cubes/components/CardHoverPreview.tsx frontend/src/shared/cards/CardHoverPreview.tsx
```

Then edit `frontend/src/shared/cards/CardHoverPreview.tsx` to drop the cubes import and take a structural prop:

```tsx
import { useState } from "react";
import type { ReactNode } from "react";

// Shows the card image in a floating panel while the row is hovered or
// focused (mouse hover or keyboard focus). Touch devices have no hover
// or focus trigger here, so the preview is not reachable by tap.
// Structural prop: any entry with an imageNormal field works.
export function CardHoverPreview({
  card,
  children,
}: {
  card: { imageNormal?: string | null };
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  if (card.imageNormal == null) return <>{children}</>;
  return (
    <span
      className="relative inline-block w-full"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => setOpen(false)}
      onFocus={() => setOpen(true)}
      onBlur={() => setOpen(false)}
    >
      {children}
      {open && (
        <span className="pointer-events-none absolute top-full left-1/2 z-10 mt-1 block w-60 -translate-x-1/2">
          <img src={card.imageNormal} alt="" className="rounded-xl shadow-lg" />
        </span>
      )}
    </span>
  );
}
```

- [ ] **Step 2: Update importers.** For every file `grep` found (expected: `frontend/src/features/cubes/components/GroupedCardList.tsx`; check for others), change

```ts
import { CardHoverPreview } from "./CardHoverPreview";
```

to

```ts
import { CardHoverPreview } from "@/shared/cards/CardHoverPreview";
```

- [ ] **Step 3: Verify**

Run: `pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend test`
Expected: clean; existing cubes tests still pass.

- [ ] **Step 4: Commit**

```bash
git add -A frontend/src
git commit -m "refactor(cards): promote CardHoverPreview to shared/cards"
```

---

### Task 10: Dialog primitive (shared/ui)

The repo has no modal yet (`CubeSettingsSection` uses `window.confirm`). Import review and the printing picker need a real one. Native `<dialog>` + `showModal()` gives focus trap, Esc-to-close, and `::backdrop` for free.

**Files:**
- Create: `frontend/src/shared/ui/dialog.tsx`
- Test: `frontend/src/shared/ui/dialog.test.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json` (one key)

**Interfaces:**
- Produces: `<Dialog open={boolean} onClose={() => void} title={string}>{children}</Dialog>` — modal, labelled by its title, closes via Esc (native `close` event) or the ✕ button.

- [ ] **Step 1: Add the message key.** In `frontend/messages/en.json` add `"dialog_close": "Close"`; in `frontend/messages/pl.json` add `"dialog_close": "Zamknij"`. Run `pnpm --filter @cube-planner/frontend gen`.

- [ ] **Step 2: Write the failing test** `frontend/src/shared/ui/dialog.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { Dialog } from "./dialog";

test("renders title and children when open, nothing when closed", () => {
  const { rerender } = render(
    <Dialog open={false} onClose={() => {}} title="Pick a card">
      <p>Body text</p>
    </Dialog>,
  );
  expect(screen.queryByText("Body text")).not.toBeInTheDocument();
  rerender(
    <Dialog open onClose={() => {}} title="Pick a card">
      <p>Body text</p>
    </Dialog>,
  );
  expect(screen.getByRole("heading", { name: "Pick a card" })).toBeInTheDocument();
  expect(screen.getByText("Body text")).toBeInTheDocument();
});

test("close button fires onClose", async () => {
  const onClose = vi.fn();
  render(
    <Dialog open onClose={onClose} title="Pick a card">
      <p>Body</p>
    </Dialog>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalled();
});
```

- [ ] **Step 3: Run to verify failure**

Run: `pnpm --filter @cube-planner/frontend test dialog`
Expected: FAIL — module not found.

- [ ] **Step 4: Implement** `frontend/src/shared/ui/dialog.tsx`:

```tsx
import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

// Modal on top of the native <dialog> element: showModal() provides the
// focus trap, Esc-to-close (fires the close event), and ::backdrop.
export function Dialog({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) {
      // Test environments may lack showModal — fall back to the open attr.
      if (typeof el.showModal === "function") el.showModal();
      else el.setAttribute("open", "");
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      onClose={onClose}
      className="m-auto w-full max-w-lg rounded-xl border border-border bg-surface p-6 text-fg shadow-lg backdrop:bg-black/50"
    >
      {open && (
        <div className="flex flex-col gap-4">
          <div className="flex items-start justify-between gap-4">
            <h2 id={titleId} className="text-lg font-semibold text-fg">
              {title}
            </h2>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={m.dialog_close()}
              onClick={onClose}
            >
              ✕
            </Button>
          </div>
          {children}
        </div>
      )}
    </dialog>
  );
}
```

- [ ] **Step 5: Run to verify pass**

Run: `pnpm --filter @cube-planner/frontend test dialog`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/shared/ui/dialog.tsx frontend/src/shared/ui/dialog.test.tsx frontend/messages
git commit -m "feat(ui): Dialog primitive on native <dialog>"
```

---

### Task 11: features/collection API hooks

**Files:**
- Create: `frontend/src/features/collection/api.ts`
- Test: `frontend/src/features/collection/api.test.tsx`

**Interfaces:**
- Consumes: Task 8 generated schemas.
- Produces (used by Tasks 12–15):
  - Types `CollectionItemEntry`, `ImportCardMatch`, `ImportResolveLine`, `WantlistEntry` (re-exported from schemas); `COLLECTION_PAGE_SIZE = 50`
  - `class UnauthorizedError extends Error {}`
  - `useCollection(search: string, page: number)` → `{ items, total, totalCopies }`; throws `UnauthorizedError` on 401
  - `useSetQuantity()` — mutation `{ scryfallId: string; quantity: number }`
  - `useChangePrinting()` — mutation `{ scryfallId: string; newScryfallId: string }`
  - `useResolveImport()` — mutation `{ text: string }` → `ImportResolveLine[]`
  - `useImportItems()` — mutation `{ items: { scryfallId: string; quantity: number }[] }` → `{ addedRows, updatedRows }`
  - `useWantlist(cubeId: string)` → `{ cubeName, items, totalMissing }`; throws `UnauthorizedError` on 401
  - All mutations invalidate the `["collection"]` query key on success.

- [ ] **Step 1: Write the failing tests** `frontend/src/features/collection/api.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { UnauthorizedError, useCollection, useImportItems, useSetQuantity, useWantlist } from "./api";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("useCollection returns items and totals, coalescing null arrays", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ items: null, total: 0, totalCopies: 0 })),
  );
  const { result } = renderHook(() => useCollection("", 0), { wrapper });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toEqual({ items: [], total: 0, totalCopies: 0 });
});

test("useCollection throws UnauthorizedError on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ title: "Unauthorized", status: 401 }, 401)),
  );
  const { result } = renderHook(() => useCollection("", 0), { wrapper });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error).toBeInstanceOf(UnauthorizedError);
});

test("useSetQuantity PUTs the quantity", async () => {
  const fetchMock = vi
    .fn()
    .mockResolvedValue(jsonResponse({ item: { scryfallId: "s1", quantity: 3 } }));
  vi.stubGlobal("fetch", fetchMock);
  const { result } = renderHook(() => useSetQuantity(), { wrapper });
  result.current.mutate({ scryfallId: "s1", quantity: 3 });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  // openapi-fetch may call fetch(Request) or fetch(url, init) — handle both.
  const [input, init] = fetchMock.mock.calls[0] as [Request | string, RequestInit | undefined];
  const url = typeof input === "string" ? input : input.url;
  const method = init?.method ?? (typeof input === "string" ? undefined : input.method);
  const rawBody =
    init?.body ?? (typeof input === "string" ? undefined : await input.clone().text());
  expect(url).toContain("/api/collection/cards/s1");
  expect(method).toBe("PUT");
  expect(JSON.parse(rawBody as string)).toEqual({ quantity: 3 });
});

test("useImportItems surfaces the summary", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ addedRows: 2, updatedRows: 1 })),
  );
  const { result } = renderHook(() => useImportItems(), { wrapper });
  result.current.mutate({ items: [{ scryfallId: "s1", quantity: 2 }] });
  await waitFor(() => expect(result.current.isSuccess).toBe(true));
  expect(result.current.data).toEqual({ addedRows: 2, updatedRows: 1 });
});

test("useWantlist throws UnauthorizedError on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ title: "Unauthorized", status: 401 }, 401)),
  );
  const { result } = renderHook(() => useWantlist("cube-1"), { wrapper });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.error).toBeInstanceOf(UnauthorizedError);
});
```

- [ ] **Step 2: Run to verify failure**

Run: `pnpm --filter @cube-planner/frontend test features/collection`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** `frontend/src/features/collection/api.ts`:

```ts
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type CollectionItemEntry = components["schemas"]["CollectionItemEntry"];
export type ImportCardMatch = components["schemas"]["ImportCardMatch"];
export type ImportResolveLine = components["schemas"]["ImportResolveLine"];
export type WantlistEntry = components["schemas"]["WantlistEntry"];

export const COLLECTION_PAGE_SIZE = 50;

/** Thrown on 401 so pages can render a login prompt instead of a generic error. */
export class UnauthorizedError extends Error {}

export function useCollection(search: string, page: number) {
  const query = search.trim();
  return useQuery({
    queryKey: ["collection", "list", query, page],
    placeholderData: keepPreviousData,
    retry: false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/collection", {
        params: {
          query: {
            search: query,
            limit: COLLECTION_PAGE_SIZE,
            offset: page * COLLECTION_PAGE_SIZE,
          },
        },
      });
      if (response.status === 401) throw new UnauthorizedError(m.collection_login_required());
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { items: data.items ?? [], total: data.total, totalCopies: data.totalCopies };
    },
  });
}

export function useSetQuantity() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { scryfallId: string; quantity: number }) => {
      const { data, error } = await client.PUT("/api/collection/cards/{scryfallId}", {
        params: { path: { scryfallId: vars.scryfallId } },
        body: { quantity: vars.quantity },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      return data?.item ?? null;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collection"] }),
  });
}

export function useChangePrinting() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { scryfallId: string; newScryfallId: string }) => {
      const { data, error } = await client.POST(
        "/api/collection/cards/{scryfallId}/change-printing",
        {
          params: { path: { scryfallId: vars.scryfallId } },
          body: { newScryfallId: vars.newScryfallId },
        },
      );
      if (error) throw new Error(error.detail ?? m.error_generic());
      return data?.item ?? null;
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collection"] }),
  });
}

export function useResolveImport() {
  return useMutation({
    mutationFn: async (vars: { text: string }): Promise<ImportResolveLine[]> => {
      const { data, error } = await client.POST("/api/collection/import/resolve", {
        body: { text: vars.text },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return data.lines ?? [];
    },
  });
}

export function useImportItems() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { items: { scryfallId: string; quantity: number }[] }) => {
      const { data, error } = await client.POST("/api/collection/import", {
        body: { items: vars.items },
      });
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { addedRows: data.addedRows, updatedRows: data.updatedRows };
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collection"] }),
  });
}

export function useWantlist(cubeId: string) {
  return useQuery({
    queryKey: ["collection", "wantlist", cubeId],
    retry: false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/cubes/{cubeId}/wantlist", {
        params: { path: { cubeId } },
      });
      if (response.status === 401) throw new UnauthorizedError(m.wantlist_login_required());
      if (error) throw new Error(error.detail ?? m.error_generic());
      if (!data) throw new Error(m.error_generic());
      return { cubeName: data.cubeName, items: data.items ?? [], totalMissing: data.totalMissing };
    },
  });
}
```

- [ ] **Step 4: Add the two message keys the hooks use.** `frontend/messages/en.json`:

```json
"collection_login_required": "Log in to manage your collection.",
"wantlist_login_required": "Log in to compare this cube with your collection."
```

`frontend/messages/pl.json`:

```json
"collection_login_required": "Zaloguj się, aby zarządzać kolekcją.",
"wantlist_login_required": "Zaloguj się, aby porównać kostkę ze swoją kolekcją."
```

Run `pnpm --filter @cube-planner/frontend gen`.

- [ ] **Step 5: Run to verify pass**

Run: `pnpm --filter @cube-planner/frontend test features/collection && pnpm --filter @cube-planner/frontend typecheck`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/features/collection frontend/messages
git commit -m "feat(collections): collection API hooks"
```

---

### Task 12: Collection page (list, search, steppers, add, remove)

**Files:**
- Create: `frontend/src/features/collection/components/QuantityStepper.tsx`
- Create: `frontend/src/features/collection/components/CollectionPage.tsx`
- Create: `frontend/src/routes/collection.tsx`
- Test: `frontend/src/features/collection/components/QuantityStepper.test.tsx`
- Test: `frontend/src/features/collection/components/CollectionPage.test.tsx`
- Modify: `frontend/src/routes/__root.tsx` (nav link)
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`

**Interfaces:**
- Consumes: Task 11 hooks, `CardAutocomplete` from `@/shared/cards/CardAutocomplete` (props `{ id: string; onSelect: (card: CardSummary) => void }`), `CardHoverPreview` from Task 9, `useDebouncedValue` from `@/shared/lib/useDebouncedValue`, existing `m.cards_set_line({ setName, collectorNumber })` message.
- Produces: `CollectionPage` (route `/collection`); `QuantityStepper` with props `{ name: string; quantity: number; onCommit: (quantity: number) => void }` — debounces rapid clicks into ONE `onCommit` with the final value (400 ms). Task 13 adds the change-printing column; Task 14 adds the Import button — both slot into this page.

- [ ] **Step 1: Add the message keys.** `frontend/messages/en.json`:

```json
"nav_collection": "Collection",
"collection_title": "My collection",
"collection_stats": "{cards} cards · {copies} copies",
"collection_search_label": "Search your collection",
"collection_search_placeholder": "Filter by card name…",
"collection_add_label": "Add a card",
"collection_empty": "Your collection is empty. Add cards above or import a list.",
"collection_no_results": "No cards match your search.",
"collection_qty_increase": "Increase quantity of {name}",
"collection_qty_decrease": "Decrease quantity of {name}",
"collection_remove_card": "Remove {name}",
"pagination_prev": "Previous",
"pagination_next": "Next",
"pagination_page": "Page {page} of {pages}"
```

`frontend/messages/pl.json`:

```json
"nav_collection": "Kolekcja",
"collection_title": "Moja kolekcja",
"collection_stats": "{cards} kart · {copies} sztuk",
"collection_search_label": "Szukaj w kolekcji",
"collection_search_placeholder": "Filtruj po nazwie karty…",
"collection_add_label": "Dodaj kartę",
"collection_empty": "Twoja kolekcja jest pusta. Dodaj karty powyżej albo zaimportuj listę.",
"collection_no_results": "Żadna karta nie pasuje do wyszukiwania.",
"collection_qty_increase": "Zwiększ liczbę {name}",
"collection_qty_decrease": "Zmniejsz liczbę {name}",
"collection_remove_card": "Usuń {name}",
"pagination_prev": "Poprzednia",
"pagination_next": "Następna",
"pagination_page": "Strona {page} z {pages}"
```

NOTE: if any `pagination_*` key already exists (the cube browser may define its own — check with `grep '"pagination_' frontend/messages/en.json`), reuse the existing keys and skip the duplicates. Run `pnpm --filter @cube-planner/frontend gen`.

- [ ] **Step 2: Write the failing stepper test** `frontend/src/features/collection/components/QuantityStepper.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { QuantityStepper } from "./QuantityStepper";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

test("rapid clicks land as ONE commit with the final value", async () => {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  const onCommit = vi.fn();
  render(<QuantityStepper name="Lightning Bolt" quantity={4} onCommit={onCommit} />);

  const inc = screen.getByRole("button", { name: "Increase quantity of Lightning Bolt" });
  await user.click(inc);
  await user.click(inc);
  await user.click(inc);
  expect(screen.getByText("7")).toBeInTheDocument(); // optimistic display
  expect(onCommit).not.toHaveBeenCalled();

  vi.advanceTimersByTime(400);
  expect(onCommit).toHaveBeenCalledTimes(1);
  expect(onCommit).toHaveBeenCalledWith(7);
});

test("decrementing to zero commits zero (remove path)", async () => {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  const onCommit = vi.fn();
  render(<QuantityStepper name="Sol Ring" quantity={1} onCommit={onCommit} />);

  await user.click(screen.getByRole("button", { name: "Decrease quantity of Sol Ring" }));
  vi.advanceTimersByTime(400);
  expect(onCommit).toHaveBeenCalledWith(0);
});

test("no commit when the value returns to the server quantity", async () => {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  const onCommit = vi.fn();
  render(<QuantityStepper name="Sol Ring" quantity={2} onCommit={onCommit} />);

  await user.click(screen.getByRole("button", { name: "Increase quantity of Sol Ring" }));
  await user.click(screen.getByRole("button", { name: "Decrease quantity of Sol Ring" }));
  vi.advanceTimersByTime(400);
  expect(onCommit).not.toHaveBeenCalled();
});
```

- [ ] **Step 3: Run to verify failure**

Run: `pnpm --filter @cube-planner/frontend test QuantityStepper`
Expected: FAIL — module not found.

- [ ] **Step 4: Implement** `frontend/src/features/collection/components/QuantityStepper.tsx`:

```tsx
import { useEffect, useRef, useState } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

const COMMIT_DELAY_MS = 400;

// Local-first stepper: clicks update the displayed value immediately and
// debounce into ONE onCommit with the final value. PUT is a set, so the
// final value winning is exactly right — no lost-update risk against
// yourself.
export function QuantityStepper({
  name,
  quantity,
  onCommit,
}: {
  name: string;
  quantity: number;
  onCommit: (quantity: number) => void;
}) {
  const [value, setValue] = useState(quantity);
  const committed = useRef(quantity);

  // Adopt server refetches (e.g. after an import bumped this row).
  useEffect(() => {
    setValue(quantity);
    committed.current = quantity;
  }, [quantity]);

  useEffect(() => {
    if (value === committed.current) return;
    const timer = setTimeout(() => {
      committed.current = value;
      onCommit(value);
    }, COMMIT_DELAY_MS);
    return () => clearTimeout(timer);
  }, [value, onCommit]);

  return (
    <span className="flex items-center gap-1">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        aria-label={m.collection_qty_decrease({ name })}
        disabled={value <= 0}
        onClick={() => setValue((v) => Math.max(0, v - 1))}
      >
        −
      </Button>
      <span className="w-8 text-center text-sm tabular-nums text-fg">{value}</span>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        aria-label={m.collection_qty_increase({ name })}
        disabled={value >= 999}
        onClick={() => setValue((v) => Math.min(999, v + 1))}
      >
        +
      </Button>
    </span>
  );
}
```

- [ ] **Step 5: Run stepper tests to verify pass**

Run: `pnpm --filter @cube-planner/frontend test QuantityStepper`
Expected: PASS.

- [ ] **Step 6: Implement the page** `frontend/src/features/collection/components/CollectionPage.tsx`:

```tsx
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { CardAutocomplete } from "@/shared/cards/CardAutocomplete";
import { CardHoverPreview } from "@/shared/cards/CardHoverPreview";
import { useDebouncedValue } from "@/shared/lib/useDebouncedValue";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import {
  COLLECTION_PAGE_SIZE,
  UnauthorizedError,
  useCollection,
  useImportItems,
  useSetQuantity,
} from "../api";
import { QuantityStepper } from "./QuantityStepper";

export function CollectionPage() {
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const debouncedSearch = useDebouncedValue(search, 300);
  const collection = useCollection(debouncedSearch, page);
  const setQuantity = useSetQuantity();
  const importItems = useImportItems();

  if (collection.isError && collection.error instanceof UnauthorizedError) {
    return (
      <Alert variant="default">
        {collection.error.message}{" "}
        <Link to="/login" className="font-medium underline">
          {m.nav_login()}
        </Link>
      </Alert>
    );
  }

  const pages = Math.max(1, Math.ceil((collection.data?.total ?? 0) / COLLECTION_PAGE_SIZE));

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-fg">{m.collection_title()}</h1>
          {collection.data && (
            <p className="text-sm text-fg-muted">
              {m.collection_stats({
                cards: collection.data.total,
                copies: collection.data.totalCopies,
              })}
            </p>
          )}
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="collection-add">{m.collection_add_label()}</Label>
          <CardAutocomplete
            id="collection-add"
            onSelect={(card) =>
              importItems.mutate({ items: [{ scryfallId: card.scryfallId, quantity: 1 }] })
            }
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="collection-search">{m.collection_search_label()}</Label>
          <Input
            id="collection-search"
            type="search"
            placeholder={m.collection_search_placeholder()}
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
              setPage(0);
            }}
          />
        </div>
      </div>

      {collection.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {collection.isError && !(collection.error instanceof UnauthorizedError) && (
        <Alert variant="danger">{collection.error.message}</Alert>
      )}
      {collection.data && collection.data.items.length === 0 && (
        <p className="text-sm text-fg-muted">
          {debouncedSearch === "" ? m.collection_empty() : m.collection_no_results()}
        </p>
      )}
      {collection.data && collection.data.items.length > 0 && (
        <ul className="divide-y divide-border">
          {collection.data.items.map((item) => (
            <li key={item.scryfallId} className="flex items-center justify-between gap-3 py-1.5">
              <CardHoverPreview card={item}>
                <span className="flex flex-col">
                  <span className="truncate text-sm text-fg">{item.name}</span>
                  <span className="text-xs text-fg-muted">
                    {m.cards_set_line({
                      setName: item.setName,
                      collectorNumber: item.collectorNumber,
                    })}
                  </span>
                </span>
              </CardHoverPreview>
              <span className="flex shrink-0 items-center gap-1">
                <QuantityStepper
                  name={item.name}
                  quantity={item.quantity}
                  onCommit={(quantity) =>
                    setQuantity.mutate({ scryfallId: item.scryfallId, quantity })
                  }
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  aria-label={m.collection_remove_card({ name: item.name })}
                  onClick={() => setQuantity.mutate({ scryfallId: item.scryfallId, quantity: 0 })}
                >
                  ✕
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}

      {collection.data && pages > 1 && (
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage((p) => p - 1)}
          >
            {m.pagination_prev()}
          </Button>
          <span className="text-sm text-fg-muted">
            {m.pagination_page({ page: page + 1, pages })}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={page + 1 >= pages}
            onClick={() => setPage((p) => p + 1)}
          >
            {m.pagination_next()}
          </Button>
        </div>
      )}
    </div>
  );
}
```

NOTE: adding a card uses the import-add endpoint (not PUT) so re-selecting an already-owned card increments instead of resetting to 1. If the existing cube-browser pagination uses different message keys or markup, mirror that page's pattern instead — consistency wins over this snippet.

- [ ] **Step 7: Create the route** `frontend/src/routes/collection.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { CollectionPage } from "@/features/collection/components/CollectionPage";

export const Route = createFileRoute("/collection")({ component: CollectionPage });
```

- [ ] **Step 8: Add the nav link.** In `frontend/src/routes/__root.tsx`, inside the logged-in block (next to the existing `<Link to="/cubes/mine">` around line 50), add:

```tsx
<Link to="/collection">{m.nav_collection()}</Link>
```

Match the exact className of its sibling links.

- [ ] **Step 9: Write the page test** `frontend/src/features/collection/components/CollectionPage.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { CollectionPage } from "./CollectionPage";

afterEach(() => vi.unstubAllGlobals());

function renderPage() {
  const rootRoute = createRootRoute();
  const index = createRoute({ getParentRoute: () => rootRoute, path: "/", component: CollectionPage });
  const login = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: () => null });
  const router = createRouter({
    routeTree: rootRoute.addChildren([index, login]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
}

const item = {
  scryfallId: "s1",
  oracleId: "o1",
  name: "Lightning Bolt",
  manaCost: "{R}",
  typeLine: "Instant",
  setCode: "m10",
  setName: "Magic 2010",
  collectorNumber: "146",
  imageSmall: null,
  imageNormal: null,
  quantity: 4,
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("renders items with stats and printing line", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ items: [item], total: 1, totalCopies: 4 })),
  );
  renderPage();
  expect(await screen.findByText("Lightning Bolt")).toBeInTheDocument();
  expect(screen.getByText(/Magic 2010/)).toBeInTheDocument();
  expect(screen.getByText("4")).toBeInTheDocument();
});

test("remove button PUTs quantity 0", async () => {
  // openapi-fetch may call fetch(Request) or fetch(url, init) — handle both.
  const callMethod = (input: Request | string, init?: RequestInit) =>
    init?.method ?? (typeof input === "string" ? "GET" : input.method);
  const fetchMock = vi.fn(async (input: Request | string, init?: RequestInit) => {
    if (callMethod(input, init) === "PUT") return jsonResponse({ item: null });
    return jsonResponse({ items: [item], total: 1, totalCopies: 4 });
  });
  vi.stubGlobal("fetch", fetchMock);
  renderPage();
  await userEvent.click(
    await screen.findByRole("button", { name: "Remove Lightning Bolt" }),
  );
  await waitFor(async () => {
    const put = fetchMock.mock.calls.find(([input, init]) =>
      callMethod(input as Request | string, init as RequestInit | undefined) === "PUT",
    );
    expect(put).toBeDefined();
    const [input, init] = put as [Request | string, RequestInit | undefined];
    const rawBody =
      init?.body ?? (typeof input === "string" ? undefined : await input.clone().text());
    expect(JSON.parse(rawBody as string)).toEqual({ quantity: 0 });
  });
});

test("shows a login prompt on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ title: "Unauthorized", status: 401 }, 401)),
  );
  renderPage();
  expect(await screen.findByText(/log in/i)).toBeInTheDocument();
});
```

- [ ] **Step 10: Run all frontend checks**

Run: `pnpm --filter @cube-planner/frontend gen && pnpm --filter @cube-planner/frontend test features/collection && pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend lint`
Expected: PASS (the `gen` refreshes routeTree for the new `/collection` route).

- [ ] **Step 11: Commit**

```bash
git add frontend/src frontend/messages
git commit -m "feat(collections): collection page with search, steppers, autocomplete add"
```

---

### Task 13: Printing picker

Promote `useCardPrintings` from `features/cards` to `shared/cards` (second consumer now exists — same promote-don't-copy move as `CardAutocomplete` in sub-project 3), build `PrintingPickerDialog` in `shared/cards/` (the cube editor is a known future consumer, sub-project 3 backlog), wire it into the collection page rows.

**Files:**
- Modify: `frontend/src/shared/cards/api.ts` (add `useCardPrintings` + `CardDetail`)
- Modify: `frontend/src/features/cards/api.ts` (remove them; re-export nothing — update importers)
- Modify: importers of `useCardPrintings`/`CardDetail` in `features/cards` (find with `grep -rn "useCardPrintings\|CardDetail" frontend/src/features/cards --include='*.ts*' -l`) to import from `@/shared/cards/api`; move the printings hook tests from `features/cards/api.test.tsx` into `shared/cards/api.test.tsx`
- Create: `frontend/src/shared/cards/PrintingPickerDialog.tsx`
- Test: `frontend/src/shared/cards/PrintingPickerDialog.test.tsx`
- Modify: `frontend/src/features/collection/components/CollectionPage.tsx` (change-printing action per row)
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`

**Interfaces:**
- Consumes: Task 10 `Dialog`, Task 11 `useChangePrinting`, existing `GET /api/cards/{oracleId}/printings`.
- Produces: `PrintingPickerDialog` with props `{ open: boolean; onClose: () => void; oracleId: string; name: string; currentScryfallId: string; onPick: (scryfallId: string) => void }` — lists printings newest-first, marks the current one, `onPick` fires with the chosen `scryfallId` (caller closes).

- [ ] **Step 1: Promote the hook.** Move `useCardPrintings` and `export type CardDetail` from `frontend/src/features/cards/api.ts` into `frontend/src/shared/cards/api.ts` (verbatim). Update every importer inside `features/cards` to `import { useCardPrintings, type CardDetail } from "@/shared/cards/api"`. Move the corresponding hook tests into `frontend/src/shared/cards/api.test.tsx`. If `features/cards/api.ts` becomes empty, delete it.

Run: `pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend test`
Expected: clean — pure move.

- [ ] **Step 2: Add message keys.** en: `"printing_picker_title": "Choose a printing — {name}"`, `"printing_picker_current": "Current"`. pl: `"printing_picker_title": "Wybierz wydanie — {name}"`, `"printing_picker_current": "Obecne"`. And the row action — en: `"collection_change_printing": "Change printing of {name}"`, pl: `"collection_change_printing": "Zmień wydanie {name}"`. Run `pnpm --filter @cube-planner/frontend gen`.

- [ ] **Step 3: Write the failing dialog test** `frontend/src/shared/cards/PrintingPickerDialog.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { PrintingPickerDialog } from "./PrintingPickerDialog";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => vi.unstubAllGlobals());

const printings = [
  { scryfallId: "new", setName: "Magic 2010", collectorNumber: "146", imageSmall: null },
  { scryfallId: "old", setName: "Limited Edition Alpha", collectorNumber: "161", imageSmall: null },
];

test("lists printings, marks the current one, picks another", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ printings }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  const onPick = vi.fn();
  render(
    <PrintingPickerDialog
      open
      onClose={() => {}}
      oracleId="o1"
      name="Lightning Bolt"
      currentScryfallId="new"
      onPick={onPick}
    />,
    { wrapper },
  );
  expect(await screen.findByText(/Magic 2010/)).toBeInTheDocument();
  // Current printing is marked and not pickable.
  expect(screen.getByText("Current")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /Limited Edition Alpha/ }));
  expect(onPick).toHaveBeenCalledWith("old");
});
```

- [ ] **Step 4: Run to verify failure**

Run: `pnpm --filter @cube-planner/frontend test PrintingPickerDialog`
Expected: FAIL — module not found.

- [ ] **Step 5: Implement** `frontend/src/shared/cards/PrintingPickerDialog.tsx`:

```tsx
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Dialog } from "@/shared/ui/dialog";
import { useCardPrintings } from "./api";

export function PrintingPickerDialog({
  open,
  onClose,
  oracleId,
  name,
  currentScryfallId,
  onPick,
}: {
  open: boolean;
  onClose: () => void;
  oracleId: string;
  name: string;
  currentScryfallId: string;
  onPick: (scryfallId: string) => void;
}) {
  // Only fetch while open — the picker mounts once per row.
  const printings = useCardPrintings(open ? oracleId : undefined);
  return (
    <Dialog open={open} onClose={onClose} title={m.printing_picker_title({ name })}>
      {printings.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {printings.isError && <Alert variant="danger">{printings.error.message}</Alert>}
      {printings.data && (
        <ul className="flex max-h-96 flex-col gap-1 overflow-y-auto">
          {printings.data.map((p) => {
            const current = p.scryfallId === currentScryfallId;
            const setLine = m.cards_set_line({
              setName: p.setName,
              collectorNumber: p.collectorNumber,
            });
            return (
              <li key={p.scryfallId}>
                {current ? (
                  <span className="flex w-full items-center gap-3 rounded-md bg-accent/10 px-2 py-1.5 text-sm text-fg">
                    {p.imageSmall != null && (
                      <img src={p.imageSmall} alt="" className="h-12 rounded" />
                    )}
                    {setLine}
                    <span className="ml-auto text-xs font-semibold text-accent">
                      {m.printing_picker_current()}
                    </span>
                  </span>
                ) : (
                  <button
                    type="button"
                    className="flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-left text-sm text-fg hover:bg-surface-muted focus-visible:outline-2"
                    onClick={() => onPick(p.scryfallId)}
                  >
                    {p.imageSmall != null && (
                      <img src={p.imageSmall} alt="" className="h-12 rounded" />
                    )}
                    {setLine}
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </Dialog>
  );
}
```

NOTE: `bg-surface-muted` / `bg-accent/10` — check `docs/architecture/structure.md`'s token list and the existing combobox option-highlight class; use whatever token the combobox uses for its active option if these don't exist.

- [ ] **Step 6: Wire into the collection page.** In `CollectionPage.tsx`:
  - add state `const [pickerItem, setPickerItem] = useState<CollectionItemEntry | null>(null);` (import the type from `../api`) and `const changePrinting = useChangePrinting();`
  - add a change-printing button between the stepper and the remove button in each row:

```tsx
<Button
  type="button"
  variant="ghost"
  size="sm"
  aria-label={m.collection_change_printing({ name: item.name })}
  onClick={() => setPickerItem(item)}
>
  ⇄
</Button>
```

  - render the dialog once, after the list:

```tsx
{pickerItem && (
  <PrintingPickerDialog
    open
    onClose={() => setPickerItem(null)}
    oracleId={pickerItem.oracleId}
    name={pickerItem.name}
    currentScryfallId={pickerItem.scryfallId}
    onPick={(newScryfallId) => {
      changePrinting.mutate({ scryfallId: pickerItem.scryfallId, newScryfallId });
      setPickerItem(null);
    }}
  />
)}
```

with `import { PrintingPickerDialog } from "@/shared/cards/PrintingPickerDialog";`.

- [ ] **Step 7: Run all checks**

Run: `pnpm --filter @cube-planner/frontend test && pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend lint`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add -A frontend/src frontend/messages
git commit -m "feat(collections): printing picker dialog, wired into collection rows"
```

---

### Task 14: Import dialog (paste → review → commit)

**Files:**
- Create: `frontend/src/features/collection/lib/importReview.ts`
- Test: `frontend/src/features/collection/lib/importReview.test.ts`
- Create: `frontend/src/features/collection/components/ImportDialog.tsx`
- Test: `frontend/src/features/collection/components/ImportDialog.test.tsx`
- Modify: `frontend/src/features/collection/components/CollectionPage.tsx` (Import button)
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`

**Interfaces:**
- Consumes: Task 10 `Dialog`, Task 11 `useResolveImport` + `useImportItems` + `ImportResolveLine`.
- Produces:
  - `defaultChoices(lines: ImportResolveLine[]): Map<number, string | null>` — matched → its printing; ambiguous → top suggestion; unmatched → null (skip)
  - `buildImportItems(lines: ImportResolveLine[], choices: Map<number, string | null>): { scryfallId: string; quantity: number }[]`
  - `<ImportDialog open onClose={…} />` self-contained two-step dialog.

- [ ] **Step 1: Add message keys.** en:

```json
"collection_import_button": "Import list",
"collection_import_title": "Import a card list",
"collection_import_hint": "One card per line: \"4 Lightning Bolt\", \"4x Lightning Bolt\" or just \"Lightning Bolt\".",
"collection_import_text_label": "Card list",
"collection_import_resolve_button": "Preview import",
"collection_import_matched": "Matched ({count})",
"collection_import_ambiguous": "Needs a choice ({count})",
"collection_import_unmatched": "Not found ({count})",
"collection_import_skip": "Skip this line",
"collection_import_choice_label": "Match for \"{raw}\"",
"collection_import_confirm": "Add to collection ({count})",
"collection_import_back": "Back",
"collection_import_result": "{added} new cards, {updated} updated.",
"collection_import_nothing": "Nothing to import."
```

pl:

```json
"collection_import_button": "Importuj listę",
"collection_import_title": "Import listy kart",
"collection_import_hint": "Jedna karta na linię: „4 Lightning Bolt”, „4x Lightning Bolt” albo samo „Lightning Bolt”.",
"collection_import_text_label": "Lista kart",
"collection_import_resolve_button": "Podgląd importu",
"collection_import_matched": "Dopasowane ({count})",
"collection_import_ambiguous": "Wymaga wyboru ({count})",
"collection_import_unmatched": "Nie znaleziono ({count})",
"collection_import_skip": "Pomiń tę linię",
"collection_import_choice_label": "Dopasowanie dla „{raw}”",
"collection_import_confirm": "Dodaj do kolekcji ({count})",
"collection_import_back": "Wstecz",
"collection_import_result": "Nowe karty: {added}, zaktualizowane: {updated}.",
"collection_import_nothing": "Nie ma nic do zaimportowania."
```

Run `pnpm --filter @cube-planner/frontend gen`.

- [ ] **Step 2: Write the failing lib tests** `frontend/src/features/collection/lib/importReview.test.ts`:

```ts
import { expect, test } from "vitest";
import type { ImportResolveLine } from "../api";
import { buildImportItems, defaultChoices } from "./importReview";

const match = (scryfallId: string) => ({
  scryfallId,
  oracleId: "o",
  name: "Card",
  manaCost: "",
  typeLine: "",
  setCode: "tst",
  setName: "Test",
  collectorNumber: "1",
  imageSmall: null,
  imageNormal: null,
});

const lines: ImportResolveLine[] = [
  { lineNumber: 1, raw: "4 Bolt", quantity: 4, status: "matched", match: match("bolt") },
  { lineNumber: 2, raw: "Blot", quantity: 1, status: "ambiguous", suggestions: [match("s1"), match("s2")] },
  { lineNumber: 3, raw: "Gibberish", quantity: 1, status: "unmatched" },
];

test("defaultChoices: matched printing, top suggestion, skip for unmatched", () => {
  const choices = defaultChoices(lines);
  expect(choices.get(1)).toBe("bolt");
  expect(choices.get(2)).toBe("s1");
  expect(choices.get(3)).toBeNull();
});

test("buildImportItems drops skipped lines and keeps quantities", () => {
  const choices = defaultChoices(lines);
  expect(buildImportItems(lines, choices)).toEqual([
    { scryfallId: "bolt", quantity: 4 },
    { scryfallId: "s1", quantity: 1 },
  ]);
});

test("a manual skip removes an ambiguous line", () => {
  const choices = defaultChoices(lines);
  choices.set(2, null);
  expect(buildImportItems(lines, choices)).toEqual([{ scryfallId: "bolt", quantity: 4 }]);
});
```

- [ ] **Step 3: Run to verify failure, then implement** `frontend/src/features/collection/lib/importReview.ts`:

Run: `pnpm --filter @cube-planner/frontend test importReview` → FAIL, then:

```ts
import type { ImportResolveLine } from "../api";

/** Chosen printing per lineNumber; null = skip the line. */
export type LineChoice = string | null;

export function defaultChoices(lines: ImportResolveLine[]): Map<number, LineChoice> {
  const choices = new Map<number, LineChoice>();
  for (const line of lines) {
    if (line.status === "matched" && line.match) {
      choices.set(line.lineNumber, line.match.scryfallId);
    } else if (line.status === "ambiguous") {
      choices.set(line.lineNumber, line.suggestions?.[0]?.scryfallId ?? null);
    } else {
      choices.set(line.lineNumber, null);
    }
  }
  return choices;
}

export function buildImportItems(
  lines: ImportResolveLine[],
  choices: Map<number, LineChoice>,
): { scryfallId: string; quantity: number }[] {
  const items: { scryfallId: string; quantity: number }[] = [];
  for (const line of lines) {
    const choice = choices.get(line.lineNumber);
    if (choice == null) continue; // the server merges duplicate printings
    items.push({ scryfallId: choice, quantity: line.quantity });
  }
  return items;
}
```

Run: `pnpm --filter @cube-planner/frontend test importReview` → PASS.

- [ ] **Step 4: Implement the dialog** `frontend/src/features/collection/components/ImportDialog.tsx`:

```tsx
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import { Label } from "@/shared/ui/label";
import type { ImportResolveLine } from "../api";
import { useImportItems, useResolveImport } from "../api";
import type { LineChoice } from "../lib/importReview";
import { buildImportItems, defaultChoices } from "../lib/importReview";

export function ImportDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [text, setText] = useState("");
  const [lines, setLines] = useState<ImportResolveLine[] | null>(null);
  const [choices, setChoices] = useState<Map<number, LineChoice>>(new Map());
  const [result, setResult] = useState<{ added: number; updated: number } | null>(null);
  const resolve = useResolveImport();
  const importItems = useImportItems();

  const reset = () => {
    setText("");
    setLines(null);
    setChoices(new Map());
    setResult(null);
  };
  const close = () => {
    reset();
    onClose();
  };

  const matched = lines?.filter((l) => l.status === "matched") ?? [];
  const ambiguous = lines?.filter((l) => l.status === "ambiguous") ?? [];
  const unmatched = lines?.filter((l) => l.status === "unmatched") ?? [];
  const items = lines ? buildImportItems(lines, choices) : [];

  return (
    <Dialog open={open} onClose={close} title={m.collection_import_title()}>
      {result !== null ? (
        <div className="flex flex-col gap-4">
          <Alert variant="default" role="status">
            {m.collection_import_result({ added: result.added, updated: result.updated })}
          </Alert>
          <Button type="button" onClick={close}>
            {m.dialog_close()}
          </Button>
        </div>
      ) : lines === null ? (
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            resolve.mutate(
              { text },
              {
                onSuccess: (resolved) => {
                  setLines(resolved);
                  setChoices(defaultChoices(resolved));
                },
              },
            );
          }}
        >
          <p className="text-sm text-fg-muted">{m.collection_import_hint()}</p>
          <Label htmlFor="import-text">{m.collection_import_text_label()}</Label>
          <textarea
            id="import-text"
            required
            rows={10}
            value={text}
            onChange={(e) => setText(e.target.value)}
            className="rounded-md border border-border bg-surface p-2 font-mono text-sm text-fg"
          />
          {resolve.isError && <Alert variant="danger">{resolve.error.message}</Alert>}
          <Button type="submit" disabled={resolve.isPending}>
            {m.collection_import_resolve_button()}
          </Button>
        </form>
      ) : (
        <div className="flex flex-col gap-4">
          {matched.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-fg">
                {m.collection_import_matched({ count: matched.length })}
              </h3>
              <ul className="text-sm text-fg-muted">
                {matched.map((l) => (
                  <li key={l.lineNumber}>
                    {l.quantity}× {l.match?.name}
                  </li>
                ))}
              </ul>
            </section>
          )}
          {ambiguous.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-fg">
                {m.collection_import_ambiguous({ count: ambiguous.length })}
              </h3>
              <ul className="flex flex-col gap-2">
                {ambiguous.map((l) => {
                  const selectId = `import-choice-${l.lineNumber}`;
                  return (
                    <li key={l.lineNumber} className="flex flex-col gap-1">
                      <Label htmlFor={selectId}>
                        {m.collection_import_choice_label({ raw: l.raw })}
                      </Label>
                      <select
                        id={selectId}
                        value={choices.get(l.lineNumber) ?? ""}
                        onChange={(e) =>
                          setChoices((prev) =>
                            new Map(prev).set(
                              l.lineNumber,
                              e.target.value === "" ? null : e.target.value,
                            ),
                          )
                        }
                        className="rounded-md border border-border bg-surface p-1.5 text-sm text-fg"
                      >
                        {(l.suggestions ?? []).map((s) => (
                          <option key={s.scryfallId} value={s.scryfallId}>
                            {s.name} ({s.setName} · #{s.collectorNumber})
                          </option>
                        ))}
                        <option value="">{m.collection_import_skip()}</option>
                      </select>
                    </li>
                  );
                })}
              </ul>
            </section>
          )}
          {unmatched.length > 0 && (
            <section>
              <h3 className="text-sm font-semibold text-fg">
                {m.collection_import_unmatched({ count: unmatched.length })}
              </h3>
              <ul className="text-sm text-fg-muted">
                {unmatched.map((l) => (
                  <li key={l.lineNumber}>{l.raw}</li>
                ))}
              </ul>
            </section>
          )}
          {importItems.isError && <Alert variant="danger">{importItems.error.message}</Alert>}
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" onClick={() => setLines(null)}>
              {m.collection_import_back()}
            </Button>
            {items.length === 0 ? (
              <p className="text-sm text-fg-muted">{m.collection_import_nothing()}</p>
            ) : (
              <Button
                type="button"
                disabled={importItems.isPending}
                onClick={() =>
                  importItems.mutate(
                    { items },
                    {
                      onSuccess: (r) =>
                        setResult({ added: r.addedRows, updated: r.updatedRows }),
                    },
                  )
                }
              >
                {m.collection_import_confirm({ count: items.length })}
              </Button>
            )}
          </div>
        </div>
      )}
    </Dialog>
  );
}
```

- [ ] **Step 5: Add the button to the page.** In `CollectionPage.tsx` header (next to the title block):

```tsx
<Button type="button" variant="outline" onClick={() => setImportOpen(true)}>
  {m.collection_import_button()}
</Button>
```

with `const [importOpen, setImportOpen] = useState(false);` and, after the list:

```tsx
<ImportDialog open={importOpen} onClose={() => setImportOpen(false)} />
```

- [ ] **Step 6: Write the dialog flow test** `frontend/src/features/collection/components/ImportDialog.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { ImportDialog } from "./ImportDialog";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => vi.unstubAllGlobals());

const card = (scryfallId: string, name: string) => ({
  scryfallId,
  oracleId: "o",
  name,
  manaCost: "",
  typeLine: "",
  setCode: "tst",
  setName: "Test Set",
  collectorNumber: "1",
  imageSmall: null,
  imageNormal: null,
});

test("paste → review groups → confirm posts matched + default ambiguous choice", async () => {
  const fetchMock = vi.fn(async (input: Request | string) => {
    const url = typeof input === "string" ? input : input.url;
    if (url.includes("/import/resolve")) {
      return new Response(
        JSON.stringify({
          lines: [
            { lineNumber: 1, raw: "4 Bolt", quantity: 4, status: "matched", match: card("bolt", "Lightning Bolt") },
            { lineNumber: 2, raw: "Blot", quantity: 1, status: "ambiguous", suggestions: [card("s1", "Lightning Bolt"), card("s2", "Lightning Blast")] },
            { lineNumber: 3, raw: "Gibberish", quantity: 1, status: "unmatched" },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }
    return new Response(JSON.stringify({ addedRows: 2, updatedRows: 0 }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);

  render(<ImportDialog open onClose={() => {}} />, { wrapper });
  await userEvent.type(screen.getByLabelText("Card list"), "4 Bolt{enter}Blot{enter}Gibberish");
  await userEvent.click(screen.getByRole("button", { name: "Preview import" }));

  expect(await screen.findByText("Matched (1)")).toBeInTheDocument();
  expect(screen.getByText("Needs a choice (1)")).toBeInTheDocument();
  expect(screen.getByText("Not found (1)")).toBeInTheDocument();

  await userEvent.click(screen.getByRole("button", { name: "Add to collection (2)" }));
  expect(await screen.findByText("2 new cards, 0 updated.")).toBeInTheDocument();

  const importCall = fetchMock.mock.calls.find(([input]) => {
    const url = typeof input === "string" ? input : (input as Request).url;
    return url.endsWith("/api/collection/import");
  });
  expect(importCall).toBeDefined();
  const body = JSON.parse((importCall?.[1] as RequestInit).body as string);
  expect(body.items).toEqual([
    { scryfallId: "bolt", quantity: 4 },
    { scryfallId: "s1", quantity: 1 },
  ]);
});
```

NOTE: if the dialog submits via `client.POST`, the request body may live on the `Request` object instead of `init` — mirror how `CollectionPage.test.tsx`'s PUT assertion ended up working and reuse that extraction.

- [ ] **Step 7: Run all checks**

Run: `pnpm --filter @cube-planner/frontend test features/collection && pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend lint`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add frontend/src frontend/messages
git commit -m "feat(collections): bulk import dialog with resolve-review-commit flow"
```

---

### Task 15: Wantlist page + Cardmarket export + cube-page entry point

**Files:**
- Create: `frontend/src/features/collection/lib/cardmarket.ts`
- Test: `frontend/src/features/collection/lib/cardmarket.test.ts`
- Create: `frontend/src/features/collection/components/WantlistPage.tsx`
- Test: `frontend/src/features/collection/components/WantlistPage.test.tsx`
- Create: `frontend/src/routes/cubes.$cubeId.wantlist.tsx`
- Modify: `frontend/src/features/cubes/components/CubeDisplayPage.tsx` (entry-point link)
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`

**Interfaces:**
- Consumes: Task 11 `useWantlist` + `WantlistEntry`, Task 9 `CardHoverPreview`.
- Produces:
  - `wantlistToCardmarketText(items: readonly { missingQuantity: number; name: string }[]): string` — `<qty> <name>` per line, the format Cardmarket's Wants import accepts
  - `wantlistFilename(cubeName: string): string` — kebab slug + `-wantlist.txt`
  - `downloadTextFile(filename: string, text: string): void`
  - `WantlistPage` at route `/cubes/$cubeId/wantlist`.

- [ ] **Step 1: Add message keys.** en:

```json
"wantlist_compare_button": "Compare with my collection",
"wantlist_title": "Wantlist — {cube}",
"wantlist_col_card": "Card",
"wantlist_col_missing": "Missing",
"wantlist_col_in_cube": "In cube",
"wantlist_col_owned": "Owned",
"wantlist_total": "{count} cards missing",
"wantlist_empty": "You own everything in this cube.",
"wantlist_download": "Download for Cardmarket"
```

pl:

```json
"wantlist_compare_button": "Porównaj z moją kolekcją",
"wantlist_title": "Lista braków — {cube}",
"wantlist_col_card": "Karta",
"wantlist_col_missing": "Brakuje",
"wantlist_col_in_cube": "W kostce",
"wantlist_col_owned": "Posiadane",
"wantlist_total": "Brakujące karty: {count}",
"wantlist_empty": "Masz wszystkie karty z tej kostki.",
"wantlist_download": "Pobierz dla Cardmarket"
```

Run `pnpm --filter @cube-planner/frontend gen`.

- [ ] **Step 2: Write the failing lib tests** `frontend/src/features/collection/lib/cardmarket.test.ts`:

```ts
import { expect, test } from "vitest";
import { wantlistFilename, wantlistToCardmarketText } from "./cardmarket";

test("one '<qty> <name>' line per entry", () => {
  expect(
    wantlistToCardmarketText([
      { missingQuantity: 1, name: "Lightning Bolt" },
      { missingQuantity: 3, name: "Borrowing 100,000 Arrows" },
    ]),
  ).toBe("1 Lightning Bolt\n3 Borrowing 100,000 Arrows");
});

test("empty list gives an empty string", () => {
  expect(wantlistToCardmarketText([])).toBe("");
});

test("filename slugs the cube name", () => {
  expect(wantlistFilename("Mat's Vintage Cube!")).toBe("mat-s-vintage-cube-wantlist.txt");
  expect(wantlistFilename("***")).toBe("cube-wantlist.txt");
});
```

- [ ] **Step 3: Run to verify failure, then implement** `frontend/src/features/collection/lib/cardmarket.ts`:

Run: `pnpm --filter @cube-planner/frontend test cardmarket` → FAIL, then:

```ts
// Cardmarket's Wants import accepts plain "<amount> <card name>" lines.
export function wantlistToCardmarketText(
  items: readonly { missingQuantity: number; name: string }[],
): string {
  return items.map((i) => `${i.missingQuantity} ${i.name}`).join("\n");
}

export function wantlistFilename(cubeName: string): string {
  const slug =
    cubeName
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "cube";
  return `${slug}-wantlist.txt`;
}

export function downloadTextFile(filename: string, text: string): void {
  const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
```

Run: `pnpm --filter @cube-planner/frontend test cardmarket` → PASS.

- [ ] **Step 4: Implement the page** `frontend/src/features/collection/components/WantlistPage.tsx`:

```tsx
import { getRouteApi, Link } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { CardHoverPreview } from "@/shared/cards/CardHoverPreview";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { UnauthorizedError, useWantlist } from "../api";
import { downloadTextFile, wantlistFilename, wantlistToCardmarketText } from "../lib/cardmarket";

const route = getRouteApi("/cubes/$cubeId/wantlist");

export function WantlistPage() {
  const { cubeId } = route.useParams();
  const wantlist = useWantlist(cubeId);

  if (wantlist.isPending) return <p className="text-sm text-fg-muted">{m.loading()}</p>;
  if (wantlist.isError) {
    if (wantlist.error instanceof UnauthorizedError) {
      return (
        <Alert variant="default">
          {wantlist.error.message}{" "}
          <Link to="/login" className="font-medium underline">
            {m.nav_login()}
          </Link>
        </Alert>
      );
    }
    return <Alert variant="danger">{wantlist.error.message}</Alert>;
  }

  const { cubeName, items, totalMissing } = wantlist.data;

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold text-fg">
            {m.wantlist_title({ cube: cubeName })}
          </h1>
          <p className="text-sm text-fg-muted">{m.wantlist_total({ count: totalMissing })}</p>
        </div>
        {items.length > 0 && (
          <Button
            type="button"
            onClick={() =>
              downloadTextFile(wantlistFilename(cubeName), wantlistToCardmarketText(items))
            }
          >
            {m.wantlist_download()}
          </Button>
        )}
      </div>

      {items.length === 0 ? (
        <p className="text-sm text-fg-muted">{m.wantlist_empty()}</p>
      ) : (
        <table className="w-full max-w-2xl text-sm">
          <thead>
            <tr className="border-b border-border text-left text-fg-muted">
              <th scope="col" className="py-1.5 font-medium">
                {m.wantlist_col_card()}
              </th>
              <th scope="col" className="py-1.5 text-right font-medium">
                {m.wantlist_col_missing()}
              </th>
              <th scope="col" className="py-1.5 text-right font-medium">
                {m.wantlist_col_in_cube()}
              </th>
              <th scope="col" className="py-1.5 text-right font-medium">
                {m.wantlist_col_owned()}
              </th>
            </tr>
          </thead>
          <tbody>
            {items.map((item) => (
              <tr key={item.oracleId} className="border-b border-border">
                <td className="py-1.5">
                  <CardHoverPreview card={item}>
                    <span className="text-fg">{item.name}</span>
                  </CardHoverPreview>
                </td>
                <td className="py-1.5 text-right font-semibold tabular-nums text-accent">
                  {item.missingQuantity}
                </td>
                <td className="py-1.5 text-right tabular-nums text-fg-muted">
                  {item.cubeQuantity}
                </td>
                <td className="py-1.5 text-right tabular-nums text-fg-muted">
                  {item.ownedQuantity}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Create the route** `frontend/src/routes/cubes.$cubeId.wantlist.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { WantlistPage } from "@/features/collection/components/WantlistPage";

export const Route = createFileRoute("/cubes/$cubeId/wantlist")({ component: WantlistPage });
```

- [ ] **Step 6: Add the entry point.** In `frontend/src/features/cubes/components/CubeDisplayPage.tsx`, add a button before the History button (shown unconditionally, like every action button in this repo — anonymous users get the login prompt on the wantlist page):

```tsx
<Button asChild variant="outline" size="sm">
  <Link to="/cubes/$cubeId/wantlist" params={{ cubeId }}>
    {m.wantlist_compare_button()}
  </Link>
</Button>
```

- [ ] **Step 7: Write the page test** `frontend/src/features/collection/components/WantlistPage.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { WantlistPage } from "./WantlistPage";

afterEach(() => vi.unstubAllGlobals());

function renderPage() {
  const rootRoute = createRootRoute();
  const wantlist = createRoute({
    getParentRoute: () => rootRoute,
    path: "/cubes/$cubeId/wantlist",
    component: WantlistPage,
  });
  const login = createRoute({ getParentRoute: () => rootRoute, path: "/login", component: () => null });
  const router = createRouter({
    routeTree: rootRoute.addChildren([wantlist, login]),
    history: createMemoryHistory({ initialEntries: ["/cubes/c1/wantlist"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("renders missing cards with quantities and the download button", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      jsonResponse({
        cubeName: "Vintage Cube",
        totalMissing: 2,
        items: [
          {
            oracleId: "o1",
            scryfallId: "s1",
            name: "Lightning Bolt",
            manaCost: "{R}",
            imageSmall: null,
            imageNormal: null,
            missingQuantity: 1,
            cubeQuantity: 4,
            ownedQuantity: 3,
          },
        ],
      }),
    ),
  );
  renderPage();
  expect(await screen.findByText("Lightning Bolt")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Download for Cardmarket" })).toBeInTheDocument();
  const row = screen.getByText("Lightning Bolt").closest("tr");
  expect(row).toHaveTextContent("1");
  expect(row).toHaveTextContent("4");
  expect(row).toHaveTextContent("3");
});

test("empty wantlist shows the own-everything state, no download", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ cubeName: "Vintage Cube", totalMissing: 0, items: [] })),
  );
  renderPage();
  expect(await screen.findByText("You own everything in this cube.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download for Cardmarket" })).not.toBeInTheDocument();
});

test("shows a login prompt on 401", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(jsonResponse({ title: "Unauthorized", status: 401 }, 401)),
  );
  renderPage();
  expect(await screen.findByText(/log in/i)).toBeInTheDocument();
});
```

- [ ] **Step 8: Run all checks**

Run: `pnpm --filter @cube-planner/frontend gen && pnpm --filter @cube-planner/frontend test && pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend lint`
Expected: PASS (gen picks up the new route).

- [ ] **Step 9: Commit**

```bash
git add frontend/src frontend/messages
git commit -m "feat(collections): wantlist page with Cardmarket export"
```

---

### Task 16: Axe smoke tests + full verification sweep

**Files:**
- Create: `frontend/src/features/collection/components/a11y.test.tsx`

**Interfaces:**
- Consumes: the real `routeTree.gen` (routes from Tasks 12 + 15 exist), mirroring `features/cubes/components/a11y.test.tsx`.

- [ ] **Step 1: Write the axe tests** `frontend/src/features/collection/components/a11y.test.tsx` (same harness as the cubes axe file — jsdom, real route tree, stubbed fetch):

```tsx
// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, waitFor } from "@testing-library/react";
import { axe } from "vitest-axe";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

const item = {
  scryfallId: "s1",
  oracleId: "o1",
  name: "Lightning Bolt",
  manaCost: "{R}",
  typeLine: "Instant",
  setCode: "m10",
  setName: "Magic 2010",
  collectorNumber: "146",
  imageSmall: null,
  imageNormal: null,
  quantity: 4,
};

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: Request | string) => {
      const url = typeof input === "string" ? input : input.url;
      const json = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url.includes("/wantlist")) {
        return json({
          cubeName: "Vintage Cube",
          totalMissing: 1,
          items: [{ ...item, missingQuantity: 1, cubeQuantity: 4, ownedQuantity: 3 }],
        });
      }
      if (url.includes("/api/collection")) {
        return json({ items: [item], total: 1, totalCopies: 4 });
      }
      return new Response("{}", { status: 401 });
    }),
  );
  vi.stubEnv("DEV", false);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

async function renderRoute(path: string) {
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
  await waitFor(() => expect(container.textContent).not.toBe(""));
  return container;
}

it("collection page has no axe violations", async () => {
  const container = await renderRoute("/collection");
  await waitFor(() => expect(container.textContent).toContain("Lightning Bolt"));
  expect(await axe(container)).toHaveNoViolations();
});

it("wantlist page has no axe violations", async () => {
  const container = await renderRoute("/cubes/c1/wantlist");
  await waitFor(() => expect(container.textContent).toContain("Lightning Bolt"));
  expect(await axe(container)).toHaveNoViolations();
});
```

NOTE: mirror the exact assertion helper the cubes `a11y.test.tsx` uses (`toHaveNoViolations` setup import, waitFor idioms). If it registers a matcher via a setup file, this file inherits it.

- [ ] **Step 2: Run the axe tests**

Run: `pnpm --filter @cube-planner/frontend test a11y`
Expected: PASS (both new screens, plus the existing cubes ones untouched).

- [ ] **Step 3: Full verification sweep**

```bash
cd backend && go test ./... && gofumpt -l . && cd ..
pnpm --filter @cube-planner/frontend gen
pnpm --filter @cube-planner/frontend lint
pnpm --filter @cube-planner/frontend typecheck
pnpm --filter @cube-planner/frontend test
git status --short   # nothing unexpected; generated client committed
```

Expected: everything green; `gofumpt -l` prints nothing.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/features/collection
git commit -m "test(collections): axe smoke tests for collection and wantlist screens"
```

---

## Deviations from the spec (deliberate, documented)

- **PUT response**: always 200 with an optional `item` field (`item?: CollectionItemEntry`, omitted after a quantity-0 delete via `omitempty`) instead of the spec's 200/204 mix — one success shape keeps the generated client simple. (Post-review fix a6a61e7: originally serialized as `item: null`, which contradicted the generated schema's required non-nullable field.)
- **`service_test.go`**: the spec lists it "where mocking is sensible"; nothing here mocks sensibly — the pure logic lives in `parse.go` (unit-tested) and everything else is covered by endpoint integration tests against real Postgres.
- **Pagination messages**: shared `pagination_*` keys are introduced if the cube browser doesn't already define equivalents; reuse whatever exists first.

