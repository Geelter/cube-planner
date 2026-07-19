# Mobile UX Follow-ups: #30, #25, #23 — Design

Date: 2026-07-19
Issues: [#30](https://github.com/Geelter/cube_planner/issues/30),
[#25](https://github.com/Geelter/cube_planner/issues/25),
[#23](https://github.com/Geelter/cube_planner/issues/23)
Status: approved

Three small follow-ups from the mobile-responsive project (PR #22, merged
2026-07-19), batched on one branch. Ordered #25 → #23 → #30 so the overlay
token lands before the bottom sheet that uses it.

## #25 — Semantic overlay token for dialog/drawer backdrops

Pure refactor, zero visual change.

- In `frontend/src/app/styles.css`, add `--overlay` to **both** theme blocks
  (`:root` and `[data-theme="dark"]`) with the same value the backdrops use
  today: black at 50% opacity (e.g. `--alpha(var(--color-black) / 50%)`).
- Map it in `@theme inline`: `--color-overlay: var(--overlay)`.
- Switch `shared/ui/dialog.tsx` and `shared/ui/drawer.tsx` from
  `backdrop:bg-black/50` to `backdrop:bg-overlay`.

Identical value in light and dark for now; a heavier dark scrim later is a
one-line theme edit. This removes the last raw palette color from the UI
layer.

## #23 — Drawer stays open when tapping the current route's nav link

The nav drawer's close effect in `frontend/src/routes/__root.tsx` keys on
`pathname`; tapping the link for the route you are already on changes
nothing, so the drawer silently stays open.

Fix: keep the `pathname` effect (still covers browser back/forward while
the drawer is open) and add one **delegated click handler** on the Drawer's
content in `__root.tsx`: if `e.target.closest("a")` is truthy, call
`setMenuOpen(false)`. One handler covers all seven links and any future
ones; the logout button keeps its existing flow (mutation → redirect →
pathname change closes the drawer).

Rejected alternative: keying the effect on `location.state.key` — a
same-route `<Link>` click that no-ops does not reliably push new history
state, and the delegated click is direct and testable.

Interaction with the a11y focus effect (also keyed on `pathname`): on a
same-route tap the pathname is unchanged, so no focus move fires; the
native `dialog.close()` restores focus to the hamburger opener, which is
the correct behavior for a navigation that went nowhere.

## #30 — Sticky pending-changes bar in the cube editor on mobile

On mobile the editor's `PendingChangesPanel` stacks below the card list
(the side-by-side breakpoint is `lg:`, not `md:` as the issue guessed), so
accumulated changes and Save are out of reach while editing a long list.

### Component: `Drawer` grows a `side` prop

Generalize `shared/ui/drawer.tsx` with `side?: "right" | "bottom"`
(default `"right"`) instead of adding a new BottomSheet primitive. Same
native `<dialog>` foundation — focus trap, Esc + backdrop dismiss, focus
restoration — with `side="bottom"` swapping only positioning classes:
pinned to the viewport bottom, full width, `max-h-[85svh]` with internal
scroll, rounded top corners. Backdrop uses the new `bg-overlay` token.

### Cube editor wiring (all below `lg:`; desktop unchanged)

- In-flow `PendingChangesPanel` becomes `hidden lg:flex`.
- New `PendingChangesBar` in `features/cubes/components/`, rendered by
  `CubeEditorPage` only while `pendingCount > 0` (**hidden until dirty**;
  it disappears again after save/discard). Positioning:
  `fixed bottom-0 inset-x-0 lg:hidden` with safe-area padding
  (`pb-[env(safe-area-inset-bottom)]`).
- Bar content:
  - a summary button — visually `+3 −1`, with a translated aria-label
    ("3 additions, 1 removal — review changes"), `aria-haspopup="dialog"`
    and `aria-controls` — that opens the bottom sheet;
  - a **Save** button that commits directly via the existing `save()`
    (note is optional; usually empty on the quick path).
- The bottom sheet (`Drawer side="bottom"`) renders the existing
  `PendingChangesPanel` unchanged — undo lists, note field, Save/Discard
  all reuse current logic. If the aside's border/rounded styling looks
  doubled-up inside the sheet, strip it via a `variant` prop rather than
  forking the component.
- Page content gets bottom padding below `lg:` while the bar is visible so
  the bar never covers the last card rows.
- Save success already navigates to the cube page (existing behavior),
  which unmounts bar and sheet; discard resets pending state → count 0 →
  bar hides.

### i18n

New Paraglide messages (en + pl) for the summary aria-label and any bar
labels. The `+N −N` numerals are symbolic and not translated.

## Testing

- `drawer.test.tsx`: `side="bottom"` renders, dismisses (Esc, backdrop,
  ✕) like the right-side drawer.
- Cube editor RTL tests: bar hidden at count 0, appears when dirty, tap
  opens sheet showing panel content, bar Save triggers commit, discard
  hides bar.
- `__root` / drawer nav test: tapping a drawer link for the current route
  closes the drawer.
- Existing conventions: happy-dom default, axe files opt into jsdom.

## Delivery

Branch `feature/mobile-followups` off fresh `master`; three commits in
order #25 → #23 → #30, each footered `Fixes #N`; one PR, CI green before
merge (master is protected).
