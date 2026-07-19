# Mobile Follow-ups (#25, #23, #30) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the three approved mobile follow-ups — semantic overlay token (#25), drawer close on same-route tap (#23), and a sticky pending-changes bar with a bottom sheet in the cube editor (#30).

**Architecture:** Pure-frontend batch on branch `feature/mobile-followups` (already cut from master; the approved spec is committed there at `docs/superpowers/specs/2026-07-19-mobile-followups-design.md`). #25 adds an `--overlay` role token to the two-tier color system and switches both `<dialog>` backdrops to it. #23 adds a delegated click handler inside the nav drawer. #30 generalizes the shared `Drawer` with a cva `side` variant (`right` | `bottom`) and adds a `PendingChangesBar` + bottom-sheet reuse of `PendingChangesPanel` in the cube editor, visible only below `lg:`.

**Tech Stack:** React 19 SPA, Tailwind v4 (semantic tokens, cva variants), Paraglide i18n (en + pl), vitest + RTL on happy-dom, oxlint/oxfmt.

## Global Constraints

- `docs/architecture/structure.md` is binding: semantic color tokens only (no raw palette classes), cva for variant-bearing components, no hardcoded user-facing strings (Paraglide `m.*()`, en + pl parity), touch targets ≥ 44px (`h-11`/`size-11`), dependency direction `routes → features → shared`.
- Never hand-edit `frontend/src/routeTree.gen.ts` or `frontend/src/paraglide/` — regenerate with `pnpm gen`.
- Tooling: oxlint + oxfmt only. Frontend commands run from `frontend/` (shell needs `nvm use 24` if `pnpm` is missing).
- Test files under `src/routes/` carry a `-` filename prefix. Tests run on happy-dom.
- Spec says "three commits, each `Fixes #N`"; this plan makes four code commits — the Drawer `side` variant is a separate prep commit without an issue footer, and the #30 feature commit follows it. Accepted deviation, noted here deliberately.
- Every commit message ends with the trailer: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Overlay token for dialog/drawer backdrops (#25)

Pure refactor, zero visual change: replace the last raw palette color in the UI layer (`backdrop:bg-black/50`) with a semantic `overlay` role token.

**Files:**
- Modify: `frontend/src/app/styles.css`
- Modify: `frontend/src/shared/ui/dialog.tsx:37`
- Modify: `frontend/src/shared/ui/drawer.tsx:48`

**Interfaces:**
- Consumes: Tailwind v4 default theme vars (`--color-black`), the existing tier-2 token pattern in `styles.css`.
- Produces: utility class `bg-overlay` (via `--color-overlay`), used as `backdrop:bg-overlay` here and again in Task 3.

- [ ] **Step 1: Add the token to both theme blocks and the `@theme inline` map**

In `frontend/src/app/styles.css`, add one line to the `:root` block (after `--danger-fg`), the identical line to the `[data-theme="dark"]` block (after its `--danger-fg`), and a mapping line to `@theme inline` (after `--color-danger-fg`):

```css
  /* in :root AND in [data-theme="dark"] — same value in both themes today */
  --overlay: --alpha(var(--color-black) / 50%);
```

```css
  /* in @theme inline */
  --color-overlay: var(--overlay);
```

`--alpha()` is Tailwind v4's build-time opacity function; it compiles to a `color-mix()` and matches the current `bg-black/50` rendering exactly.

- [ ] **Step 2: Switch both backdrops to the token**

In `frontend/src/shared/ui/dialog.tsx` (the `<dialog>` className, line 37) and `frontend/src/shared/ui/drawer.tsx` (the `<dialog>` className, line 48), replace the substring `backdrop:bg-black/50` with `backdrop:bg-overlay`. No other changes.

- [ ] **Step 3: Verify no raw palette color remains in the UI layer**

Run: `grep -rn "bg-black" frontend/src --include='*.tsx' --include='*.css'`
Expected: no matches (exit code 1).

- [ ] **Step 4: Verify the token compiles and the primitives still pass**

Run: `cd frontend && pnpm build && grep -c -- "--color-overlay" dist/assets/*.css`
Expected: build succeeds; grep prints a count ≥ 1.

Run: `cd frontend && pnpm vitest run src/shared/ui/dialog.test.tsx src/shared/ui/drawer.test.tsx`
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/app/styles.css frontend/src/shared/ui/dialog.tsx frontend/src/shared/ui/drawer.tsx
git commit -m "fix(ui): semantic overlay token for dialog/drawer backdrops

Fixes #25

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Close nav drawer on same-route link tap (#23)

The drawer-close effect in `__root.tsx` keys on `pathname`; tapping the drawer link for the current route changes nothing, so the drawer silently stays open. Add a delegated click handler; keep the pathname effect (it still covers browser back/forward while the drawer is open).

**Files:**
- Modify: `frontend/src/routes/__root.tsx` (the `<Drawer>` children, lines 113–160)
- Test: `frontend/src/routes/-root-layout.test.tsx`

**Interfaces:**
- Consumes: existing `setMenuOpen` state and `Drawer` from `@/shared/ui/drawer`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the failing test**

Append to `frontend/src/routes/-root-layout.test.tsx` (imports `screen`, `within`, `waitFor`, `userEvent` already exist at the top):

```tsx
// #23: tapping the drawer link for the route you are already on doesn't
// change the pathname, so the pathname-keyed close effect never fires —
// the delegated click handler must close the drawer anyway.
test("drawer closes when tapping the current route's link", async () => {
  await renderShell();
  // Navigate to /cards via the desktop nav first.
  await userEvent.click(screen.getByRole("link", { name: "Cards" }));
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  await userEvent.click(within(drawer).getByRole("link", { name: "Cards" }));
  // Drawer children unmount on close, so only the desktop copy remains.
  await waitFor(() => expect(screen.getAllByRole("link", { name: "Cards" })).toHaveLength(1));
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm vitest run src/routes/-root-layout.test.tsx`
Expected: the new test FAILS (waitFor timeout — two "Cards" links remain because the drawer stays open). The three existing tests still pass.

- [ ] **Step 3: Add the delegated close handler**

In `frontend/src/routes/__root.tsx`, wrap everything between `<Drawer …>` and `</Drawer>` (the `<nav>`, both `<hr>`s, the auth block, and the LanguageSwitcher div) in a single wrapper div. `className="contents"` keeps the flex column layout of the drawer's internal container intact:

```tsx
      <Drawer
        id="mobile-nav-drawer"
        open={menuOpen}
        onClose={() => setMenuOpen(false)}
        label={m.nav_menu()}
      >
        {/* Delegated close-on-link-tap: the pathname effect above misses
            same-route taps (pathname doesn't change). Links are natively
            keyboard-activatable (Enter fires click), so the wrapper needs
            no key handler of its own. */}
        {/* oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions */}
        <div
          className="contents"
          onClick={(e) => {
            if ((e.target as Element).closest("a")) setMenuOpen(false);
          }}
        >
          {/* …existing children, unchanged… */}
        </div>
      </Drawer>
```

The logout button is intentionally not covered: its mutation redirects, and the pathname effect closes the drawer then.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm vitest run src/routes/-root-layout.test.tsx`
Expected: all 4 tests PASS.

- [ ] **Step 5: Lint (verify the suppression names the rules oxlint actually fires)**

Run: `cd frontend && pnpm lint`
Expected: 0 warnings. If oxlint reports a differently-named jsx-a11y rule for the div's onClick, adjust the `oxlint-disable-next-line` comment to the reported rule names — do not restructure the code.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/routes/__root.tsx frontend/src/routes/-root-layout.test.tsx
git commit -m "fix(nav): close drawer when tapping the current route's link

Fixes #23

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: `Drawer side="bottom"` variant

Generalize the shared Drawer into a cva-variant component so #30 can use it as a bottom sheet. Same native `<dialog>` foundation (focus trap, Esc + backdrop dismiss, focus restoration); only positioning classes change.

**Files:**
- Modify: `frontend/src/shared/ui/drawer.tsx`
- Test: `frontend/src/shared/ui/drawer.test.tsx`

**Interfaces:**
- Consumes: `bg-overlay` token from Task 1; `cva` + `VariantProps` from `class-variance-authority` (already a dependency, see `button.tsx`).
- Produces: `Drawer` accepts optional `side?: "right" | "bottom"` (default `"right"`). Task 5 calls `<Drawer side="bottom" id={…} open={…} onClose={…} label={…}>`.

- [ ] **Step 1: Write the failing tests**

Append to `frontend/src/shared/ui/drawer.test.tsx`:

```tsx
// The className assertions pin the positioning contract of each side:
// right pins with ml-auto, bottom pins with mt-auto + full width.
test("default side stays the right-hand drawer", () => {
  render(
    <Drawer open onClose={() => {}} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  expect(screen.getByRole("dialog").className).toContain("ml-auto");
});

test("side=bottom pins to the viewport bottom and dismisses like the right drawer", async () => {
  const onClose = vi.fn();
  render(
    <Drawer open onClose={onClose} label="Pending changes" side="bottom">
      <p>Sheet content</p>
    </Drawer>,
  );
  const dialog = screen.getByRole("dialog", { name: "Pending changes" });
  expect(screen.getByText("Sheet content")).toBeInTheDocument();
  expect(dialog.className).toContain("mt-auto");
  expect(dialog.className).not.toContain("ml-auto");
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalledTimes(1);
  await userEvent.click(dialog); // dialog element itself = backdrop area
  expect(onClose).toHaveBeenCalledTimes(2);
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && pnpm vitest run src/shared/ui/drawer.test.tsx`
Expected: the `side=bottom` test FAILS on `toContain("mt-auto")` (the `side` prop doesn't exist yet and is ignored at runtime). The default-side test passes already — it's the regression pin.

- [ ] **Step 3: Implement the cva variant**

Replace the top of `frontend/src/shared/ui/drawer.tsx` (imports, and the component signature + className) as follows; the `useEffect` body, the explanatory comments, and the inner children markup stay exactly as they are:

```tsx
import { cva, type VariantProps } from "class-variance-authority";
import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

// Positioning trick on both sides: the UA gives a modal <dialog> inset:0
// with auto margins; zeroing all margins and re-adding a single auto
// margin pins it to the opposite edge.
const drawerVariants = cva(
  "fixed m-0 border-border bg-surface p-4 text-fg shadow-lg backdrop:bg-overlay",
  {
    variants: {
      side: {
        right: "mr-0 ml-auto h-dvh max-h-none w-72 max-w-[80vw] border-l",
        bottom: "mt-auto max-h-[85svh] w-full max-w-none overflow-y-auto rounded-t-xl border-t",
      },
    },
    defaultVariants: { side: "right" },
  },
);

// Sheet on the native <dialog> element (same foundation as Dialog):
// showModal() provides the focus trap, Esc-to-close (fires the close
// event), ::backdrop, and focus restoration to the opener.
export function Drawer({
  open,
  onClose,
  label,
  children,
  id,
  side,
}: {
  open: boolean;
  onClose: () => void;
  label: string;
  children: ReactNode;
  id?: string;
  side?: VariantProps<typeof drawerVariants>["side"];
}) {
```

and the `<dialog>`'s className becomes:

```tsx
      className={drawerVariants({ side })}
```

(The old literal className string is deleted; everything else in the file is untouched, including the `h-full`/`overflow-y-auto` inner container — `h-full` is harmless in the auto-height bottom sheet.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && pnpm vitest run src/shared/ui/drawer.test.tsx src/routes/-root-layout.test.tsx`
Expected: all PASS (root-layout included to prove the default-side nav drawer is unaffected).

- [ ] **Step 5: Typecheck and commit**

Run: `cd frontend && pnpm typecheck:raw`
Expected: clean.

```bash
git add frontend/src/shared/ui/drawer.tsx frontend/src/shared/ui/drawer.test.tsx
git commit -m "feat(ui): bottom-sheet side variant for Drawer

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: `pendingTotals` helper

The bar shows total copies per side ("+3 −1"). `pendingCount` counts distinct cards (map sizes) — a different number. Add a sibling helper.

**Files:**
- Modify: `frontend/src/features/cubes/lib/pendingDiff.ts` (append at end)
- Test: `frontend/src/features/cubes/lib/pendingDiff.test.ts` (append)

**Interfaces:**
- Consumes: existing `PendingState`, `emptyPending`, `pendingReducer` from the same file.
- Produces: `pendingTotals(state: PendingState): { adds: number; removes: number }` — Task 5's bar calls this.

- [ ] **Step 1: Write the failing test**

Append to `frontend/src/features/cubes/lib/pendingDiff.test.ts`. The file already tests the reducer — **reuse its existing card/entry fixture helpers if it has them** (match their names); otherwise add these local factories above the new test:

```ts
const totalsCard = (oracleId: string) => ({
  scryfallId: `s-${oracleId}`,
  oracleId,
  name: oracleId,
  manaCost: "{1}",
  typeLine: "Artifact",
  imageSmall: null,
});
const totalsEntry = (oracleId: string, quantity: number) => ({
  ...totalsCard(oracleId),
  cmc: 1,
  colors: [],
  colorIdentity: [],
  rarity: "common",
  imageNormal: null,
  quantity,
});

test("pendingTotals sums copies per side (pendingCount counts cards)", () => {
  expect(pendingTotals(emptyPending)).toEqual({ adds: 0, removes: 0 });
  let s = pendingReducer(emptyPending, { type: "add", card: totalsCard("a") });
  s = pendingReducer(s, { type: "add", card: totalsCard("a") });
  s = pendingReducer(s, { type: "add", card: totalsCard("b") });
  s = pendingReducer(s, { type: "decrement", entry: totalsEntry("c", 2) });
  // 2 copies of a + 1 of b = 3 adds; 1 copy of c removed.
  expect(pendingTotals(s)).toEqual({ adds: 3, removes: 1 });
});
```

Add `pendingTotals` to the file's import from `./pendingDiff`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm vitest run src/features/cubes/lib/pendingDiff.test.ts`
Expected: FAIL — `pendingTotals` is not exported.

- [ ] **Step 3: Implement**

Append to `frontend/src/features/cubes/lib/pendingDiff.ts`:

```ts
// Total copies on each side of the pending diff. pendingCount counts
// distinct cards; the mobile summary bar shows copies ("+3 −1").
export function pendingTotals(state: PendingState): { adds: number; removes: number } {
  let adds = 0;
  for (const { quantity } of state.adds.values()) adds += quantity;
  let removes = 0;
  for (const { quantity } of state.removes.values()) removes += quantity;
  return { adds, removes };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm vitest run src/features/cubes/lib/pendingDiff.test.ts`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/cubes/lib/pendingDiff.ts frontend/src/features/cubes/lib/pendingDiff.test.ts
git commit -m "feat(cubes): pendingTotals helper for the mobile summary bar

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: PendingChangesBar + bottom sheet in the cube editor (#30)

The feature commit: below `lg:` the in-flow panel hides, a fixed bar appears while dirty (summary button opens a bottom sheet reusing the panel; Save commits directly), and the panel gains a chrome-less `sheet` variant. One commit — splitting would leave an intermediate commit where the panel is hidden on mobile with no bar to replace it.

**Files:**
- Create: `frontend/src/features/cubes/components/PendingChangesBar.tsx`
- Modify: `frontend/src/features/cubes/components/PendingChangesPanel.tsx`
- Modify: `frontend/src/features/cubes/components/CubeEditorPage.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/cubes/components/CubeEditorPage.test.tsx`

**Interfaces:**
- Consumes: `pendingTotals` (Task 4), `Drawer side="bottom"` (Task 3), `PendingState` type, `cn` from `@/shared/lib/cn`, `Button` from `@/shared/ui/button`.
- Produces: `PendingChangesBar({ pending, sheetId, onExpand, onSave, saving })`; `PendingChangesPanel` gains optional `variant?: "page" | "sheet"` (default `"page"`). Nothing later depends on these.

- [ ] **Step 1: Add the i18n key (en + pl) and regenerate**

In `frontend/messages/en.json`, after `"cubes_discard_changes"`:

```json
  "cubes_pending_bar_review": "Review pending changes: +{adds}, −{removes}",
```

In `frontend/messages/pl.json`, after `"cubes_discard_changes"`:

```json
  "cubes_pending_bar_review": "Przejrzyj oczekujące zmiany: +{adds}, −{removes}",
```

Run: `cd frontend && pnpm gen`
Expected: succeeds; `m.cubes_pending_bar_review` now exists.

- [ ] **Step 2: Write the failing tests**

In `frontend/src/features/cubes/components/CubeEditorPage.test.tsx`:

**(a)** In the four existing tests that do `screen.getByRole("button", { name: /save changes/i })` (the two commit tests, the blocker test, and the multi-copy test — NOT "save disabled with no pending changes"), scope the query to the page panel, because the bar adds a second Save button once dirty:

```tsx
fireEvent.click(
  within(screen.getByRole("complementary")).getByRole("button", { name: /save changes/i }),
);
```

**(b)** Append three new tests:

```tsx
// #30: below lg the editor shows a fixed summary bar while dirty. CSS
// isn't loaded in unit tests, so visibility classes (hidden/lg:) are not
// asserted here — mount/unmount and wiring are.
test("mobile bar appears when dirty and saves directly", async () => {
  render(<CubeEditorPage />);
  expect(screen.queryByRole("region", { name: /pending changes/i })).toBeNull();
  fireEvent.click(screen.getByText("pick sol ring"));
  const bar = await screen.findByRole("region", { name: /pending changes/i });
  fireEvent.click(within(bar).getByRole("button", { name: /save changes/i }));
  expect(mocks.mutate).toHaveBeenCalledWith(
    expect.objectContaining({
      expectedVersion: 3,
      adds: [{ scryfallId: "s-ring", quantity: 1 }],
      removes: [],
    }),
    expect.anything(),
  );
});

test("tapping the bar summary opens the sheet with the full panel", async () => {
  render(<CubeEditorPage />);
  fireEvent.click(screen.getByText("pick sol ring"));
  const bar = await screen.findByRole("region", { name: /pending changes/i });
  fireEvent.click(within(bar).getByRole("button", { name: /review pending changes/i }));
  const sheet = screen.getByRole("dialog", { name: /pending changes/i });
  expect(within(sheet).getByText("Sol Ring")).toBeDefined();
  expect(within(sheet).getByRole("button", { name: /save changes/i })).toBeDefined();
});

test("discard in the sheet hides the bar and empties the sheet", async () => {
  render(<CubeEditorPage />);
  fireEvent.click(screen.getByText("pick sol ring"));
  const bar = await screen.findByRole("region", { name: /pending changes/i });
  fireEvent.click(within(bar).getByRole("button", { name: /review pending changes/i }));
  const sheet = screen.getByRole("dialog", { name: /pending changes/i });
  fireEvent.click(within(sheet).getByRole("button", { name: /discard/i }));
  await waitFor(() =>
    expect(screen.queryByRole("region", { name: /pending changes/i })).toBeNull(),
  );
  // Sheet closed too — its children unmount.
  expect(within(sheet).queryByText("Sol Ring")).toBeNull();
});
```

- [ ] **Step 3: Run tests to verify the new ones fail**

Run: `cd frontend && pnpm vitest run src/features/cubes/components/CubeEditorPage.test.tsx`
Expected: the three new tests FAIL (no `region` role exists); the re-scoped existing tests still PASS (the page panel is the only Save button until the bar exists).

- [ ] **Step 4: Create `PendingChangesBar`**

Create `frontend/src/features/cubes/components/PendingChangesBar.tsx`:

```tsx
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import type { PendingState } from "../lib/pendingDiff";
import { pendingTotals } from "../lib/pendingDiff";

// Collapsed summary of the pending diff, fixed to the viewport bottom
// below lg where the full panel stacks out of reach under a long card
// list. Rendered only while there are pending changes.
export function PendingChangesBar({
  pending,
  sheetId,
  onExpand,
  onSave,
  saving,
}: {
  pending: PendingState;
  sheetId: string;
  onExpand: () => void;
  onSave: () => void;
  saving: boolean;
}) {
  const totals = pendingTotals(pending);
  return (
    <section
      aria-label={m.cubes_pending_title()}
      className="fixed inset-x-0 bottom-0 z-10 border-t border-border bg-surface-raised shadow-lg lg:hidden"
    >
      <div className="mx-auto flex max-w-4xl items-center justify-between gap-3 p-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
        <button
          type="button"
          aria-label={m.cubes_pending_bar_review({ adds: totals.adds, removes: totals.removes })}
          aria-haspopup="dialog"
          aria-controls={sheetId}
          onClick={onExpand}
          className="flex h-11 min-w-0 flex-1 items-center gap-3 rounded-md px-2 text-sm hover:bg-surface"
        >
          <span className="font-semibold text-accent">+{totals.adds}</span>
          <span className="font-semibold text-danger">−{totals.removes}</span>
        </button>
        <Button type="button" disabled={saving} onClick={onSave}>
          {m.cubes_save_changes()}
        </Button>
      </div>
    </section>
  );
}
```

- [ ] **Step 5: Give `PendingChangesPanel` a `sheet` variant and a collision-free note id**

In `frontend/src/features/cubes/components/PendingChangesPanel.tsx`:

1. Add `useId` to the imports: `import { useId } from "react";`
2. Add a variant map above the component and extend the props:

```tsx
const panelVariants = {
  // Page flow: hidden below lg (the bar + sheet take over), side column at lg.
  page: "hidden w-full flex-col gap-3 rounded-lg border border-border bg-surface-raised p-4 lg:flex lg:w-72",
  // Inside the bottom sheet: the Drawer already provides padding and chrome.
  sheet: "flex w-full flex-col gap-3",
} as const;
```

```tsx
export function PendingChangesPanel({
  pending,
  dispatch,
  note,
  onNoteChange,
  onSave,
  onDiscard,
  saving,
  variant = "page",
}: {
  // …existing props unchanged…
  variant?: keyof typeof panelVariants;
}) {
```

3. The `<aside>` className becomes `className={panelVariants[variant]}`.
4. The page and sheet instances both render this component, so the hardcoded `id="change-note"` would duplicate in the DOM. Replace it: `const noteId = useId();` inside the component, then `htmlFor={noteId}` on the `<Label>` and `id={noteId}` on the `<textarea>`.

- [ ] **Step 6: Wire the editor page**

In `frontend/src/features/cubes/components/CubeEditorPage.tsx`:

1. Imports: add `Drawer` from `@/shared/ui/drawer`, `cn` from `@/shared/lib/cn`, `PendingChangesBar` from `./PendingChangesBar`.
2. State: `const [sheetOpen, setSheetOpen] = useState(false);` and `const sheetId = "pending-changes-sheet";` (module-level const above the component is fine too — only one editor renders at a time).
3. Extract the discard handler (currently inline on the panel) so both instances and the sheet share it:

```tsx
  const discard = () => {
    dispatch({ type: "reset" });
    setNote("");
    setSheetOpen(false);
  };
```

4. Root div gets clearance for the fixed bar while it's shown:

```tsx
    <div className={cn("flex flex-col gap-6", dirty && "pb-24 lg:pb-0")}>
```

5. The existing `<PendingChangesPanel …/>` keeps its props but its `onDiscard` becomes `{discard}` (default `variant="page"` now hides it below `lg:`).
6. After the closing `</div>` of the `flex flex-col gap-6 lg:flex-row` row (before `<CubeSettingsSection …/>`), add:

```tsx
      {dirty && (
        <PendingChangesBar
          pending={pending}
          sheetId={sheetId}
          onExpand={() => setSheetOpen(true)}
          onSave={save}
          saving={commit.isPending}
        />
      )}
      <Drawer
        side="bottom"
        id={sheetId}
        open={sheetOpen}
        onClose={() => setSheetOpen(false)}
        label={m.cubes_pending_title()}
      >
        <PendingChangesPanel
          variant="sheet"
          pending={pending}
          dispatch={dispatch}
          note={note}
          onNoteChange={setNote}
          onSave={save}
          onDiscard={discard}
          saving={commit.isPending}
        />
      </Drawer>
```

(Save from bar or sheet uses the existing `save()`, which navigates away with `ignoreBlocker` on success — bar and sheet unmount with the page.)

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd frontend && pnpm vitest run src/features/cubes/components/CubeEditorPage.test.tsx`
Expected: all tests PASS (4 re-scoped + 1 untouched + 3 new).

- [ ] **Step 8: Full frontend verification**

Run: `cd frontend && pnpm typecheck:raw && pnpm lint && pnpm fmt:check && pnpm test`
Expected: typecheck clean, 0 lint warnings, formatting clean, full suite green.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/features/cubes/components/PendingChangesBar.tsx \
        frontend/src/features/cubes/components/PendingChangesPanel.tsx \
        frontend/src/features/cubes/components/CubeEditorPage.tsx \
        frontend/src/features/cubes/components/CubeEditorPage.test.tsx \
        frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(cubes): sticky pending-changes bar with bottom sheet on mobile

Below lg the editor panel hides; a fixed bar (summary + direct Save)
appears while dirty, expanding to the full panel in a bottom sheet.

Fixes #30

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Live 360px verification, push, PR

`docs/architecture/structure.md` rule 9 mandates a manual 360px check for mobile-affecting changes. Verify all three fixes in the running app, then open the PR.

**Files:** none created/modified (verification + delivery only).

**Interfaces:**
- Consumes: the full branch; dev stack via `make up`; seeded dev login test@example.com / testpass123 (admin, owns one cube).

- [ ] **Step 1: Start the dev stack**

Run: `make up` (from the repo root; leave it running). Frontend at http://localhost:5173 (Vite), backend :8080. If the browser tools are unavailable, ask the user to run the checks below instead — do not skip them.

- [ ] **Step 2: 360×740 Playwright sweep of the three changes**

Using the Playwright browser tools, viewport 360×740, log in as test@example.com / testpass123:

1. **#23:** Navigate to `/cards`. Open the hamburger drawer, tap "Cards" (the current route). Expected: drawer closes, nothing else changes.
2. **#25:** With the drawer open, screenshot: backdrop dims the page exactly as before (black 50%) in BOTH light and dark themes (toggle via the header button).
3. **#30:** Open the owned cube → Edit. Expected: no pending panel below the list. Add a card via autocomplete. Expected: fixed bottom bar appears with "+1 −0" and Save; last list rows not covered (scroll to bottom). Tap the summary. Expected: bottom sheet slides over a dimmed page showing the full panel (undo list, note, Save/Discard); Esc and backdrop tap both dismiss. Tap Discard in the sheet. Expected: sheet closes, bar disappears. Re-add a card and tap the bar's Save. Expected: commit succeeds and navigates to the cube page.
4. **Desktop sanity:** resize to 1280×800, reload the editor. Expected: side panel present as before, no bar.

Fix anything found before proceeding (amend or new commit as appropriate).

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin feature/mobile-followups
gh pr create --title "Mobile follow-ups: overlay token, drawer same-route close, sticky pending bar" --body "$(cat <<'EOF'
Batch of three approved mobile follow-ups (spec: docs/superpowers/specs/2026-07-19-mobile-followups-design.md):

- **#25** — semantic `overlay` token; both `<dialog>` backdrops now `backdrop:bg-overlay` (zero visual change, last raw palette color removed from the UI layer).
- **#23** — nav drawer closes on same-route link tap via a delegated click handler (pathname effect kept for back/forward).
- **#30** — cube editor below `lg:`: fixed pending-changes bar (summary + direct Save) while dirty, expanding to the full panel in a `Drawer side="bottom"` bottom sheet.

Verified live at 360×740 (drawer close, backdrop dim in both themes, bar/sheet flow, desktop sanity at 1280).

Closes #25, Closes #23, Closes #30

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: Watch CI**

Run: `gh pr checks --watch`
Expected: all 4 jobs green (backend, frontend, govulncheck, api-client-fresh). If a job fails, fix on the branch and push — do not merge; Mateusz merges after CI.
