# Card preview sheet + backlog batch (#8, #9, #13, #18) — Design

Date: 2026-08-04
Status: approved (brainstorm 2026-08-04)

## Goal

Close four backlog issues from the 2026-07-18 triage:

1. **#18** — expose per-card `colors` on `CardSummary` so pending-add
   previews stop mis-bucketing modal DFCs.
2. **#9** — add an EDHREC popularity signal to card search so "bolt"
   surfaces Lightning Bolt.
3. **#13** — "Add to calendar" on the event detail page (Google Calendar
   link + `.ics` download).
4. **#8** — touch-reachable card previews: card rows open a real card
   inspector (dialog on desktop, bottom sheet on mobile) instead of being
   focusable no-op buttons.

Delivery: **four PRs by concern**, sequential branches off master.
Order: #18 → #9 → #13 → #8 (smallest first; all four are independent).

## PR 1 — #18: `colors` on CardSummary

Backend:

- `AutocompleteCards` (backend/internal/db/queries/cards.sql) additionally
  selects `colors`.
- `CardSummary` (backend/internal/platform/httpapi/cards.go) gains a
  **required** `colors: string[]` field (empty array = colorless, so the
  field is always present — no nullability).
- `make api-generate` to refresh the frontend client.

Frontend:

- `grouping.ts` uses `card.colors` directly; the mana-cost-derived color
  fallback (0676527) is **deleted entirely** — after the API change no
  card lacks the field. Valki // Tibalt and other transform/modal DFCs
  preview in their saved (both-faces-union) color bucket.
- Remove the fallback-documenting comment in `grouping.ts`.

Tests: backend autocomplete test asserts the new field; grouping unit
tests feed `colors` and drop the fallback cases.

## PR 2 — #9: search popularity signal

Backend only.

- Migration: nullable `edhrec_rank integer` on `cards` **and**
  `cards_staging`.
- Import pipeline carries it through: `scryfallCard` subset struct in
  `transform.go` reads `edhrec_rank`, staging COPY and
  `UpsertCardsFromStaging` include the column.
- Ranking — both `AutocompleteCards` and `SearchCards` insert
  `edhrec_rank asc nulls last` **after** `word_similarity` and **before**
  `similarity`:
  - Autocomplete: prefix-match tier → `word_similarity` →
    `edhrec_rank asc nulls last` → `similarity` → `name`.
  - Search: `word_similarity` → `edhrec_rank asc nulls last` →
    `similarity` → `name`.
  - Rationale: a one-word query like "bolt" gives `word_similarity = 1.0`
    to every card containing the word "bolt"; today the tie breaks toward
    short names. With the rank inserted, the tie breaks toward popularity
    (low rank = popular), so Lightning Bolt tops the list. Unranked cards
    (tokens, brand-new sets) sort last within their similarity tier.
- Rollout note: ranks are NULL until the next Scryfall sync after deploy;
  search behaves exactly as today until then. No manual backfill needed.

Tests: integration test with seeded ranks asserting the
"bolt → Lightning Bolt before other bolt-cards" ordering, and that
NULL-rank cards sort after ranked ones.

## PR 3 — #13: Add to calendar

Frontend only — no backend change. Rationale: event endpoints are
auth-gated so subscription URLs are impossible anyway, and the detail
page already has every field needed.

- Pure helpers in `features/events` (own module, unit-testable):
  - `googleCalendarUrl(event)` — builds
    `https://calendar.google.com/calendar/render?action=TEMPLATE` with
    `text`, `dates` (UTC `YYYYMMDDTHHMMSSZ/…Z`), `details`, `location`.
    Opens the pre-filled webapp on desktop; Android/iOS devices with the
    Google Calendar app deep-link into the app.
  - `buildIcs(event)` — minimal `VCALENDAR`/`VEVENT`: `PRODID`,
    `VERSION:2.0`, `UID` = `event-<id>@cubeplanner.pl`, `DTSTAMP`,
    UTC `DTSTART`/`DTEND`, `SUMMARY`, `DESCRIPTION`, `LOCATION`.
    Correct text escaping (`\` `;` `,` newlines), CRLF line endings,
    75-octet line folding.
  - Duration: **fixed 4 hours** from `startsAt` (events store no end
    time; adjudicated 2026-08-04).
- UI on `EventDetailPage`: an "Add to calendar" row with two buttons —
  **Google Calendar** (template URL, new tab) and **Apple Calendar
  (.ics)** (Blob download; also covers Outlook desktop and anything else
  speaking iCalendar). Hidden for `cancelled` and `finished` events.
  Strings in en + pl via Paraglide.

Tests: unit tests for both builders (date formatting, escaping, folding,
URL params); RTL test that the buttons render for a published event, are
absent for cancelled/finished, and trigger the right href/download.

## PR 4 — #8: card preview sheet

New shared component `CardPreviewSheet` in `shared/cards/`:

- **Responsive shell**: centered `Dialog` at ≥ Tailwind `md` (768 px),
  bottom `Drawer` (`side="bottom"`) below it. Both primitives already share the
  native `<dialog>` + `showModal()` foundation (focus trap, Esc,
  backdrop). Shell choice via a viewport media query — a small
  `useMediaQuery` hook if none exists yet, or pure CSS if the plan finds
  the two shells stylable as one element.
- **Content** = what the `/cards` search result shows: card image (front
  + back when present) plus details, and context-dependent actions.
  Desktop dialog: two columns (image left, details/actions right).
  Mobile sheet: single scrollable column (Drawer already caps at
  `85svh`).
- **Data**: rows don't all carry full detail (pending adds are
  `CardSummary`), so the sheet fetches `CardDetail` by `scryfallId` on
  open via TanStack Query — long `staleTime` (card data only changes on
  Scryfall sync), so open→close→reopen serves from cache with no round
  trip; loading state only on cold cards. Resolved at planning: reuse the
  existing printings query (`useCardPrintings(oracleId)`, same cache key
  `PrintingPickerDialog` uses) and select the row's printing by
  `scryfallId` — no new endpoint needed.
- **Triggers**: card rows everywhere the inert pattern exists —
  - `GroupedCardList` (used by the cube view page only; the edit page's
    `EditableCardList` rows already have real actions and are unaffected):
    the existing focusable button gets an `onClick` opening the sheet,
    fixing the announced-actionable-but-inert pattern (same resolution as
    the PrintingPickerDialog fix, 5f7de8d). `CardHoverPreview` stays for
    pointer hover/focus.
  - Collection and Wantlist rows: the card-name `<span>` becomes a real
    button opening the sheet (today the preview is unreachable on touch
    there too).
- **Change printing** (adjudicated 2026-08-04): the sheet shows a
  **Change printing** button **only on the `/cubes/$cubeId` view route,
  and only when the signed-in user owns that cube** — a quick printing
  fix without entering edit mode. It opens the existing
  `PrintingPickerDialog` (native `<dialog>` stacks fine on top of the
  sheet) and reuses the existing change-printing mutation.
  - `/cubes/$cubeId/edit` already has an inline change-printing
    affordance and its `EditableCardList` rows have real actions → the
    sheet is not wired there at all (pending adds included).
  - Collection and wantlist sheets are info-only.
- Strings in en + pl via Paraglide; a11y per
  `docs/architecture/structure.md` conventions (axe test opts into
  jsdom).

Tests: RTL — activating a row opens the sheet with the fetched detail;
Esc/backdrop closes; change-printing button visible only for the owner
on the cube view route (absent on edit/collection/wantlist); axe pass on
both shells.

## Out of scope

- Popularity signal beyond `edhrec_rank` (no play-rate blending, no
  recency boost) — tracked as #49.
- Calendar subscription feeds (`webcal://`) — impossible while event
  reads are auth-gated.
- End-time field on events (4h constant is a display default, not
  schema).
- Any printing-change affordance in collection/wantlist contexts.
