# Uniform Button Loading + Backlog Batch (#28, #24, #19, #17) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Uniform `loading` feedback on every request-triggering button, plus four backlog issues: #24 touch sizes, #17 ARIA tabs, #19 printing picker (needs one new backend endpoint), #28 mobile bottom tab bar.

**Architecture:** Four PRs on stacked branches. PR 1 is a mechanical frontend pass wiring the existing `Button loading` prop (single-action, row-scoped via `mutation.variables`, pagination via `isFetching && isPlaceholderData`). PR 2 is two a11y fixes. PR 3 adds a cosmetic no-changelog `change-printing` endpoint to the cube backend and reuses the shared `PrintingPickerDialog` in the cube editor and /cards details. PR 4 adds a fixed bottom tab bar below `md:` and slims the drawer.

**Tech Stack:** Go 1.25 + huma v2 + sqlc v1.31.1 (pgx/v5) + testcontainers; React 19, TanStack Router/Query, Tailwind v4 semantic tokens, Paraglide i18n, vitest + RTL (happy-dom; a11y files jsdom).

**Spec:** `docs/superpowers/specs/2026-08-02-loading-and-backlog-batch-design.md`

## Global Constraints

- `docs/architecture/structure.md` is binding: dependency direction `app`/`routes` → `features` → `shared`, semantic color tokens only, no hardcoded user-facing strings (`m.*()`, en + pl parity), ≥44px touch targets, mobile-first Tailwind, 360px support floor.
- Tooling: oxlint + oxfmt (`pnpm fmt` in `frontend/`), gofumpt + golangci-lint. Never eslint/prettier.
- Generated: never hand-edit `frontend/src/shared/api/` (regen: `make api-generate`), `backend/internal/db/*.sql.go` (regen: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`), `src/routeTree.gen.ts`, `src/paraglide/`.
- Tests: `make frontend-test`, `make backend-test` (needs Docker), `make frontend-typecheck`, `make frontend-lint`, `make backend-lint`. Test files in `src/routes/` need a `-` filename prefix.
- The Button component (`frontend/src/shared/ui/button.tsx`) already implements `loading` (spinner + disabled + `aria-busy`); `loading` on `asChild` is deliberately ignored. Do not modify it.
- Commit messages: conventional commits, `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` trailer.
- Branch/PR flow (master is protected): PR 1 = branch `feature/uniform-loading-and-backlog` (already exists, has the spec + this plan). Each later PR branches off the previous one (stacked). Open each PR against `master`; if the predecessor has not merged yet, note the dependency in the PR body and rebase after it merges.

---

## PR 1 — Uniform button loading feedback

The convention (structure.md rule 6) already mandates `<Button loading={mutation.isPending}>`; the auth pages, `EventForm`, `ResultForm`, `ImportDialog`, `CreateCubePage`, `CubeSettingsSection` save, and `TournamentPanel` upsert already comply. This PR wires the rest and documents the two extra shapes.

### Task 1: Shell logout buttons + convention docs

**Files:**
- Modify: `frontend/src/routes/__root.tsx:89` (header logout), `:156-163` (drawer logout)
- Modify: `docs/architecture/structure.md` (rule 6)

**Interfaces:**
- Produces: the documented convention later tasks follow; no exported code.

- [ ] **Step 1: Wire logout loading**

In `__root.tsx`, header logout button (line ~89):

```tsx
<Button
  type="button"
  variant="outline"
  size="sm"
  loading={logout.isPending}
  onClick={() => logout.mutate()}
>
  {m.nav_logout()}
</Button>
```

Drawer logout button (line ~156): add the same `loading={logout.isPending}` prop (keep its existing `className`).

- [ ] **Step 2: Extend structure.md rule 6**

Replace the final sentence of rule 6 ("Buttons that trigger a mutation … rather than a bare `disabled`.") with:

```markdown
   Buttons that trigger a network request show it in flight via
   `<Button loading>` (disabled + spinner + `aria-busy`) rather than a
   bare `disabled`, in one of three shapes:
   `loading={mutation.isPending}` for single-action buttons and form
   submits; row-scoped for one-mutation-many-rows lists — only the
   clicked row spins, derived from the in-flight variables
   (`const pendingId = mut.isPending ? mut.variables.<key> : null`, then
   `loading={pendingId === row.<key>}`) while other rows stay enabled;
   and `loading={query.isFetching && query.isPlaceholderData}` for
   pagination over `keepPreviousData` queries. Buttons that only change
   local state (filters, opening dialogs) get no loading prop.
```

- [ ] **Step 3: Commit**

```bash
git add frontend/src/routes/__root.tsx docs/architecture/structure.md
git commit -m "feat(shell): logout buttons show request in flight; document loading shapes"
```

### Task 2: Events feature loading pass

**Files:**
- Modify: `frontend/src/features/events/components/RegistrationPanel.tsx`
- Modify: `frontend/src/features/events/components/RegistrationsTable.tsx`
- Modify: `frontend/src/features/events/components/ManageEventPage.tsx`
- Modify: `frontend/src/features/events/components/EventCubesEditor.tsx`
- Test: `frontend/src/features/events/components/RegistrationsTable.test.tsx`

**Interfaces:**
- Consumes: the convention from Task 1. Nothing exported.
- Note: `useRefundRegistration`/`useDenyRefund` mutations take the registration id (a string) as their sole variable, so `mutation.variables` IS the row id.

- [ ] **Step 1: Write the failing per-row test**

In `RegistrationsTable.test.tsx`, make the refund mock's pending state controllable. Replace the `useRefundRegistration` mock line with a mutable state object (same style for deny):

```tsx
const refundState: { isPending: boolean; variables?: string } = { isPending: false };
// in the vi.mock factory:
useRefundRegistration: () => ({ mutate: refundMutate, error: null, ...refundState }),
```

Reset it in the existing `afterEach` (`refundState.isPending = false; delete refundState.variables;`). Add:

```tsx
test("only the acted-on row's refund button spins; other rows stay enabled", () => {
  refundState.isPending = true;
  refundState.variables = "r1"; // the paid row
  renderTable();
  // r1 (paid) and r3 (refund_requested) both render a refund button.
  const refundButtons = screen
    .getAllByRole("button")
    .filter((b) => b.textContent === m.regs_refund());
  const busy = refundButtons.filter((b) => b.getAttribute("aria-busy") === "true");
  expect(busy).toHaveLength(1);
  const idle = refundButtons.find((b) => b.getAttribute("aria-busy") !== "true");
  expect(idle).toBeEnabled();
});
```

Add `import { m } from "@/paraglide/messages";` if the file lacks it. If `getAllByRole` needs the busy (disabled) button included, it already is — RTL's `getAllByRole("button")` returns disabled buttons by default.

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && pnpm vitest run src/features/events/components/RegistrationsTable.test.tsx`
Expected: FAIL — both refund buttons disabled (blanket `disabled={refund.isPending}`), none busy.

- [ ] **Step 3: Implement**

`RegistrationsTable.tsx` — derive pending rows after the hooks:

```tsx
const refundingId = refund.isPending ? refund.variables : null;
const denyingId = deny.isPending ? deny.variables : null;
```

Row refund button: replace `disabled={refund.isPending}` with `loading={refundingId === r.id}`.
Row deny button: replace `disabled={deny.isPending}` with `loading={denyingId === r.id}`.
The dialog's confirm button fires-and-closes; leave it as-is (the row button carries the feedback).

`RegistrationPanel.tsx` — replace `disabled={X.isPending}` with `loading={X.isPending}` on all five buttons: register (~line 54), pay (~71), cancel-pending (~77), leave-waitlist (~93), and the dialog's confirm-cancel (~125; this one stays open while pending, so the spinner is visible).

`ManageEventPage.tsx` — lifecycle buttons are one shared `act` mutation whose variable is the action name; make each button spin only for its own action, and disable the siblings:

```tsx
<Button
  key={a.action}
  type="button"
  size="sm"
  {...(a.action === "cancel" ? { variant: "outline" as const } : {})}
  disabled={act.isPending}
  loading={act.isPending && act.variables === a.action}
  onClick={() => setConfirmAction(a.action)}
>
```

Dialog confirm button (~line 135): replace `disabled={act.isPending}` with `loading={act.isPending}`.

`EventCubesEditor.tsx` — the list updates optimistically (rows appear/disappear immediately), so row remove buttons need nothing; the add button (~line 95) gets `loading={setCubes.isPending}` (keep `disabled={!adding}`).

- [ ] **Step 4: Run the events tests**

Run: `cd frontend && pnpm vitest run src/features/events`
Expected: PASS (including the untouched existing tests — the blanket-disable removal must not break "gates actions by status").

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/events
git commit -m "feat(events): request-triggering buttons show loading; per-row refund/deny spinners"
```

### Task 3: Tournaments feature loading pass

**Files:**
- Modify: `frontend/src/features/tournaments/components/TournamentSection.tsx:151-159,178-187`
- Modify: `frontend/src/features/tournaments/components/TournamentPanel.tsx:133-141,191-213,272-279,302-317`
- Test: `frontend/src/features/tournaments/components/TournamentPanel.test.tsx`

**Interfaces:**
- Consumes: convention from Task 1. `useRoundAction` variables: `{ action: "reroll" | "publish" | "complete"; number: number }`; `usePlayerAction` variables: `{ playerId: string; action: "drop" | "undrop" }`.

- [ ] **Step 1: Write the failing per-button test**

In `TournamentPanel.test.tsx`, make the round-action mock controllable:

```tsx
const roundState: { isPending: boolean; variables?: { action: string; number: number } } = {
  isPending: false,
};
// in the vi.mock factory:
useRoundAction: () => ({ mutate: roundMut, error: null, ...roundState }),
```

Reset in `afterEach` (`roundState.isPending = false; delete roundState.variables;`). Add (uses the existing `draftTournament()` fixture and `renderPanel()` helper):

```tsx
test("publish spins while reroll is only disabled", () => {
  tournamentData = draftTournament();
  roundState.isPending = true;
  roundState.variables = { action: "publish", number: 1 };
  renderPanel();
  const publish = screen.getByRole("button", { name: m.tournament_publish() });
  const reroll = screen.getByRole("button", { name: m.tournament_reroll() });
  expect(publish.getAttribute("aria-busy")).toBe("true");
  expect(reroll.getAttribute("aria-busy")).not.toBe("true");
  expect(reroll).toBeDisabled();
});
```

Add the `m` import if missing. If `draftTournament()` uses a different round number, match `number` to it (the assertion only depends on `action`).

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && pnpm vitest run src/features/tournaments/components/TournamentPanel.test.tsx`
Expected: FAIL — publish has no `aria-busy`.

- [ ] **Step 3: Implement**

`TournamentPanel.tsx`:
- Pair button (~136): `disabled={pair.isPending}` → `loading={pair.isPending}`.
- Reroll button (~195): keep `disabled={roundAction.isPending}`, add
  `loading={roundAction.isPending && roundAction.variables?.action === "reroll"}`.
- Publish button (~208): keep `disabled={roundAction.isPending}`, add
  `loading={roundAction.isPending && roundAction.variables?.action === "publish"}`.
- Complete button (~275): `disabled={missing > 0 || roundAction.isPending}` stays, add
  `loading={roundAction.isPending && roundAction.variables?.action === "complete"}`.
- Player drop/undrop row buttons (~307): replace `disabled={playerAction.isPending}` with
  `loading={playerAction.isPending && playerAction.variables?.playerId === p.id}`.

`TournamentSection.tsx`:
- Undrop button (~155): `disabled={playerAction.isPending}` → `loading={playerAction.isPending}`.
- Drop-confirm dialog button (~180): `disabled={playerAction.isPending}` → `loading={playerAction.isPending}` (the dialog closes on click; the spinner then shows via the undrop/drop area on the page, which is acceptable — do not restructure the dialog).

- [ ] **Step 4: Run the tournaments tests**

Run: `cd frontend && pnpm vitest run src/features/tournaments`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/tournaments
git commit -m "feat(tournaments): per-action and per-player loading on round/player buttons"
```

### Task 4: Cubes + collection loading pass (incl. pagination)

**Files:**
- Modify: `frontend/src/features/cubes/components/PendingChangesPanel.tsx:99`
- Modify: `frontend/src/features/cubes/components/PendingChangesBar.tsx:40`
- Modify: `frontend/src/features/cubes/components/CubeSettingsSection.tsx:83-89`
- Modify: `frontend/src/features/cubes/components/CubeBrowserPage.tsx:62-79`
- Modify: `frontend/src/features/cubes/components/CubeHistoryPage.tsx:84-102`
- Modify: `frontend/src/features/collection/components/CollectionPage.tsx`
- Test: `frontend/src/features/cubes/components/CubeBrowserPage.test.tsx`

**Interfaces:**
- Consumes: convention from Task 1. Paged queries (`useCollection`, cube list, cube changes) all use `placeholderData: keepPreviousData`.

- [ ] **Step 1: Write the failing pagination test**

In `CubeBrowserPage.test.tsx` (follow its `renderWithRouter` + `vi.stubGlobal("fetch", …)` style; add a `userEvent` import):

```tsx
test("pagination buttons spin while the next page loads", async () => {
  const pageOne = () =>
    new Response(
      JSON.stringify({
        cubes: [
          {
            id: "c1",
            name: "Vintage Cube",
            description: "",
            ownerName: "Mat",
            cardCount: 540,
            visibility: "public",
            updatedAt: "2026-07-12T10:00:00Z",
          },
        ],
        total: 21, // > CUBES_PAGE_SIZE → pagination renders
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(pageOne())
    .mockImplementation(() => new Promise(() => {})); // page 2 never resolves
  vi.stubGlobal("fetch", fetchMock);
  renderWithRouter(() => <CubeBrowserPage />);
  await waitFor(() => expect(screen.getByText("Vintage Cube")).toBeDefined());
  const next = screen.getByRole("button", { name: m.pagination_next() });
  await userEvent.click(next);
  await waitFor(() => expect(next.getAttribute("aria-busy")).toBe("true"));
  // keepPreviousData: page 1 content stays visible while spinning.
  expect(screen.getByText("Vintage Cube")).toBeDefined();
});
```

Add the `m` import if missing. If the pagination buttons use visible text instead of an aria-label, match on that text.

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd frontend && pnpm vitest run src/features/cubes/components/CubeBrowserPage.test.tsx`
Expected: FAIL — no `aria-busy` on the next button.

- [ ] **Step 3: Implement**

Pagination (all three pages, both prev and next buttons — keep the existing `disabled` bounds):
- `CubeBrowserPage.tsx`: add `loading={list.isFetching && list.isPlaceholderData}` (use the actual query variable name in that file).
- `CubeHistoryPage.tsx`: add `loading={changes.isFetching && changes.isPlaceholderData}`.
- `CollectionPage.tsx` (~193, ~206): add `loading={collection.isFetching && collection.isPlaceholderData}`.

Cube save/delete:
- `PendingChangesPanel.tsx` save (~99): `disabled={count === 0 || saving}` → `disabled={count === 0} loading={saving}`. Leave the discard button unchanged (local action, but must stay blocked mid-save via its existing `disabled`).
- `PendingChangesBar.tsx` save (~40): `disabled={saving}` → `loading={saving}`.
- `CubeSettingsSection.tsx` delete (~86): `disabled={del.isPending}` → `loading={del.isPending}`.

Collection row remove — the ✕ shares `setQuantity` with the debounced stepper, so spin only on an actual remove (quantity 0), and keep the row-level disable:

```tsx
// next to the existing mutatingRowId derivation:
const removingId =
  setQuantity.isPending && setQuantity.variables.quantity === 0
    ? setQuantity.variables.scryfallId
    : null;
```

✕ button (~159): keep `disabled={mutatingRowId === item.scryfallId}`, add `loading={removingId === item.scryfallId}`. The ⇄ button only opens a dialog (local) — leave it with `disabled` only. `QuantityStepper` is deliberately local-first/debounced — no loading there.

- [ ] **Step 4: Run the cubes + collection tests**

Run: `cd frontend && pnpm vitest run src/features/cubes src/features/collection`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/cubes frontend/src/features/collection
git commit -m "feat(cubes,collection): loading on save/delete/remove and paginators"
```

### Task 5: PR 1 gates + pull request

- [ ] **Step 1: Full frontend gate**

Run: `make frontend-test && make frontend-typecheck && make frontend-lint`
Expected: all pass. Fix anything that fails before proceeding.

- [ ] **Step 2: Format**

Run: `cd frontend && pnpm fmt` — commit any formatting deltas (amend into the last commit if trivial).

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin feature/uniform-loading-and-backlog
gh pr create --title "feat: uniform loading feedback on all request-triggering buttons" --body "$(cat <<'EOF'
Every button that fires a network request now passes `Button loading` (spinner + disabled + aria-busy), in three shapes documented in structure.md rule 6: single-action (`mutation.isPending`), row-scoped via `mutation.variables` (only the clicked row spins), and pagination over keepPreviousData (`isFetching && isPlaceholderData`). Includes the spec + plan docs for this batch.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## PR 2 — a11y: #24 touch sizes + #17 ARIA tabs

### Task 6: Branch + close-affordance touch sizes (#24)

**Files:**
- Modify: `frontend/src/shared/ui/dialog.tsx:45-53`
- Modify: `frontend/src/shared/ui/theme-toggle.tsx:22-27`
- Modify: `frontend/src/features/tournaments/components/TournamentSection.tsx:84` (tablist outline room)

**Interfaces:** none exported; pure class/prop changes.

- [ ] **Step 1: Branch off PR 1**

```bash
git checkout -b feature/a11y-touch-and-tabs
```

- [ ] **Step 2: Apply the established 44px override pattern**

`dialog.tsx` ✕ button: `size="sm"` → `size="icon"` and add `className="size-11"`:

```tsx
<Button
  type="button"
  variant="ghost"
  size="icon"
  className="size-11"
  aria-label={m.dialog_close()}
  onClick={onClose}
>
  ✕
</Button>
```

Also add `-m-2` alongside `size-11` if the larger button visibly inflates the dialog header (compare before/after; the header uses `items-start justify-between`, so a negative margin keeps the title baseline).

`theme-toggle.tsx`: add `className="size-11"` to the Button (keep `size="icon"`, icon stays `size-4`).

`TournamentSection.tsx` tablist (line ~84): the `overflow-x-auto` container clips the 2px focus outline + 2px offset of the `h-11` tabs. Give it internal breathing room, compensated so alignment is unchanged:

```tsx
<div role="tablist" className="-m-1 flex gap-2 overflow-x-auto p-1">
```

- [ ] **Step 3: Verify visually**

Run: `make up`, open http://localhost:3000 (or the Vite port `make up` prints):
- open any dialog (e.g. collection printing picker) — ✕ hit area ≥44px, layout not broken;
- ThemeToggle in the header — same;
- on an event with rounds, keyboard-Tab to the round tabs — focus outline fully visible, not clipped top/bottom.
At 360px width too (structure.md rule 9).

- [ ] **Step 4: Run affected tests + commit**

Run: `cd frontend && pnpm vitest run src/shared/ui src/features/tournaments`
Expected: PASS.

```bash
git add frontend/src/shared/ui frontend/src/features/tournaments
git commit -m "fix(a11y): 44px dialog close and theme toggle; unclipped tab focus outlines (#24)"
```

### Task 7: Tournament round tabs — stale state + APG completeness (#17)

**Files:**
- Modify: `frontend/src/routes/events.$eventId.index.tsx:10`
- Modify: `frontend/src/features/tournaments/components/TournamentSection.tsx`
- Test: `frontend/src/features/tournaments/components/TournamentSection.keyboard.test.tsx`

**Interfaces:**
- Consumes: existing `onTabKeyDown`, `rounds`, `round`, `tab` state in `TournamentSection`.
- Produces: `role="tabpanel"` wrapping the match list; tab ids `${panelId}-tab-${r.number}`, panel id `${panelId}-panel`.

- [ ] **Step 1: Write the failing tests**

In `TournamentSection.keyboard.test.tsx` (uses the existing `twoRoundTournament()` fixture, `renderSection()`, and mutable `tournamentData`):

```tsx
test("Home and End jump selection and focus to first/last tab", async () => {
  tournamentData = twoRoundTournament();
  renderSection();
  const tabs = screen.getAllByRole("tab");
  tabs[1]!.focus(); // round 2 is selected (latest) and tabbable
  await userEvent.keyboard("{Home}");
  expect(tabs[0]!.getAttribute("aria-selected")).toBe("true");
  expect(document.activeElement).toBe(tabs[0]);
  await userEvent.keyboard("{End}");
  expect(tabs[1]!.getAttribute("aria-selected")).toBe("true");
  expect(document.activeElement).toBe(tabs[1]);
});

test("match list is a tabpanel labelled by the active tab", () => {
  tournamentData = twoRoundTournament();
  renderSection();
  const panel = screen.getByRole("tabpanel");
  const activeTab = screen.getAllByRole("tab")[1]!;
  expect(activeTab.getAttribute("aria-controls")).toBe(panel.id);
  expect(panel.getAttribute("aria-labelledby")).toBe(activeTab.id);
});
```

- [ ] **Step 2: Run to verify both fail**

Run: `cd frontend && pnpm vitest run src/features/tournaments/components/TournamentSection.keyboard.test.tsx`
Expected: FAIL — Home/End ignored; no `tabpanel` role.

- [ ] **Step 3: Implement**

`TournamentSection.tsx`:

1. Add `useId` to the react import; inside the component: `const panelId = useId();`
2. Extend the key handler:

```tsx
const onTabKeyDown = (e: KeyboardEvent<HTMLButtonElement>, from: number) => {
  let next: number;
  if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
    const delta = e.key === "ArrowRight" ? 1 : -1;
    next = (from + delta + rounds.length) % rounds.length;
  } else if (e.key === "Home") {
    next = 0;
  } else if (e.key === "End") {
    next = rounds.length - 1;
  } else {
    return;
  }
  e.preventDefault();
  setTab(rounds[next]!.number);
  const tabs = e.currentTarget
    .closest('[role="tablist"]')
    ?.querySelectorAll<HTMLButtonElement>('[role="tab"]');
  tabs?.[next]?.focus();
};
```

(Keep the existing comment about the roving-tabindex pattern; extend it to mention Home/End.)

3. Tab buttons get `id={`${panelId}-tab-${r.number}`}` and `aria-controls={`${panelId}-panel`}`.
4. Wrap the match `<ul>` in a tabpanel:

```tsx
<div
  id={`${panelId}-panel`}
  role="tabpanel"
  aria-labelledby={`${panelId}-tab-${round.number}`}
>
  <ul className="flex flex-col gap-1">…existing list…</ul>
</div>
```

`events.$eventId.index.tsx`: root-cause the stale tab state — remount per event:

```tsx
<TournamentSection key={eventId} eventId={eventId} />
```

Keep the in-component `?? rounds[rounds.length - 1]!` fallback (defense for rounds shrinking in-place via polling).

- [ ] **Step 4: Run the tests**

Run: `cd frontend && pnpm vitest run src/features/tournaments`
Expected: PASS (including existing arrow-key tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/tournaments frontend/src/routes/events.\$eventId.index.tsx
git commit -m "fix(a11y): complete round-tabs APG pattern; reset tab state per event (#17)"
```

### Task 8: PR 2 gates + pull request

- [ ] **Step 1: Gate**

Run: `make frontend-test && make frontend-typecheck && make frontend-lint && cd frontend && pnpm fmt`
Expected: all pass; commit any format deltas.

- [ ] **Step 2: Push and open PR**

```bash
git push -u origin feature/a11y-touch-and-tabs
gh pr create --title "fix(a11y): 44px close affordances and complete ARIA tabs pattern" --body "$(cat <<'EOF'
Closes #24, closes #17.

- Dialog ✕ and ThemeToggle raised to the 44px floor via the established `size="icon"` + `size-11` override; round tablist gets outline breathing room inside its overflow container.
- Tournament round tabs: Home/End keys, `role="tabpanel"` + `aria-controls`/`aria-labelledby` linkage, and `key={eventId}` remount so a stale round selection can no longer leak across events (fallback retained).

Stacked on #<PR1-number> — rebase/merge after it lands.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## PR 3 — #19 per-entry printing picker (backend + cube editor + /cards)

### Task 9: Backend `change-printing` endpoint (TDD via integration test)

**Files:**
- Modify: `backend/internal/db/queries/cubes.sql` (two new queries)
- Generate: `backend/internal/db/cubes.sql.go` (sqlc)
- Modify: `backend/internal/cubes/service.go` (new `ChangePrinting` method)
- Modify: `backend/internal/platform/httpapi/cubes.go` (new operation)
- Test: `backend/internal/platform/httpapi/cubes_endpoints_test.go`

**Interfaces:**
- Produces: `POST /api/cubes/{cubeId}/cards/{oracleId}/change-printing`, body `{"newScryfallId": "<uuid>"}` → 204; 401 anon, 404 unknown cube / private non-owner, 403 public non-owner, 422 `invalid-cube-change` (oracle not in cube, target not a printing of that oracle, target already current). **No version bump, no changelog entry** (adjudicated 2026-08-02: printing is cosmetic; diffs/replay are oracle-keyed).
- Produces (Go): `func (s *Service) ChangePrinting(ctx context.Context, cubeID, viewerID, oracleID, newScryfallID uuid.UUID) error`.

- [ ] **Step 1: Branch off PR 2**

```bash
git checkout -b feature/cube-printing-picker
```

- [ ] **Step 2: Write the failing integration test**

In `cubes_endpoints_test.go`, following the file's existing helpers (server constructor, `loggedInClient`, `seedCard`, `decode`, cube-creation and commit helpers — read the top of the file and reuse them; the collection analog is `TestChangePrinting` in `collections_endpoints_test.go`):

```go
func TestChangeCubePrinting(t *testing.T) {
	srv, pool, q := newCubesServer(t) // use the file's actual constructor name
	c := loggedInClient(t, srv, q, "cp1@test.dev")

	boltO := uuid.New()
	alphaS, m10S := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: alphaS, oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	seedCard(t, pool, testCard{scryfallID: m10S, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	strikeS := uuid.New()
	seedCard(t, pool, testCard{scryfallID: strikeS, oracleID: uuid.New(), name: "Lightning Strike"})

	// Create a cube and commit 2× alpha Bolt (reuse the file's helpers for both).
	cubeID := createCube(t, c, "Printing Cube", "public")
	commitAdd(t, c, cubeID, alphaS, 2) // expectedVersion 0 → version 1

	change := func(cl *cookieClient, oracle, target uuid.UUID) *http.Response {
		return cl.do(t, "POST",
			"/api/cubes/"+cubeID+"/cards/"+oracle.String()+"/change-printing",
			fmt.Sprintf(`{"newScryfallId":%q}`, target))
	}

	// Happy path: 204, list shows the new printing, version untouched.
	if resp := change(c, boltO, m10S); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change = %d, want 204", resp.StatusCode)
	}
	cards := getCubeCards(t, c, cubeID) // reuse/introduce a helper decoding GET .../cards
	if cards.Version != 1 {
		t.Fatalf("version = %d, want 1 (printing swap must not bump)", cards.Version)
	}
	if len(cards.Cards) != 1 || cards.Cards[0].ScryfallID != m10S.String() ||
		cards.Cards[0].Quantity != 2 {
		t.Fatalf("cards = %+v, want 2× m10 printing", cards.Cards)
	}

	// 422 family.
	for name, resp := range map[string]*http.Response{
		"same printing":    change(c, boltO, m10S),
		"foreign oracle":   change(c, boltO, strikeS),
		"not in cube":      change(c, uuid.New(), m10S),
		"malformed target": change(c, boltO, uuid.Nil),
	} {
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s = %d, want 422", name, resp.StatusCode)
		}
	}

	// Non-owner on a public cube: 403.
	other := loggedInClient(t, srv, q, "cp2@test.dev")
	if resp := change(other, boltO, alphaS); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-owner = %d, want 403", resp.StatusCode)
	}
}
```

Adapt helper names to what the file actually defines (it has cube-creation/commit plumbing for the existing commit tests); add a small `getCubeCards` decode helper if none exists. If `uuid.Nil` passes huma's format validation and reaches the service, "malformed target" lands in the unknown-target 422 — same family, keep the assertion.

- [ ] **Step 3: Run to verify it fails**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestChangeCubePrinting`
Expected: FAIL (404 — route not registered).

- [ ] **Step 4: SQL queries + sqlc**

Append to `backend/internal/db/queries/cubes.sql`:

```sql
-- Current printing + quantity of one oracle row (printing-swap validation).
-- name: GetCubeCardRow :one
select scryfall_id, quantity from cube_cards
where cube_id = sqlc.arg(cube_id) and oracle_id = sqlc.arg(oracle_id);

-- Cosmetic printing swap: no version bump, no changelog (see Service.ChangePrinting).
-- name: SetCubeCardPrinting :exec
update cube_cards set scryfall_id = sqlc.arg(scryfall_id)
where cube_id = sqlc.arg(cube_id) and oracle_id = sqlc.arg(oracle_id);
```

Run: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate && go build ./...`
Expected: clean build; `GetCubeCardRow`/`SetCubeCardPrinting` appear in `internal/db/cubes.sql.go`.

- [ ] **Step 5: Service method**

In `backend/internal/cubes/service.go` (after `ApplyChange`):

```go
// ChangePrinting swaps the stored printing of one oracle entry in place.
// Deliberately no version bump and no changelog entry: printing is
// cosmetic, diffs and replay are oracle-keyed, and concurrent editors'
// expectedVersion must stay valid. A lost update between the read and
// the write just means last-picker-wins, which is fine for cosmetics.
func (s *Service) ChangePrinting(ctx context.Context, cubeID, viewerID, oracleID, newScryfallID uuid.UUID) error {
	if _, err := s.getOwned(ctx, cubeID, viewerID); err != nil {
		return err
	}
	row, err := s.queries.GetCubeCardRow(ctx, db.GetCubeCardRowParams{
		CubeID: cubeID, OracleID: oracleID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: oracle %s is not in the cube", ErrInvalidChange, oracleID)
	}
	if err != nil {
		return err
	}
	if row.ScryfallID == newScryfallID {
		return fmt.Errorf("%w: target is the current printing", ErrInvalidChange)
	}
	cards, err := s.queries.GetCardsByScryfallIDs(ctx, []uuid.UUID{newScryfallID})
	if err != nil {
		return err
	}
	if len(cards) == 0 || cards[0].OracleID != oracleID {
		return fmt.Errorf("%w: target is not a printing of this card", ErrInvalidChange)
	}
	return s.queries.SetCubeCardPrinting(ctx, db.SetCubeCardPrintingParams{
		CubeID: cubeID, OracleID: oracleID, ScryfallID: newScryfallID,
	})
}
```

- [ ] **Step 6: HTTP endpoint**

In `backend/internal/platform/httpapi/cubes.go`, add the input type near the other inputs:

```go
type changeCubePrintingInput struct {
	CubeID   string `path:"cubeId"`
	OracleID string `path:"oracleId"`
	Body     struct {
		NewScryfallID uuid.UUID `json:"newScryfallId" format:"uuid"`
	}
}
```

Register inside `registerCubes`, mirroring `deleteCube`'s 204 shape and the file's existing error mapper (the `switch` handling `cubes.ErrVersionConflict` / `ErrInvalidChange`; reuse its actual function name):

```go
huma.Register(api, huma.Operation{
	OperationID:   "changeCubePrinting",
	Method:        http.MethodPost,
	Path:          "/api/cubes/{cubeId}/cards/{oracleId}/change-printing",
	Summary:       "Swap the stored printing of a cube entry (cosmetic; no new version)",
	Tags:          []string{"cubes"},
	DefaultStatus: http.StatusNoContent,
}, func(ctx context.Context, in *changeCubePrintingInput) (*struct{}, error) {
	uid, ok := CurrentUserID(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	cubeID, err := parseCubeID(in.CubeID)
	if err != nil {
		return nil, err
	}
	oracleID, err := uuid.Parse(in.OracleID)
	if err != nil {
		// A malformed oracle is just "not in the cube" — same 422 family.
		return nil, mapCubesErr(fmt.Errorf("%w: malformed oracle id", cubes.ErrInvalidChange))
	}
	if err := deps.Cubes.ChangePrinting(ctx, cubeID, uid, oracleID, in.Body.NewScryfallID); err != nil {
		return nil, mapCubesErr(err)
	}
	return nil, nil
})
```

(`mapCubesErr` = whatever the file's cube error mapper is actually called.)

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd backend && go test ./internal/platform/httpapi/ -run TestChangeCubePrinting`
Expected: PASS. Then the full backend gate: `make backend-test && make backend-lint`.

- [ ] **Step 8: Commit**

```bash
git add backend
git commit -m "feat(cubes): cosmetic change-printing endpoint (no version bump, no changelog)"
```

### Task 10: Regenerate client + frontend hook + pending-add printing action

**Files:**
- Generate: `frontend/src/shared/api/` (`make api-generate`)
- Modify: `frontend/src/features/cubes/api.ts`
- Modify: `frontend/src/features/cubes/lib/pendingDiff.ts`
- Test: `frontend/src/features/cubes/lib/pendingDiff.test.ts`

**Interfaces:**
- Produces: `useChangeCubePrinting(cubeId)` — mutation over `{ oracleId: string; newScryfallId: string }`, invalidates `["cubes"]`.
- Produces: reducer action `{ type: "setAddPrinting"; oracleId: string; scryfallId: string }` — rewrites a pending add's chosen printing; no-op when the oracle has no pending add.

- [ ] **Step 1: Regenerate the API client**

Run: `make api-generate`
Expected: `frontend/src/shared/api/schema.d.ts` gains the `changeCubePrinting` operation. Commit nothing yet.

- [ ] **Step 2: Failing reducer test**

In `pendingDiff.test.ts` (follow the file's existing fixture style for `CardSummary`s):

```ts
test("setAddPrinting rewrites a pending add's printing and ignores unknown oracles", () => {
  const withAdd = pendingReducer(emptyPending, { type: "add", card: bolt }); // existing fixture
  const swapped = pendingReducer(withAdd, {
    type: "setAddPrinting",
    oracleId: bolt.oracleId,
    scryfallId: "new-printing-id",
  });
  expect(swapped.adds.get(bolt.oracleId)?.card.scryfallId).toBe("new-printing-id");
  expect(swapped.adds.get(bolt.oracleId)?.quantity).toBe(1);
  // Unknown oracle: state returned unchanged.
  expect(pendingReducer(swapped, { type: "setAddPrinting", oracleId: "nope", scryfallId: "x" }))
    .toBe(swapped);
});
```

Run: `cd frontend && pnpm vitest run src/features/cubes/lib/pendingDiff.test.ts` — expect FAIL (unknown action type / TS error).

- [ ] **Step 3: Implement reducer action + hook**

`pendingDiff.ts` — add to `PendingAction`:

```ts
| { type: "setAddPrinting"; oracleId: string; scryfallId: string }
```

and in `pendingReducer`'s switch:

```ts
case "setAddPrinting": {
  const add = state.adds.get(action.oracleId);
  if (!add) return state;
  const next = clone(state);
  next.adds.set(action.oracleId, {
    ...add,
    card: { ...add.card, scryfallId: action.scryfallId },
  });
  return next;
}
```

`features/cubes/api.ts` — after `useCommitChange`:

```ts
export function useChangeCubePrinting(cubeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { oracleId: string; newScryfallId: string }) => {
      // 204 No Content on success — same inline error check as useDeleteCube.
      const { error } = await client.POST(
        "/api/cubes/{cubeId}/cards/{oracleId}/change-printing",
        {
          params: { path: { cubeId, oracleId: vars.oracleId } },
          body: { newScryfallId: vars.newScryfallId },
        },
      );
      if (error) throw new Error(error.detail ?? m.error_generic());
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cubes"] }),
  });
}
```

- [ ] **Step 4: Run tests + commit**

Run: `cd frontend && pnpm vitest run src/features/cubes && make frontend-typecheck`
Expected: PASS.

```bash
git add frontend/src/shared/api frontend/src/features/cubes
git commit -m "feat(cubes): change-printing client hook and pending-add printing action"
```

### Task 11: Cube editor picker UI

**Files:**
- Modify: `frontend/src/features/cubes/components/EditableCardList.tsx`
- Modify: `frontend/src/features/cubes/components/CubeEditorPage.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/cubes/components/CubeEditorPage.test.tsx`

**Interfaces:**
- Consumes: `useChangeCubePrinting`, `setAddPrinting` (Task 10), shared `PrintingPickerDialog` (`open, onClose, oracleId, name, currentScryfallId, onPick`).
- Produces: `EditableCardList` new props: `onChangePrinting: (entry: CubeCardEntry) => void` and `printingPendingOracleId: string | null`.

- [ ] **Step 1: Messages**

`en.json`: `"cubes_change_printing": "Change printing of {name}"`.
`pl.json`: mirror the existing `collection_change_printing` Polish phrasing with the same `{name}` parameter (open `pl.json` and copy its wording).

- [ ] **Step 2: Failing editor test**

In `CubeEditorPage.test.tsx`, following that file's existing fetch-stub/render helpers (read its top before writing): render an editor whose cube has one saved entry, stub the printings endpoint (`/api/cards/{oracleId}/printings`) with two printings, then:

```tsx
test("picking a new printing for a saved entry posts change-printing", async () => {
  // ...file's standard render with one saved card "Lightning Bolt"...
  await userEvent.click(
    screen.getByRole("button", { name: m.cubes_change_printing({ name: "Lightning Bolt" }) }),
  );
  // dialog fetches printings and lists the non-current one
  await userEvent.click(await screen.findByRole("button", { name: /Beta/ }));
  await waitFor(() => {
    const calls = (fetch as ReturnType<typeof vi.fn>).mock.calls.map((c) => String(c[0]));
    expect(calls.some((u) => u.includes("/change-printing"))).toBe(true);
  });
});
```

Run: `cd frontend && pnpm vitest run src/features/cubes/components/CubeEditorPage.test.tsx` — expect FAIL (no such button).

- [ ] **Step 3: Implement**

`EditableCardList.tsx` — add the two props to the signature and a ⇄ button between + and ✕ in the per-card button group:

```tsx
<Button
  type="button"
  variant="ghost"
  size="sm"
  aria-label={m.cubes_change_printing({ name: card.name })}
  loading={printingPendingOracleId === card.oracleId}
  onClick={() => onChangePrinting(card)}
>
  ⇄
</Button>
```

`CubeEditorPage.tsx`:

```tsx
const changePrinting = useChangeCubePrinting(cubeId);
const [pickerEntry, setPickerEntry] = useState<CubeCardEntry | null>(null);
```

Pass to the list:

```tsx
<EditableCardList
  cards={preview}
  serverByOracle={serverByOracle}
  groupKind="color"
  dispatch={dispatch}
  onChangePrinting={setPickerEntry}
  printingPendingOracleId={changePrinting.isPending ? changePrinting.variables.oracleId : null}
/>
```

Render the shared dialog next to the other overlays; saved entries hit the API, pending-only adds stay client-side:

```tsx
{pickerEntry && (
  <PrintingPickerDialog
    open
    onClose={() => setPickerEntry(null)}
    oracleId={pickerEntry.oracleId}
    name={pickerEntry.name}
    currentScryfallId={pickerEntry.scryfallId}
    onPick={(newScryfallId) => {
      if (serverByOracle.has(pickerEntry.oracleId)) {
        changePrinting.mutate({ oracleId: pickerEntry.oracleId, newScryfallId });
      } else {
        dispatch({ type: "setAddPrinting", oracleId: pickerEntry.oracleId, scryfallId: newScryfallId });
      }
      setPickerEntry(null);
    }}
  />
)}
```

Surface failures next to the existing commit alerts (outside `commitAlerts` is fine — printing swaps happen outside the sheet):

```tsx
{changePrinting.isError && <Alert variant="danger">{changePrinting.error.message}</Alert>}
```

Imports: `PrintingPickerDialog` from `@/shared/cards/PrintingPickerDialog`, `useChangeCubePrinting` from `../api`.

- [ ] **Step 4: Run tests + commit**

Run: `cd frontend && pnpm vitest run src/features/cubes && make frontend-typecheck`
Expected: PASS.

```bash
git add frontend/src/features/cubes frontend/messages
git commit -m "feat(cubes): per-entry printing picker in the editor (#19)"
```

### Task 12: /cards details printing picker (client-side only)

**Files:**
- Modify: `frontend/src/features/cards/components/CardSearchPage.tsx` (`SelectedCardPanel`)
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/cards/components/CardSearchPage.test.tsx`

**Interfaces:**
- Consumes: shared `PrintingPickerDialog`; `useCardPrintings(card.oracleId)` already in the panel.
- Produces: nothing persisted — local selected-printing state, reset by the existing `key={selected.scryfallId}` remount.

- [ ] **Step 1: Messages**

`en.json`: `"cards_change_printing": "Change printing"`; `pl.json`: `"cards_change_printing": "Zmień wydanie"` (align with `pl.json`'s existing printing wording).

- [ ] **Step 2: Failing test**

In `CardSearchPage.test.tsx` (follow its existing search/select flow helpers; the printings stub must return ≥2 printings with distinct `setName`s):

```tsx
test("details view can show a different printing", async () => {
  // ...file's standard flow to select a card whose printings stub returns
  // [{ setName: "Magic 2010", ... }, { setName: "Alpha", ... }]...
  expect(screen.getByText(/Magic 2010/)).toBeDefined(); // printings[0] shown
  await userEvent.click(screen.getByRole("button", { name: m.cards_change_printing() }));
  await userEvent.click(await screen.findByRole("button", { name: /Alpha/ }));
  await waitFor(() => expect(screen.getByText(/Alpha/)).toBeDefined());
});
```

Run: `cd frontend && pnpm vitest run src/features/cards` — expect FAIL.

- [ ] **Step 3: Implement**

In `SelectedCardPanel`:

```tsx
const [pickedId, setPickedId] = useState<string | null>(null);
const [pickerOpen, setPickerOpen] = useState(false);
const printingList = printings.data ?? [];
const shown = printingList.find((p) => p.scryfallId === pickedId) ?? printingList[0];
```

Rename every `latest` usage to `shown` (image, name, type line, oracle text, set line). After the printings-count line, add the affordance + dialog:

```tsx
<div>
  <Button type="button" variant="outline" size="sm" onClick={() => setPickerOpen(true)}>
    {m.cards_change_printing()}
  </Button>
</div>
<PrintingPickerDialog
  open={pickerOpen}
  onClose={() => setPickerOpen(false)}
  oracleId={card.oracleId}
  name={shown.name}
  currentScryfallId={shown.scryfallId}
  onPick={(id) => {
    setPickedId(id);
    setPickerOpen(false);
  }}
/>
```

Imports: `Button` from `@/shared/ui/button`, `PrintingPickerDialog` from `@/shared/cards/PrintingPickerDialog`. (`shared/cards` importing within `shared` and `features/cards` → `shared` are both legal directions.)

- [ ] **Step 4: Run tests + commit**

Run: `cd frontend && pnpm vitest run src/features/cards && make frontend-typecheck`
Expected: PASS.

```bash
git add frontend/src/features/cards frontend/messages
git commit -m "feat(cards): browse printings on the card details view (#19)"
```

### Task 13: PR 3 gates + pull request

- [ ] **Step 1: Full gate**

Run: `make test && make frontend-typecheck && make frontend-lint && make backend-lint && make api-check`
Expected: all pass (api-check proves the committed client is fresh). `cd frontend && pnpm fmt`; commit deltas if any.

- [ ] **Step 2: Push and open PR**

```bash
git push -u origin feature/cube-printing-picker
gh pr create --title "feat: per-entry printing picker in the cube editor and /cards details" --body "$(cat <<'EOF'
Closes #19.

- New `POST /api/cubes/{cubeId}/cards/{oracleId}/change-printing` (owner-only, 204): in-place cosmetic swap — deliberately NO version bump and NO changelog entry (diffs/replay are oracle-keyed; adjudicated in the 2026-08-02 spec). 422 taxonomy mirrors the collection analog.
- Cube editor: ⇄ per entry opens the shared PrintingPickerDialog; saved entries hit the API (per-row spinner), pending-only adds swap client-side via a new `setAddPrinting` reducer action.
- /cards details: printing browsing is local state (nothing to persist), replacing the hardwired `printings[0]`.

Stacked on the a11y PR — rebase/merge after it lands.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## PR 4 — #28 bottom tab bar (mobile)

### Task 14: Bottom nav + slimmed drawer

**Files:**
- Modify: `frontend/src/routes/__root.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/routes/-root-layout.test.tsx`

**Interfaces:**
- Consumes: existing `m.nav_cards/nav_cubes/nav_events/nav_collection` keys; TanStack `Link` `data-status="active"` attribute for active styling (avoids `activeProps` className-merge ambiguity).
- Produces: a `<nav aria-label={m.nav_primary()}>` bottom bar, always rendered below `md:` for guests and users alike.

- [ ] **Step 1: Branch off PR 3**

```bash
git checkout -b feature/bottom-tab-bar
```

- [ ] **Step 2: Messages**

`en.json`: `"nav_primary": "Primary"`; `pl.json`: `"nav_primary": "Główna"`. (Labels the bottom `<nav>` landmark; the top nav stays unlabeled so the two landmark names differ.)

- [ ] **Step 3: Update the root-layout tests (they encode the old drawer contents)**

In `-root-layout.test.tsx`:
- Add `/collection` to the `paths` array in `renderShell`.
- "hamburger opens the drawer with nav links": Cards/Cubes/Events no longer live in the drawer. Rewrite to assert the new split:

```tsx
test("primary destinations live in the bottom nav; drawer keeps secondary items", async () => {
  await renderShell();
  const bottomNav = screen.getByRole("navigation", { name: "Primary" });
  for (const name of ["Cards", "Cubes", "Events", "Collection"]) {
    expect(within(bottomNav).getByRole("link", { name })).toBeInTheDocument();
  }
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  expect(within(drawer).queryByRole("link", { name: "Cards" })).toBeNull();
  expect(within(drawer).getByRole("link", { name: "Log in" })).toBeInTheDocument();
});
```

- "drawer closes on navigation" and the same-route-tap test: drive them via the drawer's "Log in" link instead of "Cards" (the delegated close-on-link-tap wrapper must keep covering the remaining links). Where the old tests count `getAllByRole("link", { name: "Cards" })`, expect **2** copies now (desktop top nav + bottom nav).

Run: `cd frontend && pnpm vitest run src/routes` — expect FAIL (no bottom nav yet).

- [ ] **Step 4: Implement in `__root.tsx`**

1. After `</main>` (before the devtools block), add:

```tsx
<nav
  aria-label={m.nav_primary()}
  className="fixed inset-x-0 bottom-0 z-10 border-t border-border bg-surface-raised pb-[env(safe-area-inset-bottom)] md:hidden"
>
  <div className="mx-auto flex h-14 max-w-4xl items-stretch">
    {(
      [
        ["/cards", m.nav_cards],
        ["/cubes", m.nav_cubes],
        ["/events", m.nav_events],
        ["/collection", m.nav_collection],
      ] as const
    ).map(([to, label]) => (
      <Link
        key={to}
        to={to}
        className="flex flex-1 items-center justify-center text-sm text-fg-muted data-[status=active]:font-medium data-[status=active]:text-accent"
      >
        {label()}
      </Link>
    ))}
  </div>
</nav>
```

(Each tab is `h-14` full-width-quarter — comfortably past the 44px floor. Guests see all four; `/collection`'s route guard handles auth.)

2. `<main>`: `className="mx-auto max-w-4xl px-4 pt-8 pb-24 outline-none md:pb-8"` (clearance for the fixed bar below `md:`).

3. Slim the drawer: delete the primary `<nav>` block (Cards/Cubes/Events links) and the `<hr>` after it; in the signed-in block delete the Collection link (it lives in the bar now). The drawer keeps: My Cubes, account (display name), logout — and for guests the Log in link — plus the language switcher. The delegated close-on-link-tap wrapper and the drawer `<hr>`/structure otherwise stay.

- [ ] **Step 5: Run the tests**

Run: `cd frontend && pnpm vitest run src/routes`
Expected: PASS (including the `-a11y.test.tsx` suite — the labeled bottom nav must not introduce landmark violations).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/routes frontend/messages
git commit -m "feat(shell): bottom tab bar for primary destinations on mobile (#28)"
```

### Task 15: Fixed-bottom collisions + visual verify + PR

**Files:**
- Modify: `frontend/src/features/cubes/components/PendingChangesBar.tsx:26-28`

**Interfaces:**
- Consumes: the bar's height math — bottom nav is `h-14` (3.5rem) + `env(safe-area-inset-bottom)`.

- [ ] **Step 1: Stack the pending-changes bar above the tab bar**

`PendingChangesBar.tsx` `<section>` className: replace `bottom-0` with
`bottom-[calc(3.5rem+env(safe-area-inset-bottom))] md:bottom-0`.
Inner `<div>` padding: the tab bar owns the safe area below `md:`, so replace
`pb-[max(0.75rem,env(safe-area-inset-bottom))]` with
`pb-3 md:pb-[max(0.75rem,env(safe-area-inset-bottom))]`.
(`CubeEditorPage`'s dirty `pb-24 lg:pb-0` spacer stays — it clears the bar itself; `<main>`'s new `pb-24` clears the nav; the bottom-sheet Drawer is a native modal `<dialog>` in the top layer, above both.)

- [ ] **Step 2: Visual verification at 360px**

Run: `make up`. In the browser at 360px width (dev tools device toolbar):
- every page: bottom bar visible, active tab highlighted, no content hidden behind it (scroll each page to the end), no horizontal scroll;
- cube editor with pending changes: PendingChangesBar sits directly above the tab bar, both fully visible; open the review bottom sheet — it layers above both;
- open the hamburger drawer — Cards/Cubes/Events/Collection gone, secondary items present;
- ≥`md:` width: bar gone, top nav unchanged, PendingChangesBar back at the viewport bottom between `md` and `lg`.

- [ ] **Step 3: Gates**

Run: `make frontend-test && make frontend-typecheck && make frontend-lint && cd frontend && pnpm fmt`
Expected: all pass; commit format deltas if any.

- [ ] **Step 4: Commit, push, PR**

```bash
git add frontend/src/features/cubes
git commit -m "fix(cubes): pending-changes bar stacks above the mobile tab bar"
git push -u origin feature/bottom-tab-bar
gh pr create --title "feat: mobile bottom tab bar for primary navigation" --body "$(cat <<'EOF'
Closes #28.

Fixed bottom bar below `md:` with Cards / Cubes / Events / Collection (text labels for now — icons deferred to a UX consult per the spec), `data-status`-driven active styling, safe-area padding, ≥44px targets. The drawer slims to secondary items (My Cubes, account, login/logout, language). The cube editor's PendingChangesBar stacks above the bar; `<main>` gets mobile bottom clearance. Guests see all four tabs — /collection's guard redirects to login.

Stacked on the printing-picker PR — rebase/merge after it lands.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
