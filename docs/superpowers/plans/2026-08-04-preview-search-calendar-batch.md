# Card preview sheet + backlog batch (#8, #9, #13, #18) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close issues #18 (CardSummary colors), #9 (search popularity via `edhrec_rank`), #13 (event add-to-calendar), and #8 (touch-reachable card preview sheet).

**Architecture:** Four independent PRs off master, smallest first. #18 and #9 are backend-led (sqlc query + huma model changes, regenerated TS client). #13 is frontend-only (pure calendar helpers + two buttons). #8 is frontend-only: a shared responsive `CardPreviewSheet` (Dialog ≥768 px, bottom Drawer below) fed by the existing cached `useCardPrintings` query — no new endpoint.

**Tech Stack:** Go (huma + chi + sqlc + goose + pgx), Postgres pg_trgm, React + TanStack Router/Query, Tailwind v4, Paraglide i18n, vitest + RTL (happy-dom; axe files use jsdom).

Spec: `docs/superpowers/specs/2026-08-04-preview-search-calendar-batch-design.md`

## Global Constraints

- `docs/architecture/structure.md` is binding: dependency direction `app`/`routes` → `features` → `shared`; no feature→feature imports — **exception already established in this codebase:** `import { useMe } from "@/features/auth/api"` (EventDetailPage.tsx:5 does this today; follow it).
- All user-facing strings via Paraglide: add to BOTH `frontend/messages/en.json` and `frontend/messages/pl.json`, use as `m.key_name()`. Never hardcode.
- Generated code is never hand-edited: `backend/internal/db/*.sql.go` comes from `cd backend && sqlc generate`; `frontend/src/shared/api/` from `make api-generate` (tracked — commit it; CI fails if stale); `src/routeTree.gen.ts` + `src/paraglide/` are gitignored build output (`pnpm gen`).
- Tests: `make backend-test` (needs Docker for testcontainers), `make frontend-test`. Targeted: `cd backend && go test ./internal/<pkg>/...`; `pnpm --filter @cube-planner/frontend test <file-filter>`.
- Lint/format: oxlint + oxfmt (frontend), gofumpt + golangci-lint (backend) — lefthook runs them pre-commit; pre-push mirrors CI.
- Semantic color tokens only (`text-fg`, `text-fg-muted`, `bg-surface-raised`, `border-border`, `outline-accent`…).
- Master is protected: each PR group starts from up-to-date master (previous PR merged first), branch → push → `gh pr create`. PR body includes `Closes #N` and ends with the Claude Code attribution line.
- Commit messages follow repo style: `feat(cards): …`, `fix(events): …`, `test(cubes): …`.

---

## PR 1 — #18: `colors` on CardSummary (branch `feature/card-summary-colors`)

### Task 1: Backend — expose colors from autocomplete

**Files:**
- Modify: `backend/internal/db/queries/cards.sql:92` (AutocompleteCards select list)
- Modify: `backend/internal/platform/httpapi/cards.go:17-24` (CardSummary), `:130-140` (handler mapping)
- Test: `backend/internal/platform/httpapi/cards_endpoints_test.go` (TestAutocompleteEndpoint)
- Generated: `backend/internal/db/cards.sql.go` (sqlc), `frontend/src/shared/api/` (make api-generate)

**Interfaces:**
- Produces: `CardSummary` JSON gains `colors: string[]` (generated TS: `colors: string[] | null`). Task 2 relies on it.

- [ ] **Step 1: Create the branch**

```bash
git checkout master && git pull && git checkout -b feature/card-summary-colors
```

- [ ] **Step 2: Write the failing test** — in `TestAutocompleteEndpoint` (cards_endpoints_test.go:89), extend the response struct and add an assertion. `seedCard` already inserts `colors` = `c.colorIdentity` (the `$10` param is used for both columns), and the Lightning Bolt seeds default to `["R"]`:

```go
	var body struct {
		Cards []struct {
			Name     string   `json:"name"`
			OracleID string   `json:"oracleId"`
			Colors   []string `json:"colors"`
		} `json:"cards"`
	}
```

and after the existing `body.Cards[0].Name` check:

```go
	if len(body.Cards[0].Colors) != 1 || body.Cards[0].Colors[0] != "R" {
		t.Fatalf("colors = %v, want [R]", body.Cards[0].Colors)
	}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestAutocompleteEndpoint -v`
Expected: FAIL with `colors = [], want [R]` (field absent from JSON → nil slice).

- [ ] **Step 4: Add colors to the query.** In `backend/internal/db/queries/cards.sql`, change line 92:

```sql
select scryfall_id, oracle_id, name, mana_cost, type_line, image_small, colors
```

Then regenerate: `cd backend && sqlc generate` — `db.AutocompleteCardsRow` gains `Colors []string`.

- [ ] **Step 5: Expose it in the API.** In `cards.go`, add to `CardSummary` (after `TypeLine`):

```go
	Colors     []string  `json:"colors"`
```

and in the autocomplete handler mapping (after `TypeLine: r.TypeLine,`):

```go
				Colors:     r.Colors,
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestAutocompleteEndpoint -v`
Expected: PASS

- [ ] **Step 7: Regenerate the TS client**

Run: `make api-generate`, then `git diff --stat frontend/src/shared/api/` — `CardSummary` in `schema.d.ts` should gain `colors: string[] | null`.

- [ ] **Step 8: Full backend tests + commit**

```bash
cd backend && go test ./... && cd ..
git add backend frontend/src/shared/api
git commit -m "feat(cards): expose per-card colors on CardSummary"
```

### Task 2: Frontend — drop the mana-cost color fallback

**Files:**
- Modify: `frontend/src/features/cubes/lib/grouping.ts:23-51`
- Modify: `frontend/src/features/cubes/components/CubeEditorPage.tsx:50` (previewEntries)
- Test: `frontend/src/features/cubes/lib/grouping.test.ts`

**Interfaces:**
- Consumes: `CardSummary.colors` from Task 1 (via the pending-add `card` in `previewEntries`).

- [ ] **Step 1: Update the tests.** In `grouping.test.ts`, inside `describe("groupCards by color")`:
  - DELETE `test("falls back to mana cost when colors is empty (pending-add preview)")` and `test("mana-cost fallback handles hybrid, phyrexian, and twobrid pips")`.
  - ADD (reuse the file's existing entry-factory helper for the boilerplate fields — every entry is a full `CubeCardEntry`):

```ts
test("empty colors buckets as colorless, not by mana-cost pips", () => {
  // Pre-#18 this red-pip artifact would have been re-derived as red.
  const groups = groupCards([entry({ name: "Furnace Golem", manaCost: "{2}{R}", colors: [], typeLine: "Artifact Creature" })], "color");
  expect(groups.map((g) => g.key)).toEqual(["colorless"]);
});

test("modal DFC pending add buckets by union colors, not front-face mana cost", () => {
  // Valki {1}{B} // Tibalt: CardSummary now carries union colors ["B","R"].
  const groups = groupCards([entry({ name: "Valki, God of Lies", manaCost: "{1}{B}", colors: ["B", "R"], typeLine: "Legendary Creature" })], "color");
  expect(groups.map((g) => g.key)).toEqual(["multicolor"]);
});
```

(If the file builds entries with plain object literals instead of a helper, follow that style — the point is `colors` drives the bucket, `manaCost` never does.)

- [ ] **Step 2: Run tests to verify the new ones fail**

Run: `pnpm --filter @cube-planner/frontend test grouping`
Expected: the "empty colors buckets as colorless" test FAILS (fallback derives red); the deleted tests are gone.

- [ ] **Step 3: Delete the fallback.** In `grouping.ts`, delete `colorsFromManaCost` AND its entire block comment (lines 23-41), and change `colorBucket` to:

```ts
function colorBucket(card: CubeCardEntry): string {
  // Type wins over color: dual lands with a color identity are lands.
  if (card.typeLine.includes("Land")) return "land";
  // colors is nullable in the generated type; null = colorless.
  const colors = card.colors ?? [];
  if (colors.length === 0) return "colorless";
  if (colors.length > 1) return "multicolor";
  return colors[0] ?? "colorless";
}
```

- [ ] **Step 4: Thread real colors into pending adds.** In `CubeEditorPage.tsx` `previewEntries` (line 50), replace

```ts
        colors: [], // unknown here; grouping.ts falls back to card.manaCost
```

with

```ts
        colors: card.colors ?? [],
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm --filter @cube-planner/frontend test grouping` then `make frontend-test`
Expected: PASS (all files).

- [ ] **Step 6: Commit, push, PR**

```bash
git add frontend
git commit -m "fix(cubes): pending-add preview uses real card colors, drop mana-cost fallback"
git push -u origin feature/card-summary-colors
gh pr create --title "CardSummary colors: fix modal-DFC pending-add preview" --body "Closes #18. ..."
```

Wait for CI + merge before starting PR 2.

---

## PR 2 — #9: search popularity signal (branch `feature/search-popularity`)

### Task 3: `edhrec_rank` column + import pipeline

**Files:**
- Create: `backend/migrations/00009_cards_edhrec_rank.sql`
- Modify: `backend/internal/cards/transform.go` (scryfallCard :26-48, Card :51-73, transformCard :107-125)
- Modify: `backend/internal/cards/sync.go:139-156` (stagingColumns + copyBatch)
- Modify: `backend/internal/db/queries/cards.sql:29-62` (UpsertCardsFromStaging)
- Test: `backend/internal/cards/transform_test.go`

**Interfaces:**
- Produces: `cards.edhrec_rank integer NULL` column; `db.Card.EdhrecRank *int32`. Task 4 orders by it.

- [ ] **Step 1: Create the branch** (`git checkout master && git pull && git checkout -b feature/search-popularity`)

- [ ] **Step 2: Write the failing test** — add to `transform_test.go` (package `cards`, self-contained; mirror the file's existing style if it has a card-fixture helper):

```go
func TestTransformCardEdhrecRank(t *testing.T) {
	rank := int32(120)
	sc := scryfallCard{
		ID: "11111111-1111-1111-1111-111111111111", OracleID: "22222222-2222-2222-2222-222222222222",
		Name: "Lightning Bolt", ReleasedAt: "1993-08-05", Layout: "normal",
		Games: []string{"paper"}, EdhrecRank: &rank,
	}
	c, ok := transformCard(sc)
	if !ok {
		t.Fatal("expected transform ok")
	}
	if c.EdhrecRank == nil || *c.EdhrecRank != 120 {
		t.Fatalf("EdhrecRank = %v, want 120", c.EdhrecRank)
	}

	sc.EdhrecRank = nil
	c, _ = transformCard(sc)
	if c.EdhrecRank != nil {
		t.Fatalf("EdhrecRank = %v, want nil for unranked card", c.EdhrecRank)
	}
}
```

- [ ] **Step 3: Verify it fails to compile** — `cd backend && go test ./internal/cards/ -run TestTransformCardEdhrecRank` → `unknown field EdhrecRank`.

- [ ] **Step 4: Migration.** Create `backend/migrations/00009_cards_edhrec_rank.sql`:

```sql
-- +goose Up
-- Scryfall's EDHREC popularity rank (1 = most popular); NULL for cards
-- Scryfall doesn't rank (tokens, brand-new sets). Used as a search-ranking
-- tiebreak (issue #9). Backfilled by the next bulk sync.
alter table cards add column edhrec_rank integer;
alter table cards_staging add column edhrec_rank integer;

-- +goose Down
alter table cards drop column edhrec_rank;
alter table cards_staging drop column edhrec_rank;
```

- [ ] **Step 5: Import pipeline.** In `transform.go`:
  - `scryfallCard`: add `EdhrecRank *int32 \`json:"edhrec_rank"\`` (after `Promo`).
  - `Card`: add `EdhrecRank *int32` (after `Promo`).
  - `transformCard`: in the big `Card{...}` literal add `EdhrecRank: sc.EdhrecRank,`.

  In `sync.go`: append `"edhrec_rank"` to `stagingColumns` (line 139-145) and `c.EdhrecRank` to the row slice in `copyBatch` (same position — last).

  In `cards.sql` `UpsertCardsFromStaging`: add `edhrec_rank` to the insert column list, the `select ... from cards_staging` list (both before `updated_at`/`now()`), and `edhrec_rank = excluded.edhrec_rank,` to the conflict-update set.

  Then `cd backend && sqlc generate`.

- [ ] **Step 6: Run tests** — `cd backend && go test ./internal/cards/ -v` (transform test passes; `sync_test.go` exercises the COPY roundtrip against testcontainers and validates the column wiring). Then `go test ./...`.

- [ ] **Step 7: Commit** — `git add backend && git commit -m "feat(cards): import Scryfall edhrec_rank"`

### Task 4: Popularity-aware ordering

**Files:**
- Modify: `backend/internal/db/queries/cards.sql:94-98` (AutocompleteCards ORDER BY), `:121-126` (SearchCards ORDER BY)
- Test: `backend/internal/platform/httpapi/cards_endpoints_test.go`

**Interfaces:**
- Consumes: `edhrec_rank` column from Task 3. No API shape change — ordering only.

- [ ] **Step 1: Extend the seed helper.** In `cards_endpoints_test.go`, add `edhrec *int32` to `testCard` (:19-30) and extend `seedCard`'s INSERT with the column:

```go
	_, err := pool.Exec(context.Background(), `insert into cards (
		scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
		set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
		oracle_text, colors, color_identity, promo, image_small, image_normal,
		edhrec_rank
	) values ($1, $2, $3, $4, $5, $6, 'Test Set', '1', $7, 'normal', '{R}',
		$8, $9, 'Test text.', $10, $10, $11, $12, $12, $13)`,
		c.scryfallID, c.oracleID, c.name, cards.NormalizeName(c.name), c.released,
		c.setCode, c.rarity, c.cmc, c.typeLine, c.colorIdentity, c.promo, img, c.edhrec)
```

- [ ] **Step 2: Write the failing tests.** All seed names below contain the standalone word "bolt" but do NOT start with it — the autocomplete prefix tier (which deliberately stays the top criterion) would otherwise mask the popularity ordering:

```go
func rank(n int32) *int32 { return &n }

func TestAutocompletePopularityOrdering(t *testing.T) {
	srv, pool := newCardsServer(t)
	// All three tie at word_similarity 1.0 for q=bolt; popularity must break
	// the tie (low rank = popular), unranked last. None is a prefix match.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Frost Bolt", edhrec: rank(5000)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Lightning Bolt", edhrec: rank(100)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Shadow Bolt"}) // unranked
	// Prefix matches still outrank everything, popular or not.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Boltwing Marauder"})

	var body struct {
		Cards []struct {
			Name string `json:"name"`
		} `json:"cards"`
	}
	if code := getJSON(t, srv, "/api/cards/autocomplete?q=bolt", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := make([]string, len(body.Cards))
	for i, c := range body.Cards {
		got[i] = c.Name
	}
	want := []string{"Boltwing Marauder", "Lightning Bolt", "Frost Bolt", "Shadow Bolt"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSearchPopularityOrdering(t *testing.T) {
	srv, pool := newCardsServer(t)
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Frost Bolt", edhrec: rank(5000)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Lightning Bolt", edhrec: rank(100)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Shadow Bolt"})

	var body struct {
		Cards []struct {
			Name string `json:"name"`
		} `json:"cards"`
	}
	if code := getJSON(t, srv, "/api/cards/search?name=bolt", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := make([]string, len(body.Cards))
	for i, c := range body.Cards {
		got[i] = c.Name
	}
	want := []string{"Lightning Bolt", "Frost Bolt", "Shadow Bolt"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	// Without a name filter, browsing stays alphabetical — popularity must
	// not reorder it.
	if code := getJSON(t, srv, "/api/cards/search", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Cards[0].Name != "Frost Bolt" {
		t.Fatalf("no-name first = %q, want alphabetical Frost Bolt", body.Cards[0].Name)
	}
}
```

(Add `"slices"` to the test file imports.)

- [ ] **Step 3: Run to verify failure** — `cd backend && go test ./internal/platform/httpapi/ -run PopularityOrdering -v` → FAIL: today `similarity` breaks the 1.0 tie toward the shortest name (Frost Bolt / Shadow Bolt before Lightning Bolt).

- [ ] **Step 4: Change the ordering.** In `cards.sql`, AutocompleteCards ORDER BY (:94-98) becomes:

```sql
order by
    (normalized_name like sqlc.arg(prefix) || '%') desc,
    word_similarity(sqlc.arg(query), normalized_name) desc,
    edhrec_rank asc nulls last,
    similarity(sqlc.arg(query), normalized_name) desc,
    name asc
```

SearchCards ORDER BY (:121-126) becomes (the case-when keeps no-name browsing alphabetical — a null case result sorts nulls-last, i.e. no effect):

```sql
order by
    case when sqlc.narg(name)::text is not null
         then word_similarity(sqlc.narg(name), normalized_name) end desc nulls last,
    case when sqlc.narg(name)::text is not null
         then edhrec_rank end asc nulls last,
    case when sqlc.narg(name)::text is not null
         then similarity(sqlc.narg(name), normalized_name) end desc nulls last,
    name asc
```

Also extend the comment block above AutocompleteCards (:75-83) with one line: `-- edhrec_rank (1 = most popular) breaks word-similarity ties toward popular cards (issue #9).`

Then `cd backend && sqlc generate` (ordering-only change; row structs unchanged).

- [ ] **Step 5: Run tests** — `cd backend && go test ./internal/platform/httpapi/ -v` then `go test ./...` → PASS (existing autocomplete/search tests must stay green).

- [ ] **Step 6: Commit, push, PR**

```bash
git add backend
git commit -m "feat(cards): rank search results by EDHREC popularity"
git push -u origin feature/search-popularity
gh pr create --title "Card search: popularity signal (edhrec_rank)" --body "Closes #9. Ranks are NULL until the next Scryfall bulk sync after deploy; search behaves as before until then. ..."
```

Wait for CI + merge before PR 3.

---

## PR 3 — #13: Add to calendar (branch `feature/event-add-to-calendar`)

### Task 5: Calendar helpers + shared download util

**Files:**
- Create: `frontend/src/shared/lib/download.ts`
- Create: `frontend/src/features/events/lib/calendar.ts`
- Modify: `frontend/src/features/collection/lib/cardmarket.ts:17-25` (remove `downloadTextFile`; update its callers — `grep -rn downloadTextFile frontend/src`)
- Test: `frontend/src/features/events/lib/calendar.test.ts`

**Interfaces:**
- Produces: `downloadTextFile(filename: string, text: string, mime?: string): void` in `@/shared/lib/download`; `googleCalendarUrl(e: CalendarEvent): string`, `buildIcs(e: CalendarEvent): string`, `icsFilename(name: string): string`, `type CalendarEvent = { id: string; name: string; description: string; location: string; startsAt: string }`. Task 6 consumes all of these.

- [ ] **Step 1: Create the branch** (`git checkout master && git pull && git checkout -b feature/event-add-to-calendar`)

- [ ] **Step 2: Move `downloadTextFile` to shared.** Create `frontend/src/shared/lib/download.ts`:

```ts
export function downloadTextFile(
  filename: string,
  text: string,
  mime = "text/plain;charset=utf-8",
): void {
  const blob = new Blob([text], { type: mime });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
```

Delete it from `cardmarket.ts` and point every caller (grep — WantlistPage at least) at `@/shared/lib/download`. Run `make frontend-test` — existing wantlist tests must stay green.

- [ ] **Step 3: Write the failing calendar tests** — `frontend/src/features/events/lib/calendar.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { buildIcs, googleCalendarUrl, icsFilename } from "./calendar";

const event = {
  id: "11111111-2222-3333-4444-555555555555",
  name: "Summer Draft; Finals",
  description: "Bring snacks, and sleeves.\nDoors open early.",
  location: "Community Hall, Kraków",
  startsAt: "2026-08-15T16:00:00Z",
};

describe("googleCalendarUrl", () => {
  test("builds a template link with UTC start/end 4h apart", () => {
    const url = new URL(googleCalendarUrl(event));
    expect(url.origin + url.pathname).toBe("https://calendar.google.com/calendar/render");
    expect(url.searchParams.get("action")).toBe("TEMPLATE");
    expect(url.searchParams.get("text")).toBe("Summer Draft; Finals");
    expect(url.searchParams.get("dates")).toBe("20260815T160000Z/20260815T200000Z");
    expect(url.searchParams.get("details")).toBe(event.description);
    expect(url.searchParams.get("location")).toBe(event.location);
  });

  test("omits empty details and location", () => {
    const url = new URL(googleCalendarUrl({ ...event, description: "", location: "" }));
    expect(url.searchParams.has("details")).toBe(false);
    expect(url.searchParams.has("location")).toBe(false);
  });
});

describe("buildIcs", () => {
  test("emits a valid VEVENT with UTC times, 4h duration, CRLF endings", () => {
    const ics = buildIcs(event);
    expect(ics).toContain("BEGIN:VCALENDAR\r\n");
    expect(ics).toContain("UID:event-11111111-2222-3333-4444-555555555555@cubeplanner.pl\r\n");
    expect(ics).toContain("DTSTART:20260815T160000Z\r\n");
    expect(ics).toContain("DTEND:20260815T200000Z\r\n");
    expect(ics.endsWith("END:VCALENDAR\r\n")).toBe(true);
    expect(ics.includes("\n") && !ics.includes("\r\n")).toBe(false); // CRLF only
  });

  test("escapes RFC 5545 TEXT characters", () => {
    const ics = buildIcs(event);
    expect(ics).toContain("SUMMARY:Summer Draft\\; Finals");
    expect(ics).toContain("DESCRIPTION:Bring snacks\\, and sleeves.\\nDoors open early.");
    expect(ics).toContain("LOCATION:Community Hall\\, Kraków");
  });

  test("folds lines longer than 75 octets with a leading space", () => {
    const ics = buildIcs({ ...event, description: "x".repeat(200) });
    const folded = ics.split("\r\n").filter((l) => l.startsWith(" "));
    expect(folded.length).toBeGreaterThan(0);
    for (const line of ics.split("\r\n")) {
      expect(new TextEncoder().encode(line).length).toBeLessThanOrEqual(75);
    }
  });

  test("omits DESCRIPTION and LOCATION when empty", () => {
    const ics = buildIcs({ ...event, description: "", location: "" });
    expect(ics).not.toContain("DESCRIPTION:");
    expect(ics).not.toContain("LOCATION:");
  });
});

test("icsFilename slugifies", () => {
  expect(icsFilename("Summer Draft; Finals")).toBe("summer-draft-finals.ics");
});
```

- [ ] **Step 4: Run to verify failure** — `pnpm --filter @cube-planner/frontend test calendar` → FAIL (module not found).

- [ ] **Step 5: Implement** `frontend/src/features/events/lib/calendar.ts`:

```ts
// Client-side "add to calendar" builders (issue #13). Events are auth-gated,
// so subscription URLs are impossible — a Google template link and a
// downloaded .ics are the two platform-blessed flows. Events store no end
// time; a fixed 4h block was adjudicated in the 2026-08-04 spec.
export type CalendarEvent = {
  id: string;
  name: string;
  description: string;
  location: string;
  startsAt: string;
};

export const EVENT_DURATION_HOURS = 4;

// 2026-08-15T18:00:00+02:00 → 20260815T160000Z
function utcStamp(d: Date): string {
  return d.toISOString().replace(/[-:]/g, "").replace(/\.\d{3}Z$/, "Z");
}

function eventTimes(e: CalendarEvent): { start: string; end: string } {
  const start = new Date(e.startsAt);
  const end = new Date(start.getTime() + EVENT_DURATION_HOURS * 60 * 60 * 1000);
  return { start: utcStamp(start), end: utcStamp(end) };
}

export function googleCalendarUrl(e: CalendarEvent): string {
  const { start, end } = eventTimes(e);
  const params = new URLSearchParams({ action: "TEMPLATE", text: e.name, dates: `${start}/${end}` });
  if (e.description !== "") params.set("details", e.description);
  if (e.location !== "") params.set("location", e.location);
  return `https://calendar.google.com/calendar/render?${params.toString()}`;
}

// RFC 5545 §3.3.11 TEXT escaping — backslash first.
function escapeIcsText(s: string): string {
  return s
    .replaceAll("\\", "\\\\")
    .replaceAll(";", "\\;")
    .replaceAll(",", "\\,")
    .replace(/\r?\n/g, "\\n");
}

const encoder = new TextEncoder();

// RFC 5545 §3.1: lines fold at 75 octets; continuations start with a space.
function foldIcsLine(line: string): string {
  if (encoder.encode(line).length <= 75) return line;
  const parts: string[] = [];
  let current = "";
  let currentBytes = 0;
  for (const ch of line) {
    const chBytes = encoder.encode(ch).length;
    const limit = parts.length === 0 ? 75 : 74; // continuations lose one octet to the space
    if (currentBytes + chBytes > limit) {
      parts.push(current);
      current = "";
      currentBytes = 0;
    }
    current += ch;
    currentBytes += chBytes;
  }
  parts.push(current);
  return parts.join("\r\n ");
}

export function buildIcs(e: CalendarEvent): string {
  const { start, end } = eventTimes(e);
  const lines = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//Cube Planner//cubeplanner.pl//EN",
    "BEGIN:VEVENT",
    `UID:event-${e.id}@cubeplanner.pl`,
    `DTSTAMP:${start}`,
    `DTSTART:${start}`,
    `DTEND:${end}`,
    `SUMMARY:${escapeIcsText(e.name)}`,
  ];
  if (e.description !== "") lines.push(`DESCRIPTION:${escapeIcsText(e.description)}`);
  if (e.location !== "") lines.push(`LOCATION:${escapeIcsText(e.location)}`);
  lines.push("END:VEVENT", "END:VCALENDAR");
  return lines.map(foldIcsLine).join("\r\n") + "\r\n";
}

export function icsFilename(name: string): string {
  const slug =
    name
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-+|-+$/g, "") || "event";
  return `${slug}.ics`;
}
```

- [ ] **Step 6: Run tests** — `pnpm --filter @cube-planner/frontend test calendar` → PASS. Then `make frontend-test`.

- [ ] **Step 7: Commit** — `git add frontend && git commit -m "feat(events): calendar link + ics builders, shared download util"`

### Task 6: "Add to calendar" UI on the event detail page

**Files:**
- Modify: `frontend/src/features/events/components/EventDetailPage.tsx` (after the `<dl>` block, lines 84-103)
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/events/components/EventDetailPage.test.tsx` (extend if it exists — check first — else create)

**Interfaces:**
- Consumes: `googleCalendarUrl`, `buildIcs`, `icsFilename` from `../lib/calendar`; `downloadTextFile` from `@/shared/lib/download` (Task 5).

- [ ] **Step 1: Add messages.** `en.json`:

```json
"event_add_to_calendar": "Add to calendar",
"event_calendar_google": "Google Calendar",
"event_calendar_ics": "Apple Calendar (.ics)",
```

`pl.json`:

```json
"event_add_to_calendar": "Dodaj do kalendarza",
"event_calendar_google": "Kalendarz Google",
"event_calendar_ics": "Kalendarz Apple (.ics)",
```

(Place next to the existing `event_*` keys; run `pnpm gen` if types don't pick up.)

- [ ] **Step 2: Write the failing test.** Check for an existing `EventDetailPage` test file and follow its fetch-stub/router setup; if none exists, create one following `WantlistPage.test.tsx`'s pattern (memory router + `QueryClientProvider`, `vi.stubGlobal("fetch", ...)` switching on URL — `/api/events/` returns the fixture below, everything else 401/`{}`). Cases:

```ts
const eventFixture = {
  id: "e1", name: "Draft Night", description: "Casual draft", location: "Hall",
  startsAt: "2026-08-15T16:00:00Z", status: "published", feeCents: 0, currency: "pln",
  maxParticipants: 8, organizerName: "Org", attendees: [], cubes: [],
  paidCount: 0, pendingCount: 0, waitlistCount: 0,
};

test("published event shows both calendar buttons with a Google template link", async () => {
  renderEventPage(eventFixture);
  const google = await screen.findByRole("link", { name: "Google Calendar" });
  expect(google).toHaveAttribute("target", "_blank");
  expect(google.getAttribute("href")).toContain("calendar.google.com/calendar/render?action=TEMPLATE");
  expect(screen.getByRole("button", { name: "Apple Calendar (.ics)" })).toBeInTheDocument();
});

test("cancelled event hides the calendar row", async () => {
  renderEventPage({ ...eventFixture, status: "cancelled" });
  await screen.findByText("Draft Night");
  expect(screen.queryByText("Add to calendar")).not.toBeInTheDocument();
});
```

(`renderEventPage` = the file's render helper; write it if creating the file.)

- [ ] **Step 3: Run to verify failure** — `pnpm --filter @cube-planner/frontend test EventDetailPage` → FAIL (buttons absent).

- [ ] **Step 4: Implement.** In `EventDetailPage.tsx`, directly after the closing `</dl>` (line 103), add:

```tsx
      {e.status !== "cancelled" && e.status !== "finished" && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm text-fg-muted">{m.event_add_to_calendar()}</span>
          <Button asChild variant="outline" size="sm">
            <a href={googleCalendarUrl(e)} target="_blank" rel="noopener noreferrer">
              {m.event_calendar_google()}
            </a>
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => downloadTextFile(icsFilename(e.name), buildIcs(e), "text/calendar;charset=utf-8")}
          >
            {m.event_calendar_ics()}
          </Button>
        </div>
      )}
```

Imports: `buildIcs, googleCalendarUrl, icsFilename` from `../lib/calendar`; `downloadTextFile` from `@/shared/lib/download`; `Button` from `@/shared/ui/button` if not already imported.

- [ ] **Step 5: Run tests** — `pnpm --filter @cube-planner/frontend test EventDetailPage`, then `make frontend-test` → PASS.

- [ ] **Step 6: Commit, push, PR**

```bash
git add frontend
git commit -m "feat(events): add-to-calendar buttons (Google link + ics download)"
git push -u origin feature/event-add-to-calendar
gh pr create --title "Events: add to calendar (Google + .ics)" --body "Closes #13. ..."
```

Wait for CI + merge before PR 4.

---

## PR 4 — #8: card preview sheet (branch `feature/card-preview-sheet`)

### Task 7: `useMediaQuery` hook

**Files:**
- Create: `frontend/src/shared/lib/useMediaQuery.ts`
- Test: `frontend/src/shared/lib/useMediaQuery.test.ts`

**Interfaces:**
- Produces: `useMediaQuery(query: string): boolean` — reactive `matchMedia` subscription. Task 8 consumes it.

- [ ] **Step 1: Create the branch** (`git checkout master && git pull && git checkout -b feature/card-preview-sheet`)

- [ ] **Step 2: Write the failing test** — `useMediaQuery.test.ts`:

```tsx
import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { useMediaQuery } from "./useMediaQuery";

afterEach(() => vi.unstubAllGlobals());

function stubMatchMedia(initialMatches: boolean) {
  let matches = initialMatches;
  const listeners = new Set<() => void>();
  vi.stubGlobal("matchMedia", (query: string) => ({
    get matches() {
      return matches;
    },
    media: query,
    addEventListener: (_: string, cb: () => void) => listeners.add(cb),
    removeEventListener: (_: string, cb: () => void) => listeners.delete(cb),
  }));
  return (next: boolean) => {
    matches = next;
    listeners.forEach((cb) => cb());
  };
}

test("reflects the current match and reacts to changes", () => {
  const setMatches = stubMatchMedia(false);
  const { result } = renderHook(() => useMediaQuery("(min-width: 768px)"));
  expect(result.current).toBe(false);
  act(() => setMatches(true));
  expect(result.current).toBe(true);
});
```

- [ ] **Step 3: Run to verify failure** — `pnpm --filter @cube-planner/frontend test useMediaQuery` → FAIL (module not found).

- [ ] **Step 4: Implement** `frontend/src/shared/lib/useMediaQuery.ts`:

```ts
import { useCallback, useSyncExternalStore } from "react";

// Reactive matchMedia. NOTE: jsdom does not implement matchMedia — tests
// rendering consumers under `// @vitest-environment jsdom` must stub it
// (happy-dom provides a non-matching default).
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onChange: () => void) => {
      const mql = matchMedia(query);
      mql.addEventListener("change", onChange);
      return () => mql.removeEventListener("change", onChange);
    },
    [query],
  );
  return useSyncExternalStore(subscribe, () => matchMedia(query).matches);
}
```

- [ ] **Step 5: Run tests** — PASS. **Commit:** `git add frontend && git commit -m "feat(shared): useMediaQuery hook"`

### Task 8: `CardPreviewSheet` component

**Files:**
- Create: `frontend/src/shared/cards/CardPreviewSheet.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/shared/cards/CardPreviewSheet.test.tsx`, `frontend/src/shared/cards/CardPreviewSheet.a11y.test.tsx`

**Interfaces:**
- Consumes: `useMediaQuery` (Task 7), `useCardPrintings` from `./api`, `Dialog`/`Drawer` from `@/shared/ui`, `ManaCost` from `./ManaCost`.
- Produces (Tasks 9–10 rely on these exact names):

```tsx
export type PreviewCard = { oracleId: string; scryfallId?: string; name: string };
export function CardPreviewSheet(props: {
  card: PreviewCard;
  onClose: () => void;
  onChangePrinting?: (card: PreviewCard) => void; // renders the button only when provided
}): JSX.Element;
```

Rendered by parents only while open (`{previewCard && <CardPreviewSheet ... />}`), like `PrintingPickerDialog`.

- [ ] **Step 1: Add messages.** `en.json`: `"cards_preview_back_face": "Back face of {name}",` — pl.json: `"cards_preview_back_face": "Rewers karty {name}",`. (Reuses existing `m.cards_set_line`, `m.cards_printings_count`, `m.cards_change_printing`, `m.loading`.)

- [ ] **Step 2: Write the failing tests** — `CardPreviewSheet.test.tsx` (happy-dom default; `matchMedia` exists there and reports non-matching → Drawer shell; that's fine for behavior tests):

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { CardPreviewSheet } from "./CardPreviewSheet";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const printings = [
  {
    scryfallId: "s1", oracleId: "o1", name: "Valki, God of Lies",
    manaCost: "{1}{B}", typeLine: "Legendary Creature — God",
    oracleText: "Valki does things.", setName: "Kaldheim", setCode: "khm",
    collectorNumber: "114", rarity: "mythic", releasedAt: "2021-02-05",
    cmc: 2, colors: ["B", "R"], colorIdentity: ["B", "R"], promo: false,
    imageSmall: "https://img/s1-small.jpg", imageNormal: "https://img/s1.jpg",
    backImageNormal: "https://img/s1-back.jpg",
  },
  { scryfallId: "s2", oracleId: "o1", name: "Valki, God of Lies", manaCost: "{1}{B}",
    typeLine: "Legendary Creature — God", oracleText: "Valki does things.",
    setName: "Kaldheim Promos", setCode: "pkhm", collectorNumber: "114p", rarity: "mythic",
    releasedAt: "2021-02-05", cmc: 2, colors: ["B", "R"], colorIdentity: ["B", "R"],
    promo: true, imageSmall: null, imageNormal: "https://img/s2.jpg", backImageNormal: null },
];

function stubPrintingsFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ printings }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

test("shows the row's printing with details, both faces, and printing count", async () => {
  stubPrintingsFetch();
  render(
    <CardPreviewSheet card={{ oracleId: "o1", scryfallId: "s1", name: "Valki, God of Lies" }} onClose={() => {}} />,
    { wrapper },
  );
  expect(await screen.findByText("Valki does things.")).toBeInTheDocument();
  expect(screen.getByRole("img", { name: "Valki, God of Lies" })).toHaveAttribute("src", "https://img/s1.jpg");
  expect(screen.getByRole("img", { name: "Back face of Valki, God of Lies" })).toHaveAttribute("src", "https://img/s1-back.jpg");
  expect(screen.getByText("Kaldheim · #114")).toBeInTheDocument();
});

test("without a scryfallId falls back to the first printing", async () => {
  stubPrintingsFetch();
  render(<CardPreviewSheet card={{ oracleId: "o1", name: "Valki, God of Lies" }} onClose={() => {}} />, { wrapper });
  expect(await screen.findByText("Kaldheim · #114")).toBeInTheDocument();
});

test("change-printing button renders only when a handler is provided", async () => {
  stubPrintingsFetch();
  const onChangePrinting = vi.fn();
  const card = { oracleId: "o1", scryfallId: "s1", name: "Valki, God of Lies" };
  render(<CardPreviewSheet card={card} onClose={() => {}} onChangePrinting={onChangePrinting} />, { wrapper });
  await userEvent.click(await screen.findByRole("button", { name: "Change printing" }));
  expect(onChangePrinting).toHaveBeenCalledWith(card);
});

test("no change-printing button without a handler; close calls onClose", async () => {
  stubPrintingsFetch();
  const onClose = vi.fn();
  render(
    <CardPreviewSheet card={{ oracleId: "o1", scryfallId: "s1", name: "Valki, God of Lies" }} onClose={onClose} />,
    { wrapper },
  );
  await screen.findByText("Valki does things.");
  expect(screen.queryByRole("button", { name: "Change printing" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalled();
});
```

(Verify the close button's accessible name — it's `m.dialog_close()`; if the English string differs from "Close", use the real string.)

- [ ] **Step 3: Run to verify failure** — `pnpm --filter @cube-planner/frontend test CardPreviewSheet` → FAIL (module not found).

- [ ] **Step 4: Implement** `frontend/src/shared/cards/CardPreviewSheet.tsx`:

```tsx
import { m } from "@/paraglide/messages";
import { useMediaQuery } from "@/shared/lib/useMediaQuery";
import { Alert } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import { Drawer } from "@/shared/ui/drawer";
import { useCardPrintings } from "./api";
import { ManaCost } from "./ManaCost";

export type PreviewCard = { oracleId: string; scryfallId?: string; name: string };

// Full card inspector, reachable by tap/Enter on card rows (issue #8).
// Desktop gets a centered dialog, phones a bottom sheet. Data comes from the
// printings query — same key PrintingPickerDialog uses, so it's cached
// across open/close cycles and across the two components.
export function CardPreviewSheet({
  card,
  onClose,
  onChangePrinting,
}: {
  card: PreviewCard;
  onClose: () => void;
  onChangePrinting?: (card: PreviewCard) => void;
}) {
  const isDesktop = useMediaQuery("(min-width: 768px)");
  const printings = useCardPrintings(card.oracleId);
  const shown =
    printings.data?.find((p) => p.scryfallId === card.scryfallId) ?? printings.data?.[0];

  const body = (
    <>
      {printings.isPending && <p className="text-sm text-fg-muted">{m.loading()}</p>}
      {printings.isError && <Alert variant="danger">{printings.error.message}</Alert>}
      {shown && (
        <div className="flex flex-col gap-6 overflow-y-auto md:flex-row">
          <div className="flex shrink-0 flex-col gap-2">
            {shown.imageNormal != null && (
              <img src={shown.imageNormal} alt={shown.name} className="w-64 rounded-xl" />
            )}
            {shown.backImageNormal != null && (
              <img
                src={shown.backImageNormal}
                alt={m.cards_preview_back_face({ name: shown.name })}
                className="w-64 rounded-xl"
              />
            )}
          </div>
          <div className="flex max-w-md flex-col gap-2">
            <p className="text-sm text-fg-muted">
              {shown.typeLine}
              {shown.manaCost !== "" && (
                <>
                  {" · "}
                  <ManaCost cost={shown.manaCost} />
                </>
              )}
            </p>
            {shown.oracleText !== "" && (
              <p className="text-sm whitespace-pre-line text-fg">{shown.oracleText}</p>
            )}
            <p className="text-sm text-fg-muted">
              {m.cards_set_line({ setName: shown.setName, collectorNumber: shown.collectorNumber })}
            </p>
            <p className="text-sm text-fg-muted">
              {m.cards_printings_count({ count: printings.data?.length ?? 0 })}
            </p>
            {onChangePrinting && (
              <div>
                <Button type="button" variant="outline" size="sm" onClick={() => onChangePrinting(card)}>
                  {m.cards_change_printing()}
                </Button>
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );

  return isDesktop ? (
    <Dialog open onClose={onClose} title={card.name}>
      {body}
    </Dialog>
  ) : (
    <Drawer open onClose={onClose} label={card.name} side="bottom">
      {body}
    </Drawer>
  );
}
```

- [ ] **Step 5: Run tests** — `pnpm --filter @cube-planner/frontend test CardPreviewSheet` → PASS.

- [ ] **Step 6: axe test for both shells** — `CardPreviewSheet.a11y.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test, vi } from "vitest";
import { axe } from "vitest-axe";
import { CardPreviewSheet } from "./CardPreviewSheet";

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function stubEnv(desktop: boolean) {
  // jsdom has no matchMedia at all — stub it for each shell.
  vi.stubGlobal("matchMedia", () => ({
    matches: desktop,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          printings: [
            { scryfallId: "s1", oracleId: "o1", name: "Sol Ring", manaCost: "{1}",
              typeLine: "Artifact", oracleText: "Add {C}{C}.", setName: "Test", setCode: "tst",
              collectorNumber: "1", rarity: "uncommon", releasedAt: "2020-01-01", cmc: 1,
              colors: [], colorIdentity: [], promo: false, imageSmall: null,
              imageNormal: "https://img/s1.jpg", backImageNormal: null },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    ),
  );
}

for (const [shell, desktop] of [["dialog", true], ["drawer", false]] as const) {
  test(`${shell} shell has no axe violations`, async () => {
    stubEnv(desktop);
    const { container, findByText } = render(
      <CardPreviewSheet card={{ oracleId: "o1", scryfallId: "s1", name: "Sol Ring" }} onClose={() => {}} />,
      { wrapper },
    );
    await findByText("Add {C}{C}.");
    expect(await axe(container)).toHaveNoViolations();
  });
}
```

Run: `pnpm --filter @cube-planner/frontend test CardPreviewSheet` → all PASS.

- [ ] **Step 7: Commit** — `git add frontend && git commit -m "feat(cards): responsive CardPreviewSheet"`

### Task 9: Wire cube view — rows open the sheet, owner gets change-printing

**Files:**
- Modify: `frontend/src/features/cubes/components/GroupedCardList.tsx`
- Modify: `frontend/src/features/cubes/components/CubeDisplayPage.tsx`
- Test: `frontend/src/features/cubes/components/CubeDisplayPage.test.tsx` (extend if exists — check — else create following `WantlistPage.test.tsx`'s memory-router pattern)

**Interfaces:**
- Consumes: `CardPreviewSheet`/`PreviewCard` (Task 8), `useChangeCubePrinting(cubeId)` + `PrintingPickerDialog` (existing), `useMe` from `@/features/auth/api` (existing).
- Produces: `GroupedCardList` gains a required prop `onCardActivate: (card: CubeCardEntry) => void` (CubeDisplayPage is its only consumer).

- [ ] **Step 1: Write the failing tests.** Fetch stub switches on URL: `/api/cubes/<id>` → cube fixture (include `ownerId: "u1"`, `ownerName`, `version: 1`, `cardCount: 1`, `visibility: "public"`, `name`, `description: ""`), `/cards` → `{ version: 1, cards: [cardEntry] }` with `cardEntry = { scryfallId: "s1", oracleId: "o1", name: "Sol Ring", manaCost: "{1}", typeLine: "Artifact", cmc: 1, colors: [], colorIdentity: [], rarity: "uncommon", imageSmall: null, imageNormal: "https://img/s1.jpg", quantity: 1 }`, `/printings` → the Task 8-style printings body, `/api/auth/me`-style user endpoint → `{ id: "u1", displayName: "Owner", email: "o@x", providers: null }` or 401 depending on the case (grep `useMe` in `features/auth/api.ts` for the real path). Cases:

```ts
test("activating a card row opens the preview sheet", async () => {
  renderCubePage({ me: owner });
  await userEvent.click(await screen.findByRole("button", { name: /Sol Ring/ }));
  expect(await screen.findByText("Add {C}{C}.")).toBeInTheDocument();
});

test("owner sees change printing; picking fires the mutation", async () => {
  renderCubePage({ me: owner });
  await userEvent.click(await screen.findByRole("button", { name: /Sol Ring/ }));
  await userEvent.click(await screen.findByRole("button", { name: "Change printing" }));
  // PrintingPickerDialog opens on top; non-current row calls the mutation.
  // Assert the POST to /change-printing happened via the fetch mock's calls.
});

test("non-owner gets an info-only sheet", async () => {
  renderCubePage({ me: null }); // me endpoint → 401
  await userEvent.click(await screen.findByRole("button", { name: /Sol Ring/ }));
  await screen.findByText("Add {C}{C}.");
  expect(screen.queryByRole("button", { name: "Change printing" })).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run to verify failure** — `pnpm --filter @cube-planner/frontend test CubeDisplayPage` → FAIL (row click does nothing).

- [ ] **Step 3: GroupedCardList.** Add the prop and onClick:

```tsx
export function GroupedCardList({
  cards,
  groupKind,
  onCardActivate,
}: {
  cards: CubeCardEntry[];
  groupKind: GroupKind;
  onCardActivate: (card: CubeCardEntry) => void;
}) {
```

and on the row button (GroupedCardList.tsx:30): `onClick={() => onCardActivate(card)}`. The hover preview wrapper stays untouched.

- [ ] **Step 4: CubeDisplayPage.** Add hooks before the early returns (after line 23):

```tsx
  const me = useMe();
  const changePrinting = useChangeCubePrinting(cubeId);
  const [previewCard, setPreviewCard] = useState<CubeCardEntry | null>(null);
  const [pickerCard, setPickerCard] = useState<PreviewCard | null>(null);
```

After the early returns: `const isOwner = me.data?.id === cube.data.ownerId;`

Change line 102 and append the overlays before the closing `</div>`:

```tsx
      {cards.data && (
        <GroupedCardList cards={cards.data.cards} groupKind={groupKind} onCardActivate={setPreviewCard} />
      )}
      {previewCard && (
        <CardPreviewSheet
          card={previewCard}
          onClose={() => setPreviewCard(null)}
          onChangePrinting={isOwner && !viewingPast ? setPickerCard : undefined}
        />
      )}
      {pickerCard && pickerCard.scryfallId !== undefined && (
        <PrintingPickerDialog
          open
          onClose={() => setPickerCard(null)}
          oracleId={pickerCard.oracleId}
          name={pickerCard.name}
          currentScryfallId={pickerCard.scryfallId}
          onPick={(newScryfallId) => {
            changePrinting.mutate({ oracleId: pickerCard.oracleId, newScryfallId });
            setPickerCard(null);
            // Keep the sheet showing the freshly picked printing (the cubes
            // query invalidation refetches the list in the background).
            setPreviewCard((prev) => (prev ? { ...prev, scryfallId: newScryfallId } : prev));
          }}
        />
      )}
```

Imports: `useMe` from `@/features/auth/api`; `useChangeCubePrinting` from `../api`; `CardPreviewSheet`, type `PreviewCard` from `@/shared/cards/CardPreviewSheet`; `PrintingPickerDialog` from `@/shared/cards/PrintingPickerDialog`.

- [ ] **Step 5: Run tests** — `pnpm --filter @cube-planner/frontend test CubeDisplayPage`, then `make frontend-test` (the `GroupedCardList` prop is required — TypeScript surfaces any other call site).

- [ ] **Step 6: Commit** — `git add frontend && git commit -m "feat(cubes): card rows open preview sheet; owner change-printing on view page"`

### Task 10: Wire collection + wantlist rows

**Files:**
- Modify: `frontend/src/features/collection/components/CollectionPage.tsx:136-146`
- Modify: `frontend/src/features/collection/components/WantlistPage.tsx:78-82`
- Test: extend the existing test files for both pages

**Interfaces:**
- Consumes: `CardPreviewSheet`/`PreviewCard` (Task 8). Info-only — no `onChangePrinting` here (adjudicated).

- [ ] **Step 1: Write the failing tests.** In each page's existing test file, add (adapting to its render helper; stub the `/printings` fetch route as in Task 8):

```ts
test("clicking a card name opens the info-only preview sheet", async () => {
  renderPage();
  await userEvent.click(await screen.findByRole("button", { name: /Sol Ring/ }));
  expect(await screen.findByText("Add {C}{C}.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Change printing" })).not.toBeInTheDocument();
});
```

(CollectionPage note: its rows already have adjacent "Change printing" action buttons from the collection flow — if that string collides, scope the `queryByRole` to `within` the dialog element.)

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: CollectionPage.** Add state `const [previewItem, setPreviewItem] = useState<PreviewCard | null>(null);`. Replace the row's inner `<span className="flex flex-col">` (:137) with a button:

```tsx
<CardHoverPreview card={item}>
  <button
    type="button"
    onClick={() => setPreviewItem({ oracleId: item.oracleId, scryfallId: item.scryfallId, name: item.name })}
    className="flex w-full flex-col rounded text-left hover:bg-surface-raised focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
  >
    <span className="truncate text-sm text-fg">{item.name}</span>
    <span className="text-xs text-fg-muted">
      {m.cards_set_line({ setName: item.setName, collectorNumber: item.collectorNumber })}
    </span>
  </button>
</CardHoverPreview>
```

and render near the page's existing PrintingPickerDialog block:

```tsx
{previewItem && <CardPreviewSheet card={previewItem} onClose={() => setPreviewItem(null)} />}
```

- [ ] **Step 4: WantlistPage.** Same pattern: state + wrap the name in a button inside the `<td>`:

```tsx
<td className="py-1.5">
  <CardHoverPreview card={item}>
    <button
      type="button"
      onClick={() => setPreviewItem({ oracleId: item.oracleId, scryfallId: item.scryfallId, name: item.name })}
      className="rounded text-left text-fg hover:bg-surface-raised focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
    >
      {item.name}
    </button>
  </CardHoverPreview>
</td>
```

If the wantlist item type has no `scryfallId` (check the schema type the page maps over), omit it — `PreviewCard.scryfallId` is optional and the sheet falls back to the newest printing. If the row `<td>` had a `jsx-a11y/control-has-associated-label` eslint-disable that the real button now satisfies, remove the disable and confirm oxlint passes.

- [ ] **Step 5: Run tests** — both page test files, then `make frontend-test` → PASS.

- [ ] **Step 6: Full verification + commit, push, PR**

```bash
make test
git add frontend
git commit -m "feat(collection): card rows open preview sheet"
git push -u origin feature/card-preview-sheet
gh pr create --title "Touch-reachable card previews (responsive sheet)" --body "Closes #8. ..."
```
