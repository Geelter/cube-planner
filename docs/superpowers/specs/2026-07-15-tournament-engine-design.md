# Tournament Engine — Sub-project 5b Design

**Date:** 2026-07-15
**Status:** Approved. Implements the second half of master design §7.5
(`2026-07-10-cube-planner-master-design.md`); builds on sub-project 5a
(`2026-07-13-events-registration-payments-design.md`). The paid roster
of a `started` event is the tournament roster.

## 1. Scope

Swiss tournament engine for started events: roster snapshot, round
pairing with organizer preview/adjust/publish, player-reported Bo3
results with organizer override, drops, MTR-standard standings.

| Decision | Choice |
|---|---|
| Structure | Swiss only; default rounds `ceil(log2(players))`, min 1, organizer-overridable |
| Results | Game-level Bo3 scores (enables GW%/OGW%) |
| Result entry | Either player in the match reports; organizer can override; last write wins |
| Drops | Self-drop + organizer drop, effective at next pairing; undrop until next pairing |
| Pairings | Auto-generated draft → organizer may swap/re-roll → publish |
| Architecture | New `backend/internal/tournaments` package; pure pairing/standings core; standings computed on demand, never stored |

**Non-goals (future work, §8):** playoff/top-cut brackets, seatings for
draft pods, printed pairings, match timers, penalties/game losses,
spectator-facing big-screen view.

## 2. Data model

One goose migration `00008_tournaments.sql`, sqlc queries in
`backend/internal/db/queries/`. Conventions follow 00006: uuid PKs,
`created_at`/`updated_at`, inline CHECKs.

```sql
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

create table rounds (
    id uuid primary key default gen_random_uuid(),
    tournament_id uuid not null references tournaments (id) on delete cascade,
    number int not null check (number >= 1),
    status text not null default 'draft'
        check (status in ('draft', 'published', 'completed')),
    -- Pairing seed: draft regeneration ("re-roll") stores a new seed;
    -- pairing is deterministic given (roster, history, seed).
    seed bigint not null,
    published_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (tournament_id, number)
);

create table matches (
    id uuid primary key default gen_random_uuid(),
    round_id uuid not null references rounds (id) on delete cascade,
    table_number int not null check (table_number >= 1),
    player1_id uuid not null references tournament_players (id),
    -- Null player2 = bye. Byes are created with an auto-filled 2-0
    -- result so standings math has a single input shape.
    player2_id uuid references tournament_players (id),
    p1_games int check (p1_games between 0 and 2),
    p2_games int check (p2_games between 0 and 2),
    draws int check (draws between 0 and 3),
    -- Result columns are all null (unreported) or all non-null; enforced
    -- in the service, plus: p1_games + p2_games + draws <= 3 and not
    -- both players at 2.
    reported_by uuid references users (id),
    reported_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (round_id, table_number),
    check (player2_id is null or player1_id <> player2_id)
);
```

Indexes: `rounds (tournament_id)`, `matches (round_id)`,
`tournament_players (tournament_id)`.

Standings are never stored — computed on demand from matches (§3.4).

## 3. Domain rules & state machines

### 3.1 Tournament creation (roster snapshot)

Created lazily on the first organizer mutation (`PUT tournament` or
pairing round 1). Requires event status `started`; otherwise problem
type `event-not-started`. Creation snapshots all `paid` registrations
into `tournament_players` and sets `planned_rounds` to
`max(1, ceil(log2(paid_count)))` unless the organizer supplied a value.
Zero paid players → `tournament-no-players`. The snapshot is final:
later registration changes (post-start refunds) never mutate the
roster; the organizer handles a refunded no-show with a drop.

`planned_rounds` is editable while the event is `started`, but never
below the number of rounds already created (`planned-rounds-too-low`).

### 3.2 Round lifecycle

`draft → published → completed`, strictly one round in flight:

- **Pair next round** (organizer): allowed when every earlier round is
  `completed`, `rounds_count < planned_rounds`, event `started`, and at
  least 2 active (non-dropped) players remain (`too-few-players`).
  Generates matches from the pairing algorithm (§3.3) with a fresh
  seed; round starts as `draft`, visible only to the organizer.
- **Draft edits** (organizer): `reroll` (new seed, regenerate matches),
  `swap` (exchange two player slots between two matches of the draft
  round; validation only forbids pairing a player against themselves —
  the organizer may knowingly create a rematch). Byes may be swapped
  like any slot; the auto 2-0 bye result is (re)applied on publish.
- **Publish** (organizer): `draft → published`, sets `published_at`;
  players now see pairings and can report.
- **Report result**: while the round is `published`, either player in
  the match may `PUT` the result (last write wins, `reported_by`
  audited). The organizer may enter/override results of the latest
  round until the next round is paired — including on a `completed`
  round (standings recompute on read, so corrections are safe).
- **Complete** (organizer): requires every match reported
  (`round-incomplete`); sets `completed_at`. Enables pairing the next
  round or finishing the event.

Event `finish` (existing 5a endpoint) gains one guard: rejected while a
round exists that is not `completed` (`round-open`). Finishing is
allowed before `planned_rounds` are played (organizer cuts the event
short). Once the event is `finished` (or `cancelled`), every tournament
mutation is rejected (`event-not-started`); the aggregate GET keeps
working and standings are final.

### 3.3 Pairing algorithm (pure function)

`Pair(players []Player, history History, seed int64) []Pairing` in a
pure package, `backend/internal/tournaments/swiss`. Inputs are plain
structs (player id, match points, had-bye flag, past opponents);
deterministic for a given seed.

1. Active players only (dropped players excluded).
2. If the count is odd, assign the bye first: seeded-random pick from
   the lowest match-point group among players without a previous bye
   (widening to the next group up if all of the lowest group had one;
   in the extreme case every player has had a bye and the constraint is
   waived).
3. Sort remaining players into match-point groups, seeded-shuffle
   within each group, pair top-down; an odd group pairs its leftover
   down into the next group.
4. Backtracking search rejects rematches. If no rematch-free pairing
   exists (provable via exhausted search — realistic in late rounds of
   tiny events), retry allowing the minimum number of rematches.
5. Table numbers: 1..n ordered by the higher-ranked player (current
   standings) in each pairing; the bye match takes the last table
   number.

Byes count as a 2-0 match win: 3 match points, and a won round in the
recipient's own MW%. For tiebreaks, the bye is not an opponent and its
games are excluded from the recipient's own GW% (§3.4).

### 3.4 Standings (pure function)

`ComputeStandings(players, matches) []Standing` in the same pure
package. MTR-standard:

- Match points: win 3, draw 1, loss 0. Game points: win 3, draw 1.
- Sort: match points → OMW% → GW% → OGW%. Percentages use MTR floors:
  each opponent's MW%/GW% counts as at least 0.33. Byes are excluded
  everywhere in tiebreaks: not an opponent for OMW%/OGW%, and the bye's
  games are excluded from the recipient's own GW%.
- Dropped players remain in standings (flagged), and their results keep
  counting in opponents' tiebreaks.
- Players equal on all four keys share the rank (competition ranking:
  1, 2, 2, 4); ordering within a shared rank is by display name for
  stability.

### 3.5 Drops

- Self-drop: any active player on their own row, any time while the
  event is `started`.
- Organizer drop: any player, any time.
- Effect: excluded from the next pairing onward. An unreported match in
  the current round still requires a result before the round can
  complete (organizer enters 2-0 for the opponent, or the real score).
- Undrop (self or organizer): allowed until a round has been paired
  after the drop; concretely, undrop is rejected if any round was
  created after `dropped_at` (`undrop-too-late`).

## 4. API surface

Huma handlers in `backend/internal/tournaments`, wired in the server
like the other features. The tournaments service reads events through a
narrow interface (status, organizer id, paid roster) — dependency
direction stays tournaments → events.

### User endpoints (session auth)

- `GET /events/{eventID}/tournament` → the single aggregate read:
  `TournamentInfo` (below). 404 `tournament-not-found` until the
  tournament exists. A `draft` round is included only for admins.
  Visible to anyone who can see the event (5a visibility rules).
- `PUT /events/{eventID}/tournament/matches/{matchID}/result`
  `{p1Games, p2Games, draws}` → 200 updated `MatchInfo`. Allowed for a
  player in that match while its round is `published`, or an admin
  under §3.2 rules; others 403 `not-in-match`. Validation per §2 CHECK
  rules (`result-invalid`). Byes reject results (`bye-immutable`).
- `POST /events/{eventID}/tournament/players/{playerID}/drop` and
  `/undrop` — the player themself or an admin; §3.5 rules.

### Organizer endpoints (session auth + `admin` role, else 403 problem type `admin-required`)

- `PUT /events/{eventID}/tournament` `{plannedRounds?}` — creates the
  tournament (roster snapshot, §3.1) or updates `plannedRounds`.
- `POST /events/{eventID}/tournament/rounds` — pair next round (draft);
  creates the tournament with defaults if absent.
- `POST /events/{eventID}/tournament/rounds/{number}/reroll`
- `POST /events/{eventID}/tournament/rounds/{number}/swap`
  `{a: {matchID, slot}, b: {matchID, slot}}` (slot: 1|2)
- `POST /events/{eventID}/tournament/rounds/{number}/publish`
- `POST /events/{eventID}/tournament/rounds/{number}/complete`

### Domain problem types

`event-not-started`, `tournament-not-found`, `tournament-no-players`,
`planned-rounds-too-low`, `too-few-players`, `round-exists` (pairing
while one is open), `round-not-draft`, `round-incomplete`,
`round-open` (on event finish), `not-in-match`, `result-invalid`,
`bye-immutable`, `already-dropped`, `undrop-too-late`,
`swap-invalid`. All RFC 7807, same shape as 5a.

### Wire types

- `TournamentInfo`: eventID, plannedRounds, currentRound? (number of
  the latest round, absent before round 1),
  players: `PlayerInfo[]`, rounds: `RoundInfo[]`,
  standings: `StandingInfo[]`
- `PlayerInfo`: id, userID, displayName, dropped
- `RoundInfo`: number, status, matches: `MatchInfo[]`
- `MatchInfo`: id, tableNumber, player1ID, player2ID? (absent = bye),
  p1Games?, p2Games?, draws?, reportedAt?
- `StandingInfo`: rank, playerID, matchPoints, omwPercent, gwPercent,
  ogwPercent, dropped

`make api-generate` regenerates the client; frontend consumes only
generated types.

## 5. Frontend structure & UX

New `frontend/src/features/tournaments/` feature; routes stay thin and
compose it into the existing event pages (no feature→feature imports,
per `docs/architecture/structure.md`).

### Player view (event detail, event `started`/`finished`)

- Tournament section with round tabs (Round 1..n). Per round: pairings
  list — table number, both players, result (or "playing…" while
  unreported). The caller's own match is highlighted with an inline Bo3
  result form (two game-win steppers + draws, submit).
- Standings table: rank, player, points, OMW%, GW%, OGW%; dropped
  players flagged; caller's row highlighted. Rendered from the
  aggregate query — no client-side math.
- Self-drop button (confirm dialog explaining the effect), undrop while
  allowed.
- The aggregate query polls with TanStack Query `refetchInterval` 10 s
  while the event is `started`; no polling once `finished`.

### Organizer view (manage page, new Tournament tab)

- Setup: planned rounds (default shown), "Pair round N" button.
- Draft editor: generated pairings with select-a-slot → select-a-slot
  swap interaction, re-roll, publish. Focus management per a11y
  conventions.
- Results grid for the published round: every match, missing results
  visually loud, inline entry/override for any match. Complete-round
  button (disabled until all reported, with count).
- Player list with drop/undrop actions.
- Finish-event button already exists in 5a manage UI; it now surfaces
  the `round-open` error as a toast.

### Conventions

Paraglide `m.*()` strings (en + pl), cva variants, semantic color
tokens, table semantics + labeled inputs per structure.md. No
optimistic updates: every mutation invalidates the aggregate query and
re-renders from server truth (mirrors 5a's webhook-is-truth
philosophy).

## 6. Testing & error handling

- **Pure core (bulk of coverage), table-driven Go tests:**
  - `Pair`: no rematches when avoidable; score-group integrity and
    pair-down; bye to lowest group without prior bye, no repeat byes,
    widening rule; dropped players excluded; determinism per seed;
    minimal-rematch fallback when rematch-free is impossible; property
    check — every active player appears exactly once.
  - `ComputeStandings`: MTR fixtures with hand-computed OMW%/GW%/OGW%,
    0.33 floor cases, bye exclusion, dropped-player tiebreak
    contribution, shared-rank ties.
- **Integration (testcontainers):** lazy creation + snapshot;
  pair → swap → reroll → publish → report → complete → next round;
  permission matrix (player in match, other player, non-player, admin);
  drop/undrop timing rules; organizer override after complete; event
  finish guard; mutations rejected once finished.
- **Frontend (vitest + RTL):** result form validation/submission,
  standings rendering (ranks, flags), draft-editor swap flow; axe test
  (jsdom) for player and organizer tournament views.
- Errors surface as toasts/inline errors mapped from problem types,
  same pattern as 5a.

## 7. Order of work

1. Migration + sqlc queries.
2. Pure swiss package (`Pair`, `ComputeStandings`) with the full
   table-driven suite — no DB dependency.
3. Tournaments service + state machine + events read-interface; unit
   tests.
4. Huma handlers + problem types; integration tests; finish-guard
   change in events; `make api-generate`.
5. Frontend feature: player view, then organizer view; tests + i18n.
6. Whole-branch review, manual acceptance with a real multi-user event.

## 8. Future work (logged, not in scope)

- Top-cut single-elimination bracket after swiss.
- Draft-pod seatings (seat order for the actual draft portion).
- Printable pairings / big-screen standings view.
- Match timer / round clock.
- Penalties (game losses) and no-show auto-results.
- 5a backlog items unchanged (capacity increase, transfers, pay window,
  iCal).
