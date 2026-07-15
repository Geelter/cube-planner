# Tournament Engine (Sub-project 5b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Swiss tournament engine for started events — roster snapshot from paid registrations, round pairing with organizer draft/swap/publish, player-reported Bo3 results with organizer override, drops, and MTR-standard standings — per `docs/superpowers/specs/2026-07-15-tournament-engine-design.md`.

**Architecture:** Migration 00008 adds `tournaments`, `tournament_players`, `rounds`, `matches`. New backend package `internal/tournaments` with a pure sub-package `internal/tournaments/swiss` (`Pair`, `ComputeStandings` — plain structs in, plain structs out, deterministic per seed). The service wraps the pure core with sqlc queries and the round state machine (`draft → published → completed`, one round in flight), reading events only through `db.Queries` (no import of `internal/events`). huma endpoints in `platform/httpapi/tournaments.go`; standings are computed on every read, never stored. Frontend: new `features/tournaments` slice composed into the event routes at route level (player `TournamentSection`, organizer `TournamentPanel`).

**Tech Stack:** Go + huma v2 + chi + sqlc (pgx/v5) + goose; React + TanStack Router/Query + Tailwind v4 + Paraglide; vitest + RTL + vitest-axe; testcontainers-go.

## Global Constraints

- Go module path: `github.com/mjabloniec/cube-planner/backend`. Frontend alias `@/` = `frontend/src/`.
- Format/lint: `gofumpt -w` on touched Go files; `pnpm --filter @cube-planner/frontend fmt` (oxfmt) and `lint` (oxlint). Never eslint/prettier. lefthook runs these pre-commit — if a commit fails on formatting, run the formatter and retry the commit.
- Backend tests: `cd backend && go test ./...` (integration tests need Docker running). Frontend: `pnpm --filter @cube-planner/frontend test`, typecheck via `pnpm --filter @cube-planner/frontend typecheck`.
- Every user-facing string is `m.key()` from `@/paraglide/messages`; `frontend/messages/en.json` and `frontend/messages/pl.json` must carry identical key sets. After editing messages run `pnpm --filter @cube-planner/frontend gen` if tests complain about missing message functions.
- Semantic color tokens only (`bg-surface`, `bg-surface-raised`, `text-fg`, `text-fg-muted`, `border-border`, `bg-accent`, `text-danger`, …). No raw palette classes.
- Generated artifacts: `frontend/src/shared/api/` regenerated ONLY via `make api-generate` and committed; `src/routeTree.gen.ts` + `src/paraglide/` are gitignored build output (`pnpm gen` recreates). `backend/internal/db/*.sql.go` regenerated via `cd backend && sqlc generate` and committed. Never hand-edit any of them.
- Organizer = `users.role = 'admin'` (single-organizer model). Organizer endpoints use the existing `requireAdmin(ctx, deps)` from `httpapi/events.go` → 403 problem type `admin-required`.
- Tournament domain invariants (spec §3): tournament exists only on a `started` event; roster snapshot of `paid` registrations is final; one round in flight (`draft → published → completed`); match points 3/1/0; bye = 2-0 win worth 3 match points, excluded from ALL tiebreak math (not an opponent for OMW%/OGW%, bye games excluded from recipient's own GW%); MW%/GW% floored at 1/3 when used as an opponent metric; standings sort MP → OMW% → GW% → OGW%, competition ranking (1,2,2,4), ties ordered by display name; results editable by a match's players while the round is `published`, by the organizer until the NEXT round is paired; drops effective at next pairing; undrop rejected once a round was created after `dropped_at`; event `finish` refuses while any round is not `completed`.
- Bo3 result validity: `p1Games`, `p2Games` ∈ 0..2, `draws` ∈ 0..3, `p1Games + p2Games + draws ≤ 3`, not both players at 2. Byes reject result writes (`bye-immutable`).
- Standings percentages travel on the wire ×100 (`omwPercent` 33.3, not 0.333).
- Commit messages: conventional prefixes (`feat(tournaments): …`), imperative mood, trailer:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Existing httpapi test helpers (package `httpapi_test`, shared across files): `loggedInClient(t, srv, q, email)`, `newCookieClient(t, srv)`, `c.do(t, method, path, body)`, `decode[T](t, resp)`, `makeAdmin(t, pool, email)`, `noopMailer{}`. `testdb.New(t)` gives a migrated testcontainers pool. Reuse them; add tournament-specific seeding helpers in the new test file only.
- No test files in `src/routes/` (they'd need a `-` prefix) — frontend tests live in `features/`.
- `features/tournaments` may import `features/auth` (`useMe`) — the same exemption `features/events` already uses. It must NOT import `features/events`; where it needs the event status it queries `GET /api/events/{eventId}` through the shared generated client under the SAME queryKey `["events", "detail", eventId]` the events feature uses (identical endpoint + shape → TanStack dedupes, and events-feature invalidations refresh it).

---

### Task 1: Migration 00008 + tournament queries + sqlc generate

**Files:**
- Create: `backend/migrations/00008_tournaments.sql`
- Create: `backend/internal/db/queries/tournaments.sql`
- Generated: `cd backend && sqlc generate` (new models + query methods in `backend/internal/db/`)

**Interfaces:**
- Produces: tables exactly as below; sqlc structs `db.Tournament`, `db.TournamentPlayer`, `db.Round`, `db.Match` (nullable columns are pointers: `*int32`, `*time.Time`, `*uuid.UUID`); query methods named below — every later backend task builds on them.

- [ ] **Step 1: Write the migration** `backend/migrations/00008_tournaments.sql`:

```sql
-- +goose Up
create table tournaments (
    id uuid primary key default gen_random_uuid(),
    event_id uuid not null unique references events (id) on delete cascade,
    planned_rounds int not null check (planned_rounds >= 1),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table tournament_players (
    id uuid primary key default gen_random_uuid(),
    tournament_id uuid not null references tournaments (id) on delete cascade,
    user_id uuid not null references users (id),
    dropped_at timestamptz,
    created_at timestamptz not null default now(),
    unique (tournament_id, user_id)
);

create index tournament_players_tournament_idx
    on tournament_players (tournament_id);

create table rounds (
    id uuid primary key default gen_random_uuid(),
    tournament_id uuid not null references tournaments (id) on delete cascade,
    number int not null check (number >= 1),
    status text not null default 'draft'
        check (status in ('draft', 'published', 'completed')),
    -- Pairing seed: re-roll stores a new seed; pairing is deterministic
    -- given (roster, history, seed).
    seed bigint not null,
    published_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (tournament_id, number)
);

create index rounds_tournament_idx on rounds (tournament_id);

create table matches (
    id uuid primary key default gen_random_uuid(),
    round_id uuid not null references rounds (id) on delete cascade,
    table_number int not null check (table_number >= 1),
    player1_id uuid not null references tournament_players (id),
    -- Null player2 = bye. Byes are inserted with an auto 2-0 result so
    -- standings math has a single input shape.
    player2_id uuid references tournament_players (id),
    p1_games int check (p1_games between 0 and 2),
    p2_games int check (p2_games between 0 and 2),
    draws int check (draws between 0 and 3),
    -- Result columns are all null (unreported) or all non-null; the
    -- service enforces that plus p1+p2+draws <= 3 and not both at 2.
    reported_by uuid references users (id),
    reported_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (round_id, table_number),
    check (player2_id is null or player1_id <> player2_id)
);

create index matches_round_idx on matches (round_id);

-- +goose Down
drop table matches;
drop table rounds;
drop table tournament_players;
drop table tournaments;
```

- [ ] **Step 2: Write the queries** `backend/internal/db/queries/tournaments.sql`:

```sql
-- name: CreateTournament :one
insert into tournaments (event_id, planned_rounds)
values (sqlc.arg(event_id), sqlc.arg(planned_rounds))
returning *;

-- name: GetTournamentByEvent :one
select * from tournaments where event_id = sqlc.arg(event_id);

-- Mutations lock the tournament row: one round in flight, no racing
-- pair/publish/complete/report.
-- name: GetTournamentByEventForUpdate :one
select * from tournaments where event_id = sqlc.arg(event_id) for update;

-- name: UpdatePlannedRounds :one
update tournaments set planned_rounds = sqlc.arg(planned_rounds),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- Roster snapshot source (spec §3.1): paid registrations only.
-- name: ListPaidRegistrationUsers :many
select r.user_id, u.display_name
from registrations r
join users u on u.id = r.user_id
where r.event_id = sqlc.arg(event_id) and r.status = 'paid'
order by u.display_name;

-- name: InsertTournamentPlayer :one
insert into tournament_players (tournament_id, user_id)
values (sqlc.arg(tournament_id), sqlc.arg(user_id))
returning *;

-- name: ListTournamentPlayers :many
select tp.id, tp.user_id, tp.dropped_at, u.display_name
from tournament_players tp
join users u on u.id = tp.user_id
where tp.tournament_id = sqlc.arg(tournament_id)
order by u.display_name;

-- name: GetTournamentPlayer :one
select tp.id, tp.tournament_id, tp.user_id, tp.dropped_at, u.display_name
from tournament_players tp
join users u on u.id = tp.user_id
where tp.id = sqlc.arg(id);

-- name: SetPlayerDropped :one
update tournament_players set dropped_at = sqlc.narg(dropped_at)
where id = sqlc.arg(id)
returning *;

-- name: CreateRound :one
insert into rounds (tournament_id, number, seed)
values (sqlc.arg(tournament_id), sqlc.arg(number), sqlc.arg(seed))
returning *;

-- name: ListRounds :many
select * from rounds
where tournament_id = sqlc.arg(tournament_id)
order by number;

-- name: GetRoundByNumber :one
select * from rounds
where tournament_id = sqlc.arg(tournament_id)
    and number = sqlc.arg(number);

-- name: SetRoundSeed :one
update rounds set seed = sqlc.arg(seed), updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: SetRoundStatus :one
update rounds set status = sqlc.arg(status),
    published_at = coalesce(rounds.published_at, sqlc.narg(published_at)),
    completed_at = coalesce(rounds.completed_at, sqlc.narg(completed_at)),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteMatchesForRound :exec
delete from matches where round_id = sqlc.arg(round_id);

-- name: InsertMatch :one
insert into matches (round_id, table_number, player1_id, player2_id,
    p1_games, p2_games, draws, reported_at)
values (sqlc.arg(round_id), sqlc.arg(table_number), sqlc.arg(player1_id),
    sqlc.narg(player2_id), sqlc.narg(p1_games), sqlc.narg(p2_games),
    sqlc.narg(draws), sqlc.narg(reported_at))
returning *;

-- name: ListMatchesForRound :many
select * from matches
where round_id = sqlc.arg(round_id)
order by table_number;

-- name: ListMatchesForTournament :many
select m.*, r.number as round_number, r.status as round_status
from matches m
join rounds r on r.id = m.round_id
where r.tournament_id = sqlc.arg(tournament_id)
order by r.number, m.table_number;

-- name: GetMatch :one
select m.*, r.number as round_number, r.status as round_status,
    r.tournament_id
from matches m
join rounds r on r.id = m.round_id
where m.id = sqlc.arg(id);

-- name: UpdateMatchResult :one
update matches set p1_games = sqlc.arg(p1_games),
    p2_games = sqlc.arg(p2_games), draws = sqlc.arg(draws),
    reported_by = sqlc.narg(reported_by), reported_at = now(),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: SetMatchPlayers :one
update matches set player1_id = sqlc.arg(player1_id),
    player2_id = sqlc.narg(player2_id), updated_at = now()
where id = sqlc.arg(id)
returning *;

-- Finish guard (spec §3.2): events.Transition("finish") refuses while
-- any round is not completed.
-- name: CountOpenRoundsForEvent :one
select count(*) from rounds r
join tournaments t on t.id = r.tournament_id
where t.event_id = sqlc.arg(event_id) and r.status <> 'completed';
```

- [ ] **Step 3: Generate and build**

Run: `cd backend && sqlc generate && go build ./...`
Expected: builds clean; `internal/db/tournaments.sql.go` and updated `internal/db/models.go` appear.

- [ ] **Step 4: Run backend tests (regression only)**

Run: `cd backend && go test ./...`
Expected: PASS (nothing consumes the new tables yet).

- [ ] **Step 5: Commit**

```bash
git add backend/migrations/00008_tournaments.sql backend/internal/db
git commit -m "feat(tournaments): migration 00008 and sqlc queries for swiss engine"
```

---

### Task 2: Pure swiss package — types + ComputeStandings

**Files:**
- Create: `backend/internal/tournaments/swiss/swiss.go`
- Create: `backend/internal/tournaments/swiss/standings.go`
- Test: `backend/internal/tournaments/swiss/standings_test.go`

**Interfaces:**
- Produces (used by Task 3, 4, 6):
  - `swiss.Player{ID uuid.UUID; DisplayName string; Dropped bool}`
  - `swiss.Result{P1Games, P2Games, Draws int}`
  - `swiss.Match{Player1 uuid.UUID; Player2 *uuid.UUID; Result *Result}` (`Player2 == nil` = bye, `Result == nil` = unreported)
  - `swiss.Pairing{TableNumber int; Player1 uuid.UUID; Player2 *uuid.UUID}` (defined here, produced by Task 3's `Pair`)
  - `swiss.Standing{Rank int; PlayerID uuid.UUID; DisplayName string; Dropped bool; MatchPoints int; OMWPercent, GWPercent, OGWPercent float64}` — percentages ×100
  - `func ComputeStandings(players []Player, matches []Match) []Standing`

- [ ] **Step 1: Write the shared types** `backend/internal/tournaments/swiss/swiss.go`:

```go
// Package swiss implements pure swiss-pairing and standings math.
// No I/O: plain structs in, plain structs out; Pair is deterministic
// for a given seed.
package swiss

import "github.com/google/uuid"

// Player is a roster entry as the engine sees it.
type Player struct {
	ID          uuid.UUID
	DisplayName string
	Dropped     bool
}

// Result is a reported Bo3 score (games won per player, plus drawn games).
type Result struct {
	P1Games int
	P2Games int
	Draws   int
}

// Match is one pairing from any round. Player2 == nil is a bye;
// Result == nil is an unreported match (ignored by standings).
type Match struct {
	Player1 uuid.UUID
	Player2 *uuid.UUID
	Result  *Result
}

// Pairing is one table of a newly paired round. Player2 == nil is the bye.
type Pairing struct {
	TableNumber int
	Player1     uuid.UUID
	Player2     *uuid.UUID
}
```

- [ ] **Step 2: Write the failing standings tests** `backend/internal/tournaments/swiss/standings_test.go`:

```go
package swiss

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

// ids[i] gives stable test identities; names sort a < b < c < ...
func testPlayers(n int) ([]Player, []uuid.UUID) {
	ids := make([]uuid.UUID, n)
	players := make([]Player, n)
	for i := range n {
		ids[i] = uuid.New()
		players[i] = Player{ID: ids[i], DisplayName: string(rune('a' + i))}
	}
	return players, ids
}

func res(p1, p2, d int) *Result { return &Result{P1Games: p1, P2Games: p2, Draws: d} }

func vs(p1, p2 uuid.UUID, r *Result) Match { return Match{Player1: p1, Player2: &p2, Result: r} }

func bye(p uuid.UUID) Match { return Match{Player1: p, Result: res(2, 0, 0)} }

func within(t *testing.T, got, want float64, label string) {
	t.Helper()
	if math.Abs(got-want) > 0.05 {
		t.Errorf("%s = %.3f, want %.3f", label, got, want)
	}
}

func byID(s []Standing, id uuid.UUID) Standing {
	for _, row := range s {
		if row.PlayerID == id {
			return row
		}
	}
	return Standing{}
}

func TestStandingsMatchPoints(t *testing.T) {
	players, ids := testPlayers(4)
	// r1: 0 beats 1 (2-0), 2 draws 3 (1-1-1)
	s := ComputeStandings(players, []Match{
		vs(ids[0], ids[1], res(2, 0, 0)),
		vs(ids[2], ids[3], res(1, 1, 1)),
	})
	if got := byID(s, ids[0]).MatchPoints; got != 3 {
		t.Errorf("winner MP = %d, want 3", got)
	}
	if got := byID(s, ids[1]).MatchPoints; got != 0 {
		t.Errorf("loser MP = %d, want 0", got)
	}
	for _, id := range []uuid.UUID{ids[2], ids[3]} {
		if got := byID(s, id).MatchPoints; got != 1 {
			t.Errorf("draw MP = %d, want 1", got)
		}
	}
	if s[0].PlayerID != ids[0] || s[0].Rank != 1 {
		t.Errorf("rank 1 = %v (rank %d), want player 0", s[0].PlayerID, s[0].Rank)
	}
}

func TestStandingsUnreportedIgnored(t *testing.T) {
	players, ids := testPlayers(2)
	s := ComputeStandings(players, []Match{vs(ids[0], ids[1], nil)})
	for _, row := range s {
		if row.MatchPoints != 0 {
			t.Errorf("unreported match granted points: %+v", row)
		}
	}
}

// MTR fixture, hand-computed. 4 players, 2 rounds:
//   r1: A beats B 2-1, C beats D 2-0
//   r2: A beats C 2-1, B beats D 2-1
// A: 6 MP. B: 3, C: 3, D: 0.
// MW%: A=1.0, B=.5, C=.5, D=0→floor 1/3.
// A OMW% = avg(B .5, C .5) = 50.0
// B OMW% = avg(A 1.0, D 1/3) = 66.7 → B ranks above C
// C OMW% = avg(D 1/3, A 1.0) = 66.7 — equal; game tiebreaks decide:
// B GW%: games 3+3=6, points 3*(1+2)=9 → wait: B won 1 game r1, 2 games r2
//   → gamePoints 9, games 6 → 50.0
// C GW%: r1 2 wins of 2, r2 1 win of 3 → points 9, games 5 → 60.0 → C above B
func TestStandingsTiebreakerChain(t *testing.T) {
	players, ids := testPlayers(4)
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]
	s := ComputeStandings(players, []Match{
		vs(a, b, res(2, 1, 0)), vs(c, d, res(2, 0, 0)),
		vs(a, c, res(2, 1, 0)), vs(b, d, res(2, 1, 0)),
	})
	within(t, byID(s, a).OMWPercent, 50.0, "A OMW%")
	within(t, byID(s, b).OMWPercent, 66.7, "B OMW%")
	within(t, byID(s, c).OMWPercent, 66.7, "C OMW%")
	within(t, byID(s, b).GWPercent, 50.0, "B GW%")
	within(t, byID(s, c).GWPercent, 60.0, "C GW%")
	if s[0].PlayerID != a || s[1].PlayerID != c || s[2].PlayerID != b || s[3].PlayerID != d {
		t.Errorf("order = %v, want A C B D", []uuid.UUID{s[0].PlayerID, s[1].PlayerID, s[2].PlayerID, s[3].PlayerID})
	}
	if s[0].Rank != 1 || s[1].Rank != 2 || s[2].Rank != 3 || s[3].Rank != 4 {
		t.Errorf("ranks = %d %d %d %d, want 1 2 3 4", s[0].Rank, s[1].Rank, s[2].Rank, s[3].Rank)
	}
}

// Byes: 3 MP; included in own MW% denominator (MTR: a bye is an awarded
// win) but the bye is not an opponent and its games don't count in GW%.
func TestStandingsByeExclusion(t *testing.T) {
	players, ids := testPlayers(3)
	a, b, c := ids[0], ids[1], ids[2]
	s := ComputeStandings(players, []Match{
		vs(a, b, res(2, 0, 0)), bye(c),
	})
	rc := byID(s, c)
	if rc.MatchPoints != 3 {
		t.Errorf("bye MP = %d, want 3", rc.MatchPoints)
	}
	// c played nobody: OMW% 0, GW% 0 (no real games).
	within(t, rc.OMWPercent, 0, "C OMW% (bye only)")
	within(t, rc.GWPercent, 0, "C GW% (bye only)")
	// a's OMW% = b's floored MW% = 33.3; b's OMW% = a's 100.
	within(t, byID(s, a).OMWPercent, 33.3, "A OMW%")
	within(t, byID(s, b).OMWPercent, 100, "B OMW%")
}

// The 1/3 floor: an 0-2 opponent contributes 33.3, not 0.
func TestStandingsFloor(t *testing.T) {
	players, ids := testPlayers(4)
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]
	s := ComputeStandings(players, []Match{
		vs(a, d, res(2, 0, 0)), vs(b, c, res(2, 0, 0)),
		vs(a, b, res(2, 0, 0)), vs(c, d, res(2, 0, 0)),
	})
	// d lost both: raw MW% 0 → floored 1/3 in a's OMW%.
	within(t, byID(s, a).OMWPercent, (100.0/3+50)/2, "A OMW% with floored D")
}

// Ties on every key share a rank; order within the tie is by name.
func TestStandingsSharedRank(t *testing.T) {
	players, ids := testPlayers(4)
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]
	// Two independent identical results: a beats b, c beats d — a/c and
	// b/d are symmetric on all keys.
	s := ComputeStandings(players, []Match{
		vs(a, b, res(2, 0, 0)), vs(c, d, res(2, 0, 0)),
	})
	if s[0].Rank != 1 || s[1].Rank != 1 || s[2].Rank != 3 || s[3].Rank != 3 {
		t.Errorf("ranks = %d %d %d %d, want 1 1 3 3", s[0].Rank, s[1].Rank, s[2].Rank, s[3].Rank)
	}
	if s[0].DisplayName != "a" || s[1].DisplayName != "c" {
		t.Errorf("tie order = %s, %s; want a, c", s[0].DisplayName, s[1].DisplayName)
	}
}

// Dropped players stay ranked (flagged) and feed opponents' tiebreaks.
func TestStandingsDropped(t *testing.T) {
	players, ids := testPlayers(2)
	players[1].Dropped = true
	s := ComputeStandings(players, []Match{vs(ids[0], ids[1], res(0, 2, 0))})
	top := s[0]
	if top.PlayerID != ids[1] || !top.Dropped {
		t.Errorf("dropped winner should lead standings flagged, got %+v", top)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd backend && go test ./internal/tournaments/swiss/`
Expected: FAIL — `undefined: ComputeStandings`, `undefined: Standing`.

- [ ] **Step 4: Implement** `backend/internal/tournaments/swiss/standings.go`:

```go
package swiss

import (
	"sort"

	"github.com/google/uuid"
)

// Standing is one computed standings row. Percentages are ×100
// (33.3, not 0.333), matching the wire format.
type Standing struct {
	Rank        int
	PlayerID    uuid.UUID
	DisplayName string
	Dropped     bool
	MatchPoints int
	OMWPercent  float64
	GWPercent   float64
	OGWPercent  float64
}

const tiebreakFloor = 1.0 / 3.0

// tally accumulates one player's reported results.
type tally struct {
	matchPoints int
	matches     int // reported matches, byes included
	gamePoints  int // 3 per game win, 1 per drawn game; byes excluded
	games       int // games played, byes excluded
	opponents   []uuid.UUID
}

// ComputeStandings ranks players per MTR: match points, then OMW%, GW%,
// OGW%. A bye is an awarded 2-0 win (3 MP, counts in own MW%) but is
// excluded from tiebreaks otherwise: not an opponent, and its games do
// not enter GW%. Opponents' MW%/GW% are floored at 1/3. Dropped players
// stay ranked and keep feeding opponents' tiebreaks. Ties on all keys
// share a rank (competition ranking) ordered by display name.
func ComputeStandings(players []Player, matches []Match) []Standing {
	tallies := make(map[uuid.UUID]*tally, len(players))
	for _, p := range players {
		tallies[p.ID] = &tally{}
	}
	for _, m := range matches {
		if m.Result == nil {
			continue
		}
		r := *m.Result
		t1 := tallies[m.Player1]
		if m.Player2 == nil {
			if t1 != nil { // bye: match points only
				t1.matchPoints += 3
				t1.matches++
			}
			continue
		}
		t2 := tallies[*m.Player2]
		if t1 == nil || t2 == nil {
			continue
		}
		t1.matches++
		t2.matches++
		t1.opponents = append(t1.opponents, *m.Player2)
		t2.opponents = append(t2.opponents, m.Player1)
		switch {
		case r.P1Games > r.P2Games:
			t1.matchPoints += 3
		case r.P2Games > r.P1Games:
			t2.matchPoints += 3
		default:
			t1.matchPoints++
			t2.matchPoints++
		}
		games := r.P1Games + r.P2Games + r.Draws
		t1.games += games
		t2.games += games
		t1.gamePoints += 3*r.P1Games + r.Draws
		t2.gamePoints += 3*r.P2Games + r.Draws
	}

	// Opponent metrics (floored per MTR).
	mwFloored := func(t *tally) float64 {
		if t.matches == 0 {
			return tiebreakFloor
		}
		return max(float64(t.matchPoints)/float64(3*t.matches), tiebreakFloor)
	}
	gwRaw := func(t *tally) float64 {
		if t.games == 0 {
			return 0
		}
		return float64(t.gamePoints) / float64(3*t.games)
	}
	gwFloored := func(t *tally) float64 { return max(gwRaw(t), tiebreakFloor) }

	rows := make([]Standing, len(players))
	for i, p := range players {
		t := tallies[p.ID]
		var omw, ogw float64
		for _, opp := range t.opponents {
			omw += mwFloored(tallies[opp])
			ogw += gwFloored(tallies[opp])
		}
		if n := len(t.opponents); n > 0 {
			omw /= float64(n)
			ogw /= float64(n)
		}
		rows[i] = Standing{
			PlayerID: p.ID, DisplayName: p.DisplayName, Dropped: p.Dropped,
			MatchPoints: t.matchPoints,
			OMWPercent:  omw * 100, GWPercent: gwRaw(t) * 100, OGWPercent: ogw * 100,
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.MatchPoints != b.MatchPoints {
			return a.MatchPoints > b.MatchPoints
		}
		if a.OMWPercent != b.OMWPercent {
			return a.OMWPercent > b.OMWPercent
		}
		if a.GWPercent != b.GWPercent {
			return a.GWPercent > b.GWPercent
		}
		if a.OGWPercent != b.OGWPercent {
			return a.OGWPercent > b.OGWPercent
		}
		return a.DisplayName < b.DisplayName
	})
	const eps = 1e-9
	for i := range rows {
		if i > 0 && rows[i].MatchPoints == rows[i-1].MatchPoints &&
			absDiff(rows[i].OMWPercent, rows[i-1].OMWPercent) < eps &&
			absDiff(rows[i].GWPercent, rows[i-1].GWPercent) < eps &&
			absDiff(rows[i].OGWPercent, rows[i-1].OGWPercent) < eps {
			rows[i].Rank = rows[i-1].Rank
		} else {
			rows[i].Rank = i + 1
		}
	}
	return rows
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/tournaments/swiss/ -v`
Expected: PASS (all `TestStandings*`).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/tournaments
git commit -m "feat(tournaments): pure swiss standings with MTR tiebreakers"
```

---

### Task 3: Pure swiss package — Pair

**Files:**
- Create: `backend/internal/tournaments/swiss/pair.go`
- Test: `backend/internal/tournaments/swiss/pair_test.go`

**Interfaces:**
- Consumes: `Player`, `Match`, `Pairing`, `Result` from Task 2.
- Produces: `func Pair(players []Player, history []Match, seed int64) []Pairing` — active players each appear exactly once; bye (if any) is the last pairing with `Player2 == nil`; table numbers 1..n; deterministic per seed. Never returns an error: when rematches are unavoidable it minimizes them.

- [ ] **Step 1: Write the failing tests** `backend/internal/tournaments/swiss/pair_test.go`:

```go
package swiss

import (
	"testing"

	"github.com/google/uuid"
)

func matchPointsOf(history []Match) map[uuid.UUID]int {
	pts := map[uuid.UUID]int{}
	for _, m := range history {
		if m.Result == nil {
			continue
		}
		if m.Player2 == nil {
			pts[m.Player1] += 3
			continue
		}
		switch {
		case m.Result.P1Games > m.Result.P2Games:
			pts[m.Player1] += 3
		case m.Result.P2Games > m.Result.P1Games:
			pts[*m.Player2] += 3
		default:
			pts[m.Player1]++
			pts[*m.Player2]++
		}
	}
	return pts
}

// every active player exactly once; dropped players absent; tables 1..n.
func assertValidPairings(t *testing.T, players []Player, got []Pairing) {
	t.Helper()
	seen := map[uuid.UUID]bool{}
	for i, p := range got {
		if p.TableNumber != i+1 {
			t.Errorf("table %d at index %d", p.TableNumber, i)
		}
		for _, id := range pairingIDs(p) {
			if seen[id] {
				t.Errorf("player %v paired twice", id)
			}
			seen[id] = true
		}
	}
	for _, pl := range players {
		if pl.Dropped && seen[pl.ID] {
			t.Errorf("dropped player %v paired", pl.ID)
		}
		if !pl.Dropped && !seen[pl.ID] {
			t.Errorf("active player %v unpaired", pl.ID)
		}
	}
}

func pairingIDs(p Pairing) []uuid.UUID {
	ids := []uuid.UUID{p.Player1}
	if p.Player2 != nil {
		ids = append(ids, *p.Player2)
	}
	return ids
}

func TestPairRoundOne(t *testing.T) {
	players, _ := testPlayers(8)
	got := Pair(players, nil, 42)
	if len(got) != 4 {
		t.Fatalf("pairings = %d, want 4", len(got))
	}
	assertValidPairings(t, players, got)
}

func TestPairDeterministicPerSeed(t *testing.T) {
	players, _ := testPlayers(8)
	a := Pair(players, nil, 7)
	b := Pair(players, nil, 7)
	for i := range a {
		if a[i].Player1 != b[i].Player1 {
			t.Fatalf("same seed produced different pairings")
		}
	}
}

func TestPairOddCountAssignsOneBye(t *testing.T) {
	players, _ := testPlayers(7)
	got := Pair(players, nil, 1)
	assertValidPairings(t, players, got)
	last := got[len(got)-1]
	if last.Player2 != nil {
		t.Fatalf("last pairing should be the bye")
	}
	for _, p := range got[:len(got)-1] {
		if p.Player2 == nil {
			t.Errorf("extra bye at table %d", p.TableNumber)
		}
	}
}

func TestPairByeGoesToLowestGroupWithoutPriorBye(t *testing.T) {
	players, ids := testPlayers(5)
	// r1: 0 beats 1, 2 beats 3, bye 4. Losers (1,3) are the low group;
	// 4 has 3 MP and a prior bye.
	history := []Match{
		vs(ids[0], ids[1], res(2, 0, 0)),
		vs(ids[2], ids[3], res(2, 0, 0)),
		bye(ids[4]),
	}
	for seed := int64(0); seed < 20; seed++ {
		got := Pair(players, history, seed)
		byeP := got[len(got)-1]
		if byeP.Player2 != nil {
			t.Fatalf("seed %d: no bye", seed)
		}
		if byeP.Player1 != ids[1] && byeP.Player1 != ids[3] {
			t.Errorf("seed %d: bye to %v, want a 0-point player", seed, byeP.Player1)
		}
	}
}

func TestPairGroupsByPoints(t *testing.T) {
	players, ids := testPlayers(8)
	// r1 winners: 0,2,4,6.
	history := []Match{
		vs(ids[0], ids[1], res(2, 0, 0)), vs(ids[2], ids[3], res(2, 0, 0)),
		vs(ids[4], ids[5], res(2, 0, 0)), vs(ids[6], ids[7], res(2, 0, 0)),
	}
	pts := matchPointsOf(history)
	got := Pair(players, history, 3)
	assertValidPairings(t, players, got)
	for _, p := range got {
		if pts[p.Player1] != pts[*p.Player2] {
			t.Errorf("cross-group pairing %v(%d) vs %v(%d) with even groups",
				p.Player1, pts[p.Player1], *p.Player2, pts[*p.Player2])
		}
	}
}

func TestPairAvoidsRematches(t *testing.T) {
	players, ids := testPlayers(4)
	history := []Match{
		vs(ids[0], ids[1], res(2, 0, 0)), vs(ids[2], ids[3], res(2, 0, 0)),
	}
	played := map[[2]uuid.UUID]bool{
		{ids[0], ids[1]}: true, {ids[1], ids[0]}: true,
		{ids[2], ids[3]}: true, {ids[3], ids[2]}: true,
	}
	for seed := int64(0); seed < 20; seed++ {
		for _, p := range Pair(players, history, seed) {
			if played[[2]uuid.UUID{p.Player1, *p.Player2}] {
				t.Errorf("seed %d: rematch %v vs %v", seed, p.Player1, *p.Player2)
			}
		}
	}
}

// 2 players, already played: a rematch is unavoidable — Pair must still
// return a full pairing rather than fail.
func TestPairMinimalRematchFallback(t *testing.T) {
	players, ids := testPlayers(2)
	history := []Match{vs(ids[0], ids[1], res(2, 0, 0))}
	got := Pair(players, history, 5)
	if len(got) != 1 || got[0].Player2 == nil {
		t.Fatalf("want the single forced rematch, got %+v", got)
	}
}

func TestPairExcludesDropped(t *testing.T) {
	players, _ := testPlayers(6)
	players[2].Dropped = true
	got := Pair(players, nil, 9)
	assertValidPairings(t, players, got)
	// 5 actives → 2 tables + bye.
	if len(got) != 3 {
		t.Fatalf("pairings = %d, want 3", len(got))
	}
}

func TestPairNoRepeatByeWidensUpward(t *testing.T) {
	players, ids := testPlayers(3)
	// Everyone at 0 except: 0 had the bye already (3 MP).
	history := []Match{bye(ids[0]), vs(ids[1], ids[2], res(1, 1, 1))}
	for seed := int64(0); seed < 20; seed++ {
		got := Pair(players, history, seed)
		byeP := got[len(got)-1]
		if byeP.Player1 == ids[0] {
			t.Errorf("seed %d: repeat bye", seed)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/tournaments/swiss/`
Expected: FAIL — `undefined: Pair`.

- [ ] **Step 3: Implement** `backend/internal/tournaments/swiss/pair.go`:

```go
package swiss

import (
	"math/rand"
	"sort"

	"github.com/google/uuid"
)

// Pair generates the next round: seeded shuffle within match-point
// groups, pair-down for odd groups, backtracking to avoid rematches
// (falling back to the minimum number of rematches when unavoidable),
// and — for an odd player count — a bye from the lowest score group
// among players without a prior bye. Deterministic per seed.
func Pair(players []Player, history []Match, seed int64) []Pairing {
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducible pairing, not crypto

	points := map[uuid.UUID]int{}
	hadBye := map[uuid.UUID]bool{}
	played := map[uuid.UUID]map[uuid.UUID]bool{}
	for _, m := range history {
		if m.Player2 == nil {
			hadBye[m.Player1] = true
		} else {
			if played[m.Player1] == nil {
				played[m.Player1] = map[uuid.UUID]bool{}
			}
			if played[*m.Player2] == nil {
				played[*m.Player2] = map[uuid.UUID]bool{}
			}
			played[m.Player1][*m.Player2] = true
			played[*m.Player2][m.Player1] = true
		}
		if m.Result == nil {
			continue
		}
		switch {
		case m.Player2 == nil, m.Result.P1Games > m.Result.P2Games:
			points[m.Player1] += 3
		case m.Result.P2Games > m.Result.P1Games:
			points[*m.Player2] += 3
		default:
			points[m.Player1]++
			points[*m.Player2]++
		}
	}

	var active []Player
	for _, p := range players {
		if !p.Dropped {
			active = append(active, p)
		}
	}

	var byeID *uuid.UUID
	if len(active)%2 == 1 {
		id := pickBye(active, points, hadBye, rng)
		byeID = &id
	}

	// Order: points desc, seeded-random within a group. Sorting by a
	// per-player random key implements the within-group shuffle.
	rnd := make(map[uuid.UUID]float64, len(active))
	var order []uuid.UUID
	for _, p := range active {
		if byeID != nil && p.ID == *byeID {
			continue
		}
		rnd[p.ID] = rng.Float64()
		order = append(order, p.ID)
	}
	sort.Slice(order, func(i, j int) bool {
		if points[order[i]] != points[order[j]] {
			return points[order[i]] > points[order[j]]
		}
		return rnd[order[i]] < rnd[order[j]]
	})

	// Backtracking with a growing rematch budget: budget 0 finds a
	// rematch-free pairing when one exists; otherwise the smallest
	// budget that works = the minimum number of rematches.
	var pairs [][2]uuid.UUID
	for budget := 0; ; budget++ {
		if pairs = tryPair(order, played, budget); pairs != nil {
			break
		}
	}

	out := make([]Pairing, 0, len(pairs)+1)
	for i, pr := range pairs {
		p2 := pr[1]
		out = append(out, Pairing{TableNumber: i + 1, Player1: pr[0], Player2: &p2})
	}
	if byeID != nil {
		out = append(out, Pairing{TableNumber: len(pairs) + 1, Player1: *byeID})
	}
	return out
}

// pickBye draws from the lowest match-point group among players without
// a prior bye, widening upward group by group; if every active player
// has had a bye the constraint is waived (lowest group).
func pickBye(active []Player, points map[uuid.UUID]int, hadBye map[uuid.UUID]bool, rng *rand.Rand) uuid.UUID {
	groups := map[int][]uuid.UUID{}
	var keys []int
	for _, p := range active {
		pts := points[p.ID]
		if len(groups[pts]) == 0 {
			keys = append(keys, pts)
		}
		groups[pts] = append(groups[pts], p.ID)
	}
	sort.Ints(keys)
	for _, k := range keys {
		var fresh []uuid.UUID
		for _, id := range groups[k] {
			if !hadBye[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) > 0 {
			return fresh[rng.Intn(len(fresh))]
		}
	}
	lowest := groups[keys[0]]
	return lowest[rng.Intn(len(lowest))]
}

// tryPair pairs order[0] with the nearest available opponent (same
// score group first, then pairing down), recursing over the rest;
// each rematch consumes budget. Returns nil when no pairing fits.
func tryPair(order []uuid.UUID, played map[uuid.UUID]map[uuid.UUID]bool, budget int) [][2]uuid.UUID {
	if len(order) == 0 {
		return [][2]uuid.UUID{}
	}
	p1 := order[0]
	for i := 1; i < len(order); i++ {
		p2 := order[i]
		cost := 0
		if played[p1][p2] {
			cost = 1
		}
		if cost > budget {
			continue
		}
		rest := make([]uuid.UUID, 0, len(order)-2)
		rest = append(rest, order[1:i]...)
		rest = append(rest, order[i+1:]...)
		if sub := tryPair(rest, played, budget-cost); sub != nil {
			return append([][2]uuid.UUID{{p1, p2}}, sub...)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/tournaments/swiss/ -v`
Expected: PASS (all `TestPair*` and `TestStandings*`).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/tournaments/swiss
git commit -m "feat(tournaments): seeded swiss pairing with rematch avoidance"
```

---

### Task 4: Tournaments service (state machine)

**Files:**
- Create: `backend/internal/tournaments/service.go`
- Test: `backend/internal/tournaments/service_test.go` (testcontainers)

**Interfaces:**
- Consumes: `swiss.Pair`, `swiss.ComputeStandings`, sqlc methods from Task 1, `testdb.New(t)`.
- Produces (used by Task 6's handlers):
  - `tournaments.NewService(queries *db.Queries, pool *pgxpool.Pool, log *slog.Logger) *Service`
  - Error vars: `ErrEventNotFound, ErrEventNotStarted, ErrTournamentNotFound, ErrNoPlayers, ErrPlannedRoundsTooLow, ErrAllRoundsPaired, ErrTooFewPlayers, ErrRoundNotFound, ErrRoundExists, ErrRoundNotDraft, ErrRoundIncomplete, ErrMatchNotFound, ErrNotInMatch, ErrResultLocked, ErrResultInvalid, ErrByeImmutable, ErrPlayerNotFound, ErrNotYourPlayer, ErrAlreadyDropped, ErrNotDropped, ErrUndropTooLate, ErrSwapInvalid`
  - `type Result struct{ P1Games, P2Games, Draws int32 }`
  - `type SlotRef struct{ MatchID uuid.UUID; Slot int32 }` (Slot 1|2)
  - Methods: `Get(ctx, eventID uuid.UUID, admin bool) (*Detail, error)`; `Upsert(ctx, eventID uuid.UUID, plannedRounds *int32) error`; `PairNextRound(ctx, eventID uuid.UUID) error`; `Reroll(ctx, eventID uuid.UUID, number int32) error`; `Swap(ctx, eventID uuid.UUID, number int32, a, b SlotRef) error`; `Publish(ctx, eventID uuid.UUID, number int32) error`; `Complete(ctx, eventID uuid.UUID, number int32) error`; `ReportResult(ctx, eventID, matchID, callerID uuid.UUID, admin bool, r Result) error`; `Drop(ctx, eventID, playerID, callerID uuid.UUID, admin bool) error`; `Undrop(ctx, eventID, playerID, callerID uuid.UUID, admin bool) error`
  - `type Detail struct{ EventID uuid.UUID; PlannedRounds int32; Players []PlayerDetail; Rounds []RoundDetail; Standings []swiss.Standing }`; `PlayerDetail{ID, UserID uuid.UUID; DisplayName string; Dropped bool}`; `RoundDetail{Number int32; Status string; Matches []MatchDetail}`; `MatchDetail{ID uuid.UUID; TableNumber int32; Player1ID uuid.UUID; Player2ID *uuid.UUID; P1Games, P2Games, Draws *int32; ReportedAt *time.Time}`

**Spec anchors (§3):** creation lazily snapshots `paid` registrations; one round in flight; results by players while `published`, by admin until the next round is paired (completed-latest included, draft never); byes auto-2-0 at publish, immutable; drops effective at next pairing; undrop rejected once a round was created after `dropped_at`.

- [ ] **Step 1: Write the service** `backend/internal/tournaments/service.go`:

```go
// Package tournaments owns the swiss tournament state machine on top of
// the pure swiss core: roster snapshot, round lifecycle (draft →
// published → completed, one in flight), results, drops. Standings are
// computed on read, never stored. It reads events only through
// db.Queries — no dependency on internal/events.
package tournaments

import (
	"context"
	"errors"
	"log/slog"
	"math/bits"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/tournaments/swiss"
)

var (
	// ErrEventNotFound mirrors events' convention: drafts read as absent.
	ErrEventNotFound       = errors.New("event not found")
	ErrEventNotStarted     = errors.New("event not started")
	ErrTournamentNotFound  = errors.New("tournament not found")
	ErrNoPlayers           = errors.New("no paid players to seed the tournament")
	ErrPlannedRoundsTooLow = errors.New("planned rounds below rounds already paired")
	ErrAllRoundsPaired     = errors.New("all planned rounds already paired")
	ErrTooFewPlayers       = errors.New("fewer than two active players")
	ErrRoundNotFound       = errors.New("round not found")
	ErrRoundExists         = errors.New("a round is already in progress")
	ErrRoundNotDraft       = errors.New("round is not a draft")
	ErrRoundIncomplete     = errors.New("round has unreported matches")
	ErrMatchNotFound       = errors.New("match not found")
	ErrNotInMatch          = errors.New("caller is not in this match")
	ErrResultLocked        = errors.New("result can no longer be changed")
	ErrResultInvalid       = errors.New("invalid result")
	ErrByeImmutable        = errors.New("bye results are fixed")
	ErrPlayerNotFound      = errors.New("player not found")
	ErrNotYourPlayer       = errors.New("cannot act for another player")
	ErrAlreadyDropped      = errors.New("player already dropped")
	ErrNotDropped          = errors.New("player is not dropped")
	ErrUndropTooLate       = errors.New("a round was paired after the drop")
	ErrSwapInvalid         = errors.New("invalid swap")
)

type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
	log     *slog.Logger
	now     func() time.Time
	newSeed func() int64
}

func NewService(queries *db.Queries, pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{
		queries: queries, pool: pool, log: log, now: time.Now,
		newSeed: rand.Int63, //nolint:gosec // pairing seed, not crypto
	}
}

func (s *Service) withTx(ctx context.Context, fn func(qtx *db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- read side ----

type PlayerDetail struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	DisplayName string
	Dropped     bool
}

type MatchDetail struct {
	ID          uuid.UUID
	TableNumber int32
	Player1ID   uuid.UUID
	Player2ID   *uuid.UUID
	P1Games     *int32
	P2Games     *int32
	Draws       *int32
	ReportedAt  *time.Time
}

type RoundDetail struct {
	Number  int32
	Status  string
	Matches []MatchDetail
}

type Detail struct {
	EventID       uuid.UUID
	PlannedRounds int32
	Players       []PlayerDetail
	Rounds        []RoundDetail
	Standings     []swiss.Standing
}

// Get returns the whole tournament aggregate. Draft rounds are included
// only for admins; standings never include draft matches.
func (s *Service) Get(ctx context.Context, eventID uuid.UUID, admin bool) (*Detail, error) {
	tour, err := s.queries.GetTournamentByEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTournamentNotFound
	}
	if err != nil {
		return nil, err
	}
	players, err := s.queries.ListTournamentPlayers(ctx, tour.ID)
	if err != nil {
		return nil, err
	}
	rounds, err := s.queries.ListRounds(ctx, tour.ID)
	if err != nil {
		return nil, err
	}
	allMatches, err := s.queries.ListMatchesForTournament(ctx, tour.ID)
	if err != nil {
		return nil, err
	}

	d := &Detail{EventID: eventID, PlannedRounds: tour.PlannedRounds}
	swissPlayers := make([]swiss.Player, len(players))
	for i, p := range players {
		d.Players = append(d.Players, PlayerDetail{
			ID: p.ID, UserID: p.UserID, DisplayName: p.DisplayName,
			Dropped: p.DroppedAt != nil,
		})
		swissPlayers[i] = swiss.Player{
			ID: p.ID, DisplayName: p.DisplayName, Dropped: p.DroppedAt != nil,
		}
	}

	byRound := map[uuid.UUID][]MatchDetail{}
	var swissMatches []swiss.Match
	for _, m := range allMatches {
		byRound[m.RoundID] = append(byRound[m.RoundID], MatchDetail{
			ID: m.ID, TableNumber: m.TableNumber, Player1ID: m.Player1ID,
			Player2ID: m.Player2ID, P1Games: m.P1Games, P2Games: m.P2Games,
			Draws: m.Draws, ReportedAt: m.ReportedAt,
		})
		if m.RoundStatus == "draft" {
			continue // draft matches never count toward standings
		}
		sm := swiss.Match{Player1: m.Player1ID, Player2: m.Player2ID}
		if m.P1Games != nil && m.P2Games != nil && m.Draws != nil {
			sm.Result = &swiss.Result{
				P1Games: int(*m.P1Games), P2Games: int(*m.P2Games), Draws: int(*m.Draws),
			}
		}
		swissMatches = append(swissMatches, sm)
	}
	for _, r := range rounds {
		if r.Status == "draft" && !admin {
			continue
		}
		d.Rounds = append(d.Rounds, RoundDetail{
			Number: r.Number, Status: r.Status, Matches: byRound[r.ID],
		})
	}
	d.Standings = swiss.ComputeStandings(swissPlayers, swissMatches)
	return d, nil
}

// ---- shared mutation guards ----

// startedEvent loads the event and requires status 'started'. Draft
// events read as not found (same convention as internal/events).
func startedEvent(ctx context.Context, qtx *db.Queries, eventID uuid.UUID) (db.Event, error) {
	ev, err := qtx.GetEventForUpdate(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Event{}, ErrEventNotFound
	}
	if err != nil {
		return db.Event{}, err
	}
	if ev.Status == "draft" {
		return db.Event{}, ErrEventNotFound
	}
	if ev.Status != "started" {
		return db.Event{}, ErrEventNotStarted
	}
	return ev, nil
}

// lockTournament fetches the tournament under FOR UPDATE, serializing
// all round/result/drop mutations per tournament.
func lockTournament(ctx context.Context, qtx *db.Queries, eventID uuid.UUID) (db.Tournament, error) {
	tour, err := qtx.GetTournamentByEventForUpdate(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Tournament{}, ErrTournamentNotFound
	}
	return tour, err
}

func defaultRounds(playerCount int) int32 {
	if playerCount <= 2 {
		return 1
	}
	return int32(bits.Len(uint(playerCount - 1))) // ceil(log2(n))
}

// createTournament snapshots the paid roster (spec §3.1).
func (s *Service) createTournament(ctx context.Context, qtx *db.Queries, eventID uuid.UUID, plannedRounds *int32) (db.Tournament, error) {
	roster, err := qtx.ListPaidRegistrationUsers(ctx, eventID)
	if err != nil {
		return db.Tournament{}, err
	}
	if len(roster) == 0 {
		return db.Tournament{}, ErrNoPlayers
	}
	rounds := defaultRounds(len(roster))
	if plannedRounds != nil {
		rounds = *plannedRounds
	}
	tour, err := qtx.CreateTournament(ctx, db.CreateTournamentParams{
		EventID: eventID, PlannedRounds: rounds,
	})
	if err != nil {
		return db.Tournament{}, err
	}
	for _, r := range roster {
		if _, err := qtx.InsertTournamentPlayer(ctx, db.InsertTournamentPlayerParams{
			TournamentID: tour.ID, UserID: r.UserID,
		}); err != nil {
			return db.Tournament{}, err
		}
	}
	return tour, nil
}

// Upsert creates the tournament (roster snapshot) or updates
// planned_rounds — never below the rounds already paired.
func (s *Service) Upsert(ctx context.Context, eventID uuid.UUID, plannedRounds *int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if errors.Is(err, ErrTournamentNotFound) {
			_, err = s.createTournament(ctx, qtx, eventID, plannedRounds)
			return err
		}
		if err != nil {
			return err
		}
		if plannedRounds == nil {
			return nil
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		if int(*plannedRounds) < len(rounds) {
			return ErrPlannedRoundsTooLow
		}
		_, err = qtx.UpdatePlannedRounds(ctx, db.UpdatePlannedRoundsParams{
			ID: tour.ID, PlannedRounds: *plannedRounds,
		})
		return err
	})
}

// pairingInputs loads swiss inputs from the DB rows.
func pairingInputs(ctx context.Context, qtx *db.Queries, tournamentID uuid.UUID) ([]swiss.Player, []swiss.Match, error) {
	players, err := qtx.ListTournamentPlayers(ctx, tournamentID)
	if err != nil {
		return nil, nil, err
	}
	sp := make([]swiss.Player, len(players))
	for i, p := range players {
		sp[i] = swiss.Player{ID: p.ID, DisplayName: p.DisplayName, Dropped: p.DroppedAt != nil}
	}
	rows, err := qtx.ListMatchesForTournament(ctx, tournamentID)
	if err != nil {
		return nil, nil, err
	}
	var sm []swiss.Match
	for _, m := range rows {
		if m.RoundStatus == "draft" {
			continue
		}
		mm := swiss.Match{Player1: m.Player1ID, Player2: m.Player2ID}
		if m.P1Games != nil && m.P2Games != nil && m.Draws != nil {
			mm.Result = &swiss.Result{
				P1Games: int(*m.P1Games), P2Games: int(*m.P2Games), Draws: int(*m.Draws),
			}
		}
		sm = append(sm, mm)
	}
	return sp, sm, nil
}

func insertPairings(ctx context.Context, qtx *db.Queries, roundID uuid.UUID, pairings []swiss.Pairing) error {
	for _, p := range pairings {
		if _, err := qtx.InsertMatch(ctx, db.InsertMatchParams{
			RoundID: roundID, TableNumber: int32(p.TableNumber),
			Player1ID: p.Player1, Player2ID: p.Player2,
		}); err != nil {
			return err
		}
	}
	return nil
}

// PairNextRound creates the tournament with defaults when absent (spec:
// "just pair round 1" works in one click), then pairs the next round as
// a draft. Requires every earlier round completed and planned capacity.
func (s *Service) PairNextRound(ctx context.Context, eventID uuid.UUID) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if errors.Is(err, ErrTournamentNotFound) {
			if tour, err = s.createTournament(ctx, qtx, eventID, nil); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		for _, r := range rounds {
			if r.Status != "completed" {
				return ErrRoundExists
			}
		}
		if len(rounds) >= int(tour.PlannedRounds) {
			return ErrAllRoundsPaired
		}
		players, history, err := pairingInputs(ctx, qtx, tour.ID)
		if err != nil {
			return err
		}
		activeCount := 0
		for _, p := range players {
			if !p.Dropped {
				activeCount++
			}
		}
		if activeCount < 2 {
			return ErrTooFewPlayers
		}
		seed := s.newSeed()
		round, err := qtx.CreateRound(ctx, db.CreateRoundParams{
			TournamentID: tour.ID, Number: int32(len(rounds) + 1), Seed: seed,
		})
		if err != nil {
			return err
		}
		return insertPairings(ctx, qtx, round.ID, swiss.Pair(players, history, seed))
	})
}

// draftRound loads round `number` and requires draft status.
func draftRound(ctx context.Context, qtx *db.Queries, tournamentID uuid.UUID, number int32) (db.Round, error) {
	round, err := qtx.GetRoundByNumber(ctx, db.GetRoundByNumberParams{
		TournamentID: tournamentID, Number: number,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Round{}, ErrRoundNotFound
	}
	if err != nil {
		return db.Round{}, err
	}
	if round.Status != "draft" {
		return db.Round{}, ErrRoundNotDraft
	}
	return round, nil
}

// Reroll regenerates the draft round's pairings with a fresh seed.
func (s *Service) Reroll(ctx context.Context, eventID uuid.UUID, number int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := draftRound(ctx, qtx, tour.ID, number)
		if err != nil {
			return err
		}
		if err := qtx.DeleteMatchesForRound(ctx, round.ID); err != nil {
			return err
		}
		players, history, err := pairingInputs(ctx, qtx, tour.ID)
		if err != nil {
			return err
		}
		seed := s.newSeed()
		if _, err := qtx.SetRoundSeed(ctx, db.SetRoundSeedParams{ID: round.ID, Seed: seed}); err != nil {
			return err
		}
		return insertPairings(ctx, qtx, round.ID, swiss.Pair(players, history, seed))
	})
}

type SlotRef struct {
	MatchID uuid.UUID
	Slot    int32 // 1 | 2
}

// slotPlayer reads the player occupying a slot; nil slot-2 (a bye's
// empty side) is not swappable.
func slotPlayer(m db.GetMatchRow, slot int32) (uuid.UUID, error) {
	switch slot {
	case 1:
		return m.Player1ID, nil
	case 2:
		if m.Player2ID == nil {
			return uuid.Nil, ErrSwapInvalid
		}
		return *m.Player2ID, nil
	default:
		return uuid.Nil, ErrSwapInvalid
	}
}

func setSlot(ctx context.Context, qtx *db.Queries, m db.GetMatchRow, slot int32, player uuid.UUID) error {
	p1, p2 := m.Player1ID, m.Player2ID
	if slot == 1 {
		p1 = player
	} else {
		p2 = &player
	}
	if p2 != nil && p1 == *p2 {
		return ErrSwapInvalid
	}
	_, err := qtx.SetMatchPlayers(ctx, db.SetMatchPlayersParams{
		ID: m.ID, Player1ID: p1, Player2ID: p2,
	})
	return err
}

// Swap exchanges the players in two slots of the draft round. The only
// hard rule is no self-pairing; the organizer may knowingly create a
// rematch (spec §3.2).
func (s *Service) Swap(ctx context.Context, eventID uuid.UUID, number int32, a, b SlotRef) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := draftRound(ctx, qtx, tour.ID, number)
		if err != nil {
			return err
		}
		if a.MatchID == b.MatchID && a.Slot == b.Slot {
			return ErrSwapInvalid
		}
		ma, err := qtx.GetMatch(ctx, a.MatchID)
		if err != nil || ma.RoundID != round.ID {
			return ErrSwapInvalid
		}
		mb, err := qtx.GetMatch(ctx, b.MatchID)
		if err != nil || mb.RoundID != round.ID {
			return ErrSwapInvalid
		}
		pa, err := slotPlayer(ma, a.Slot)
		if err != nil {
			return err
		}
		pb, err := slotPlayer(mb, b.Slot)
		if err != nil {
			return err
		}
		if err := setSlot(ctx, qtx, ma, a.Slot, pb); err != nil {
			return err
		}
		if a.MatchID == b.MatchID {
			// Same-match swap: re-read so the second write sees the first.
			ma2, err := qtx.GetMatch(ctx, b.MatchID)
			if err != nil {
				return err
			}
			return setSlot(ctx, qtx, ma2, b.Slot, pa)
		}
		return setSlot(ctx, qtx, mb, b.Slot, pa)
	})
}

// Publish flips draft → published and auto-fills bye results 2-0
// (spec §3.2: "(re)applied on publish").
func (s *Service) Publish(ctx context.Context, eventID uuid.UUID, number int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := draftRound(ctx, qtx, tour.ID, number)
		if err != nil {
			return err
		}
		matches, err := qtx.ListMatchesForRound(ctx, round.ID)
		if err != nil {
			return err
		}
		for _, m := range matches {
			if m.Player2ID == nil {
				if _, err := qtx.UpdateMatchResult(ctx, db.UpdateMatchResultParams{
					ID: m.ID, P1Games: 2, P2Games: 0, Draws: 0, ReportedBy: nil,
				}); err != nil {
					return err
				}
			}
		}
		now := s.now()
		_, err = qtx.SetRoundStatus(ctx, db.SetRoundStatusParams{
			ID: round.ID, Status: "published", PublishedAt: &now,
		})
		return err
	})
}

// Complete requires every match reported.
func (s *Service) Complete(ctx context.Context, eventID uuid.UUID, number int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := qtx.GetRoundByNumber(ctx, db.GetRoundByNumberParams{
			TournamentID: tour.ID, Number: number,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoundNotFound
		}
		if err != nil {
			return err
		}
		if round.Status != "published" {
			return ErrRoundNotDraft
		}
		matches, err := qtx.ListMatchesForRound(ctx, round.ID)
		if err != nil {
			return err
		}
		for _, m := range matches {
			if m.ReportedAt == nil {
				return ErrRoundIncomplete
			}
		}
		now := s.now()
		_, err = qtx.SetRoundStatus(ctx, db.SetRoundStatusParams{
			ID: round.ID, Status: "completed", CompletedAt: &now,
		})
		return err
	})
}

type Result struct {
	P1Games int32
	P2Games int32
	Draws   int32
}

func (r Result) valid() bool {
	return r.P1Games >= 0 && r.P1Games <= 2 &&
		r.P2Games >= 0 && r.P2Games <= 2 &&
		r.Draws >= 0 && r.Draws <= 3 &&
		r.P1Games+r.P2Games+r.Draws <= 3 &&
		!(r.P1Games == 2 && r.P2Games == 2)
}

// ReportResult writes a Bo3 score. Players in the match may report
// while the round is published and latest; the organizer may write
// until the next round is paired (draft rounds never accept results).
func (s *Service) ReportResult(ctx context.Context, eventID, matchID, callerID uuid.UUID, admin bool, r Result) error {
	if !r.valid() {
		return ErrResultInvalid
	}
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		m, err := qtx.GetMatch(ctx, matchID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMatchNotFound
		}
		if err != nil {
			return err
		}
		if m.TournamentID != tour.ID {
			return ErrMatchNotFound
		}
		if m.Player2ID == nil {
			return ErrByeImmutable
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		latest := rounds[len(rounds)-1]
		if m.RoundNumber != latest.Number || m.RoundStatus == "draft" {
			return ErrResultLocked
		}
		if !admin {
			p1, err1 := qtx.GetTournamentPlayer(ctx, m.Player1ID)
			p2, err2 := qtx.GetTournamentPlayer(ctx, *m.Player2ID)
			if err1 != nil || err2 != nil {
				return errors.Join(err1, err2)
			}
			if p1.UserID != callerID && p2.UserID != callerID {
				return ErrNotInMatch
			}
			if m.RoundStatus != "published" {
				return ErrResultLocked
			}
		}
		_, err = qtx.UpdateMatchResult(ctx, db.UpdateMatchResultParams{
			ID: m.ID, P1Games: r.P1Games, P2Games: r.P2Games, Draws: r.Draws,
			ReportedBy: &callerID,
		})
		return err
	})
}

// playerForAction loads the player and enforces self-or-admin.
func playerForAction(ctx context.Context, qtx *db.Queries, tournamentID, playerID, callerID uuid.UUID, admin bool) (db.GetTournamentPlayerRow, error) {
	p, err := qtx.GetTournamentPlayer(ctx, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetTournamentPlayerRow{}, ErrPlayerNotFound
	}
	if err != nil {
		return db.GetTournamentPlayerRow{}, err
	}
	if p.TournamentID != tournamentID {
		return db.GetTournamentPlayerRow{}, ErrPlayerNotFound
	}
	if !admin && p.UserID != callerID {
		return db.GetTournamentPlayerRow{}, ErrNotYourPlayer
	}
	return p, nil
}

func (s *Service) Drop(ctx context.Context, eventID, playerID, callerID uuid.UUID, admin bool) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		p, err := playerForAction(ctx, qtx, tour.ID, playerID, callerID, admin)
		if err != nil {
			return err
		}
		if p.DroppedAt != nil {
			return ErrAlreadyDropped
		}
		now := s.now()
		_, err = qtx.SetPlayerDropped(ctx, db.SetPlayerDroppedParams{ID: playerID, DroppedAt: &now})
		return err
	})
}

// Undrop is rejected once any round was created after the drop
// (spec §3.5): the player already missed a pairing.
func (s *Service) Undrop(ctx context.Context, eventID, playerID, callerID uuid.UUID, admin bool) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		p, err := playerForAction(ctx, qtx, tour.ID, playerID, callerID, admin)
		if err != nil {
			return err
		}
		if p.DroppedAt == nil {
			return ErrNotDropped
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		for _, r := range rounds {
			if r.CreatedAt.After(*p.DroppedAt) {
				return ErrUndropTooLate
			}
		}
		_, err = qtx.SetPlayerDropped(ctx, db.SetPlayerDroppedParams{ID: playerID, DroppedAt: nil})
		return err
	})
}
```

Note: sqlc generates `GetMatchRow` (join row) and `GetTournamentPlayerRow`; if the generated names differ, adapt call sites — never the generated code.

- [ ] **Step 2: Build**

Run: `cd backend && go build ./...`
Expected: builds clean. If sqlc named `UpdateMatchResultParams` fields differently (nullable args are pointers: `ReportedBy *uuid.UUID`), match the generated signature.

- [ ] **Step 3: Write integration tests** `backend/internal/tournaments/service_test.go`:

```go
package tournaments

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

type fixture struct {
	svc     *Service
	q       *db.Queries
	pool    *pgxpool.Pool
	eventID uuid.UUID
	users   []uuid.UUID // paid players, index-stable
}

// newFixture seeds an organizer, a started event, and n paid players.
func newFixture(t *testing.T, n int) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.New(t)
	q := db.New(pool)

	var organizerID uuid.UUID
	err := pool.QueryRow(ctx,
		`insert into users (email, display_name, role, email_verified)
		 values ('org@test', 'org', 'admin', true) returning id`).Scan(&organizerID)
	if err != nil {
		t.Fatal(err)
	}
	var eventID uuid.UUID
	err = pool.QueryRow(ctx,
		`insert into events (organizer_id, name, starts_at, fee_cents,
		    max_participants, status)
		 values ($1, 'tourney', now(), 0, 64, 'started') returning id`,
		organizerID).Scan(&eventID)
	if err != nil {
		t.Fatal(err)
	}
	users := make([]uuid.UUID, n)
	for i := range n {
		err = pool.QueryRow(ctx,
			`insert into users (email, display_name, email_verified)
			 values ($1, $2, true) returning id`,
			fmt.Sprintf("p%d@test", i), fmt.Sprintf("player%02d", i)).Scan(&users[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx,
			`insert into registrations (event_id, user_id, status, paid_at)
			 values ($1, $2, 'paid', now())`, eventID, users[i]); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(q, pool, slog.Default())
	svc.newSeed = func() int64 { return 42 }
	return &fixture{svc: svc, q: q, pool: pool, eventID: eventID, users: users}
}

func (f *fixture) detail(t *testing.T) *Detail {
	t.Helper()
	d, err := f.svc.Get(context.Background(), f.eventID, true)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *fixture) playerByUser(t *testing.T, userID uuid.UUID) PlayerDetail {
	t.Helper()
	for _, p := range f.detail(t).Players {
		if p.UserID == userID {
			return p
		}
	}
	t.Fatalf("player for user %v not found", userID)
	return PlayerDetail{}
}

// reportAll enters 2-0 (admin) for every unreported match of round n.
func (f *fixture) reportAll(t *testing.T, roundNumber int32) {
	t.Helper()
	ctx := context.Background()
	for _, r := range f.detail(t).Rounds {
		if r.Number != roundNumber {
			continue
		}
		for _, m := range r.Matches {
			if m.ReportedAt != nil {
				continue
			}
			if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
				Result{P1Games: 2}); err != nil {
				t.Fatalf("report match %v: %v", m.ID, err)
			}
		}
	}
}

func TestLazyCreationSnapshotsPaidRoster(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if _, err := f.svc.Get(ctx, f.eventID, true); err != ErrTournamentNotFound {
		t.Fatalf("pre-creation Get err = %v, want ErrTournamentNotFound", err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	if len(d.Players) != 4 || d.PlannedRounds != 2 {
		t.Errorf("players=%d planned=%d, want 4 and 2", len(d.Players), d.PlannedRounds)
	}
	// Late registration flip must not change the snapshot.
	if _, err := f.pool.Exec(ctx,
		`update registrations set status='refunded' where user_id=$1`, f.users[0]); err != nil {
		t.Fatal(err)
	}
	if got := len(f.detail(t).Players); got != 4 {
		t.Errorf("roster changed after refund: %d players", got)
	}
}

func TestRoundLifecycleHappyPath(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	// Second pair while draft in flight → round-exists.
	if err := f.svc.PairNextRound(ctx, f.eventID); err != ErrRoundExists {
		t.Fatalf("pair with draft open = %v, want ErrRoundExists", err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.eventID, 1); err != ErrRoundIncomplete {
		t.Fatalf("complete unreported = %v, want ErrRoundIncomplete", err)
	}
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	f2 := f.detail(t)
	if len(f2.Rounds) != 2 || f2.Rounds[1].Status != "draft" {
		t.Fatalf("round 2 missing or not draft: %+v", f2.Rounds)
	}
	// Round 3 would exceed planned 2 (after completing 2).
	if err := f.svc.Publish(ctx, f.eventID, 2); err != nil {
		t.Fatal(err)
	}
	f.reportAll(t, 2)
	if err := f.svc.Complete(ctx, f.eventID, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != ErrAllRoundsPaired {
		t.Fatalf("pair past planned = %v, want ErrAllRoundsPaired", err)
	}
}

func TestOddRosterGetsByeAutoFilledOnPublish(t *testing.T) {
	f := newFixture(t, 5)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	var byeSeen bool
	for _, m := range d.Rounds[0].Matches {
		if m.Player2ID == nil {
			byeSeen = true
			if m.P1Games == nil || *m.P1Games != 2 || m.ReportedAt == nil {
				t.Errorf("bye not auto-filled 2-0: %+v", m)
			}
			if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
				Result{P1Games: 2}); err != ErrByeImmutable {
				t.Errorf("bye report = %v, want ErrByeImmutable", err)
			}
		}
	}
	if !byeSeen {
		t.Error("no bye in a 5-player round")
	}
}

func TestResultPermissions(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	var inMatch, stranger uuid.UUID
	for _, p := range d.Players {
		if p.ID == m.Player1ID {
			inMatch = p.UserID
		}
	}
	for _, u := range f.users {
		hit := false
		for _, p := range d.Players {
			if p.UserID == u && (p.ID == m.Player1ID || (m.Player2ID != nil && p.ID == *m.Player2ID)) {
				hit = true
			}
		}
		if !hit {
			stranger = u
		}
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, stranger, false,
		Result{P1Games: 2}); err != ErrNotInMatch {
		t.Fatalf("stranger report = %v, want ErrNotInMatch", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, inMatch, false,
		Result{P1Games: 2, P2Games: 1}); err != nil {
		t.Fatalf("player report = %v", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, inMatch, false,
		Result{P1Games: 2, P2Games: 2}); err != ErrResultInvalid {
		t.Fatalf("2-2 = %v, want ErrResultInvalid", err)
	}
	// After completing the round, players are locked out; the admin
	// can still override until the next round is paired.
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, inMatch, false,
		Result{P1Games: 2}); err != ErrResultLocked {
		t.Fatalf("player post-complete = %v, want ErrResultLocked", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 0, P2Games: 2}); err != nil {
		t.Fatalf("admin override post-complete = %v", err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 2}); err != ErrResultLocked {
		t.Fatalf("admin after next pair = %v, want ErrResultLocked", err)
	}
}

func TestDropAndUndropTiming(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.Upsert(ctx, f.eventID, nil); err != nil {
		t.Fatal(err)
	}
	victim := f.playerByUser(t, f.users[3])
	other := f.playerByUser(t, f.users[2])
	// Self-drop by someone else's account is rejected.
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[2], false); err != ErrNotYourPlayer {
		t.Fatalf("foreign drop = %v, want ErrNotYourPlayer", err)
	}
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[3], false); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[3], false); err != ErrAlreadyDropped {
		t.Fatalf("double drop = %v, want ErrAlreadyDropped", err)
	}
	// Undrop before any pairing: fine.
	if err := f.svc.Undrop(ctx, f.eventID, victim.ID, f.users[3], false); err != nil {
		t.Fatal(err)
	}
	// Drop again, pair a round → undrop now rejected.
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[3], false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // round.created_at must be after dropped_at
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Undrop(ctx, f.eventID, victim.ID, f.users[3], false); err != ErrUndropTooLate {
		t.Fatalf("late undrop = %v, want ErrUndropTooLate", err)
	}
	// Dropped player is excluded from the paired round.
	for _, m := range f.detail(t).Rounds[0].Matches {
		if m.Player1ID == victim.ID || (m.Player2ID != nil && *m.Player2ID == victim.ID) {
			t.Error("dropped player was paired")
		}
	}
	// Admin can drop anyone.
	if err := f.svc.Drop(ctx, f.eventID, other.ID, f.users[0], true); err != nil {
		t.Fatal(err)
	}
}

func TestPlannedRoundsGuardrails(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	one := int32(1)
	three := int32(3)
	if err := f.svc.Upsert(ctx, f.eventID, &three); err != nil {
		t.Fatalf("raise planned = %v", err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Upsert(ctx, f.eventID, &one); err != ErrPlannedRoundsTooLow {
		t.Fatalf("lower below paired = %v, want ErrPlannedRoundsTooLow", err)
	}
}

func TestSwapAndReroll(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	r := d.Rounds[0]
	m1, m2 := r.Matches[0], r.Matches[1]
	// Swap m1 slot1 with m2 slot1.
	if err := f.svc.Swap(ctx, f.eventID, 1,
		SlotRef{MatchID: m1.ID, Slot: 1}, SlotRef{MatchID: m2.ID, Slot: 1}); err != nil {
		t.Fatal(err)
	}
	after := f.detail(t).Rounds[0]
	if after.Matches[0].Player1ID != m2.Player1ID || after.Matches[1].Player1ID != m1.Player1ID {
		t.Error("swap did not exchange players")
	}
	// Self-pair rejected: swap m1.slot2 into m1.slot1's opponent seat.
	if err := f.svc.Swap(ctx, f.eventID, 1,
		SlotRef{MatchID: m1.ID, Slot: 1}, SlotRef{MatchID: m1.ID, Slot: 1}); err != ErrSwapInvalid {
		t.Fatalf("same-slot swap = %v, want ErrSwapInvalid", err)
	}
	if err := f.svc.Reroll(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if got := len(f.detail(t).Rounds[0].Matches); got != 2 {
		t.Fatalf("reroll left %d matches, want 2", got)
	}
	// Published rounds refuse draft edits.
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Reroll(ctx, f.eventID, 1); err != ErrRoundNotDraft {
		t.Fatalf("reroll published = %v, want ErrRoundNotDraft", err)
	}
}

func TestMutationsRejectedOnNonStartedEvent(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx,
		`update events set status='finished' where id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != ErrEventNotStarted {
		t.Fatalf("publish on finished = %v, want ErrEventNotStarted", err)
	}
	// Reads still work.
	if _, err := f.svc.Get(ctx, f.eventID, true); err != nil {
		t.Fatalf("Get on finished = %v", err)
	}
}

func TestDraftRoundHiddenFromNonAdmin(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	d, err := f.svc.Get(ctx, f.eventID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rounds) != 0 {
		t.Errorf("non-admin sees %d draft rounds, want 0", len(d.Rounds))
	}
}
```

Adjust the raw `insert into users` columns to the real auth schema (check `backend/migrations/00001_auth.sql` for NOT NULL columns such as `password_hash` — supply a dummy value if required).

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/tournaments/ -v` (Docker must be running)
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/tournaments
git commit -m "feat(tournaments): service with round state machine, results, drops"
```

---

### Task 5: Event finish guard (`round-open`)

**Files:**
- Modify: `backend/internal/events/service.go` (error var + `Transition`)
- Modify: `backend/internal/platform/httpapi/events.go` (`mapEventErr`)
- Test: extend `backend/internal/tournaments/service_test.go`

**Interfaces:**
- Consumes: `CountOpenRoundsForEvent` (Task 1).
- Produces: `events.ErrRoundOpen`; problem type `round-open` (409). Task 6's endpoint test exercises it end-to-end.

- [ ] **Step 1: Add the error var** in `backend/internal/events/service.go`, inside the existing `var (...)` block:

```go
	// ErrRoundOpen: finish refused while a tournament round is not
	// completed (5b spec §3.2).
	ErrRoundOpen = errors.New("a tournament round is still open")
```

- [ ] **Step 2: Add the guard in `Transition`.** Locate the `finish` branch in `func (s *Service) Transition` (it validates `started → finished`). Inside the transaction, before the status write for `action == "finish"`, add:

```go
		if action == "finish" {
			open, err := qtx.CountOpenRoundsForEvent(ctx, eventID)
			if err != nil {
				return err
			}
			if open > 0 {
				return ErrRoundOpen
			}
		}
```

(Adapt names to the actual `Transition` shape — the transition switch already runs inside `withTx`; place the check next to the other `finish` validations.)

- [ ] **Step 3: Map the problem type** in `backend/internal/platform/httpapi/events.go`, in `mapEventErr`:

```go
	case errors.Is(err, events.ErrRoundOpen):
		return eventProblem(http.StatusConflict, "round-open", err.Error())
```

- [ ] **Step 4: Add the test** — append to `backend/internal/tournaments/service_test.go`:

```go
func TestFinishGuardBlocksOpenRound(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	open, err := f.q.CountOpenRoundsForEvent(ctx, f.eventID)
	if err != nil {
		t.Fatal(err)
	}
	if open != 1 {
		t.Fatalf("open rounds = %d, want 1", open)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	open, err = f.q.CountOpenRoundsForEvent(ctx, f.eventID)
	if err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Fatalf("open rounds after complete = %d, want 0", open)
	}
}
```

Also add an events-side transition test in the events package if `Transition` has unit coverage there (`backend/internal/events/service_test.go`): seed a started event with an open round via raw SQL and assert `Transition(ctx, id, "finish")` returns `ErrRoundOpen`.

- [ ] **Step 5: Run tests**

Run: `cd backend && go test ./internal/tournaments/ ./internal/events/ ./internal/platform/httpapi/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/events backend/internal/platform/httpapi backend/internal/tournaments
git commit -m "feat(events): refuse finish while a tournament round is open"
```

---

### Task 6: huma endpoints + wiring + generated client

**Files:**
- Create: `backend/internal/platform/httpapi/tournaments.go`
- Modify: `backend/internal/platform/httpapi/api.go` (Deps field + register call)
- Modify: `backend/cmd/server/main.go` (service wiring)
- Test: `backend/internal/platform/httpapi/tournaments_endpoints_test.go`
- Generated: `frontend/src/shared/api/` via `make api-generate` (committed)

**Interfaces:**
- Consumes: Task 4's service + errors; existing helpers `requireAdmin`, `isAdmin`, `parseEventID`, `eventProblem`, `CurrentUserID`.
- Produces: wire types `TournamentInfo`, `TournamentPlayerInfo`, `TournamentRoundInfo`, `TournamentMatchInfo`, `TournamentStandingInfo` (names become the generated TS types); operations `getTournament`, `upsertTournament`, `pairNextRound`, `rerollRound`, `swapRoundSlots`, `publishRound`, `completeRound`, `reportMatchResult`, `dropTournamentPlayer`, `undropTournamentPlayer`.

- [ ] **Step 1: Write the handlers** `backend/internal/platform/httpapi/tournaments.go`:

```go
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/tournaments"
)

type TournamentPlayerInfo struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"userId"`
	DisplayName string    `json:"displayName"`
	Dropped     bool      `json:"dropped"`
}

type TournamentMatchInfo struct {
	ID          uuid.UUID  `json:"id"`
	TableNumber int32      `json:"tableNumber"`
	Player1ID   uuid.UUID  `json:"player1Id"`
	Player2ID   *uuid.UUID `json:"player2Id,omitempty"`
	P1Games     *int32     `json:"p1Games,omitempty"`
	P2Games     *int32     `json:"p2Games,omitempty"`
	Draws       *int32     `json:"draws,omitempty"`
	ReportedAt  *time.Time `json:"reportedAt,omitempty"`
}

type TournamentRoundInfo struct {
	Number  int32                 `json:"number"`
	Status  string                `json:"status" enum:"draft,published,completed"`
	Matches []TournamentMatchInfo `json:"matches"`
}

type TournamentStandingInfo struct {
	Rank        int     `json:"rank"`
	PlayerID    uuid.UUID `json:"playerId"`
	DisplayName string  `json:"displayName"`
	Dropped     bool    `json:"dropped"`
	MatchPoints int     `json:"matchPoints"`
	OmwPercent  float64 `json:"omwPercent"`
	GwPercent   float64 `json:"gwPercent"`
	OgwPercent  float64 `json:"ogwPercent"`
}

type TournamentInfo struct {
	EventID       uuid.UUID                `json:"eventId"`
	PlannedRounds int32                    `json:"plannedRounds"`
	CurrentRound  *int32                   `json:"currentRound,omitempty"`
	Players       []TournamentPlayerInfo   `json:"players"`
	Rounds        []TournamentRoundInfo    `json:"rounds"`
	Standings     []TournamentStandingInfo `json:"standings"`
}

func tournamentInfoFrom(d *tournaments.Detail) TournamentInfo {
	out := TournamentInfo{
		EventID: d.EventID, PlannedRounds: d.PlannedRounds,
		Players:   make([]TournamentPlayerInfo, len(d.Players)),
		Rounds:    make([]TournamentRoundInfo, len(d.Rounds)),
		Standings: make([]TournamentStandingInfo, len(d.Standings)),
	}
	for i, p := range d.Players {
		out.Players[i] = TournamentPlayerInfo{
			ID: p.ID, UserID: p.UserID, DisplayName: p.DisplayName, Dropped: p.Dropped,
		}
	}
	for i, r := range d.Rounds {
		matches := make([]TournamentMatchInfo, len(r.Matches))
		for j, m := range r.Matches {
			matches[j] = TournamentMatchInfo{
				ID: m.ID, TableNumber: m.TableNumber, Player1ID: m.Player1ID,
				Player2ID: m.Player2ID, P1Games: m.P1Games, P2Games: m.P2Games,
				Draws: m.Draws, ReportedAt: m.ReportedAt,
			}
		}
		out.Rounds[i] = TournamentRoundInfo{Number: r.Number, Status: r.Status, Matches: matches}
	}
	if n := len(d.Rounds); n > 0 {
		num := d.Rounds[n-1].Number
		out.CurrentRound = &num
	}
	for i, s := range d.Standings {
		out.Standings[i] = TournamentStandingInfo{
			Rank: s.Rank, PlayerID: s.PlayerID, DisplayName: s.DisplayName,
			Dropped: s.Dropped, MatchPoints: s.MatchPoints,
			OmwPercent: s.OMWPercent, GwPercent: s.GWPercent, OgwPercent: s.OGWPercent,
		}
	}
	return out
}

func mapTournamentErr(err error) error {
	switch {
	case errors.Is(err, tournaments.ErrEventNotFound):
		return huma.Error404NotFound("event not found")
	case errors.Is(err, tournaments.ErrTournamentNotFound):
		return eventProblem(http.StatusNotFound, "tournament-not-found", "no tournament yet")
	case errors.Is(err, tournaments.ErrEventNotStarted):
		return eventProblem(http.StatusConflict, "event-not-started", err.Error())
	case errors.Is(err, tournaments.ErrNoPlayers):
		return eventProblem(http.StatusConflict, "tournament-no-players", err.Error())
	case errors.Is(err, tournaments.ErrPlannedRoundsTooLow):
		return eventProblem(http.StatusConflict, "planned-rounds-too-low", err.Error())
	case errors.Is(err, tournaments.ErrAllRoundsPaired):
		return eventProblem(http.StatusConflict, "planned-rounds-reached", err.Error())
	case errors.Is(err, tournaments.ErrTooFewPlayers):
		return eventProblem(http.StatusConflict, "too-few-players", err.Error())
	case errors.Is(err, tournaments.ErrRoundNotFound):
		return eventProblem(http.StatusNotFound, "round-not-found", err.Error())
	case errors.Is(err, tournaments.ErrRoundExists):
		return eventProblem(http.StatusConflict, "round-exists", err.Error())
	case errors.Is(err, tournaments.ErrRoundNotDraft):
		return eventProblem(http.StatusConflict, "round-not-draft", err.Error())
	case errors.Is(err, tournaments.ErrRoundIncomplete):
		return eventProblem(http.StatusConflict, "round-incomplete", err.Error())
	case errors.Is(err, tournaments.ErrMatchNotFound):
		return eventProblem(http.StatusNotFound, "match-not-found", "no such match")
	case errors.Is(err, tournaments.ErrNotInMatch):
		return eventProblem(http.StatusForbidden, "not-in-match", err.Error())
	case errors.Is(err, tournaments.ErrResultLocked):
		return eventProblem(http.StatusConflict, "result-locked", err.Error())
	case errors.Is(err, tournaments.ErrResultInvalid):
		return eventProblem(http.StatusUnprocessableEntity, "result-invalid", err.Error())
	case errors.Is(err, tournaments.ErrByeImmutable):
		return eventProblem(http.StatusConflict, "bye-immutable", err.Error())
	case errors.Is(err, tournaments.ErrPlayerNotFound):
		return eventProblem(http.StatusNotFound, "player-not-found", "no such player")
	case errors.Is(err, tournaments.ErrNotYourPlayer):
		return eventProblem(http.StatusForbidden, "not-your-player", err.Error())
	case errors.Is(err, tournaments.ErrAlreadyDropped):
		return eventProblem(http.StatusConflict, "already-dropped", err.Error())
	case errors.Is(err, tournaments.ErrNotDropped):
		return eventProblem(http.StatusConflict, "not-dropped", err.Error())
	case errors.Is(err, tournaments.ErrUndropTooLate):
		return eventProblem(http.StatusConflict, "undrop-too-late", err.Error())
	case errors.Is(err, tournaments.ErrSwapInvalid):
		return eventProblem(http.StatusUnprocessableEntity, "swap-invalid", err.Error())
	default:
		return err
	}
}

type tournamentOutput struct {
	Body TournamentInfo
}

// tournamentBody re-reads the aggregate after any mutation so every
// endpoint returns the same fresh TournamentInfo.
func (deps Deps) tournamentBody(ctx context.Context, eventID uuid.UUID, admin bool) (*tournamentOutput, error) {
	d, err := deps.Tournaments.Get(ctx, eventID, admin)
	if err != nil {
		return nil, mapTournamentErr(err)
	}
	return &tournamentOutput{Body: tournamentInfoFrom(d)}, nil
}

type upsertTournamentInput struct {
	EventID string `path:"eventId"`
	Body    struct {
		PlannedRounds *int32 `json:"plannedRounds,omitempty" minimum:"1" maximum:"30"`
	}
}

type roundNumberInput struct {
	EventID string `path:"eventId"`
	Number  int32  `path:"number" minimum:"1"`
}

// SwapSlot is exported for a distinct huma schema name (see EventCubeLink).
type SwapSlot struct {
	MatchID uuid.UUID `json:"matchId"`
	Slot    int32     `json:"slot" minimum:"1" maximum:"2"`
}

type swapInput struct {
	EventID string `path:"eventId"`
	Number  int32  `path:"number" minimum:"1"`
	Body    struct {
		A SwapSlot `json:"a"`
		B SwapSlot `json:"b"`
	}
}

type reportResultInput struct {
	EventID string `path:"eventId"`
	MatchID string `path:"matchId"`
	Body    struct {
		P1Games int32 `json:"p1Games" minimum:"0" maximum:"2"`
		P2Games int32 `json:"p2Games" minimum:"0" maximum:"2"`
		Draws   int32 `json:"draws" minimum:"0" maximum:"3"`
	}
}

type playerActionInput struct {
	EventID  string `path:"eventId"`
	PlayerID string `path:"playerId"`
}

func registerTournaments(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "getTournament",
		Method:      http.MethodGet,
		Path:        "/api/events/{eventId}/tournament",
		Summary:     "Tournament aggregate: players, rounds, matches, standings",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *eventIDInput) (*tournamentOutput, error) {
		if _, ok := CurrentUserID(ctx); !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		return deps.tournamentBody(ctx, id, isAdmin(ctx, deps))
	})

	huma.Register(api, huma.Operation{
		OperationID: "upsertTournament",
		Method:      http.MethodPut,
		Path:        "/api/events/{eventId}/tournament",
		Summary:     "Create the tournament (snapshot paid roster) or set planned rounds (organizer)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *upsertTournamentInput) (*tournamentOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		if err := deps.Tournaments.Upsert(ctx, id, in.Body.PlannedRounds); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, true)
	})

	huma.Register(api, huma.Operation{
		OperationID: "pairNextRound",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/tournament/rounds",
		Summary:     "Pair the next round as a draft (organizer; creates the tournament if absent)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *eventIDInput) (*tournamentOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		if err := deps.Tournaments.PairNextRound(ctx, id); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, true)
	})

	type roundAction struct {
		id  string
		sum string
		fn  func(ctx context.Context, eventID uuid.UUID, number int32) error
	}
	for _, a := range []roundAction{
		{"rerollRound", "Regenerate the draft round's pairings", deps.Tournaments.Reroll},
		{"publishRound", "Publish the draft round", deps.Tournaments.Publish},
		{"completeRound", "Complete the round (all results in)", deps.Tournaments.Complete},
	} {
		path := "/api/events/{eventId}/tournament/rounds/{number}/" +
			map[string]string{"rerollRound": "reroll", "publishRound": "publish", "completeRound": "complete"}[a.id]
		huma.Register(api, huma.Operation{
			OperationID: a.id,
			Method:      http.MethodPost,
			Path:        path,
			Summary:     a.sum + " (organizer)",
			Tags:        []string{"tournaments"},
		}, func(ctx context.Context, in *roundNumberInput) (*tournamentOutput, error) {
			if _, err := requireAdmin(ctx, deps); err != nil {
				return nil, err
			}
			id, err := parseEventID(in.EventID)
			if err != nil {
				return nil, err
			}
			if err := a.fn(ctx, id, in.Number); err != nil {
				return nil, mapTournamentErr(err)
			}
			return deps.tournamentBody(ctx, id, true)
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "swapRoundSlots",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/tournament/rounds/{number}/swap",
		Summary:     "Swap two player slots in the draft round (organizer)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *swapInput) (*tournamentOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		a := tournaments.SlotRef{MatchID: in.Body.A.MatchID, Slot: in.Body.A.Slot}
		b := tournaments.SlotRef{MatchID: in.Body.B.MatchID, Slot: in.Body.B.Slot}
		if err := deps.Tournaments.Swap(ctx, id, in.Number, a, b); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, true)
	})

	huma.Register(api, huma.Operation{
		OperationID: "reportMatchResult",
		Method:      http.MethodPut,
		Path:        "/api/events/{eventId}/tournament/matches/{matchId}/result",
		Summary:     "Report or override a Bo3 result (player in the match, or organizer)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *reportResultInput) (*tournamentOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		matchID, err := uuid.Parse(in.MatchID)
		if err != nil {
			return nil, eventProblem(http.StatusNotFound, "match-not-found", "no such match")
		}
		admin := isAdmin(ctx, deps)
		if err := deps.Tournaments.ReportResult(ctx, id, matchID, uid, admin, tournaments.Result{
			P1Games: in.Body.P1Games, P2Games: in.Body.P2Games, Draws: in.Body.Draws,
		}); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, admin)
	})

	for _, action := range []string{"drop", "undrop"} {
		huma.Register(api, huma.Operation{
			OperationID: action + "TournamentPlayer",
			Method:      http.MethodPost,
			Path:        "/api/events/{eventId}/tournament/players/{playerId}/" + action,
			Summary:     "Player " + action + " (self or organizer)",
			Tags:        []string{"tournaments"},
		}, func(ctx context.Context, in *playerActionInput) (*tournamentOutput, error) {
			uid, ok := CurrentUserID(ctx)
			if !ok {
				return nil, huma.Error401Unauthorized("authentication required")
			}
			id, err := parseEventID(in.EventID)
			if err != nil {
				return nil, err
			}
			playerID, err := uuid.Parse(in.PlayerID)
			if err != nil {
				return nil, eventProblem(http.StatusNotFound, "player-not-found", "no such player")
			}
			admin := isAdmin(ctx, deps)
			fn := deps.Tournaments.Drop
			if action == "undrop" {
				fn = deps.Tournaments.Undrop
			}
			if err := fn(ctx, id, playerID, uid, admin); err != nil {
				return nil, mapTournamentErr(err)
			}
			return deps.tournamentBody(ctx, id, admin)
		})
	}
}
```

NOTE the loop-variable capture: `a` and `action` are per-iteration copies (Go ≥1.22 loop semantics) — safe to close over.

- [ ] **Step 2: Wire into `api.go` and `main.go`.**

In `backend/internal/platform/httpapi/api.go`: add to imports `"github.com/mjabloniec/cube-planner/backend/internal/tournaments"`; add field `Tournaments *tournaments.Service` to `Deps` (after `Events`); add `registerTournaments(api, deps)` after `registerEvents(api, deps)`.

In `backend/cmd/server/main.go`: import the package, and in the `Deps{...}` literal add:

```go
				Tournaments:         tournaments.NewService(queries, pool, slog.Default()),
```

- [ ] **Step 3: Build + regression**

Run: `cd backend && go build ./... && go test ./internal/platform/httpapi/ -run TestHealth`
Expected: builds; huma registers without panics (the health test boots the full API).

- [ ] **Step 4: Write endpoint tests** `backend/internal/platform/httpapi/tournaments_endpoints_test.go` (package `httpapi_test`; reuse `loggedInClient`, `makeAdmin`, `decode`, and seed like the fixture in Task 4 via raw SQL):

```go
package httpapi_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
	"github.com/mjabloniec/cube-planner/backend/internal/tournaments"
)

func newTournamentServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:        auth.NewService(q, noopMailer{}, "http://test"),
		Sessions:    auth.NewSessions(q, false),
		Queries:     q,
		Tournaments: tournaments.NewService(q, pool, slog.Default()),
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q
}

// seedStartedEvent creates a started event with n paid registered users
// (emails p0@test.. / password-free session via loggedInClient).
func seedStartedEvent(t *testing.T, pool *pgxpool.Pool, q *db.Queries, srv *httptest.Server, n int) (uuid.UUID, []string) {
	t.Helper()
	ctx := context.Background()
	var organizerID uuid.UUID
	// loggedInClient(t, srv, q, email) creates+verifies the user; reuse it
	// so password/session plumbing matches the rest of the suite.
	_ = loggedInClient(t, srv, q, "org@test")
	makeAdmin(t, pool, "org@test")
	if err := pool.QueryRow(ctx,
		`select id from users where email = 'org@test'`).Scan(&organizerID); err != nil {
		t.Fatal(err)
	}
	var eventID uuid.UUID
	if err := pool.QueryRow(ctx,
		`insert into events (organizer_id, name, starts_at, fee_cents,
		    max_participants, status)
		 values ($1, 'tourney', now(), 0, 64, 'started') returning id`,
		organizerID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	emails := make([]string, n)
	for i := range n {
		emails[i] = fmt.Sprintf("p%d@test", i)
		_ = loggedInClient(t, srv, q, emails[i])
		if _, err := pool.Exec(ctx,
			`insert into registrations (event_id, user_id, status, paid_at)
			 select $1, id, 'paid', now() from users where email = $2`,
			eventID, emails[i]); err != nil {
			t.Fatal(err)
		}
	}
	return eventID, emails
}

func TestTournamentEndpointsHappyPath(t *testing.T) {
	srv, pool, q := newTournamentServer(t)
	eventID, emails := seedStartedEvent(t, pool, q, srv, 4)
	org := loggedInClient(t, srv, q, "org@test")
	player := loggedInClient(t, srv, q, emails[0])
	base := fmt.Sprintf("/api/events/%s/tournament", eventID)

	// 404 before creation.
	resp := player.do(t, http.MethodGet, base, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pre-creation GET = %d, want 404", resp.StatusCode)
	}
	// Non-admin cannot pair.
	resp = player.do(t, http.MethodPost, base+"/rounds", nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("player pair = %d, want 403", resp.StatusCode)
	}
	// Organizer pairs round 1 (auto-creates), sees the draft round.
	resp = org.do(t, http.MethodPost, base+"/rounds", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pair = %d", resp.StatusCode)
	}
	tourOrg := decode[httpapi.TournamentInfo](t, resp)
	if len(tourOrg.Rounds) != 1 || tourOrg.Rounds[0].Status != "draft" {
		t.Fatalf("organizer draft round missing: %+v", tourOrg.Rounds)
	}
	// Player does NOT see the draft round.
	resp = player.do(t, http.MethodGet, base, nil)
	tourPl := decode[httpapi.TournamentInfo](t, resp)
	if len(tourPl.Rounds) != 0 {
		t.Fatalf("player sees draft round")
	}
	// Publish; player now sees pairings and reports own match.
	resp = org.do(t, http.MethodPost, base+"/rounds/1/publish", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d", resp.StatusCode)
	}
	resp = player.do(t, http.MethodGet, base, nil)
	tourPl = decode[httpapi.TournamentInfo](t, resp)
	var myPlayerID uuid.UUID
	for _, p := range tourPl.Players {
		if p.DisplayName == emails[0] { // loggedInClient uses email as display name; adjust if different
			myPlayerID = p.ID
		}
	}
	var myMatch *httpapi.TournamentMatchInfo
	for i, m := range tourPl.Rounds[0].Matches {
		if m.Player1ID == myPlayerID || (m.Player2ID != nil && *m.Player2ID == myPlayerID) {
			myMatch = &tourPl.Rounds[0].Matches[i]
		}
	}
	if myMatch == nil {
		t.Fatal("caller's match not found")
	}
	resp = player.do(t, http.MethodPut,
		fmt.Sprintf("%s/matches/%s/result", base, myMatch.ID),
		map[string]any{"p1Games": 2, "p2Games": 1, "draws": 0})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("player report = %d", resp.StatusCode)
	}
	// Standings react.
	resp = player.do(t, http.MethodGet, base, nil)
	tourPl = decode[httpapi.TournamentInfo](t, resp)
	if tourPl.Standings[0].MatchPoints != 3 {
		t.Fatalf("leader MP = %d, want 3", tourPl.Standings[0].MatchPoints)
	}
	// Drop self.
	resp = player.do(t, http.MethodPost,
		fmt.Sprintf("%s/players/%s/drop", base, myPlayerID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("self-drop = %d", resp.StatusCode)
	}
	// Complete blocked while a result is missing.
	resp = org.do(t, http.MethodPost, base+"/rounds/1/complete", nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("complete incomplete = %d, want 409", resp.StatusCode)
	}
}

func TestTournamentAdminGates(t *testing.T) {
	srv, pool, q := newTournamentServer(t)
	eventID, emails := seedStartedEvent(t, pool, q, srv, 4)
	player := loggedInClient(t, srv, q, emails[1])
	base := fmt.Sprintf("/api/events/%s/tournament", eventID)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPut, base},
		{http.MethodPost, base + "/rounds"},
		{http.MethodPost, base + "/rounds/1/reroll"},
		{http.MethodPost, base + "/rounds/1/publish"},
		{http.MethodPost, base + "/rounds/1/complete"},
		{http.MethodPost, base + "/rounds/1/swap"},
	} {
		resp := player.do(t, tc.method, tc.path, map[string]any{})
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
}
```

Adjust `seedStartedEvent`/display-name lookup to the real `loggedInClient` helper behavior (read its definition in `session_endpoints_test.go` first — if it derives display names differently, match players by `userId` via a `/api/me` call instead).

- [ ] **Step 5: Run tests**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestTournament -v`
Expected: PASS.

- [ ] **Step 6: Regenerate the client**

Run: `make api-generate && git status --short frontend/src/shared/api/`
Expected: `frontend/src/shared/api/schema.d.ts` (and friends) updated with the tournament paths + `TournamentInfo` etc.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/platform/httpapi backend/cmd/server frontend/src/shared/api
git commit -m "feat(tournaments): huma endpoints, wiring, generated client"
```

---

### Task 7: Frontend API hooks + i18n messages

**Files:**
- Create: `frontend/src/features/tournaments/api.ts`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`

**Interfaces:**
- Consumes: generated client (`Task 6`), TanStack Query.
- Produces (used by Tasks 8–9): `TournamentInfo/TournamentRound/TournamentMatch/TournamentPlayer/TournamentStanding` type aliases; hooks `useEventStatus(eventId)`, `useTournament(eventId, opts?)`, `useUpsertTournament(eventId)`, `usePairNextRound(eventId)`, `useRoundAction(eventId)` (reroll/publish/complete), `useSwapSlots(eventId)`, `useReportResult(eventId)`, `usePlayerAction(eventId)` (drop/undrop); `NotFoundError` marker class; message keys `tournament_*`.

- [ ] **Step 1: Write the hooks** `frontend/src/features/tournaments/api.ts`:

```ts
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type TournamentInfo = components["schemas"]["TournamentInfo"];
export type TournamentRound = components["schemas"]["TournamentRoundInfo"];
export type TournamentMatch = components["schemas"]["TournamentMatchInfo"];
export type TournamentPlayer = components["schemas"]["TournamentPlayerInfo"];
export type TournamentStanding = components["schemas"]["TournamentStandingInfo"];

/** 404 = no tournament yet — a normal state, not an error banner. */
export class NotFoundError extends Error {}

function unwrap<T>(data: T | undefined, error: { detail?: string | null } | undefined): T {
  if (error) throw new Error(error.detail ?? m.error_generic());
  if (!data) throw new Error(m.error_generic());
  return data;
}

// The event detail under the events feature's queryKey: same endpoint +
// key, so TanStack dedupes with features/events and their invalidations
// keep both fresh. features must not import features (structure.md).
export function useEventStatus(eventId: string) {
  return useQuery({
    queryKey: ["events", "detail", eventId],
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/events/{eventId}", {
        params: { path: { eventId } },
      });
      return unwrap(data, error);
    },
  });
}

export function useTournament(eventId: string, opts?: { refetchInterval?: number | false }) {
  return useQuery({
    queryKey: ["tournaments", eventId],
    retry: false,
    refetchInterval: opts?.refetchInterval ?? false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/events/{eventId}/tournament", {
        params: { path: { eventId } },
      });
      if (response.status === 404) throw new NotFoundError(m.tournament_none_yet());
      return unwrap(data, error);
    },
  });
}

// Server truth only (no optimistic updates): every mutation refetches
// the aggregate.
function useTournamentMutation<TVars>(eventId: string, fn: (vars: TVars) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tournaments", eventId] }),
  });
}

export function useUpsertTournament(eventId: string) {
  return useTournamentMutation(eventId, async (plannedRounds: number | undefined) => {
    const { data, error } = await client.PUT("/api/events/{eventId}/tournament", {
      params: { path: { eventId } },
      body: plannedRounds == null ? {} : { plannedRounds },
    });
    return unwrap(data, error);
  });
}

export function usePairNextRound(eventId: string) {
  return useTournamentMutation(eventId, async () => {
    const { data, error } = await client.POST("/api/events/{eventId}/tournament/rounds", {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

export type RoundAction = "reroll" | "publish" | "complete";

const ROUND_PATHS = {
  reroll: "/api/events/{eventId}/tournament/rounds/{number}/reroll",
  publish: "/api/events/{eventId}/tournament/rounds/{number}/publish",
  complete: "/api/events/{eventId}/tournament/rounds/{number}/complete",
} as const;

export function useRoundAction(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ action, number }: { action: RoundAction; number: number }) => {
      const { data, error } = await client.POST(ROUND_PATHS[action], {
        params: { path: { eventId, number } },
      });
      return unwrap(data, error);
    },
  );
}

export type SwapSlotRef = { matchId: string; slot: 1 | 2 };

export function useSwapSlots(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ number, a, b }: { number: number; a: SwapSlotRef; b: SwapSlotRef }) => {
      const { data, error } = await client.POST(
        "/api/events/{eventId}/tournament/rounds/{number}/swap",
        { params: { path: { eventId, number } }, body: { a, b } },
      );
      return unwrap(data, error);
    },
  );
}

export type ResultInput = { p1Games: number; p2Games: number; draws: number };

export function useReportResult(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ matchId, result }: { matchId: string; result: ResultInput }) => {
      const { data, error } = await client.PUT(
        "/api/events/{eventId}/tournament/matches/{matchId}/result",
        { params: { path: { eventId, matchId } }, body: result },
      );
      return unwrap(data, error);
    },
  );
}

export function usePlayerAction(eventId: string) {
  return useTournamentMutation(
    eventId,
    async ({ playerId, action }: { playerId: string; action: "drop" | "undrop" }) => {
      const path =
        action === "drop"
          ? ("/api/events/{eventId}/tournament/players/{playerId}/drop" as const)
          : ("/api/events/{eventId}/tournament/players/{playerId}/undrop" as const);
      const { data, error } = await client.POST(path, {
        params: { path: { eventId, playerId } },
      });
      return unwrap(data, error);
    },
  );
}
```

- [ ] **Step 2: Add the messages.** Append to `frontend/messages/en.json` (keep alphabetical ordering if the file uses it; otherwise append at the end):

```json
{
  "tournament_title": "Tournament",
  "tournament_none_yet": "The tournament hasn't started yet.",
  "tournament_none_yet_organizer": "No rounds yet — pair round 1 to start the tournament.",
  "tournament_round_tab": "Round {number}",
  "tournament_table": "Table {number}",
  "tournament_bye": "Bye",
  "tournament_playing": "Playing…",
  "tournament_your_match": "Your match",
  "tournament_report_result": "Report result",
  "tournament_result_saved": "Result saved.",
  "tournament_games_won": "{name}: games won",
  "tournament_drawn_games": "Drawn games",
  "tournament_result_invalid": "Enter a valid best-of-3 score.",
  "tournament_standings": "Standings",
  "tournament_rank": "#",
  "tournament_player": "Player",
  "tournament_points": "Points",
  "tournament_omw": "OMW%",
  "tournament_gw": "GW%",
  "tournament_ogw": "OGW%",
  "tournament_dropped_flag": "dropped",
  "tournament_drop_self": "Drop from tournament",
  "tournament_drop_confirm": "Drop from the tournament? You'll be excluded from the next round's pairings. Results you played stay on the books.",
  "tournament_undrop": "Rejoin",
  "tournament_drop": "Drop",
  "tournament_planned_rounds": "Planned rounds",
  "tournament_save": "Save",
  "tournament_pair_round": "Pair round {number}",
  "tournament_draft_heading": "Draft pairings — round {number}",
  "tournament_draft_hint": "Select two players to swap their seats, re-roll for new pairings, then publish.",
  "tournament_reroll": "Re-roll",
  "tournament_publish": "Publish pairings",
  "tournament_swap_selected": "Swap selected",
  "tournament_results_heading": "Results — round {number}",
  "tournament_missing_results": "{count} results missing",
  "tournament_complete_round": "Complete round",
  "tournament_players_heading": "Players",
  "tournament_vs": "vs"
}
```

And the same keys in Polish in `frontend/messages/pl.json`:

```json
{
  "tournament_title": "Turniej",
  "tournament_none_yet": "Turniej jeszcze się nie rozpoczął.",
  "tournament_none_yet_organizer": "Brak rund — sparuj rundę 1, aby rozpocząć turniej.",
  "tournament_round_tab": "Runda {number}",
  "tournament_table": "Stolik {number}",
  "tournament_bye": "Wolny los",
  "tournament_playing": "W trakcie…",
  "tournament_your_match": "Twój mecz",
  "tournament_report_result": "Zgłoś wynik",
  "tournament_result_saved": "Wynik zapisany.",
  "tournament_games_won": "{name}: wygrane gry",
  "tournament_drawn_games": "Gry zremisowane",
  "tournament_result_invalid": "Podaj poprawny wynik best-of-3.",
  "tournament_standings": "Klasyfikacja",
  "tournament_rank": "#",
  "tournament_player": "Gracz",
  "tournament_points": "Punkty",
  "tournament_omw": "OMW%",
  "tournament_gw": "GW%",
  "tournament_ogw": "OGW%",
  "tournament_dropped_flag": "wycofany",
  "tournament_drop_self": "Wycofaj się z turnieju",
  "tournament_drop_confirm": "Wycofać się z turnieju? Zostaniesz pominięty przy parowaniu kolejnej rundy. Rozegrane wyniki pozostają.",
  "tournament_undrop": "Wróć do turnieju",
  "tournament_drop": "Wycofaj",
  "tournament_planned_rounds": "Zaplanowane rundy",
  "tournament_save": "Zapisz",
  "tournament_pair_round": "Sparuj rundę {number}",
  "tournament_draft_heading": "Robocze pary — runda {number}",
  "tournament_draft_hint": "Wybierz dwóch graczy, aby zamienić ich miejscami, przelosuj pary lub opublikuj.",
  "tournament_reroll": "Przelosuj",
  "tournament_publish": "Opublikuj pary",
  "tournament_swap_selected": "Zamień wybranych",
  "tournament_results_heading": "Wyniki — runda {number}",
  "tournament_missing_results": "Brakujące wyniki: {count}",
  "tournament_complete_round": "Zakończ rundę",
  "tournament_players_heading": "Gracze",
  "tournament_vs": "vs"
}
```

- [ ] **Step 3: Regenerate paraglide + typecheck**

Run: `pnpm --filter @cube-planner/frontend gen && pnpm --filter @cube-planner/frontend typecheck`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/features/tournaments frontend/messages
git commit -m "feat(tournaments): frontend api hooks and i18n messages"
```

---

### Task 8: Player view — TournamentSection

**Files:**
- Create: `frontend/src/features/tournaments/components/TournamentSection.tsx`
- Create: `frontend/src/features/tournaments/components/ResultForm.tsx`
- Create: `frontend/src/features/tournaments/components/StandingsTable.tsx`
- Modify: `frontend/src/routes/events.$eventId.index.tsx`
- Test: `frontend/src/features/tournaments/components/ResultForm.test.tsx`
- Test: `frontend/src/features/tournaments/components/StandingsTable.test.tsx`
- Test: `frontend/src/features/tournaments/components/TournamentSection.test.tsx`

**Interfaces:**
- Consumes: Task 7 hooks/types; `useMe` from `@/features/auth/api`; `Button`, `Dialog` from `@/shared/ui`.
- Produces: `<TournamentSection eventId={string} />` (self-gating: renders nothing unless the event is `started`/`finished` and a tournament exists); `<ResultForm match, players, onSubmit, pending, error />`; `<StandingsTable standings, highlightPlayerId? />` (reused by Task 9).

- [ ] **Step 1: StandingsTable** `frontend/src/features/tournaments/components/StandingsTable.tsx`:

```tsx
import { m } from "@/paraglide/messages";
import type { TournamentStanding } from "../api";

const pct = (v: number) => v.toFixed(1);

export function StandingsTable({
  standings,
  highlightPlayerId,
}: {
  standings: TournamentStanding[];
  highlightPlayerId?: string;
}) {
  return (
    <table className="w-full text-sm">
      <caption className="sr-only">{m.tournament_standings()}</caption>
      <thead>
        <tr className="border-b border-border text-left text-fg-muted">
          <th scope="col" className="py-1 pr-2">{m.tournament_rank()}</th>
          <th scope="col" className="py-1 pr-2">{m.tournament_player()}</th>
          <th scope="col" className="py-1 pr-2 text-right">{m.tournament_points()}</th>
          <th scope="col" className="py-1 pr-2 text-right">{m.tournament_omw()}</th>
          <th scope="col" className="py-1 pr-2 text-right">{m.tournament_gw()}</th>
          <th scope="col" className="py-1 text-right">{m.tournament_ogw()}</th>
        </tr>
      </thead>
      <tbody>
        {standings.map((s) => (
          <tr
            key={s.playerId}
            className={`border-b border-border ${
              s.playerId === highlightPlayerId ? "bg-surface-raised font-medium" : ""
            }`}
          >
            <td className="py-1 pr-2">{s.rank}</td>
            <td className="py-1 pr-2 text-fg">
              {s.displayName}
              {s.dropped && (
                <span className="ml-2 text-xs text-fg-muted">({m.tournament_dropped_flag()})</span>
              )}
            </td>
            <td className="py-1 pr-2 text-right">{s.matchPoints}</td>
            <td className="py-1 pr-2 text-right">{pct(s.omwPercent)}</td>
            <td className="py-1 pr-2 text-right">{pct(s.gwPercent)}</td>
            <td className="py-1 text-right">{pct(s.ogwPercent)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
```

- [ ] **Step 2: ResultForm** `frontend/src/features/tournaments/components/ResultForm.tsx`:

```tsx
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import type { ResultInput, TournamentMatch } from "../api";

function nameOf(players: Map<string, string>, id: string) {
  return players.get(id) ?? "?";
}

const validResult = (r: ResultInput) =>
  r.p1Games >= 0 && r.p1Games <= 2 && r.p2Games >= 0 && r.p2Games <= 2 &&
  r.draws >= 0 && r.draws <= 3 && r.p1Games + r.p2Games + r.draws <= 3 &&
  !(r.p1Games === 2 && r.p2Games === 2);

function GamesField({
  id, label, value, max, onChange,
}: {
  id: string; label: string; value: number; max: number;
  onChange: (v: number) => void;
}) {
  return (
    <div className="flex flex-col gap-1">
      <Label htmlFor={id}>{label}</Label>
      <input
        id={id}
        type="number"
        min={0}
        max={max}
        value={value}
        onChange={(e) => onChange(Number(e.target.value))}
        className="w-20 rounded-md border border-border bg-surface px-2 py-1 text-fg"
      />
    </div>
  );
}

export function ResultForm({
  match, playerNames, onSubmit, pending, error,
}: {
  match: TournamentMatch;
  playerNames: Map<string, string>;
  onSubmit: (result: ResultInput) => void;
  pending: boolean;
  error: Error | null;
}) {
  const [result, setResult] = useState<ResultInput>({
    p1Games: match.p1Games ?? 0,
    p2Games: match.p2Games ?? 0,
    draws: match.draws ?? 0,
  });
  const [touchedInvalid, setTouchedInvalid] = useState(false);

  return (
    <form
      className="flex flex-wrap items-end gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        if (!validResult(result)) {
          setTouchedInvalid(true);
          return;
        }
        setTouchedInvalid(false);
        onSubmit(result);
      }}
    >
      <GamesField
        id={`p1-${match.id}`}
        label={m.tournament_games_won({ name: nameOf(playerNames, match.player1Id) })}
        value={result.p1Games}
        max={2}
        onChange={(v) => setResult({ ...result, p1Games: v })}
      />
      <GamesField
        id={`p2-${match.id}`}
        label={m.tournament_games_won({
          name: match.player2Id ? nameOf(playerNames, match.player2Id) : "—",
        })}
        value={result.p2Games}
        max={2}
        onChange={(v) => setResult({ ...result, p2Games: v })}
      />
      <GamesField
        id={`draws-${match.id}`}
        label={m.tournament_drawn_games()}
        value={result.draws}
        max={3}
        onChange={(v) => setResult({ ...result, draws: v })}
      />
      <Button type="submit" size="sm" disabled={pending}>
        {m.tournament_report_result()}
      </Button>
      {touchedInvalid && (
        <p role="alert" className="w-full text-sm text-danger">
          {m.tournament_result_invalid()}
        </p>
      )}
      {error && (
        <p role="alert" className="w-full text-sm text-danger">
          {error.message}
        </p>
      )}
    </form>
  );
}
```

- [ ] **Step 3: TournamentSection** `frontend/src/features/tournaments/components/TournamentSection.tsx`:

```tsx
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import {
  NotFoundError, usePlayerAction, useReportResult, useEventStatus, useTournament,
} from "../api";
import type { TournamentMatch, TournamentRound } from "../api";
import { ResultForm } from "./ResultForm";
import { StandingsTable } from "./StandingsTable";

const LIVE_POLL_MS = 10_000;

function score(match: TournamentMatch) {
  if (match.reportedAt == null) return m.tournament_playing();
  return `${match.p1Games}–${match.p2Games}${match.draws ? ` (${match.draws})` : ""}`;
}

export function TournamentSection({ eventId }: { eventId: string }) {
  const me = useMe();
  const event = useEventStatus(eventId);
  const live = event.data?.status === "started";
  const relevant = live || event.data?.status === "finished";
  const tournament = useTournament(eventId, {
    refetchInterval: live ? LIVE_POLL_MS : false,
  });
  const report = useReportResult(eventId);
  const playerAction = usePlayerAction(eventId);
  const [tab, setTab] = useState<number | null>(null);
  const [confirmDrop, setConfirmDrop] = useState(false);

  // Not started, no tournament yet, or still loading: render nothing.
  if (!relevant || tournament.isPending || tournament.error instanceof NotFoundError) return null;
  if (tournament.error)
    return (
      <p role="alert" className="text-danger">
        {tournament.error.message}
      </p>
    );

  const t = tournament.data;
  const rounds = t.rounds.filter((r) => r.status !== "draft");
  if (rounds.length === 0) return null;
  const activeNumber = tab ?? rounds[rounds.length - 1].number;
  const round = rounds.find((r) => r.number === activeNumber) as TournamentRound;
  const playerNames = new Map(t.players.map((p) => [p.id, p.displayName]));
  const myPlayer = t.players.find((p) => p.userId === me.data?.id);
  const myMatch =
    myPlayer &&
    round.matches.find(
      (mt) => mt.player1Id === myPlayer.id || mt.player2Id === myPlayer.id,
    );
  const canReportMine =
    live && round.status === "published" && myMatch && myMatch.player2Id != null &&
    round.number === rounds[rounds.length - 1].number;

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-medium text-fg">{m.tournament_title()}</h2>

      <div role="tablist" className="flex gap-2">
        {rounds.map((r) => (
          <button
            key={r.number}
            role="tab"
            aria-selected={r.number === activeNumber}
            className={`rounded-md border border-border px-3 py-1 text-sm ${
              r.number === activeNumber ? "bg-accent text-on-accent" : "text-fg"
            }`}
            onClick={() => setTab(r.number)}
          >
            {m.tournament_round_tab({ number: r.number })}
          </button>
        ))}
      </div>

      <ul className="flex flex-col gap-1">
        {round.matches.map((mt) => (
          <li
            key={mt.id}
            className={`flex flex-wrap items-center gap-2 rounded-md border border-border p-2 text-sm ${
              myMatch?.id === mt.id ? "bg-surface-raised" : ""
            }`}
          >
            <span className="text-fg-muted">{m.tournament_table({ number: mt.tableNumber })}</span>
            <span className="text-fg">
              {playerNames.get(mt.player1Id)}{" "}
              {mt.player2Id ? (
                <>
                  {m.tournament_vs()} {playerNames.get(mt.player2Id)}
                </>
              ) : (
                <span className="text-fg-muted">— {m.tournament_bye()}</span>
              )}
            </span>
            <span className="ml-auto text-fg-muted">{score(mt)}</span>
          </li>
        ))}
      </ul>

      {canReportMine && myMatch && (
        <div className="rounded-lg border border-border bg-surface-raised p-3">
          <h3 className="mb-2 text-sm font-medium text-fg">{m.tournament_your_match()}</h3>
          <ResultForm
            match={myMatch}
            playerNames={playerNames}
            pending={report.isPending}
            error={report.error}
            onSubmit={(result) => report.mutate({ matchId: myMatch.id, result })}
          />
        </div>
      )}

      <h3 className="text-base font-medium text-fg">{m.tournament_standings()}</h3>
      <StandingsTable standings={t.standings} highlightPlayerId={myPlayer?.id} />

      {live && myPlayer && !myPlayer.dropped && (
        <div>
          <Button type="button" variant="outline" size="sm" onClick={() => setConfirmDrop(true)}>
            {m.tournament_drop_self()}
          </Button>
        </div>
      )}
      {live && myPlayer?.dropped && (
        <div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={playerAction.isPending}
            onClick={() => playerAction.mutate({ playerId: myPlayer.id, action: "undrop" })}
          >
            {m.tournament_undrop()}
          </Button>
        </div>
      )}
      {playerAction.error && (
        <p role="alert" className="text-sm text-danger">
          {playerAction.error.message}
        </p>
      )}

      <Dialog
        open={confirmDrop}
        onClose={() => setConfirmDrop(false)}
        title={m.tournament_drop_self()}
      >
        <p className="text-sm text-fg">{m.tournament_drop_confirm()}</p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => setConfirmDrop(false)}>
            {m.dialog_close()}
          </Button>
          <Button
            type="button"
            disabled={playerAction.isPending}
            onClick={() => {
              if (myPlayer) playerAction.mutate({ playerId: myPlayer.id, action: "drop" });
              setConfirmDrop(false);
            }}
          >
            {m.tournament_drop()}
          </Button>
        </div>
      </Dialog>
    </section>
  );
}
```

Check the accent text token name used elsewhere (`text-on-accent` — grep an existing primary Button variant and reuse its exact class). Check `useMe()` exposes `id`; if the field differs (e.g. `userId`), adjust.

- [ ] **Step 4: Compose into the route** — `frontend/src/routes/events.$eventId.index.tsx` becomes:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { EventDetailPage } from "@/features/events/components/EventDetailPage";
import { TournamentSection } from "@/features/tournaments/components/TournamentSection";

function EventDetailRoute() {
  const { eventId } = Route.useParams();
  return (
    <div className="flex flex-col gap-8">
      <EventDetailPage />
      <TournamentSection eventId={eventId} />
    </div>
  );
}

export const Route = createFileRoute("/events/$eventId/")({
  component: EventDetailRoute,
  validateSearch: (s: Record<string, unknown>): { checkout?: "success" | "cancelled" } => ({
    ...(s.checkout === "success" || s.checkout === "cancelled" ? { checkout: s.checkout } : {}),
  }),
});
```

- [ ] **Step 5: Write the tests** (pattern from `RegistrationPanel.test.tsx`: `vi.mock("../api")` + QueryClientProvider wrapper).

`ResultForm.test.tsx`:

```tsx
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentMatch } from "../api";
import { ResultForm } from "./ResultForm";

afterEach(cleanup);

const match: TournamentMatch = {
  id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2",
};
const names = new Map([["pl1", "Ann"], ["pl2", "Bob"]]);

function renderForm(onSubmit = vi.fn()) {
  render(
    <ResultForm match={match} playerNames={names} onSubmit={onSubmit} pending={false} error={null} />,
  );
  return onSubmit;
}

test("submits a valid 2-1", async () => {
  const onSubmit = renderForm();
  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "2");
  await userEvent.clear(screen.getByLabelText("Bob: games won"));
  await userEvent.type(screen.getByLabelText("Bob: games won"), "1");
  await userEvent.click(screen.getByRole("button", { name: "Report result" }));
  expect(onSubmit).toHaveBeenCalledWith({ p1Games: 2, p2Games: 1, draws: 0 });
});

test("rejects 2-2 with a validation message", async () => {
  const onSubmit = renderForm();
  await userEvent.clear(screen.getByLabelText("Ann: games won"));
  await userEvent.type(screen.getByLabelText("Ann: games won"), "2");
  await userEvent.clear(screen.getByLabelText("Bob: games won"));
  await userEvent.type(screen.getByLabelText("Bob: games won"), "2");
  await userEvent.click(screen.getByRole("button", { name: "Report result" }));
  expect(onSubmit).not.toHaveBeenCalled();
  expect(screen.getByRole("alert")).toHaveTextContent("Enter a valid best-of-3 score.");
});
```

`StandingsTable.test.tsx`:

```tsx
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import type { TournamentStanding } from "../api";
import { StandingsTable } from "./StandingsTable";

afterEach(cleanup);

const rows: TournamentStanding[] = [
  { rank: 1, playerId: "a", displayName: "Ann", dropped: false, matchPoints: 6, omwPercent: 50, gwPercent: 66.7, ogwPercent: 45 },
  { rank: 2, playerId: "b", displayName: "Bob", dropped: true, matchPoints: 3, omwPercent: 66.7, gwPercent: 50, ogwPercent: 55 },
];

test("renders ranks, points, percentages, and the dropped flag", () => {
  render(<StandingsTable standings={rows} highlightPlayerId="a" />);
  const [, first, second] = screen.getAllByRole("row");
  expect(first).toHaveTextContent("Ann");
  expect(first).toHaveTextContent("6");
  expect(first).toHaveTextContent("66.7");
  expect(second).toHaveTextContent("(dropped)");
});
```

`TournamentSection.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentInfo } from "../api";

const report = vi.fn();
const playerAct = vi.fn();
let tournamentData: TournamentInfo | undefined;
let eventStatus = "started";

vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: { id: "u1", role: "user" } }),
}));
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventStatus: () => ({ data: { status: eventStatus } }),
  useTournament: () => ({ data: tournamentData, isPending: false, error: null }),
  useReportResult: () => ({ mutate: report, isPending: false, error: null }),
  usePlayerAction: () => ({ mutate: playerAct, isPending: false, error: null }),
}));

import { TournamentSection } from "./TournamentSection";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <TournamentSection eventId="e1" />
    </QueryClientProvider>,
  );
}

function baseTournament(): TournamentInfo {
  return {
    eventId: "e1",
    plannedRounds: 2,
    currentRound: 1,
    players: [
      { id: "pl1", userId: "u1", displayName: "Ann", dropped: false },
      { id: "pl2", userId: "u2", displayName: "Bob", dropped: false },
    ],
    rounds: [
      {
        number: 1,
        status: "published",
        matches: [{ id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" }],
      },
    ],
    standings: [
      { rank: 1, playerId: "pl1", displayName: "Ann", dropped: false, matchPoints: 0, omwPercent: 0, gwPercent: 0, ogwPercent: 0 },
      { rank: 1, playerId: "pl2", displayName: "Bob", dropped: false, matchPoints: 0, omwPercent: 0, gwPercent: 0, ogwPercent: 0 },
    ],
  } as TournamentInfo;
}

test("shows pairings, my-match result form, standings, and drop", () => {
  tournamentData = baseTournament();
  renderSection();
  expect(screen.getByRole("tab", { name: "Round 1" })).toBeInTheDocument();
  expect(screen.getByText("Your match")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Report result" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Drop from tournament" })).toBeInTheDocument();
});

test("no result form on a completed round; undrop for dropped player", () => {
  tournamentData = baseTournament();
  tournamentData.rounds[0].status = "completed";
  tournamentData.players[0].dropped = true;
  renderSection();
  expect(screen.queryByRole("button", { name: "Report result" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Rejoin" })).toBeInTheDocument();
});

test("renders nothing before the event starts", () => {
  eventStatus = "published";
  tournamentData = baseTournament();
  const { container } = renderSection();
  expect(container).toBeEmptyDOMElement();
  eventStatus = "started";
});
```

- [ ] **Step 6: Run tests + typecheck + lint**

Run: `pnpm --filter @cube-planner/frontend test && pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend lint`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/features/tournaments frontend/src/routes/events.\$eventId.index.tsx
git commit -m "feat(tournaments): player tournament section with results and standings"
```

---

### Task 9: Organizer view — TournamentPanel

**Files:**
- Create: `frontend/src/features/tournaments/components/TournamentPanel.tsx`
- Modify: `frontend/src/routes/events.$eventId.manage.tsx`
- Test: `frontend/src/features/tournaments/components/TournamentPanel.test.tsx`

**Interfaces:**
- Consumes: Task 7 hooks, Task 8's `ResultForm` + `StandingsTable`, `useMe`, `Button`, `Dialog`.
- Produces: `<TournamentPanel eventId={string} />` — self-gating on admin + event `started`/`finished`.

- [ ] **Step 1: TournamentPanel** `frontend/src/features/tournaments/components/TournamentPanel.tsx`:

```tsx
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import {
  NotFoundError, usePairNextRound, usePlayerAction, useReportResult,
  useRoundAction, useSwapSlots, useEventStatus, useTournament, useUpsertTournament,
} from "../api";
import type { SwapSlotRef, TournamentInfo, TournamentRound } from "../api";
import { ResultForm } from "./ResultForm";
import { StandingsTable } from "./StandingsTable";

function latestRound(t: TournamentInfo): TournamentRound | undefined {
  return t.rounds[t.rounds.length - 1];
}

export function TournamentPanel({ eventId }: { eventId: string }) {
  const me = useMe();
  const event = useEventStatus(eventId);
  const tournament = useTournament(eventId);
  const upsert = useUpsertTournament(eventId);
  const pair = usePairNextRound(eventId);
  const roundAction = useRoundAction(eventId);
  const swap = useSwapSlots(eventId);
  const report = useReportResult(eventId);
  const playerAction = usePlayerAction(eventId);

  const [plannedRounds, setPlannedRounds] = useState<string>("");
  const [selectedSlot, setSelectedSlot] = useState<SwapSlotRef | null>(null);
  const [editingMatch, setEditingMatch] = useState<string | null>(null);

  if (me.data?.role !== "admin") return null;
  const status = event.data?.status;
  if (status !== "started" && status !== "finished") return null;

  const noTournament = tournament.error instanceof NotFoundError;
  if (tournament.error && !noTournament)
    return (
      <p role="alert" className="text-danger">
        {tournament.error.message}
      </p>
    );
  if (tournament.isPending && !noTournament) return null;

  const t = noTournament ? null : tournament.data!;
  const latest = t ? latestRound(t) : undefined;
  const draft = latest?.status === "draft" ? latest : undefined;
  const published = latest?.status === "published" ? latest : undefined;
  const playerNames = new Map((t?.players ?? []).map((p) => [p.id, p.displayName]));
  const nextRoundNumber = (latest?.number ?? 0) + 1;
  const canPair =
    status === "started" && !draft && !published &&
    (!t || t.rounds.length < t.plannedRounds);
  const missing = published ? published.matches.filter((mt) => mt.reportedAt == null).length : 0;

  const mutationError =
    upsert.error ?? pair.error ?? roundAction.error ?? swap.error ?? playerAction.error;

  const toggleSlot = (ref: SwapSlotRef) => {
    if (!draft) return;
    if (selectedSlot == null) {
      setSelectedSlot(ref);
      return;
    }
    if (selectedSlot.matchId === ref.matchId && selectedSlot.slot === ref.slot) {
      setSelectedSlot(null);
      return;
    }
    swap.mutate({ number: draft.number, a: selectedSlot, b: ref });
    setSelectedSlot(null);
  };

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-medium text-fg">{m.tournament_title()}</h2>
      {mutationError && (
        <p role="alert" className="text-sm text-danger">
          {mutationError.message}
        </p>
      )}

      {status === "started" && (
        <form
          className="flex items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            const v = Number(plannedRounds);
            if (Number.isInteger(v) && v >= 1) upsert.mutate(v);
          }}
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="planned-rounds">{m.tournament_planned_rounds()}</Label>
            <input
              id="planned-rounds"
              type="number"
              min={1}
              max={30}
              value={plannedRounds || (t?.plannedRounds ?? "")}
              onChange={(e) => setPlannedRounds(e.target.value)}
              className="w-24 rounded-md border border-border bg-surface px-2 py-1 text-fg"
            />
          </div>
          <Button type="submit" size="sm" variant="outline" disabled={upsert.isPending}>
            {m.tournament_save()}
          </Button>
          {canPair && (
            <Button type="button" size="sm" disabled={pair.isPending} onClick={() => pair.mutate(undefined)}>
              {m.tournament_pair_round({ number: nextRoundNumber })}
            </Button>
          )}
        </form>
      )}

      {!t && <p className="text-sm text-fg-muted">{m.tournament_none_yet_organizer()}</p>}

      {draft && (
        <div className="flex flex-col gap-2 rounded-lg border border-border bg-surface-raised p-3">
          <h3 className="text-sm font-medium text-fg">
            {m.tournament_draft_heading({ number: draft.number })}
          </h3>
          <p className="text-xs text-fg-muted">{m.tournament_draft_hint()}</p>
          <ul className="flex flex-col gap-1">
            {draft.matches.map((mt) => (
              <li key={mt.id} className="flex items-center gap-2 text-sm">
                <span className="text-fg-muted">
                  {m.tournament_table({ number: mt.tableNumber })}
                </span>
                {([1, 2] as const).map((slot) => {
                  const playerId = slot === 1 ? mt.player1Id : mt.player2Id;
                  if (playerId == null)
                    return (
                      <span key={slot} className="text-fg-muted">
                        {m.tournament_bye()}
                      </span>
                    );
                  const isSelected =
                    selectedSlot?.matchId === mt.id && selectedSlot?.slot === slot;
                  return (
                    <button
                      key={slot}
                      type="button"
                      aria-pressed={isSelected}
                      className={`rounded-md border px-2 py-0.5 ${
                        isSelected ? "border-accent bg-accent text-on-accent" : "border-border text-fg"
                      }`}
                      onClick={() => toggleSlot({ matchId: mt.id, slot })}
                    >
                      {playerNames.get(playerId)}
                    </button>
                  );
                })}
              </li>
            ))}
          </ul>
          <div className="flex gap-2">
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={roundAction.isPending}
              onClick={() => roundAction.mutate({ action: "reroll", number: draft.number })}
            >
              {m.tournament_reroll()}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={roundAction.isPending}
              onClick={() => roundAction.mutate({ action: "publish", number: draft.number })}
            >
              {m.tournament_publish()}
            </Button>
          </div>
        </div>
      )}

      {published && (
        <div className="flex flex-col gap-2 rounded-lg border border-border p-3">
          <h3 className="text-sm font-medium text-fg">
            {m.tournament_results_heading({ number: published.number })}
          </h3>
          <ul className="flex flex-col gap-2">
            {published.matches.map((mt) => (
              <li key={mt.id} className="flex flex-col gap-1 text-sm">
                <div className="flex items-center gap-2">
                  <span className="text-fg-muted">
                    {m.tournament_table({ number: mt.tableNumber })}
                  </span>
                  <span className="text-fg">
                    {playerNames.get(mt.player1Id)}{" "}
                    {mt.player2Id
                      ? `${m.tournament_vs()} ${playerNames.get(mt.player2Id)}`
                      : `— ${m.tournament_bye()}`}
                  </span>
                  {mt.reportedAt == null ? (
                    <span className="font-medium text-danger">{m.tournament_playing()}</span>
                  ) : (
                    <span className="text-fg-muted">
                      {mt.p1Games}–{mt.p2Games}
                      {mt.draws ? ` (${mt.draws})` : ""}
                    </span>
                  )}
                  {mt.player2Id != null && (
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() => setEditingMatch(editingMatch === mt.id ? null : mt.id)}
                    >
                      {m.tournament_report_result()}
                    </Button>
                  )}
                </div>
                {editingMatch === mt.id && (
                  <ResultForm
                    match={mt}
                    playerNames={playerNames}
                    pending={report.isPending}
                    error={report.error}
                    onSubmit={(result) => {
                      report.mutate(
                        { matchId: mt.id, result },
                        { onSuccess: () => setEditingMatch(null) },
                      );
                    }}
                  />
                )}
              </li>
            ))}
          </ul>
          <div className="flex items-center gap-3">
            <Button
              type="button"
              size="sm"
              disabled={missing > 0 || roundAction.isPending}
              onClick={() => roundAction.mutate({ action: "complete", number: published.number })}
            >
              {m.tournament_complete_round()}
            </Button>
            {missing > 0 && (
              <span className="text-sm text-fg-muted">
                {m.tournament_missing_results({ count: missing })}
              </span>
            )}
          </div>
        </div>
      )}

      {t && (
        <>
          <h3 className="text-base font-medium text-fg">{m.tournament_standings()}</h3>
          <StandingsTable standings={t.standings} />

          <h3 className="text-base font-medium text-fg">{m.tournament_players_heading()}</h3>
          <ul className="flex flex-col gap-1">
            {t.players.map((p) => (
              <li key={p.id} className="flex items-center gap-2 text-sm">
                <span className="text-fg">{p.displayName}</span>
                {p.dropped && (
                  <span className="text-xs text-fg-muted">({m.tournament_dropped_flag()})</span>
                )}
                {status === "started" && (
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={playerAction.isPending}
                    onClick={() =>
                      playerAction.mutate({
                        playerId: p.id,
                        action: p.dropped ? "undrop" : "drop",
                      })
                    }
                  >
                    {p.dropped ? m.tournament_undrop() : m.tournament_drop()}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
```

- [ ] **Step 2: Compose into the manage route** — `frontend/src/routes/events.$eventId.manage.tsx` becomes:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { ManageEventPage } from "@/features/events/components/ManageEventPage";
import { TournamentPanel } from "@/features/tournaments/components/TournamentPanel";

function ManageEventRoute() {
  const { eventId } = Route.useParams();
  return (
    <div className="flex flex-col gap-8">
      <ManageEventPage />
      <TournamentPanel eventId={eventId} />
    </div>
  );
}

export const Route = createFileRoute("/events/$eventId/manage")({ component: ManageEventRoute });
```

- [ ] **Step 3: Write the tests** `frontend/src/features/tournaments/components/TournamentPanel.test.tsx` (same mock pattern as Task 8):

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { TournamentInfo } from "../api";
import { NotFoundError } from "../api";

const pairMut = vi.fn();
const swapMut = vi.fn();
const roundMut = vi.fn();
let tournamentData: TournamentInfo | null = null;

vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: { id: "org", role: "admin" } }),
}));
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventStatus: () => ({ data: { status: "started" } }),
  useTournament: () =>
    tournamentData
      ? { data: tournamentData, isPending: false, error: null }
      : { data: undefined, isPending: false, error: new NotFoundError("none") },
  useUpsertTournament: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  usePairNextRound: () => ({ mutate: pairMut, isPending: false, error: null }),
  useRoundAction: () => ({ mutate: roundMut, isPending: false, error: null }),
  useSwapSlots: () => ({ mutate: swapMut, isPending: false, error: null }),
  useReportResult: () => ({ mutate: vi.fn(), isPending: false, error: null }),
  usePlayerAction: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));

import { TournamentPanel } from "./TournamentPanel";

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
  tournamentData = null;
});

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <TournamentPanel eventId="e1" />
    </QueryClientProvider>,
  );
}

function draftTournament(): TournamentInfo {
  return {
    eventId: "e1",
    plannedRounds: 2,
    currentRound: 1,
    players: [
      { id: "pl1", userId: "u1", displayName: "Ann", dropped: false },
      { id: "pl2", userId: "u2", displayName: "Bob", dropped: false },
      { id: "pl3", userId: "u3", displayName: "Cid", dropped: false },
      { id: "pl4", userId: "u4", displayName: "Dee", dropped: false },
    ],
    rounds: [
      {
        number: 1,
        status: "draft",
        matches: [
          { id: "m1", tableNumber: 1, player1Id: "pl1", player2Id: "pl2" },
          { id: "m2", tableNumber: 2, player1Id: "pl3", player2Id: "pl4" },
        ],
      },
    ],
    standings: [],
  } as TournamentInfo;
}

test("no tournament yet: shows pair-round-1 CTA", async () => {
  renderPanel();
  expect(screen.getByText(/pair round 1/i)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /pair round 1/i }));
  expect(pairMut).toHaveBeenCalled();
});

test("draft round: select two slots → swap fires", async () => {
  tournamentData = draftTournament();
  renderPanel();
  await userEvent.click(screen.getByRole("button", { name: "Ann" }));
  await userEvent.click(screen.getByRole("button", { name: "Cid" }));
  expect(swapMut).toHaveBeenCalledWith({
    number: 1,
    a: { matchId: "m1", slot: 1 },
    b: { matchId: "m2", slot: 1 },
  });
});

test("published round: complete disabled while results missing", () => {
  tournamentData = draftTournament();
  tournamentData.rounds[0].status = "published";
  // One of the two matches reported → one missing.
  tournamentData.rounds[0].matches[1] = {
    ...tournamentData.rounds[0].matches[1],
    p1Games: 2, p2Games: 0, draws: 0, reportedAt: "2026-07-20T18:00:00Z",
  };
  renderPanel();
  expect(screen.getByRole("button", { name: "Complete round" })).toBeDisabled();
  expect(screen.getByText("1 results missing")).toBeInTheDocument();
});

test("publish button fires for a draft round", async () => {
  tournamentData = draftTournament();
  renderPanel();
  await userEvent.click(screen.getByRole("button", { name: "Publish pairings" }));
  expect(roundMut).toHaveBeenCalledWith({ action: "publish", number: 1 });
});
```

- [ ] **Step 4: Run tests + typecheck + lint**

Run: `pnpm --filter @cube-planner/frontend test && pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend lint`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/tournaments frontend/src/routes/events.\$eventId.manage.tsx
git commit -m "feat(tournaments): organizer panel with draft editor and results grid"
```

---

### Task 10: a11y tests + full verification

**Files:**
- Create: `frontend/src/features/tournaments/components/a11y.test.tsx`

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Write the axe test** `frontend/src/features/tournaments/components/a11y.test.tsx` (mirror `features/events/components/a11y.test.tsx` — jsdom + vitest-axe; reuse its exact setup imports):

```tsx
// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { axe } from "vitest-axe";
import { expect, test, vi } from "vitest";
import type { TournamentMatch, TournamentStanding } from "../api";
import { ResultForm } from "./ResultForm";
import { StandingsTable } from "./StandingsTable";

vi.mock("@/features/auth/api", () => ({
  useMe: () => ({ data: { id: "u1", role: "user" } }),
}));

const match: TournamentMatch = { id: "m1", tableNumber: 1, player1Id: "a", player2Id: "b" };
const names = new Map([["a", "Ann"], ["b", "Bob"]]);
const standings: TournamentStanding[] = [
  { rank: 1, playerId: "a", displayName: "Ann", dropped: false, matchPoints: 3, omwPercent: 50, gwPercent: 66.7, ogwPercent: 45 },
];

test("ResultForm has no axe violations", async () => {
  const qc = new QueryClient();
  const { container } = render(
    <QueryClientProvider client={qc}>
      <ResultForm match={match} playerNames={names} onSubmit={() => {}} pending={false} error={null} />
    </QueryClientProvider>,
  );
  expect(await axe(container)).toHaveNoViolations();
});

test("StandingsTable has no axe violations", async () => {
  const { container } = render(<StandingsTable standings={standings} />);
  expect(await axe(container)).toHaveNoViolations();
});
```

(Check how the events a11y test asserts — if it uses `expect(results.violations).toEqual([])` instead of `toHaveNoViolations`, copy that form.)

- [ ] **Step 2: Full verification**

Run, expecting every step green:

```bash
make test                                      # backend (Docker) + frontend
pnpm --filter @cube-planner/frontend typecheck
pnpm --filter @cube-planner/frontend lint
cd backend && golangci-lint run ./... && cd ..
make api-check                                 # generated client is fresh
```

- [ ] **Step 3: i18n parity check**

Run: `node -e "const en=require('./frontend/messages/en.json'),pl=require('./frontend/messages/pl.json');const a=Object.keys(en),b=new Set(Object.keys(pl));const miss=a.filter(k=>!b.has(k)).concat(Object.keys(pl).filter(k=>!(k in en)));if(miss.length){console.error(miss);process.exit(1)}console.log('ok')"`
Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/features/tournaments
git commit -m "test(tournaments): axe coverage for tournament components"
```

---

## Post-plan

After all tasks: whole-branch review (most capable model), then manual acceptance — a real multi-user event: start event with 4–5 paid players (odd count exercises the bye), pair/swap/publish, report from two browsers, drop mid-event, complete all rounds, finish event, verify standings against a hand calculation.
