# Production demo feedback batch — Design

Date: 2026-09-03
Status: approved (brainstorm 2026-09-03)

## Goal

Close the 12 findings from the first production demo with a real tester
on cubeplanner.pl, plus two gaps surfaced while brainstorming them. They
fall into four waves, delivered as **11 PRs**, smallest-first, each
independently shippable behind the PR flow (master is protected).

| # | Finding | PR |
|---|---|---|
| 1 | Date picker accepts nonsense values | 3 (backend only) |
| 2 | Registrations/Players sections redundant per status | 4 |
| 3 | No way to remove a participant from a free event | 5 |
| 4 | Planned-rounds input empty on render | 6 |
| 5 | Result reporting hides overwrites / score disputes | 8 |
| 6 | Tournament section flickers on refetch | 6 |
| 7 | Opening another result form discards edits silently | 8 |
| 8 | Drawn-games input expresses no extra information | 7 |
| 9 | Bulk import wanted for cubes (creation *and* details) | 9 |
| 10 | Card search input not cleared after picking | 2 |
| 11 | Wantlists should be set-aware, and carry set codes | 10 |
| 12 | Backdrops don't block scrolling behind the overlay | 1 |
| 13 | Organizer DQ-without-refund (surfaced in brainstorm) | 5 |
| 14 | Imports can't specify a set or an exact printing | 11 |

Sequencing: PRs 1–4 and 6 are independent. 5 → 8 (shared confirm
dialog), 7 → 8 (both touch `ResultForm`), 9 → 11 (11 extends the parser
that 9 moves). PR 10's set-annotated export only round-trips back into
the app once 11 lands, but the two are otherwise independent.

## Wave 1 — shared polish

### PR 1 — Overlay scroll lock and focus restoration (#12)

Current state is better than the finding implies. Every overlay in the
app is a native `<dialog>` driven by `showModal()` — `shared/ui/dialog.tsx`
and `shared/ui/drawer.tsx` are the only two primitives, and there is no
Radix, no vaul, no portal anywhere. That means background inertness,
focus trapping and Esc already work correctly, and the codebase
explicitly relies on it (`routes/__root.tsx`, `CubeEditorPage.tsx`).

Two real defects remain:

**a) No body scroll lock.** There is no `overflow: hidden` rule, no
scroll-lock hook, and no occurrence of "scroll" in `src/` outside
generated Paraglide output. Desktop browsers suppress root scroll for
`dialog:modal`; **iOS Safari does not**. The page scrolls and
rubber-bands behind the sheet — worst on the two phone-first surfaces:
the mobile nav `Drawer`, and `Drawer side="bottom"` (`max-h-[85svh]`,
leaving 15% of the page directly touchable).

- New `shared/lib/useScrollLock.ts`. **Ref-counted** — nested overlays
  are real (`CubeDisplayPage` can have `CardPreviewSheet` and
  `PrintingPickerDialog` open together), so the last unlock must win,
  not the first. Preserves and restores scroll position.
- Consumed by both primitives, so every current and future overlay gets
  it without opting in.
- `overscroll-behavior: contain` on both scroll panels, so touch-scroll
  at the end of a sheet's own `overflow-y-auto` region does not chain
  into the document.

**b) Focus is not restored from card previews.** `CardPreviewSheet` and
`PrintingPickerDialog` hardcode `open` and are *conditionally mounted*
by their parents (`CubeDisplayPage`, `CollectionPage`, `WantlistPage`,
`CardSearchPage`, `CubeEditorPage`). React removes the `<dialog>` while
it is still open, so the effect's `el.close()` never runs — and removing
a top-layer element does not return focus to the opener. Keyboard and
AT users land on `<body>`. Fixed at the primitive level with an unmount
cleanup that calls `close()`, which repairs every call site at once.

Testing: unit tests for `useScrollLock` (ref counting, position
restore). **Modality itself stays untestable** — happy-dom has no
`showModal`, so the primitives fall back to a non-modal `open`
attribute and no unit test can catch a real modality regression. The
acceptance gate for the scroll lock is manual verification at 360px on
iOS Safari.

Out of scope, for the backlog: `Dialog` has no backdrop-click dismiss
while `Drawer` does. That is an inconsistency about *dismissal*, not
about blocking, and `Drawer`'s implementation has the classic
mousedown-inside/mouseup-outside false positive.

### PR 2 — Card search clears after picking (#10)

`shared/cards/CardAutocomplete.tsx` currently does `setQuery(c.name)` on
select, leaving the picked card's name in the box. Consequences: the
user clears it by hand before every next card, re-focusing re-opens a
list containing only that card, and re-selecting silently adds another
copy.

Change to `setQuery("")` and keep focus in the input so a list can be
typed straight through. Both consumers (cube editor, collection) want
this behaviour. One line plus a test.

## Wave 2 — events

### PR 3 — Backend event schedule validation (#1)

Per the brainstorm, **no frontend changes.** `<input type="datetime-local">`
already renders as segments that refuse out-of-range digits, and its
calendar panel is UA chrome we can neither restyle nor constrain
further. Hand-rolling a segmented picker to guarantee `mm <= 59` was
judged not worth the cost.

The real hole is the server, which validates nothing temporal.
`StartsAt time.Time` and `RefundDeadline *time.Time` carry **no huma
tags at all**, and neither `Service.Create` nor `Service.Update` checks
them. The API today accepts a start time in the past, a refund deadline
after the start, and a refund deadline on a free event (the form
suppresses that last one client-side only).

- New `ErrInvalidSchedule` → 422 `invalid-event-schedule`.
- Rules: `startsAt` must be in the future; `refundDeadline <= startsAt`;
  no `refundDeadline` when `feeCents == 0`.
- Enforced in `Create` **and** `Update` — the deadline stays editable
  after publish, so `Update` must re-validate against the *stored*
  `startsAt`, not a request field that isn't there.
- No far-future cap. YAGNI.

No message keys: backend RFC 7807 `detail` strings render verbatim per
the i18n exception in `docs/architecture/structure.md`.

### PR 4 — Section visibility by event status (#2)

Registrations (`RegistrationsTable`) and the public attendee chips are
currently rendered at every status; the organizer Players list is
already gated to `started`.

| Section | draft | published | started | finished | cancelled |
|---|---|---|---|---|---|
| Registrations: paid | — | yes | roster-only | — | — |
| Registrations: pending / waitlist / history | — | yes | — | — | — |
| Registrations: refund-queue group | if non-empty | yes | if non-empty | if non-empty | if non-empty |
| Attendee chips (public page) | — | yes | yes | — | yes |
| Players list (organizer panel) | — | — | yes | — | — |

The refund-queue carve-out is deliberate: a player who self-cancels past
the refund deadline lands in `refund_requested`, and hiding the whole
section on `started`/`finished` would leave the organizer no screen on
which to approve or deny that request. It renders only when the queue is
non-empty, so the common case is still a clean page.

**Amended 2026-09-04.** The paid group also survives into `started`. As
first written, this matrix and PR 5's button gating each matched their own
section but never intersected: remove renders only for
`paid`/`pending_payment`/`waitlisted`, none of which were visible once the
event began — so the feature was unreachable in its headline case, a
no-show, which is by definition discovered after the start. On `started`
the paid group renders roster-style: the Refund button is suppressed
(refund-vs-keep becomes a Stripe-dashboard decision there), leaving only
Remove. Removing then also drops the player from the tournament — see
below.

Two readings made explicit, since "the players section" is ambiguous —
there are two of them:

- On `finished`, **both** the public attendee chips and the organizer
  Players list are hidden; standings supersede both.
- On `draft`, the attendee chips are hidden too (they render "0 spots"
  noise for an event nobody can join yet).

### PR 5 — Remove a participant, and a shared confirm dialog (#3, #13)

Today the organizer's Refund button renders for every `paid` row, and
`OrganizerRefund` rejects it with `invalid event transition: nothing was
paid` when the row has no Stripe payment intent. Free events always
register straight to `paid` with no intent, so **free-event participants
cannot be removed at all**.

This PR also adds a fifth copy of the confirm-dialog pattern already
duplicated across `RegistrationsTable`, `ManageEventPage`,
`RegistrationPanel` and `TournamentSection`, so it **starts** by
extracting `shared/ui/confirm-dialog.tsx` and re-pointing those four.
PR 8 consumes it too.

#### New `removed` status

A migration extends the `registrations` status check constraint with
`removed`, rather than reusing `cancelled`. The distinction carries
weight: `cancelled` means *the player withdrew*, and a late payment on
such a row is legitimately reclaimable — they wanted in. `removed` means
*the organizer ejected them* and must never be reclaimable.

`removed` is absent from `registrations_one_active_idx`, so it occupies
no capacity and blocks no re-registration, with no index change needed.

**Known limitation, accepted:** a removed player can therefore
re-register and be removed again. A ban list is over-engineering for a
single local community.

#### Endpoint

```
POST /api/events/{eventId}/registrations/{registrationId}/remove
body: { keepPayment: bool }   // required; omitting it is rejected
```

Admin-only. Re-reads the registration under `GetEventForUpdate` and
validates there — never trusting the client's view of the row.

- `pending_payment` / `waitlisted` / `paid` **without** an intent →
  `removed`; frees the spot and runs `promoteLocked` if the row was
  holding one. `keepPayment` is irrelevant.
- `paid` **with** an intent → requires `keepPayment: true`, else **409
  `remove-needs-decision`**. `keepPayment` is a required field rather
  than a defaulted one: an omitted field is rejected by schema
  validation before the handler runs, so a stale client can never
  accidentally pocket a fee (fail-closed, and covered by
  `TestRemoveOmittedKeepPaymentIsRejected`). On success the row is `removed` and
  `stripe_payment_intent_id` is **preserved**, so a later dashboard
  refund still resolves through `handleChargeRefunded`.
- `refund_requested` → **409**, directing the organizer to Refund or
  Deny. The money-in-limbo queue stays the single authority.
- Terminal rows (`cancelled`, `refunded`, `expired`, `removed`) → 409.

The existing `/refund` endpoint is untouched; its "nothing was paid" 409
simply becomes unreachable from the UI rather than being papered over.

#### Tournament roster (added 2026-09-04)

`tournament_players` is snapshotted from paid registrations when the event
starts and nothing else writes it, so a player removed after the start
would keep being paired. `RemoveRegistration` therefore also drops them
(`DropTournamentPlayerByEventUser`), which is exactly what the organizer's
existing Drop button does: future rounds skip them, results they already
played stay on the books. It is a no-op before the start, for events with
no tournament, and for an already-dropped player — whose original
`dropped_at` is preserved rather than restamped, so a self-drop keeps its
real time.

#### Money safety

The full case matrix, and what each does:

| # | Row state | Money | Outcome |
|---|---|---|---|
| A | `waitlisted` | none | Safe. Holds no capacity; leaves a `waitlist_pos` gap, harmless because promotion orders by position. |
| B | `pending_payment`, never clicked Pay | none | Safe. Free spot, promote. |
| C1 | `pending_payment`, live session, never completes | none | Session expires → `handleCheckoutExpired` resolves by stored session id, finds a non-pending row, no-ops. Safe. |
| C2 | `pending_payment`, **completes checkout after removal** | charged | Needs the guard below. |
| D | `paid`, free event (no intent) | none | Safe. |
| E | `paid`, paid event, `keepPayment: true` | kept | Safe **because** the intent is preserved on the row. |
| F | `paid`, refunding | returned | Existing Refund button, unchanged. |
| G | Remove races the paid webhook | either | Both take `GetEventForUpdate`; they serialize. See below. |

**C2 is the one genuine hazard.** `handleCheckoutCompleted`'s `default:`
branch (the expired/cancelled path) *reclaims* a row to `paid` when the
event is still `published`, a spot is free, and the user holds no other
active row — which would silently reinstate a removed player who
completed a checkout moments later. Two layers close it:

1. **Authoritative:** the reclaim condition gains
   `&& reg.Status != "removed"`. A completed payment on a removed row
   therefore falls through to the existing, battle-tested `lateRefund`
   path — auto-refund plus an apology email — with removal-specific copy
   replacing `paymentAfterExpiryEmail`.
2. **Fast path:** `ExpireCheckoutSession` is added to the `StripeClient`
   interface (Stripe's `V1CheckoutSessions.Expire`) and called
   best-effort on removal, shrinking the window to near-zero. It is not
   the guarantee — a user who already submitted their card cannot be
   stopped — which is why layer 1 exists.

**Case G** resolves itself. If the webhook wins, Remove sees `paid` with
an intent and 409s, prompting the organizer to choose Refund or
remove-with-`keepPayment`. If Remove wins, the webhook auto-refunds via
layer 1.

#### Frontend

`RegistrationInfo` gains `hasPayment: bool` so the table renders exactly
the right actions per row:

- free / unpaid rows → `[Remove]`
- paid rows → `[Refund]` `[Remove — no refund]`, the second danger-styled
  behind a confirm that names the fee and states plainly that it will
  not be returned.

The removed player is always emailed, with copy branching on whether the
fee was kept — silently ejecting someone from an event they paid for is
worse than telling them.

## Wave 3 — tournaments

### PR 6 — Tournament "not created yet", rounds prefill, flicker (#4, #6)

Both findings share one root cause: `GET /tournament` 404s with
`tournament-not-found` when no tournament exists, and the backend's
`defaultRounds(n) = ceil(log2(n))` (`tournaments/service.go`) is never
exposed — so the organizer's input has nothing to prefill from and the
section has an error state to flicker through.

**Backend:** the endpoint returns **200** with
`{exists: false, recommendedRounds, paidPlayerCount, rounds: []}`
instead of 404. `recommendedRounds` comes from the existing
`defaultRounds` helper, recomputed against the current paid roster on
every GET. A genuinely unknown event still 404s. Removing the
`tournament-not-found` type also fixes a latent bug: the frontend keys
off the raw HTTP status, so "no tournament yet" and "event not found"
are currently indistinguishable.

**Frontend, prefill:** the input becomes
`plannedRounds ?? t.plannedRounds ?? t.recommendedRounds`. Submitting
while empty currently no-ops silently via `Number.isInteger(NaN)`; it
now cannot be empty.

**Frontend, flicker.** Three distinct causes, all addressed:

1. `useTournament` has **no `placeholderData: keepPreviousData`** —
   unlike every other query in the app (`features/cubes/api.ts`,
   `features/collection/api.ts`, `shared/cards/api.ts`).
2. The `isPending`/`isError` branches are evaluated **before** `data`,
   so a single failed 10-second poll blanks the section even though the
   cache is warm — `NotFoundError` returns `null` (section vanishes,
   reappears next poll) and other errors replace it with a red line.
   `TournamentPanel` has the same shape.
3. `isPending → null` means a hard refresh pops the section in with a
   layout shift, and it waits on a *second* query (`useEventStatus`)
   resolving before `relevant` is true, so it appears in two steps.

Fix: `keepPreviousData`, render from `data` first with `error` checked
last, and a skeleton instead of `null`.

Explicitly **not** doing: global `QueryClient` defaults (`staleTime`
etc.). `main.tsx` constructs a bare `new QueryClient()`, and changing
that would alter refetch behaviour app-wide to fix a localised bug.

### PR 7 — Drop the drawn-games input (#8)

A separate draws field expresses no extra information: a drawn match is
either 1-1 in games or 0-0 (termination in game one), and both are
already read as a draw by `p1Games == p2Games`.

- The wire request drops `draws`; the server writes `0`.
- **The DB column stays** — no migration — so existing results keep
  computing identically in `swiss/standings.go` and `swiss/pair.go`.
- `ResultForm` becomes two 0–2 fields, still rejecting 2-2, with equal
  values rendering a "recorded as a draw" hint so the semantics stay
  visible now that the explicit field is gone.
- `0-0` is already valid and already division-by-zero-safe (`gwRaw`
  guards `games == 0`), but gets a standings test since the UI can now
  reach it easily.

### PR 8 — Result disputes, organizer lock, discard guard (#5, #7)

Results are overwritten in place today: `UpdateMatchResult` is
last-writer-wins, and `reported_by` is not even exposed on the wire, so
an overwrite leaves no trace for the organizer to see.

#### Append-only report history

Migration adds
`match_result_reports (match_id, reported_by, is_organizer, p1_games, p2_games, reported_at)`.
`ReportResult` inserts a row and *then* updates the match exactly as it
does now — current last-wins behaviour for the authoritative result is
preserved.

#### Organizer lock

Once the organizer reports a match, no ordinary player can overwrite it:
"let players do the work until the owner steps in". Derived from the
same table, so no new column —
`exists(… where match_id = ? and is_organizer)`. In `ReportResult`,
`!admin && hasOrganizerReport` returns the existing `ErrResultLocked` →
409 `result-locked`, which the frontend already maps. The player's form
is replaced by "the organizer recorded this result"; the organizer stays
free to re-report.

Admins are organizers globally, so an admin who is also playing has
their own report treated as authoritative. Accepted — this app serves
one small local community and needs no moderation tiers.

#### Two dispute signals

Both derived from `match_result_reports`; still no dispute state column
to keep in sync.

```
latest_per_player = one row per reporting player, most recent wins
disputed   = count(distinct score in latest_per_player) > 1
             AND no organizer report exists
hadDispute = latest_per_player scores ever disagreed   (sticky)
```

- **`disputed`** — live, shown to players and organizer, and it
  **clears when the players agree**. Ann reports 2-1, Bob reports 1-2 →
  disputed. Bob corrects to 2-1 → latest-per-player agree → badge
  clears, and the stored result stays consistent because last-write-wins
  and the last write *is* the agreeing correction. Deriving from all
  reports rather than latest-per-player would brand the match disputed
  forever; that is why the rule is per-player.
- **`hadDispute`** — sticky and **organizer-only**, rendered as a quiet
  "resolved after disagreement" marker. A player who claims a win and
  then quietly backs down is exactly the cheating pattern the finding
  asks us to catch, and a badge that vanishes would hide it.

`TournamentMatchInfo` gains `disputed`, `hadDispute` and `reports[]`.
Players additionally see a one-line "your opponent reported a different
result"; the organizer panel expands the history with each score, who
reported it, and when.

#### Discard guard (#7)

`TournamentPanel` holds a single-slot `editingMatch`, so opening another
row silently discards unsaved edits. `ResultForm` reports dirty state
up; the panel intercepts opening a different row while dirty and raises
the shared confirm from PR 5 — "Discard the unsaved result for table N?".

## Wave 4 — cubes and collection

### PR 9 — Shared card-list import, and cube bulk import (#9)

The commit side already exists: `POST /cubes/{cubeId}/changes` accepts
up to 1000 adds. What is missing is *resolution and review*, which
exists but is trapped inside the collection feature — and
`features/cubes` may not import `features/collection`
(`docs/architecture/structure.md` rule 1).

**Backend:** parse + resolve move from `internal/collections` to
`internal/cards`, and the endpoint is re-pointed to
`POST /api/cards/resolve-list`, deleting
`POST /api/collection/import/resolve`. This is pure name → printing
resolution touching only `cards` tables; leaving it in place would
permanently serve a cubes feature from a `/api/collection/` URL. Cost is
one generated-client churn on a single-consumer API — accepted to avoid
the organisational debt.

**Frontend:** the dialog is promoted to
`shared/cards/CardListImportDialog` with an `onApply(items)` callback.
Collection keeps its existing commit mutation. The cube editor feeds
resolved items into its client-side pending diff, so an import is
reviewed and committed as an ordinary change with a note — no new commit
path, and cube history stays honest.

**Cube creation** gains an optional paste textarea. Flow: resolve (which
is side-effect-free) → create the cube → navigate to the editor with the
resolved items staged as change #1. The mechanism for carrying items
across that navigation is an implementation detail to settle against the
editor's reducer.

**Drive-by parse fix:** `1996 World Champion` currently parses as
quantity 1996, blows the 999 cap, and is reported as unmatched. A
numeric prefix above 999 followed by a non-empty remainder should be
treated as part of the name.

### PR 10 — Set-aware wantlist (#11)

`cube_cards` is keyed `(cube_id, oracle_id)` with a single `scryfall_id`
per row, so "the printing used in the cube" is unambiguous and this is a
clean change. `collection_items` is already printing-level
(`(user_id, scryfall_id)`), so both modes are a `group by` swap.

**Backend:** `GET /cubes/{cubeId}/wantlist?match=oracle|printing`,
defaulting to `oracle` — today's behaviour, unchanged for existing
callers. `printing` mode swaps the ownership subquery from
`group by oracle_id` to `group by scryfall_id`, joined on the cube's
exact printing, so owning Lightning Bolt (LEB) no longer satisfies a
cube slot that calls for (MM2).

`WantlistEntry` gains `setCode`, `setName` and `collectorNumber` — all
already available from the existing `cards` join.

**Frontend:** `WantlistPage` gets a Set column and a two-option mode
toggle held in the URL via `validateSearch` (shareable, survives
refresh).

Two download buttons, because the existing one must not break:

- **"Download for Cardmarket"** — byte-identical to today,
  `<qty> <name>`. Cardmarket's Wants import accepts only that grammar, so
  appending set codes here risks silently breaking a working import.
- **"Download with sets"** — new, `<qty> <name> (SET)`, the format
  Moxfield and Archidekt accept.

Until PR 11 lands, our own list importer parses quantity + name only, so
the set-annotated export does not round-trip back into the app. PR 11
closes that loop.

### PR 11 — Set and printing selectors in the import grammar (#14)

Today an import line is quantity + name, and resolution picks a
*representative* printing per oracle card
(`distinct on (oracle_id) … order by oracle_id, promo, released_at desc`).
A user who wants Urza's Mine (ATQ) 83a specifically — one of four
variants in Antiquities — must import the list and then click through
each card in the printing picker afterwards. For a bulk import that is
exactly the manual work bulk import is supposed to remove.

#### Grammar

```
[<qty>[x]] <name> [(<set>)] [<collector-number>]
```

Both new selectors are optional and independent of quantity, so every
list that parses today keeps parsing identically.

- **Set:** parenthesized, 2–5 characters of `[a-z0-9]`, matched
  case-insensitively. Our mirror stores Scryfall's `set` verbatim, which
  is lowercase, so both sides are lowered before comparison.
- **Collector number:** accepted **only** when a set was given (a bare
  trailing number is far more likely to be part of a name). Charset
  `[a-z0-9★†-]`, case-insensitive, covering `83a`, `123`, `★12` and
  hyphenated promo numbers.

This is deliberately the Moxfield / Archidekt convention
(`1 Lightning Bolt (LEB) 123`), which buys two things beyond the
feature itself: lists exported from those tools paste in directly, and
PR 10's "Download with sets" export round-trips back into the app.

**Names that contain parentheses** are the one real hazard — they exist
(`B.F.M. (Big Furry Monster)`, `Erase (Not the Urza's Legacy One)`). Two
guards, in order:

1. The parenthesized token must satisfy the set-code charset and length
   above. Both examples fail it on length alone, so the parenthesis stays
   part of the name.
2. Belt and braces: if a set-aware interpretation resolves to nothing,
   the line is retried with the **entire raw line as a bare name** before
   being reported unresolved. So no pathological name can be made
   unimportable by the new syntax.

#### Resolution ladder

Per line, first match wins:

| Input | Resolution | Miss |
|---|---|---|
| set + collector number | exact printing | `printing_not_found`, suggesting every printing of that name |
| set only | printings of that name in that set — one → matched; several (the Urza's Mine case) → `ambiguous` with those variants as the suggestions | `printing_not_found` |
| neither | today's behaviour: oracle representative → `ambiguous` → per-line fuzzy → `unmatched` | unchanged |

New line status **`printing_not_found`**: the name is real but the
requested printing is not. Distinguishing it from `unmatched` matters —
the fix is picking a different printing, not correcting a typo, and the
suggestions list already gives the user that choice inside the review
dialog. `ambiguous` reuses the existing per-line `<select>` UI
unchanged, so variant disambiguation needs no new component.

**Determinism does not depend on `(set_code, collector_number)` being
unique.** Scryfall's `default-cards` bulk file is one object per
printing, so in practice it is, and there is no unique index backing it.
Rather than assume, every new lookup carries the same
`order by promo, released_at desc, (image_small is null)` tie-break the
existing queries use, so a duplicate pair resolves stably instead of
arbitrarily.

#### Query shape

Resolution must stay batched — 500 lines cannot become 500 round trips.
Three constant-count batch queries replace the current one, plus the
existing per-line fuzzy fallback for lines that reach it:

1. `GetCardsByNormalizedNames` — existing, for lines with no set.
2. New: batch lookup over `(set_code, collector_number)` pairs, via
   `unnest` of two arrays.
3. New: batch lookup over `(normalized_name, set_code)` pairs, returning
   **all** matching printings so variants can surface as `ambiguous`.

`GetPrintingsByOracleID` is reused as-is for `printing_not_found`
suggestions.

#### Wire and consumers

`ResolvedLine` gains the parsed `setCode` and `collectorNumber` echoes
(so the review dialog can show what it understood) and the new status.
When a selector was given, `match` is the **exact** printing rather than
the oracle representative.

No consumer-side commit changes are needed, which is what makes this
cheap: collection items are already keyed per printing
(`(user_id, scryfall_id)`), and `POST /cubes/{cubeId}/changes` already
takes `{scryfallId, quantity}` per add. So importing
`1 Urza's Mine (ATQ) 83a` into a cube sets 83a as that cube card's
chosen printing at import time — the manual pass through the printing
picker disappears for both features at once, because PR 9 already made
resolution shared.

Tests: table-driven parser cases for every grammar branch and both
paren-name guards; integration tests per ladder row, including the
four-variant Antiquities case and a `printing_not_found` with
suggestions.

## Cross-cutting

- New message keys in `messages/en.json` **and** `pl.json` for every new
  string; the Paraglide compiler enforces parity.
- `make api-generate` on PRs 5, 6, 8, 9, 10, 11.
- Frontend: vitest + RTL, an axe smoke test per changed screen
  (`// @vitest-environment jsdom`), and 360px verification per rule 9.
- Backend: table-driven service tests plus testcontainers integration
  tests for every new or changed endpoint. PR 5's money matrix (cases
  A–G) is covered case by case, including the C2 reclaim guard and the
  case G lock ordering.
