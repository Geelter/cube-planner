# Uniform button loading + backlog batch (#28, #24, #19, #17) — Design

Date: 2026-08-02
Status: approved (brainstorm 2026-08-02)

## Goal

Two threads, one batch:

1. Extend the Button `loading` prop (added for auth pages in the ux-polish
   project, PR #42) to **every** button that triggers a network request, so
   visual feedback is uniform across the app.
2. Close four backlog issues: #28 (bottom tab bar), #24 (touch sizes),
   #19 (printing picker), #17 (ARIA tabs follow-ups).

Delivery: **four PRs by concern**, sequential branches off master.

## PR 1 — Uniform button loading feedback

Convention: any `Button` whose activation fires a network request passes
`loading`. Three call-site shapes:

1. **Single-action buttons and form submits** — `loading={mutation.isPending}`.
   Known gaps include: header + drawer logout (`__root.tsx`), tournament
   drop/undrop and drop-confirm dialog (`TournamentSection`), registration
   actions (`RegistrationPanel`, `RegistrationsTable`), event management
   (`ManageEventPage`, `EventCubesEditor`, `NewEventPage` via `EventForm`),
   cube save/delete (`PendingChangesPanel`, `PendingChangesBar`,
   `CubeSettingsSection` delete), collection import, wantlist actions.
   The implementation plan enumerates the exact list.
2. **Row-scoped buttons** (one shared mutation, many rows) — only the clicked
   row spins. Use the pattern CollectionPage already established:
   `const pendingId = mutation.isPending ? mutation.variables.<key> : null`,
   then `loading={pendingId === row.<key>}`. Other rows stay enabled.
   No helper hook unless the plan finds 4+ copies of nontrivial wiring.
3. **Pagination prev/next** — paged queries use `keepPreviousData`, so today
   a page click gives zero feedback. Pass
   `loading={query.isFetching && query.isPlaceholderData}` (both buttons spin
   briefly). Applies to Collection, CubeBrowser, EventsList, CubeHistory,
   Wantlist, and any other paged list.

Out of scope: buttons that only change local state (filters, group-by,
opening dialogs/drawers). `asChild` link-buttons (navigation) keep ignoring
`loading` by design.

Document the convention in `docs/architecture/structure.md` (one rule with
the three shapes).

Testing: extend existing component tests where a mutation button is already
covered — assert `aria-busy`/disabled while pending (pattern from the auth
page tests). No snapshot churn.

## PR 2 — a11y pair: #24 touch sizes + #17 ARIA tabs

### #24 close-affordance touch sizes

- `shared/ui/dialog.tsx` ✕: `size="sm"` → `size="icon"` +
  `className="size-11"` (established override pattern; visual glyph size
  unchanged).
- `shared/ui/theme-toggle.tsx`: same treatment (36px → 44px).
- While in the area: verify focus outlines are not clipped by the tournament
  round tablist's `overflow-x-auto` container. If clipped, give the tablist
  internal breathing room (e.g. `p-0.5 -m-0.5` or scroll-padding), do not
  remove the overflow behavior.

### #17 tournament round tabs

- **Stale tab root cause:** mount `TournamentSection` with `key={eventId}`
  at the call site (`EventDetailPage`), so all per-tournament state (`tab`,
  `confirmDrop`) resets when the event changes. The existing in-component
  fallback (`?? rounds[rounds.length - 1]`) stays as defense.
- **Home/End keys** in the tablist keydown handler: jump focus + selection
  to first/last round, per the WAI-ARIA APG tabs pattern.
- **Tabpanel linkage:** wrap the round content (match list) in
  `role="tabpanel"` with `id`/`aria-labelledby` tied to the active tab, and
  `aria-controls` on tabs. Keep the existing roving tabindex.

Testing: extend `TournamentSection.keyboard.test.tsx` (Home/End, tabpanel
roles); a11y test files stay jsdom.

## PR 3 — #19 per-entry printing picker

Issue #19 says "API side is ready" — true only for `/cards` and collection.
The cube commit API cannot change an existing entry's printing: the same
oracle is rejected on both diff sides, and add-onto-existing deliberately
keeps the old printing. So this PR has a small backend addition.

### Backend

New endpoint `POST /api/cubes/{cubeId}/cards/{oracleId}/change-printing`,
owner-only, body `{ scryfallId }` (the new printing):

- In-place `cube_cards.scryfall_id` update. **No version bump, no changelog
  entry** — printing is auxiliary/cosmetic information; diffs and replay are
  oracle-keyed, so concurrent commits stay valid and history replay showing
  the current printing for still-present cards is acceptable (decided
  2026-08-02).
- Semantics and error taxonomy mirror the collection's `change-printing`
  and the cube commit's invalid-change handling: 404 unknown cube / private
  non-owner, 403 public non-owner, 422 (`invalid-cube-change`) when the
  oracle is not in the cube, `scryfallId` is not a printing of that oracle,
  or it is already the current printing.
- `make api-generate` regenerates the frontend client.

Testing: table-driven service tests + endpoint coverage in the
testcontainers integration suite, mirroring the collection analog.

### Cube editor (`EditableCardList`)

- ⇄ button per entry (same affordance and `aria-label` message shape as
  collection rows) opening the shared `PrintingPickerDialog`.
- **Saved entries:** picking fires the new mutation immediately (separate
  from the pending diff, like `CubeSettingsSection`), then invalidates cube
  cards. Per-row loading per PR 1's pattern.
- **Pending-only adds:** picker updates the pending add's `scryfallId`
  client-side (new `setPrintingForAdd`-style reducer action); nothing hits
  the network until save. The commit API already accepts any printing on
  adds.

### /cards details (`CardSearchPage` → `SelectedCardPanel`)

- Currently always renders `printings[0]`. Add local selected-printing state
  plus a change-printing affordance opening the same dialog. Client-side
  only; nothing to persist. Selection resets when the selected card changes
  (existing `key={scryfallId}` remount).

## PR 4 — #28 bottom tab bar (mobile)

- Fixed bottom bar below `md:`, four tabs: Cards, Cubes, Events, Collection.
  Text labels only for now — real icons deferred to a future UX consult
  (decided 2026-08-02). TanStack `Link` active styling, ≥44px targets,
  semantic color tokens, `env(safe-area-inset-bottom)` padding.
- Visible for guests too; Collection's route guard already redirects to
  login. Simpler than conditional tabs.
- Drawer slims down to secondary items: My Cubes, account, login/logout,
  language. Cards/Cubes/Events links leave the drawer (the bar covers them).
  The `md:` top nav is unchanged.
- **Collisions:** root layout `<main>` gets bottom padding below `md:` so
  content never hides under the bar; the cube editor's fixed
  `PendingChangesBar` moves up to sit above the tab bar on mobile (its
  `pb-24` spacer adjusts accordingly); the bottom-sheet drawer must open
  above the bar (z-order and offset checked).
- New Paraglide messages only if labels differ from existing `nav_*` keys
  (they should not — reuse).

Testing: RTL coverage for tab rendering/active state and guest visibility;
a11y check that the bar is a `<nav>` with an accessible name distinct from
the top nav.

## Out of scope

- Icons for the tab bar (future UX consult).
- Changelog-tracked printing changes (rejected: heavy for no user benefit).
- Any tournament-engine 5b work.
