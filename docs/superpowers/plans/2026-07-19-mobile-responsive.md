# Mobile Responsive Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every screen usable on a 360px-wide phone and encode mobile display as a binding acceptance criterion in `docs/architecture/structure.md`.

**Architecture:** Convention-first sweep. First the binding rule and the shared pieces (nav drawer primitive, dialog mobile sizing, button `lg` size, iOS-zoom-safe inputs), then the root layout gets a hamburger + drawer below `md:`, then feature-by-feature fixes with plain Tailwind breakpoint classes, then a full-app 360px verification sweep in a real browser.

**Tech Stack:** React 19 + Vite, Tailwind v4, TanStack Router/Query, cva + `cn()`, Paraglide i18n, vitest + RTL (happy-dom; axe files use jsdom), Playwright MCP browser for viewport verification.

**Spec:** `docs/superpowers/specs/2026-07-19-mobile-responsive-design.md`

## Global Constraints

- Work on branch `feature/mobile-responsive` (already exists, contains the spec commit). Master is protected — integration happens via PR at the end.
- **Support floor 360px:** no horizontal page scroll at ≥360px viewport width; wide content scrolls inside its own `overflow-x-auto` container.
- **Mobile-first Tailwind:** base classes are the phone layout; `sm:`/`md:`/`lg:` layer desktop on top.
- **Touch targets:** ≥44px tall (h-11/size-11/h-12) for player-facing interactive controls.
- **Semantic color tokens only** (`bg-surface`, `bg-surface-raised`, `text-fg`, `text-fg-muted`, `border-border`, `bg-accent`, `text-accent-fg`, `bg-danger`, `text-danger-fg`). Never raw palette classes.
- **No hardcoded user-facing strings.** Every string is `m.some_key()` from `@/paraglide/messages`; add keys to BOTH `frontend/messages/en.json` and `frontend/messages/pl.json` (compiler enforces parity). After editing messages run `pnpm gen` (or any vite/vitest run) to regenerate.
- **Conditional/merged classes go through `cn()`** from `@/shared/lib/cn`.
- **Never hand-edit generated code:** `frontend/src/paraglide/`, `frontend/src/routeTree.gen.ts`, `frontend/src/shared/api/`.
- **Test placement:** next to the code as `*.test.tsx`; inside `frontend/src/routes/` the filename needs a `-` prefix (route generation skips it). Axe tests need `// @vitest-environment jsdom` as line 1.
- **Tooling:** oxlint + oxfmt run via lefthook pre-commit automatically. Frontend commands run from `frontend/` with pnpm (shell needs `nvm use 24` if pnpm is missing).
- Run tests from `frontend/`: `pnpm vitest run <path>` for one file, `pnpm test` for all.

---

### Task 1: Responsive design rule in structure.md

**Files:**
- Modify: `docs/architecture/structure.md` (insert new rule 9 after rule 8 "Generated artifacts", before "### Adding shadcn/ui components")

**Interfaces:**
- Produces: the binding convention every later task implements; reviewers of Tasks 2–8 check against this text.

- [ ] **Step 1: Add rule 9 to the rules list**

Append to the numbered list under `### The rules` (after item 8):

```markdown
9. **Responsive design.** Mobile is an acceptance criterion, not a
   follow-up: every new screen/component is verified at 360px and
   desktop width before PR (same footing as the axe smoke test).
   - **Support floor 360px.** No horizontal page scroll at ≥360px.
     Intrinsically wide content (tables) scrolls inside its own
     `overflow-x-auto` wrapper.
   - **Mobile-first Tailwind.** Base classes are the phone layout;
     `sm:`/`md:`/`lg:` layer desktop on top.
   - **Touch targets.** Interactive controls on player-facing flows are
     ≥44px tall (`h-11`+) or have equivalent hit area; never two small
     targets adjacent without a gap.
   - **Use the established patterns.** App nav = the header drawer
     (`shared/ui/drawer.tsx`); wide tables = `overflow-x-auto` wrapper;
     dialogs = `shared/ui/dialog.tsx` (handles mobile sizing); forms =
     single-column `max-w-md`. Inputs use ≥16px font on mobile
     (`text-base sm:text-sm`) so iOS Safari does not zoom on focus.
```

- [ ] **Step 2: Commit**

```bash
git add docs/architecture/structure.md
git commit -m "docs(architecture): add binding responsive design rule"
```

---

### Task 2: Button `lg` size variant

**Files:**
- Modify: `frontend/src/shared/ui/button.tsx:17-21`
- Test: `frontend/src/shared/ui/button.test.tsx`

**Interfaces:**
- Produces: `<Button size="lg">` → `h-11 px-6` (44px tall). Used by Task 6 (ResultForm) and available everywhere.

- [ ] **Step 1: Write the failing test**

Add to the `describe("Button", ...)` block in `frontend/src/shared/ui/button.test.tsx`:

```tsx
  it("renders the lg touch size", () => {
    render(<Button size="lg">Report</Button>);
    const btn = screen.getByRole("button", { name: "Report" });
    expect(btn.className).toContain("h-11");
  });
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm vitest run src/shared/ui/button.test.tsx`
Expected: FAIL — TS/type error or assertion failure (`size="lg"` is not a valid variant yet).

- [ ] **Step 3: Add the variant**

In `frontend/src/shared/ui/button.tsx`, change the `size` block:

```tsx
      size: {
        default: "h-9 px-4 py-2",
        sm: "h-8 px-3 text-xs",
        lg: "h-11 px-6",
        icon: "size-9",
      },
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm vitest run src/shared/ui/button.test.tsx`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/shared/ui/button.tsx frontend/src/shared/ui/button.test.tsx
git commit -m "feat(ui): add lg (44px) button size for touch targets"
```

---

### Task 3: Dialog and Input mobile sizing

**Files:**
- Modify: `frontend/src/shared/ui/dialog.tsx:37`
- Modify: `frontend/src/shared/ui/input.tsx:8`

**Interfaces:**
- Consumes: nothing new.
- Produces: `Dialog` no longer edge-to-edge on phones, scrolls internally when tall; `Input` renders 16px font on mobile (no iOS focus zoom). No API changes — all call sites (ImportDialog, forms) inherit.

These are CSS-only changes; happy-dom does no layout so there are no new unit tests (per spec: classname-assertion tests are noise). Existing tests guard against regressions in behavior.

- [ ] **Step 1: Fix dialog sizing**

In `frontend/src/shared/ui/dialog.tsx`, replace the `<dialog>` className:

```tsx
      className="m-auto max-h-[85svh] w-[calc(100%-2rem)] max-w-lg overflow-y-auto rounded-xl border border-border bg-surface p-6 text-fg shadow-lg backdrop:bg-black/50"
```

(`w-full` → `w-[calc(100%-2rem)]` keeps a 16px gutter on each side below the `max-w-lg` breakpoint; `max-h-[85svh] overflow-y-auto` scrolls tall content inside the panel — `svh` so the iOS dynamic toolbar doesn't hide the bottom.)

- [ ] **Step 2: Fix input font size**

In `frontend/src/shared/ui/input.tsx`, replace `text-sm` with `text-base sm:text-sm` inside the `cn(...)` string, so it reads:

```tsx
        "h-9 w-full rounded-md border border-border bg-surface-raised px-3 py-1 text-base text-fg placeholder:text-fg-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-50 sm:text-sm",
```

- [ ] **Step 3: Run existing shared/ui tests**

Run: `cd frontend && pnpm vitest run src/shared/ui`
Expected: PASS (dialog, button, combobox, theme-toggle tests all green).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/shared/ui/dialog.tsx frontend/src/shared/ui/input.tsx
git commit -m "fix(ui): dialog gutters + internal scroll on mobile; 16px inputs to stop iOS zoom"
```

---

### Task 4: Drawer primitive

**Files:**
- Create: `frontend/src/shared/ui/drawer.tsx`
- Test: `frontend/src/shared/ui/drawer.test.tsx`

**Interfaces:**
- Consumes: `Button` from `@/shared/ui/button`, `m.dialog_close()` (existing message).
- Produces: `Drawer({ open: boolean; onClose: () => void; label: string; children: ReactNode })` — a right-side sheet on the native `<dialog>` element. Task 5 composes it in the root layout.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/shared/ui/drawer.test.tsx` (mirrors `dialog.test.tsx` conventions):

```tsx
import { render, screen, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { Drawer } from "./drawer";

afterEach(() => {
  cleanup();
});

test("renders children when open, nothing when closed", () => {
  const { rerender } = render(
    <Drawer open={false} onClose={() => {}} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  expect(screen.queryByText("Nav items")).not.toBeInTheDocument();
  rerender(
    <Drawer open onClose={() => {}} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  expect(screen.getByText("Nav items")).toBeInTheDocument();
  expect(screen.getByRole("dialog")).toHaveAccessibleName("Menu");
});

test("close button fires onClose", async () => {
  const onClose = vi.fn();
  render(
    <Drawer open onClose={onClose} label="Menu">
      <p>Nav items</p>
    </Drawer>,
  );
  await userEvent.click(screen.getByRole("button", { name: "Close" }));
  expect(onClose).toHaveBeenCalled();
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd frontend && pnpm vitest run src/shared/ui/drawer.test.tsx`
Expected: FAIL — cannot resolve `./drawer`.

- [ ] **Step 3: Implement the drawer**

Create `frontend/src/shared/ui/drawer.tsx`:

```tsx
import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";

// Right-side sheet on the native <dialog> element (same foundation as
// Dialog): showModal() provides the focus trap, Esc-to-close (fires the
// close event), ::backdrop, and focus restoration to the opener.
export function Drawer({
  open,
  onClose,
  label,
  children,
}: {
  open: boolean;
  onClose: () => void;
  label: string;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) {
      // Test environments may lack showModal — fall back to the open attr.
      if (typeof el.showModal === "function") el.showModal();
      else el.setAttribute("open", "");
    } else if (!open && el.open) {
      el.close();
    }
  }, [open]);
  return (
    <dialog
      ref={ref}
      aria-label={label}
      onClose={onClose}
      className="fixed m-0 mr-0 ml-auto h-dvh max-h-none w-72 max-w-[80vw] border-l border-border bg-surface p-4 text-fg shadow-lg backdrop:bg-black/50"
    >
      {open && (
        <div className="flex h-full flex-col gap-2 overflow-y-auto">
          <div className="flex justify-end">
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={m.dialog_close()}
              onClick={onClose}
            >
              ✕
            </Button>
          </div>
          {children}
        </div>
      )}
    </dialog>
  );
}
```

(The UA gives modal dialogs `position: fixed; inset: 0; margin: auto; max-height: calc(100% - …)`. `mr-0 ml-auto h-dvh max-h-none` pins it to the right edge full-height; `max-w-[80vw]` keeps a tap-to-dismiss backdrop strip at 360px.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd frontend && pnpm vitest run src/shared/ui/drawer.test.tsx`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/shared/ui/drawer.tsx frontend/src/shared/ui/drawer.test.tsx
git commit -m "feat(ui): drawer primitive on native dialog for mobile nav"
```

---

### Task 5: Root layout — mobile header with nav drawer

**Files:**
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json` (add `nav_menu`)
- Modify: `frontend/src/routes/__root.tsx`
- Test: `frontend/src/routes/-root-layout.test.tsx` (create)
- Test: `frontend/src/routes/-a11y.test.tsx` (create)

**Interfaces:**
- Consumes: `Drawer` from Task 4 (`open`, `onClose`, `label`, `children`).
- Produces: `RootLayout` exported from `__root.tsx` (needed by the test); header shows full nav at `md:`+ and brand + ThemeToggle + hamburger below; drawer closes on route change.

- [ ] **Step 1: Add the message key**

In `frontend/messages/en.json`, after the `"nav_logout"` entry add:

```json
  "nav_menu": "Menu",
```

In `frontend/messages/pl.json`, in the same position add:

```json
  "nav_menu": "Menu",
```

- [ ] **Step 2: Write the failing test**

Create `frontend/src/routes/-root-layout.test.tsx` (the `-` prefix keeps it out of route generation). Tailwind CSS is not loaded in tests, so the desktop nav is "visible" too — assertions count elements and scope queries to the drawer via `within`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { RootLayout } from "./__root";

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 401 })),
  );
  vi.stubEnv("DEV", false);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

function renderShell() {
  const rootRoute = createRootRoute({ component: RootLayout });
  const paths = ["/", "/cards", "/cubes", "/events", "/login"];
  const children = paths.map((path) =>
    createRoute({ getParentRoute: () => rootRoute, path, component: () => null }),
  );
  const router = createRouter({
    routeTree: rootRoute.addChildren(children),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
      <RouterProvider router={router as any} />
    </QueryClientProvider>,
  );
}

test("hamburger opens the drawer with nav links", async () => {
  renderShell();
  // Desktop nav renders one copy; drawer is closed so no second copy.
  expect(screen.getAllByRole("link", { name: "Cards" })).toHaveLength(1);
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  expect(within(drawer).getByRole("link", { name: "Cards" })).toBeInTheDocument();
  expect(within(drawer).getByRole("link", { name: "Events" })).toBeInTheDocument();
  expect(within(drawer).getByRole("link", { name: "Log in" })).toBeInTheDocument();
});

test("drawer closes on navigation", async () => {
  renderShell();
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  const drawer = screen.getByRole("dialog");
  await userEvent.click(within(drawer).getByRole("link", { name: "Cards" }));
  await waitFor(() =>
    expect(screen.getAllByRole("link", { name: "Cards" })).toHaveLength(1),
  );
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd frontend && pnpm vitest run src/routes/-root-layout.test.tsx`
Expected: FAIL — `RootLayout` is not exported (and no button named "Menu").

- [ ] **Step 4: Implement the mobile header**

Rewrite the `RootLayout` component in `frontend/src/routes/__root.tsx` (imports change too — add `useState`, `Drawer`, keep everything else):

```tsx
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackDevtools } from "@tanstack/react-devtools";
import { createRootRoute, Link, Outlet, useRouterState } from "@tanstack/react-router";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { useEffect, useRef, useState } from "react";
import { useLogout, useMe } from "@/features/auth/api";
import { m } from "@/paraglide/messages";
import { LanguageSwitcher } from "@/shared/i18n/LanguageSwitcher";
import { Button } from "@/shared/ui/button";
import { Drawer } from "@/shared/ui/drawer";
import { ThemeToggle } from "@/shared/ui/theme-toggle";

export const Route = createRootRoute({ component: RootLayout });

const drawerItem =
  "flex h-12 items-center rounded-md px-3 text-fg hover:bg-surface-raised";

export function RootLayout() {
  const me = useMe();
  const logout = useLogout();
  const mainRef = useRef<HTMLElement>(null);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const firstRender = useRef(true);
  const [menuOpen, setMenuOpen] = useState(false);

  // A11y: move focus to the page content on route change so screen readers
  // announce the new page instead of staying on the clicked link.
  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false;
      return;
    }
    mainRef.current?.focus();
  }, [pathname]);

  // Close the mobile drawer whenever the route changes.
  useEffect(() => {
    setMenuOpen(false);
  }, [pathname]);

  return (
    <div className="min-h-svh">
      <header className="border-b border-border bg-surface-raised">
        <div className="mx-auto flex h-14 max-w-4xl items-center justify-between gap-4 px-4">
          <div className="flex items-center gap-4">
            <Link to="/" className="font-semibold text-fg hover:text-accent">
              {m.app_name()}
            </Link>
            <nav className="hidden items-center gap-4 md:flex">
              <Link to="/cards" className="text-sm text-fg-muted hover:text-fg">
                {m.nav_cards()}
              </Link>
              <Link to="/cubes" className="text-sm text-fg-muted hover:text-fg">
                {m.nav_cubes()}
              </Link>
              <Link to="/events" className="text-sm text-fg-muted hover:text-fg">
                {m.nav_events()}
              </Link>
            </nav>
          </div>
          <div className="flex items-center gap-2">
            <div className="hidden items-center gap-2 md:flex">
              {me.data ? (
                <>
                  <Button asChild variant="ghost" size="sm">
                    <Link to="/cubes/mine">{m.cubes_mine_title()}</Link>
                  </Button>
                  <Button asChild variant="ghost" size="sm">
                    <Link to="/collection">{m.nav_collection()}</Link>
                  </Button>
                  <Button asChild variant="ghost" size="sm">
                    <Link to="/account">{me.data.displayName}</Link>
                  </Button>
                  <Button type="button" variant="outline" size="sm" onClick={() => logout.mutate()}>
                    {m.nav_logout()}
                  </Button>
                </>
              ) : (
                <Button asChild variant="outline" size="sm">
                  <Link to="/login">{m.nav_login()}</Link>
                </Button>
              )}
              <LanguageSwitcher />
            </div>
            <ThemeToggle />
            <Button
              type="button"
              variant="ghost"
              size="icon"
              className="md:hidden"
              aria-label={m.nav_menu()}
              aria-expanded={menuOpen}
              onClick={() => setMenuOpen(true)}
            >
              ☰
            </Button>
          </div>
        </div>
      </header>
      <Drawer open={menuOpen} onClose={() => setMenuOpen(false)} label={m.nav_menu()}>
        <nav className="flex flex-col">
          <Link to="/cards" className={drawerItem}>
            {m.nav_cards()}
          </Link>
          <Link to="/cubes" className={drawerItem}>
            {m.nav_cubes()}
          </Link>
          <Link to="/events" className={drawerItem}>
            {m.nav_events()}
          </Link>
        </nav>
        <hr className="border-border" />
        {me.data ? (
          <div className="flex flex-col">
            <Link to="/cubes/mine" className={drawerItem}>
              {m.cubes_mine_title()}
            </Link>
            <Link to="/collection" className={drawerItem}>
              {m.nav_collection()}
            </Link>
            <Link to="/account" className={drawerItem}>
              {me.data.displayName}
            </Link>
            <Button
              type="button"
              variant="ghost"
              className="h-12 justify-start px-3 text-base font-normal"
              onClick={() => logout.mutate()}
            >
              {m.nav_logout()}
            </Button>
          </div>
        ) : (
          <Link to="/login" className={drawerItem}>
            {m.nav_login()}
          </Link>
        )}
        <hr className="border-border" />
        <div className="px-3 py-2">
          <LanguageSwitcher />
        </div>
      </Drawer>
      <main ref={mainRef} tabIndex={-1} className="mx-auto max-w-4xl px-4 py-8 outline-none">
        <Outlet />
      </main>
      {import.meta.env.DEV && (
        <TanStackDevtools
          plugins={[
            { name: "TanStack Router", render: <TanStackRouterDevtoolsPanel /> },
            { name: "TanStack Query", render: <ReactQueryDevtoolsPanel /> },
          ]}
        />
      )}
    </div>
  );
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd frontend && pnpm vitest run src/routes/-root-layout.test.tsx src/shared/ui`
Expected: PASS. (If `getAllByRole("link", { name: "Cards" })` finds 2 before opening: the drawer children render only when `open` — check the `{open && ...}` guard in Drawer.)

- [ ] **Step 6: Write the a11y test (drawer open, axe clean)**

Create `frontend/src/routes/-a11y.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, createRouter, RouterProvider } from "@tanstack/react-router";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "vitest-axe";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { routeTree } from "@/routeTree.gen";

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("{}", { status: 401 })),
  );
  // See features/auth/components/a11y.test.tsx for why devtools are off.
  vi.stubEnv("DEV", false);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("root layout with open nav drawer has no axe violations", async () => {
  const router = createRouter({
    routeTree,
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const { container } = render(
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  await userEvent.click(screen.getByRole("button", { name: "Menu" }));
  expect(await axe(container)).toHaveNoViolations();
});
```

- [ ] **Step 7: Run the a11y test**

Run: `cd frontend && pnpm vitest run src/routes/-a11y.test.tsx`
Expected: PASS.

- [ ] **Step 8: Run the full frontend suite**

Run: `cd frontend && pnpm test`
Expected: PASS — existing feature a11y tests render the whole app through `routeTree` and now include the hamburger; if any fail, read the failure, it is likely a duplicate-name query that must be scoped with `within(...)`.

- [ ] **Step 9: Commit**

```bash
git add frontend/messages/en.json frontend/messages/pl.json frontend/src/routes/__root.tsx frontend/src/routes/-root-layout.test.tsx frontend/src/routes/-a11y.test.tsx
git commit -m "feat(shell): mobile header with hamburger nav drawer below md"
```

---

### Task 6: Tournaments — standings scroll, tab strip, touch-size result form

**Files:**
- Modify: `frontend/src/features/tournaments/components/StandingsTable.tsx:13-14`
- Modify: `frontend/src/features/tournaments/components/TournamentSection.tsx:84`
- Modify: `frontend/src/features/tournaments/components/ResultForm.tsx:44,106`

**Interfaces:**
- Consumes: `Button` `size="lg"` from Task 2.
- Produces: no API changes; purely presentational.

Existing tests (`StandingsTable.test.tsx`, `TournamentSection.test.tsx`, `ResultForm.test.tsx`) query by role/name and are unaffected by wrappers and classes — they are the regression net.

- [ ] **Step 1: Wrap StandingsTable in a scroll container**

In `StandingsTable.tsx`, wrap the `<table>` (the documented wide-table pattern) and give the table a minimum width so 6 columns never crush below legibility:

```tsx
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-md text-sm">
        {/* ...unchanged... */}
      </table>
    </div>
  );
```

(Close the wrapper `</div>` after `</table>`. Everything inside the table is unchanged.)

- [ ] **Step 2: Make the round tab strip scrollable**

In `TournamentSection.tsx` line 84, the tablist can hold 7+ round tabs. Change:

```tsx
      <div role="tablist" className="flex gap-2 overflow-x-auto">
```

and add `shrink-0 whitespace-nowrap` to the tab button className template so tabs scroll instead of squashing:

```tsx
            className={`shrink-0 rounded-md border border-border px-3 py-1 text-sm whitespace-nowrap ${
              r.number === round.number ? "bg-accent text-accent-fg" : "text-fg"
            }`}
```

- [ ] **Step 3: Touch-size the result form**

In `ResultForm.tsx`:

The `GamesField` input (line 44) — players type game counts mid-match; make it 44px tall with 16px font (no iOS zoom):

```tsx
        className="h-11 w-20 rounded-md border border-border bg-surface px-2 py-1 text-base text-fg"
```

The submit button (line 106) — change `size="sm"` to `size="lg"`:

```tsx
      <Button type="submit" size="lg" disabled={pending}>
        {m.tournament_report_result()}
      </Button>
```

- [ ] **Step 4: Run the tournaments tests**

Run: `cd frontend && pnpm vitest run src/features/tournaments`
Expected: PASS (all files, including keyboard and a11y tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/features/tournaments
git commit -m "feat(tournaments): mobile-ready standings scroll, tab strip, touch-size result form"
```

---

### Task 7: Collection — 44px quantity stepper

**Files:**
- Modify: `frontend/src/features/collection/components/QuantityStepper.tsx:40-60`

**Interfaces:**
- Consumes: existing `Button` `size="icon"` variant (`size-9`) + `className` override via `cn()` (consumer classes win).
- Produces: no API changes.

Existing `QuantityStepper.test.tsx` queries by `aria-label` — unaffected.

- [ ] **Step 1: Enlarge the stepper buttons**

In `QuantityStepper.tsx`, change both buttons from `size="sm"` to `size="icon"` with a 44px override (the `−` button shown; apply identically to `+`):

```tsx
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="size-11 text-base"
        aria-label={m.collection_qty_decrease({ name })}
        disabled={value <= 0}
        onClick={() => setValue((v) => Math.max(0, v - 1))}
      >
        −
      </Button>
```

- [ ] **Step 2: Run the collection tests**

Run: `cd frontend && pnpm vitest run src/features/collection`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/features/collection/components/QuantityStepper.tsx
git commit -m "feat(collection): 44px touch targets on quantity stepper"
```

---

### Task 8: Full-app 360px verification sweep

**Files:**
- Modify: any component that overflows at 360px (fixes use the established patterns; expected candidates listed below)

**Interfaces:**
- Consumes: the running dev stack (`make up`), the Playwright MCP browser tools, patterns from Tasks 1–7.
- Produces: every route passes the 360px check; list of per-feature fix commits.

This task is deliberately exploratory: the earlier tasks fixed everything found by reading code; this one catches what only a rendered browser shows. **The check for every route:** at 360×740, `document.documentElement.scrollWidth <= 360` and nothing looks broken in a screenshot; then a 1280px sanity screenshot confirming desktop is unchanged.

- [ ] **Step 1: Start the dev stack**

Run: `make up` (from repo root; Postgres + Mailpit in Docker, Go + Vite on host). Frontend at the URL Vite prints (default `http://localhost:5173`).

- [ ] **Step 2: Create a verified account and seed minimal data**

1. Browser: navigate to `/register`, register with `test@example.com` / a password / display name.
2. Open `http://localhost:8025` (Mailpit), open the verification email, follow the link.
3. Log in. Create one cube (`/cubes/new`), add a handful of cards in the editor, and create one event (`/events/new`) so detail pages have content.

- [ ] **Step 3: Sweep every route at 360×740**

Resize the browser to 360×740. For each route below: navigate, run `document.documentElement.scrollWidth` via browser evaluate (must be ≤ 360), take a screenshot, eyeball for broken layout (overlaps, unreadable squeeze, tap targets touching):

```
/            /login        /register      /forgot-password   /reset-password
/verify-email             /account
/cards
/cubes       /cubes/mine   /cubes/new
/cubes/<id>  /cubes/<id>/edit   /cubes/<id>/history   /cubes/<id>/wantlist
/collection  (+ open the Import dialog)
/events      /events/new   /events/<id>   /events/<id>/manage
```

Also exercise: the nav drawer (open, navigate, confirm it closes), the combobox dropdown on `/cards` at 360px, and — if a tournament exists on the test event — the round tabs and result form.

- [ ] **Step 4: Fix what the sweep finds**

Apply only the established patterns: stack with `flex-col` + `sm:flex-row`, wrap wide tables in `overflow-x-auto`, let flex rows `flex-wrap`, constrain fixed widths with `max-w-full`, `min-w-0` on flex children that must shrink. Likely candidates from code reading (verify rather than assume): `EventDetailPage` `<dl>` at `grid-cols-2` on 360px, `RegistrationsTable` row metadata, `CubeEditorPage` toolbar rows, `combobox.tsx` dropdown width, `EventForm`/`CubeSettingsSection` side-by-side fields. After each fix re-run the route check. Run the affected feature's tests (`pnpm vitest run src/features/<feature>`) after edits.

- [ ] **Step 5: Desktop sanity pass**

Resize to 1280×800; screenshot `/`, `/cards`, `/cubes/<id>/edit`, `/events/<id>` — confirm desktop layout is unchanged (header shows full nav, no hamburger).

- [ ] **Step 6: Commit per feature**

One commit per feature area touched, e.g.:

```bash
git add frontend/src/features/events
git commit -m "fix(events): 360px layout fixes from mobile sweep"
```

(Repeat for each feature with changes. If a shared/ui file needed a fix, commit it separately as `fix(ui): ...`.)

---

### Task 9: Final validation and PR

**Files:**
- None new (fixes only if validation fails).

- [ ] **Step 1: Full test suite**

Run: `make test` (from repo root — backend + frontend).
Expected: PASS. Fix anything red before proceeding.

- [ ] **Step 2: Lint + format check**

Run: `cd frontend && pnpm lint && pnpm fmt:check` (lefthook pre-push mirrors CI, but run explicitly).
Expected: clean. (If `fmt:check` doesn't exist, `pnpm fmt` then `git diff --exit-code`.)

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin feature/mobile-responsive
gh pr create --title "Mobile responsive support" --body "$(cat <<'EOF'
Implements docs/superpowers/specs/2026-07-19-mobile-responsive-design.md:

- structure.md: binding responsive-design rule (360px floor, mobile-first, 44px touch targets, established patterns)
- Nav drawer below md: (native-dialog Drawer primitive in shared/ui)
- Dialog mobile gutters + internal scroll; 16px inputs (no iOS focus zoom)
- Button lg (44px) size; touch-sized result form and quantity stepper
- StandingsTable scroll wrapper; scrollable round tab strip
- Full-app 360px sweep fixes (see per-feature commits)

Verified every route at 360×740 and 1280×800.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: Watch CI**

Run: `gh pr checks --watch`
Expected: all green. Merge per project flow (PR review + CI, master is protected).
