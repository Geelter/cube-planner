# Mobile Responsive Support — Design

**Date:** 2026-07-19
**Status:** Approved

## Problem

The app will be used by players on phones during active events, but the
frontend is desktop-only in practice: the header packs 7+ items into one
row and overflows well before 640px, StandingsTable is a 6-column table
with nowhere to scroll, dialogs go edge-to-edge, and touch targets are
as small as 32px. Only 8 Tailwind breakpoint usages exist in the entire
codebase. There is also no standing rule requiring new components to
work on mobile, so the gap will keep reopening.

## Goals

1. Every existing screen works on a 360px-wide viewport.
2. Mobile display becomes a binding acceptance criterion for all future
   components, encoded in `docs/architecture/structure.md`.

## Decisions (adjudicated with Mateusz)

| Question | Decision |
|---|---|
| Scope | **Everything equal** — all ~20 screens get full mobile treatment, including authoring/organizer screens |
| Mobile navigation | **Hamburger drawer** — brand + theme toggle stay visible; nav links, account actions, language switcher collapse into a slide-in drawer |
| Wide tables | **Scroll wrapper** — `overflow-x-auto` container around the table; no column hiding, no card transformation |
| Enforcement | **Rule + manual check** — structure.md rule verified at dev/review time; no new CI infra |
| Work organization | **Convention-first sweep** — rules + shared pieces first, then a feature-by-feature sweep with plain Tailwind breakpoint classes |

## 1. The standard (new rule in `docs/architecture/structure.md`)

A new numbered rule under "The rules," binding for review like the rest:

- **Support floor: 360px viewport width.** No horizontal page scroll at
  ≥360px. Intrinsically wide content (tables) scrolls inside its own
  `overflow-x-auto` container.
- **Mobile-first Tailwind.** Base classes are the phone layout;
  `sm:`/`lg:` layer desktop on top.
- **Touch targets.** Interactive controls on player-facing flows are
  ≥44px tall or have equivalent hit area; never two small targets
  adjacent without a gap.
- **Use the established patterns.** Nav = the app drawer; wide tables =
  scroll wrapper; dialogs = `shared/ui/dialog.tsx` (handles mobile
  sizing); forms = single-column `max-w-md`.
- **Acceptance criterion.** Every new screen/component is verified at
  360px and desktop width before PR — same footing as the axe smoke
  test convention.

## 2. Shared pieces (built once, up front)

### Nav drawer

- Desktop header unchanged at `md:` and up.
- Below `md:`: brand + theme toggle + hamburger button.
- New domain-blind primitive `shared/ui/drawer.tsx`: native `<dialog>`
  (same foundation as `dialog.tsx`) styled as a right-side slide-in
  panel, so focus trapping, Escape, and backdrop dismissal come free.
- Root layout composes it with nav links, account actions (My cubes,
  Collection, Account, Logout / Login), and the LanguageSwitcher.
- Drawer items are `h-12` full-width tap rows.
- Closes on route navigation.
- New Paraglide messages (en + pl) for the menu button and drawer
  labels; RTL + axe tests for the drawer.

### Dialog mobile sizing

`shared/ui/dialog.tsx` currently renders `w-full max-w-lg` —
edge-to-edge on phones. Add inset margin and `max-h-[85svh]` with
internal scroll. ImportDialog and all future dialogs inherit this.

### Button `lg` size

Add `lg: h-11` to the button cva size config for touch-critical actions
(result entry, quantity steppers). Existing sizes untouched.

### Table scrolling

A documented pattern, not a component: `<div className="overflow-x-auto">`
around the `<table>`. One line; not worth abstracting.

## 3. The sweep (all screens)

Feature-by-feature audit applying the conventions. Known concrete fixes
from code reading; every screen additionally gets a 360px verification
pass, since more issues will surface:

- **tournaments** — StandingsTable gets the scroll wrapper; the round
  `role="tablist"` strip becomes horizontally scrollable (many rounds
  overflow); ResultForm outcome buttons move to `lg` size (players tap
  these mid-match).
- **collection** — QuantityStepper buttons get ≥44px hit area;
  ImportDialog inherits the dialog fix; the page grid
  (`grid sm:grid-cols-2`) is already responsive.
- **cubes** — editor already stacks below `lg` (PendingChangesPanel
  below the card list — keep that; no sticky panel);
  GroupedCardList/EditableCardList already collapse to one column
  (`columns-1 sm:columns-2`); verify browser, history, settings, create
  pages.
- **events** — EventDetail `<dl>` and RegistrationsTable largely
  already wrap; verify EventForm, ManageEventPage, EventCubesEditor,
  RegistrationPanel.
- **cards / auth** — single-column `max-w-md` forms, mostly fine;
  verify combobox dropdown width on narrow screens.

## 4. Verification & testing

- During implementation, each screen is verified at 360px and 1280px in
  a real browser (Playwright resize + screenshot) against the running
  dev stack — the mechanical version of the manual check the rule
  prescribes.
- New unit tests only where there is behavior: drawer open/close,
  Escape, close-on-navigate, axe smoke test (jsdom). No
  classname-assertion tests — happy-dom does no layout, so they would
  only check that class strings exist.
- No new CI infrastructure.

## Error handling

No new failure modes: the drawer is client-only UI; everything else is
CSS. The drawer must not trap focus when closed and must restore focus
to the hamburger button on close (native `<dialog>` behavior).

## Out of scope (noted for future issues)

- PWA / offline support
- Bottom tab bar navigation
- Per-table responsive column hiding
- Sticky pending-changes panel in the cube editor
