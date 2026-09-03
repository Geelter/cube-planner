# Production Demo Feedback Batch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the 12 findings from the first production demo on
cubeplanner.pl, plus two gaps surfaced while brainstorming them, as 11
independently shippable PRs.

**Architecture:** Four waves. Wave 1 fixes two shared primitives
(`shared/ui/dialog.tsx` + `shared/ui/drawer.tsx`, `shared/cards/CardAutocomplete.tsx`)
so every consumer benefits without opting in. Wave 2 hardens the events
domain: server-side schedule validation, per-status section visibility,
and a new participant-removal path whose money safety rests on a
`removed` registration status the Stripe reclaim branch must refuse.
Wave 3 makes the tournament API state its own emptiness (200 instead of
404) and adds an append-only result-report log from which disputes are
derived rather than stored. Wave 4 moves card-list resolution into
`internal/cards` so cubes and collection share it, then extends the
import grammar with set and printing selectors.

**Tech Stack:** Go 1.25 (huma + chi + sqlc + goose + pgx), React 19
(Vite, TanStack Router/Query, Tailwind v4, cva, Paraglide i18n), vitest +
RTL on happy-dom, testcontainers for backend integration tests.

**Spec:** `docs/superpowers/specs/2026-09-03-demo-feedback-batch-design.md`

## Global Constraints

- **`docs/architecture/structure.md` is binding.** Dependency direction
  `app`/`routes` → `features` → `shared`; **never** feature → feature.
  Anything two features need moves down into `shared/` (promote, don't
  copy).
- **Semantic color tokens only:** `bg-surface`, `bg-surface-raised`,
  `text-fg`, `text-fg-muted`, `border-border`, `bg-accent`,
  `text-accent-fg`, `bg-danger`, `text-danger-fg`, `bg-overlay`. Raw
  palette utilities (`bg-zinc-800`) are allowed **only** in
  `src/app/styles.css`.
- **No hardcoded user-facing strings.** Every display string is
  `m.some_key()` from `@/paraglide/messages`. Add each new key to
  **both** `frontend/messages/en.json` and `frontend/messages/pl.json` —
  the Paraglide compiler fails the build on key mismatch. Keep keys in
  the same alphabetical neighbourhood as their siblings.
  **Exception:** backend RFC 7807 `detail` strings render verbatim and
  need no key.
- **One file per component; variants are typed cva config.** All
  conditional/merged classes go through `cn()` from `@/shared/lib/cn`.
- **a11y:** every input has a `<Label htmlFor>` with a matching `id`;
  errors render in `<Alert>` (`role="alert"`), associated to a single
  field via `aria-describedby`. Buttons firing a network request use
  `<Button loading>`, never a bare `disabled`. For one-mutation-many-rows
  lists, derive the spinner from in-flight variables
  (`const pendingId = mut.isPending ? mut.variables.<key> : null`, then
  `loading={pendingId === row.<key>}`) so other rows stay enabled.
  Dialogs confirming a mutation defer closing until it settles
  (`mutate(vars, { onSettled: () => close() })`).
- **Responsive:** 360px support floor, no horizontal page scroll. Wide
  tables scroll inside their own `overflow-x-auto` wrapper. Touch targets
  ≥44px (`h-11`+); icon-only affordances use `size="icon"` at `size-11`.
  Inputs use `text-base sm:text-sm` so iOS Safari does not zoom on focus.
- **Test placement:** tests sit next to what they test. Inside
  `src/routes/` a test file needs a `-` filename prefix. Axe tests opt
  into jsdom with `// @vitest-environment jsdom` as the **first** line.
- **Generated artifacts are never hand-edited.** `frontend/src/shared/api/`
  is regenerated with `make api-generate` (tracked; CI fails if stale).
  `routeTree.gen.ts` and `src/paraglide/` are gitignored build output.
- **Tooling:** oxlint + oxfmt (never eslint/prettier), gofumpt +
  golangci-lint. Use the Makefile: `make test`, `make api-generate`,
  `make db-reset`. Never loosen `tsconfig`.
- **Branch per PR, off latest `master`.** `master` is protected — every
  PR merges through GitHub after CI passes. Branch names are given per
  PR section below.
- **Commit style:** `type(scope): summary` (e.g. `fix(shared): …`,
  `feat(events): …`, `test(tournaments): …`).

---

## PR 1 — Overlay scroll lock and focus restoration (branch `feature/overlay-scroll-lock`)

Spec: PR 1. Closes finding #12.

Background the implementer needs: **every** overlay in this app is a
native `<dialog>` driven by `showModal()` — there is no Radix, no vaul,
no portal. `shared/ui/dialog.tsx` and `shared/ui/drawer.tsx` are the only
two primitives, and everything else composes them. `showModal()` already
gives us the focus trap, background inertness, Esc-to-close and
`::backdrop`, so **do not** add focus-trap or inert logic. Two things it
does not give us are the subject of this PR.

### Task 1: `useScrollLock` hook

**Files:**
- Create: `frontend/src/shared/lib/useScrollLock.ts`
- Test: `frontend/src/shared/lib/useScrollLock.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `useScrollLock(active: boolean): void` — locks
  `document.body` scroll while `active` is true. Ref-counted across
  concurrent callers; restores the previous inline styles and scroll
  position when the last caller releases.

Why ref-counted: nested overlays are real in this codebase.
`CubeDisplayPage` can have `CardPreviewSheet` and `PrintingPickerDialog`
open simultaneously, so a naive "unlock on unmount" would unlock while
the outer sheet is still open.

- [ ] **Step 1: Write the failing test**

```ts
import { renderHook } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { useScrollLock } from "./useScrollLock";

afterEach(() => {
  document.body.style.overflow = "";
  document.body.style.paddingRight = "";
  window.scrollY = 0;
});

test("locks body scroll while active and restores on release", () => {
  const { unmount } = renderHook(() => useScrollLock(true));
  expect(document.body.style.overflow).toBe("hidden");
  unmount();
  expect(document.body.style.overflow).toBe("");
});

test("inactive callers never touch the body", () => {
  renderHook(() => useScrollLock(false));
  expect(document.body.style.overflow).toBe("");
});

test("ref-counts concurrent locks so the last release wins", () => {
  const outer = renderHook(() => useScrollLock(true));
  const inner = renderHook(() => useScrollLock(true));
  expect(document.body.style.overflow).toBe("hidden");
  inner.unmount();
  // Outer overlay is still open — the body must stay locked.
  expect(document.body.style.overflow).toBe("hidden");
  outer.unmount();
  expect(document.body.style.overflow).toBe("");
});

test("toggling active off releases without unmounting", () => {
  const { rerender } = renderHook(({ on }) => useScrollLock(on), {
    initialProps: { on: true },
  });
  expect(document.body.style.overflow).toBe("hidden");
  rerender({ on: false });
  expect(document.body.style.overflow).toBe("");
});

test("restores the scroll position the page had when locked", () => {
  window.scrollY = 250;
  const restore: number[] = [];
  const original = window.scrollTo;
  window.scrollTo = ((x: number, y: number) => restore.push(y)) as typeof window.scrollTo;
  const { unmount } = renderHook(() => useScrollLock(true));
  unmount();
  window.scrollTo = original;
  expect(restore).toEqual([250]);
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && pnpm vitest run src/shared/lib/useScrollLock.test.ts`
Expected: FAIL — cannot resolve `./useScrollLock`.

- [ ] **Step 3: Write the implementation**

```ts
import { useEffect } from "react";

// Module-scoped because the lock is a property of the document, not of
// any one component: nested overlays (CardPreviewSheet over
// PrintingPickerDialog) must not unlock the body while an outer one is
// still open, so the last release wins rather than the first.
let lockCount = 0;
let restore: { overflow: string; paddingRight: string; scrollY: number } | null = null;

function acquire() {
  lockCount += 1;
  if (lockCount > 1) return;
  const { body } = document;
  restore = {
    overflow: body.style.overflow,
    paddingRight: body.style.paddingRight,
    scrollY: window.scrollY,
  };
  // Compensate for the scrollbar the lock removes, so fixed-width
  // layouts don't jump sideways on desktop.
  const gap = window.innerWidth - document.documentElement.clientWidth;
  body.style.overflow = "hidden";
  if (gap > 0) body.style.paddingRight = `${gap}px`;
}

function release() {
  lockCount -= 1;
  if (lockCount > 0 || restore === null) return;
  const { body } = document;
  body.style.overflow = restore.overflow;
  body.style.paddingRight = restore.paddingRight;
  const { scrollY } = restore;
  restore = null;
  window.scrollTo(0, scrollY);
}

// Locks body scroll while `active`. Needed because iOS Safari does NOT
// suppress root scroll for a modal <dialog> the way desktop browsers do:
// without this the page scrolls and rubber-bands behind bottom sheets.
export function useScrollLock(active: boolean): void {
  useEffect(() => {
    if (!active) return;
    acquire();
    return release;
  }, [active]);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && pnpm vitest run src/shared/lib/useScrollLock.test.ts`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/shared/lib/useScrollLock.ts frontend/src/shared/lib/useScrollLock.test.ts
git commit -m "feat(shared): ref-counted useScrollLock hook"
```

### Task 2: Wire the lock into both primitives, and fix focus restoration

**Files:**
- Modify: `frontend/src/shared/ui/dialog.tsx`
- Modify: `frontend/src/shared/ui/drawer.tsx:16` (bottom variant classes)
- Test: `frontend/src/shared/ui/dialog.test.tsx` (extend)
- Test: `frontend/src/shared/ui/drawer.test.tsx` (extend)

**Interfaces:**
- Consumes: `useScrollLock` from Task 1.
- Produces: no public API change. Both primitives lock scroll while open
  and call `el.close()` on unmount.

The second defect: `CardPreviewSheet` and `PrintingPickerDialog` hardcode
`open` and are *conditionally mounted* by their parents
(`CubeDisplayPage`, `CollectionPage`, `WantlistPage`, `CardSearchPage`,
`CubeEditorPage`). React removes the `<dialog>` while it is still open,
so the effect's `el.close()` never runs — and removing a top-layer
element does **not** return focus to the opener, dumping keyboard and AT
users on `<body>`. Fixing it inside the primitives repairs every call
site at once; do not change the call sites.

- [ ] **Step 1: Write the failing tests**

Append to `frontend/src/shared/ui/dialog.test.tsx`:

```tsx
test("locks body scroll while open and releases when closed", () => {
  const { rerender } = render(
    <Dialog open={false} onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  expect(document.body.style.overflow).toBe("");
  rerender(
    <Dialog open onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  expect(document.body.style.overflow).toBe("hidden");
  rerender(
    <Dialog open={false} onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  expect(document.body.style.overflow).toBe("");
});

test("closes the dialog element when unmounted while still open", () => {
  const { unmount } = render(
    <Dialog open onClose={() => {}} title="T">
      <p>Body</p>
    </Dialog>,
  );
  const el = screen.getByRole("dialog") as HTMLDialogElement;
  const close = vi.spyOn(el, "close");
  unmount();
  expect(close).toHaveBeenCalled();
  expect(document.body.style.overflow).toBe("");
});
```

Append the equivalent pair to `frontend/src/shared/ui/drawer.test.tsx`,
using `<Drawer open onClose={() => {}} label="Menu">` in place of the
`Dialog` element and `screen.getByRole("dialog")` unchanged.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && pnpm vitest run src/shared/ui/dialog.test.tsx src/shared/ui/drawer.test.tsx`
Expected: FAIL — `document.body.style.overflow` is `""` when open, and
`close` is not called on unmount.

- [ ] **Step 3: Implement in `dialog.tsx`**

Add the import:

```tsx
import { useScrollLock } from "@/shared/lib/useScrollLock";
```

Call it inside the component, above the existing effect:

```tsx
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useScrollLock(open);
```

Replace the existing effect with one that also closes on unmount:

```tsx
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
    // Consumers that hardcode `open` and conditionally mount (CardPreviewSheet,
    // PrintingPickerDialog) would otherwise be removed from the DOM while still
    // open, and a removed top-layer element does not restore focus to its opener.
    return () => {
      if (el.open) el.close();
    };
  }, [open]);
```

- [ ] **Step 4: Implement the same change in `drawer.tsx`**

Add the identical `useScrollLock` import and `useScrollLock(open)` call,
and apply the identical `useEffect` cleanup. Then add
`overscroll-contain` to the `bottom` variant so touch-scroll at the end
of the sheet's own scroll region does not chain into the document:

```tsx
        bottom:
          "mt-auto max-h-[85svh] w-full max-w-none overflow-y-auto overscroll-contain rounded-t-xl border-t",
```

Also add `overscroll-contain` to the inner scroll wrapper (`drawer.tsx`
line ~70) and to the `Dialog` root (`dialog.tsx` line ~37), which is
itself `overflow-y-auto`:

```tsx
        <div className="flex h-full flex-col gap-2 overflow-y-auto overscroll-contain">
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd frontend && pnpm vitest run src/shared/ui`
Expected: PASS, including the pre-existing dialog/drawer tests.

- [ ] **Step 6: Run the full frontend suite**

Run: `cd frontend && pnpm vitest run`
Expected: PASS. The scroll lock is global state, so watch for
cross-test leakage — if any suite fails on a stray `overflow: hidden`,
the culprit is a test that unmounts an open overlay without `cleanup()`;
fix the test, not the hook.

- [ ] **Step 7: Verify manually at 360px**

Run `make up`, open the app at 360px width (and on a real iOS Safari if
available), then: open the mobile nav drawer, a card preview sheet on the
collection page, and the cube editor's pending-changes sheet. In each,
attempt to scroll the page behind the overlay. Expected: the background
does not move, the sheet's own content still scrolls, and closing the
sheet returns focus to the row/button that opened it (check with Tab).

This manual check **is** the acceptance gate for the lock: happy-dom has
no `showModal`, so both primitives fall back to a non-modal `open`
attribute and no unit test can exercise real modality.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/shared/ui/dialog.tsx frontend/src/shared/ui/drawer.tsx \
  frontend/src/shared/ui/dialog.test.tsx frontend/src/shared/ui/drawer.test.tsx
git commit -m "fix(shared): lock body scroll and restore focus for overlays"
```

---

## PR 2 — Card search clears after picking (branch `feature/card-search-clear`)

Spec: PR 2. Closes finding #10.

### Task 3: Clear the query and keep focus after selecting a card

**Files:**
- Modify: `frontend/src/shared/cards/CardAutocomplete.tsx:44-47`
- Modify: `frontend/src/shared/ui/combobox.tsx` (refocus after select)
- Test: `frontend/src/shared/cards/CardAutocomplete.test.tsx` (extend, or
  create if absent)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: no API change. `CardAutocomplete` leaves an empty input after
  a selection, focused and ready for the next name.

Today `onSelect` does `setQuery(c.name)`, leaving the picked card's name
in the box. The user must clear it by hand before every next card,
re-focusing re-opens a list containing only that card, and re-selecting
silently adds another copy.

Refocusing is safe and does **not** re-open the list:
`showList = open && value.trim().length >= minChars` in
`combobox.tsx:46`, and `CardAutocomplete` passes `minChars={2}`, so an
empty value keeps the list closed even though `onFocus` sets `open`.

- [ ] **Step 1: Write the failing test**

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { CardAutocomplete } from "./CardAutocomplete";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function renderWithClient(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

test("clears the input and keeps focus after a card is picked", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(
      JSON.stringify([
        {
          scryfallId: "11111111-1111-1111-1111-111111111111",
          oracleId: "22222222-2222-2222-2222-222222222222",
          name: "Lightning Bolt",
          manaCost: "{R}",
          typeLine: "Instant",
          colors: ["R"],
          setCode: "leb",
          setName: "Limited Edition Beta",
          collectorNumber: "162",
          imageSmall: null,
          imageNormal: null,
        },
      ]),
      { status: 200, headers: { "content-type": "application/json" } },
    ),
  );
  const onSelect = vi.fn();
  renderWithClient(<CardAutocomplete id="add" onSelect={onSelect} />);

  const input = screen.getByRole("combobox");
  await userEvent.type(input, "bolt");
  const option = await waitFor(() => screen.getByRole("option", { name: /Lightning Bolt/ }));
  await userEvent.click(option);

  expect(onSelect).toHaveBeenCalledTimes(1);
  expect(input).toHaveValue("");
  expect(input).toHaveFocus();
  // An empty query is below minChars, so no list reopens on refocus.
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && pnpm vitest run src/shared/cards/CardAutocomplete.test.tsx`
Expected: FAIL — input holds `"Lightning Bolt"` and does not have focus.

- [ ] **Step 3: Clear the query in `CardAutocomplete.tsx`**

```tsx
      onSelect={(c) => {
        onSelect(c);
        // Clear rather than echo the name: these inputs are used to enter a
        // list of cards in sequence, so the next name should be typeable
        // immediately.
        setQuery("");
      }}
```

- [ ] **Step 4: Keep focus on the input in `combobox.tsx`**

Add a ref beside the existing state, attach it to `Input`, and refocus in
`select()`. Clicking an option blurs the input because options are
non-focusable `<li>` elements; keyboard selection already keeps focus, so
this only repairs the pointer path.

```tsx
  const inputRef = useRef<HTMLInputElement>(null);
```

```tsx
  function select(option: T) {
    onSelect(option);
    close();
    // Options are non-focusable <li>s, so a pointer selection blurs the
    // input. Sequential entry needs the caret back.
    inputRef.current?.focus();
  }
```

```tsx
      <Input
        id={id}
        ref={inputRef}
        role="combobox"
```

Add `useRef` to the existing `react` import. If `Input`
(`frontend/src/shared/ui/input.tsx`) does not already forward its ref,
React 19 passes `ref` through function components as a normal prop — verify
by running the test rather than refactoring `Input`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd frontend && pnpm vitest run src/shared/cards/CardAutocomplete.test.tsx`
Expected: PASS.

- [ ] **Step 6: Run the affected suites**

Run: `cd frontend && pnpm vitest run src/shared src/features/cubes src/features/collection`
Expected: PASS. The cube editor and collection page both consume this
component; a test asserting the old echo behaviour must be updated to
assert the empty input instead.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/shared/cards/CardAutocomplete.tsx \
  frontend/src/shared/ui/combobox.tsx \
  frontend/src/shared/cards/CardAutocomplete.test.tsx
git commit -m "fix(cards): clear the search input after picking a card"
```

---
## PR 3 — Backend event schedule validation (branch `feature/event-schedule-validation`)

Spec: PR 3. Closes finding #1.

**No frontend changes in this PR.** `<input type="datetime-local">`
already renders as segments that refuse out-of-range digits, and its
calendar panel is UA chrome we cannot restyle or constrain. The gap is
the server: `StartsAt time.Time` and `RefundDeadline *time.Time` carry
**no huma tags at all**, and neither `Create` nor `Update` checks
anything temporal. The API today accepts a start in the past, a refund
deadline after the start, and a refund deadline on a free event.

### Task 4: `ErrInvalidSchedule` in Create and Update

**Files:**
- Modify: `backend/internal/events/service.go:28-45` (sentinel),
  `:88-105` (`Create`), `:121-152` (`Update`)
- Modify: `backend/internal/platform/httpapi/events.go:84-110` (`mapEventErr`)
- Test: `backend/internal/events/service_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `events.ErrInvalidSchedule` (sentinel), mapped to HTTP 422
  with RFC 7807 `type: "invalid-event-schedule"`. Validation helper
  `validateSchedule(now, startsAt time.Time, refundDeadline *time.Time, feeCents int32) error`.

Rules:
1. `startsAt` must be strictly after now.
2. `refundDeadline`, when present, must be at or before `startsAt`.
3. `refundDeadline` must be absent when `feeCents == 0`.

`Update` is the subtle one. The deadline stays editable after publish
while `startsAt` does not, so a PATCH carrying only `refundDeadline` must
validate against the **stored** `startsAt` and the **stored** `feeCents`,
read under the existing `GetEventForUpdate` lock. Rule 1 is checked only
when the request actually carries a `startsAt` — re-validating a stored
past start would make finished events unpatchable.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/events/service_test.go`:

```go
// ---- PR 3: schedule validation ----

func TestCreateRejectsPastStart(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	_, err := e.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Cube Night", StartsAt: e.clock.Now().Add(-time.Hour), MaxParticipants: 8,
	})
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("want ErrInvalidSchedule, got %v", err)
	}
}

func TestCreateRejectsRefundDeadlineAfterStart(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	starts := e.clock.Now().Add(48 * time.Hour)
	after := starts.Add(time.Hour)
	_, err := e.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Cube Night", StartsAt: starts, FeeCents: 2000,
		MaxParticipants: 8, RefundDeadline: &after,
	})
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("want ErrInvalidSchedule, got %v", err)
	}
}

func TestCreateRejectsRefundDeadlineOnFreeEvent(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	starts := e.clock.Now().Add(48 * time.Hour)
	before := starts.Add(-time.Hour)
	_, err := e.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Cube Night", StartsAt: starts, FeeCents: 0,
		MaxParticipants: 8, RefundDeadline: &before,
	})
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("want ErrInvalidSchedule, got %v", err)
	}
}

func TestCreateAcceptsDeadlineAtStart(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	starts := e.clock.Now().Add(48 * time.Hour)
	_, err := e.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Cube Night", StartsAt: starts, FeeCents: 2000,
		MaxParticipants: 8, RefundDeadline: &starts,
	})
	if err != nil {
		t.Fatalf("deadline exactly at start must be allowed: %v", err)
	}
}

func TestUpdateValidatesDeadlineAgainstStoredStart(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	ev := e.createEvent(t, org, 2000, 8)
	e.publish(t, ev.ID)

	after := ev.StartsAt.Add(time.Hour)
	_, err := e.svc.Update(context.Background(), ev.ID, UpdateEventParams{RefundDeadline: &after})
	if !errors.Is(err, ErrInvalidSchedule) {
		t.Fatalf("want ErrInvalidSchedule, got %v", err)
	}

	before := ev.StartsAt.Add(-2 * time.Hour)
	if _, err := e.svc.Update(context.Background(), ev.ID, UpdateEventParams{RefundDeadline: &before}); err != nil {
		t.Fatalf("deadline before the stored start must be allowed: %v", err)
	}
}

func TestUpdatePastEventStaysPatchable(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	ev := e.createEvent(t, org, 0, 8)
	e.publish(t, ev.ID)
	// The event starts in 7 days; jump past it so the stored start is in
	// the past, then patch a field that is still editable.
	e.clock.Advance(8 * 24 * time.Hour)
	loc := "New venue"
	if _, err := e.svc.Update(context.Background(), ev.ID, UpdateEventParams{Location: &loc}); err != nil {
		t.Fatalf("a past event must stay patchable: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/events/ -run 'Schedule|PastStart|DeadlineAtStart|StaysPatchable' -v`
Expected: FAIL — `ErrInvalidSchedule` undefined (compile error).

- [ ] **Step 3: Add the sentinel and the helper**

In `backend/internal/events/service.go`, add to the `var (...)` block
alongside `ErrEventLocked`:

```go
	// ErrInvalidSchedule: the requested times are not coherent (start in
	// the past, refund deadline after the start, or a deadline on a free
	// event). The frontend's datetime-local input constrains typing but
	// cannot be trusted, and nothing else checked this before.
	ErrInvalidSchedule = errors.New("invalid event schedule")
```

Add the helper below the `var` block:

```go
// validateSchedule enforces the three temporal rules shared by Create and
// Update. checkStart is false for a PATCH that does not carry a start
// time: re-validating a stored past start would make finished events
// unpatchable.
func validateSchedule(now, startsAt time.Time, refundDeadline *time.Time, feeCents int32, checkStart bool) error {
	if checkStart && !startsAt.After(now) {
		return fmt.Errorf("%w: start time must be in the future", ErrInvalidSchedule)
	}
	if refundDeadline == nil {
		return nil
	}
	if feeCents == 0 {
		return fmt.Errorf("%w: a free event has no refund deadline", ErrInvalidSchedule)
	}
	if refundDeadline.After(startsAt) {
		return fmt.Errorf("%w: refund deadline must not be after the start time", ErrInvalidSchedule)
	}
	return nil
}
```

- [ ] **Step 4: Call it from `Create`**

Insert immediately after the `ErrPaymentsUnconfigured` check in `Create`:

```go
	if err := validateSchedule(s.now(), p.StartsAt, p.RefundDeadline, p.FeeCents, true); err != nil {
		return nil, err
	}
```

- [ ] **Step 5: Call it from `Update`**

Inside the transaction, after the `ErrEventLocked` whitelist check and
the `ErrPaymentsUnconfigured` check, resolve effective values from the
request falling back to the locked row, then validate:

```go
		// A PATCH may carry any subset. Validate the schedule the event
		// would have after the patch, reading the rest from the locked row.
		startsAt := ev.StartsAt
		if p.StartsAt != nil {
			startsAt = *p.StartsAt
		}
		feeCents := ev.FeeCents
		if p.FeeCents != nil {
			feeCents = *p.FeeCents
		}
		refundDeadline := ev.RefundDeadline
		if p.RefundDeadline != nil {
			refundDeadline = p.RefundDeadline
		}
		if err := validateSchedule(s.now(), startsAt, refundDeadline, feeCents, p.StartsAt != nil); err != nil {
			return err
		}
```

Confirm `fmt` is already imported in `service.go` (it is — `ErrInvalidTransition`
wrapping uses it).

- [ ] **Step 6: Map the sentinel to 422**

In `backend/internal/platform/httpapi/events.go`, add to `mapEventErr`
beside the other `StatusUnprocessableEntity` case:

```go
	case errors.Is(err, events.ErrInvalidSchedule):
		return eventProblem(http.StatusUnprocessableEntity, "invalid-event-schedule", err.Error())
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/events/ -v`
Expected: PASS. Existing tests keep passing because the `createEvent`
helper already schedules 7 days out.

- [ ] **Step 8: Run the whole backend suite**

Run: `make test`
Expected: PASS. Any endpoint test that created an event with a past or
zero `startsAt` now fails and must be given a future time — that is the
bug being fixed, not a test to weaken.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/events/service.go \
  backend/internal/platform/httpapi/events.go \
  backend/internal/events/service_test.go
git commit -m "feat(events): validate event schedule server-side"
```

---

## PR 4 — Section visibility by event status (branch `feature/event-section-visibility`)

Spec: PR 4. Closes finding #2.

Target matrix:

| Section | draft | published | started | finished | cancelled |
|---|---|---|---|---|---|
| Registrations: paid / pending / waitlist / history | — | yes | — | — | — |
| Registrations: refund-queue group | if non-empty | yes | if non-empty | if non-empty | if non-empty |
| Attendee chips (public page) | — | yes | yes | — | yes |
| Players list (organizer panel) | — | — | yes | — | — |

The refund-queue carve-out is deliberate, not an oversight: a player who
self-cancels past the refund deadline lands in `refund_requested`, and
hiding the whole section on `started`/`finished` would leave the
organizer no screen on which to resolve it. The Players list is already
gated to `started` in `TournamentPanel.tsx:305` — verify, don't
re-implement.

### Task 5: Gate the registrations groups on event status

**Files:**
- Modify: `frontend/src/features/events/components/RegistrationsTable.tsx:9-19,23-27`
- Modify: `frontend/src/features/events/components/ManageEventPage.tsx:123`
- Test: `frontend/src/features/events/components/RegistrationsTable.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `RegistrationsTable` gains a required
  `status: EventSummary["status"]` prop. `ManageEventPage` already holds
  the event as `e`, so it passes `status={e.status}`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/features/events/components/RegistrationsTable.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup, waitFor } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { RegistrationsTable } from "./RegistrationsTable";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

type Row = {
  id: string;
  displayName: string;
  email: string;
  status: string;
  expiresAt: string | null;
  paidAt: string | null;
  waitlistPos: number | null;
  hasPayment: boolean;
};

function row(over: Partial<Row>): Row {
  return {
    id: crypto.randomUUID(),
    displayName: "Ann",
    email: "ann@example.com",
    status: "paid",
    expiresAt: null,
    paidAt: "2026-09-01T10:00:00Z",
    waitlistPos: null,
    hasPayment: false,
    ...over,
  };
}

function renderTable(status: string, rows: Row[]) {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response(JSON.stringify(rows), {
      status: 200,
      headers: { "content-type": "application/json" },
    }),
  );
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationsTable eventId="e1" status={status as never} />
    </QueryClientProvider>,
  );
}

test("published shows every group", async () => {
  renderTable("published", [row({ status: "paid" })]);
  expect(await screen.findByRole("heading", { name: /Registrations/i })).toBeInTheDocument();
  expect(screen.getByText("Ann")).toBeInTheDocument();
});

test("draft renders nothing when the refund queue is empty", async () => {
  renderTable("draft", [row({ status: "paid" })]);
  await waitFor(() => expect(screen.queryByText("Ann")).not.toBeInTheDocument());
  expect(screen.queryByRole("heading", { name: /Registrations/i })).not.toBeInTheDocument();
});

test("started shows only the refund queue", async () => {
  renderTable("started", [
    row({ status: "paid", displayName: "Ann" }),
    row({ status: "refund_requested", displayName: "Bob", hasPayment: true }),
  ]);
  expect(await screen.findByText("Bob")).toBeInTheDocument();
  expect(screen.queryByText("Ann")).not.toBeInTheDocument();
});

test("finished with an empty queue renders nothing", async () => {
  renderTable("finished", [row({ status: "paid" })]);
  await waitFor(() => expect(screen.queryByText("Ann")).not.toBeInTheDocument());
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && pnpm vitest run src/features/events/components/RegistrationsTable.test.tsx`
Expected: FAIL — `status` is not a prop, and every group renders at every
status.

- [ ] **Step 3: Implement the gating**

Add the prop and derive which groups to render. Replace the `GROUPS`
const and the component signature:

```tsx
import type { EventSummary } from "../api";

// `queue` is the only group that outlives `published`: a player who
// self-cancels past the refund deadline lands in refund_requested, and the
// organizer still needs a screen to resolve it after the event starts.
const GROUPS: { key: string; title: () => string; statuses: string[]; publishedOnly: boolean }[] = [
  { key: "paid", title: () => m.regs_group_paid(), statuses: ["paid"], publishedOnly: true },
  {
    key: "pending",
    title: () => m.regs_group_pending(),
    statuses: ["pending_payment"],
    publishedOnly: true,
  },
  {
    key: "waitlist",
    title: () => m.regs_group_waitlist(),
    statuses: ["waitlisted"],
    publishedOnly: true,
  },
  {
    key: "queue",
    title: () => m.regs_group_refund_queue(),
    statuses: ["refund_requested"],
    publishedOnly: false,
  },
  {
    key: "history",
    title: () => m.regs_group_history(),
    statuses: ["cancelled", "refunded", "expired", "removed"],
    publishedOnly: true,
  },
];

export function RegistrationsTable({
  eventId,
  status,
}: {
  eventId: string;
  status: EventSummary["status"];
}) {
```

After the existing `isPending` / `error` guards, compute the visible
groups and bail out entirely when nothing is left:

```tsx
  const rowsFor = (statuses: string[]) =>
    (regs.data ?? [])
      .filter((r) => statuses.includes(r.status))
      .sort((a, b) => (a.waitlistPos ?? 0) - (b.waitlistPos ?? 0));

  const published = status === "published";
  const visible = GROUPS.filter((g) =>
    published ? true : !g.publishedOnly && rowsFor(g.statuses).length > 0,
  );
  if (visible.length === 0) return null;
```

Then map over `visible` instead of `GROUPS`, reusing `rowsFor(g.statuses)`
in place of the inline filter+sort.

Note the `history` group also gains `"removed"` — that status arrives in
PR 5. Adding it here is harmless (no row carries it yet) and saves a
second edit to this file.

- [ ] **Step 4: Pass the status from `ManageEventPage`**

```tsx
      <RegistrationsTable eventId={eventId} status={e.status} />
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `cd frontend && pnpm vitest run src/features/events`
Expected: PASS.

- [ ] **Step 6: Gate the attendee chips**

In `frontend/src/features/events/components/EventDetailPage.tsx`, wrap
the attendees `<section>` (line ~172) so it renders only for statuses
where the roster is meaningful. Standings supersede it once finished, and
a draft has nobody:

```tsx
      {(e.status === "published" || e.status === "started" || e.status === "cancelled") && (
        <section className="flex flex-col gap-2">
          {/* …existing attendees markup unchanged… */}
        </section>
      )}
```

- [ ] **Step 7: Verify the Players list gate already matches the matrix**

Run: `grep -n 'status === "started"' frontend/src/features/tournaments/components/TournamentPanel.tsx`
Expected: the per-player drop/undrop button is already gated. The
**heading and list** are inside the `status !== "started" && status !== "finished"`
early return, so on `finished` they still render — that violates the
matrix. Wrap the `tournament_players_heading` heading and its `<ul>`
(around `TournamentPanel.tsx:297-312`) in `{status === "started" && ( … )}`.

- [ ] **Step 8: Add an axe smoke test for the event detail page at draft status**

Extend the existing event-detail axe test (or create
`frontend/src/features/events/components/EventDetailPage.a11y.test.tsx`
with `// @vitest-environment jsdom` as its first line) to render a draft
event and assert no violations. Conditionally-rendered sections are a
common source of empty-heading and orphaned-list violations.

- [ ] **Step 9: Run the full frontend suite and check 360px**

Run: `cd frontend && pnpm vitest run`
Expected: PASS.

Then `make up` and confirm at 360px: a draft event's manage page shows no
Registrations section; a published one shows all groups; after Start, only
a non-empty refund queue appears.

- [ ] **Step 10: Commit**

```bash
git add frontend/src/features/events/components/RegistrationsTable.tsx \
  frontend/src/features/events/components/RegistrationsTable.test.tsx \
  frontend/src/features/events/components/ManageEventPage.tsx \
  frontend/src/features/events/components/EventDetailPage.tsx \
  frontend/src/features/tournaments/components/TournamentPanel.tsx
git commit -m "feat(events): hide redundant sections per event status"
```

---
## PR 5 — Remove a participant, and a shared confirm dialog (branch `feature/event-remove-participant`)

Spec: PR 5. Closes findings #3 and #13.

The bug: `RegistrationsTable.tsx:87` renders Refund for **every** `paid`
row, and `OrganizerRefund` (`service.go:796-798`) rejects it with
`invalid event transition: nothing was paid` when the row has no Stripe
payment intent. Free events register straight to `paid` with no intent
(`service.go:453-456`), so **free-event participants cannot be removed at
all**.

### Task 6: Extract the shared confirm dialog

**Files:**
- Create: `frontend/src/shared/ui/confirm-dialog.tsx`
- Test: `frontend/src/shared/ui/confirm-dialog.test.tsx`
- Modify: `frontend/src/features/events/components/RegistrationsTable.tsx:117-144`
- Modify: `frontend/src/features/events/components/ManageEventPage.tsx:125-149`
- Modify: `frontend/src/features/events/components/RegistrationPanel.tsx:109-132`
- Modify: `frontend/src/features/tournaments/components/TournamentSection.tsx:189-218`

**Interfaces:**
- Consumes: `Dialog` from `@/shared/ui/dialog`.
- Produces:

```tsx
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  message,
  confirmLabel,
  pending,
  danger,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  message: string;
  confirmLabel: string;
  pending?: boolean;
  danger?: boolean;
}): ReactNode
```

The pattern is currently copy-pasted four times, and this PR would add a
fifth. Two of the copies also get the convention wrong:
`RegistrationsTable.tsx:138` and `RegistrationPanel.tsx` close the dialog
immediately after `mutate()`, so the spinner never shows.
`ConfirmDialog` takes `pending` and leaves closing to the caller's
`onSettled`, which fixes both by construction.

- [ ] **Step 1: Write the failing test**

```tsx
import { render, screen, cleanup } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { ConfirmDialog } from "./confirm-dialog";

afterEach(() => {
  cleanup();
});

test("renders the message and fires onConfirm", async () => {
  const onConfirm = vi.fn();
  render(
    <ConfirmDialog
      open
      onClose={() => {}}
      onConfirm={onConfirm}
      title="Remove Ann"
      message="Remove Ann from this event?"
      confirmLabel="Remove"
    />,
  );
  expect(screen.getByText("Remove Ann from this event?")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Remove" }));
  expect(onConfirm).toHaveBeenCalledTimes(1);
});

test("cancel fires onClose but not onConfirm", async () => {
  const onClose = vi.fn();
  const onConfirm = vi.fn();
  render(
    <ConfirmDialog
      open
      onClose={onClose}
      onConfirm={onConfirm}
      title="T"
      message="M"
      confirmLabel="Go"
    />,
  );
  await userEvent.click(screen.getByRole("button", { name: /close/i }));
  expect(onClose).toHaveBeenCalled();
  expect(onConfirm).not.toHaveBeenCalled();
});

test("pending marks the confirm button busy", () => {
  render(
    <ConfirmDialog
      open
      onClose={() => {}}
      onConfirm={() => {}}
      title="T"
      message="M"
      confirmLabel="Go"
      pending
    />,
  );
  expect(screen.getByRole("button", { name: "Go" })).toHaveAttribute("aria-busy", "true");
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && pnpm vitest run src/shared/ui/confirm-dialog.test.tsx`
Expected: FAIL — cannot resolve `./confirm-dialog`.

- [ ] **Step 3: Write the component**

```tsx
import type { ReactNode } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";

// The confirm-a-mutation shape, previously copy-pasted across events and
// tournaments. `pending` keeps the spinner visible: callers close from
// `onSettled`, never straight after `mutate()`.
export function ConfirmDialog({
  open,
  onClose,
  onConfirm,
  title,
  message,
  confirmLabel,
  pending,
  danger,
}: {
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
  title: string;
  message: string;
  confirmLabel: string;
  pending?: boolean;
  danger?: boolean;
}): ReactNode {
  return (
    <Dialog open={open} onClose={onClose} title={title}>
      <p className="text-sm text-fg">{message}</p>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onClose}>
          {m.dialog_close()}
        </Button>
        <Button
          type="button"
          variant={danger === true ? "danger" : "default"}
          loading={pending === true}
          onClick={onConfirm}
        >
          {confirmLabel}
        </Button>
      </div>
    </Dialog>
  );
}
```

Check the available `Button` variants first:
`grep -n 'variants' -A 12 frontend/src/shared/ui/button.tsx`. Use the
danger variant's real name; if none exists, add one to the cva config
using `bg-danger text-danger-fg` rather than inventing classes at the
call site.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && pnpm vitest run src/shared/ui/confirm-dialog.test.tsx`
Expected: PASS (3 tests).

- [ ] **Step 5: Re-point the four existing call sites**

Replace each hand-rolled `Dialog` + two-button block with `ConfirmDialog`,
preserving each site's existing title/message/label messages. In
`RegistrationsTable` and `RegistrationPanel`, move closing into
`onSettled` so the spinner shows:

```tsx
      <ConfirmDialog
        open={confirm != null}
        onClose={() => setConfirm(null)}
        title={confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
        message={
          confirm == null
            ? ""
            : confirm.kind === "deny"
              ? m.regs_deny_confirm({ name: confirm.row.displayName })
              : m.regs_refund_confirm({ name: confirm.row.displayName })
        }
        confirmLabel={confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
        pending={refund.isPending || deny.isPending}
        onConfirm={() => {
          if (confirm == null) return;
          const mut = confirm.kind === "deny" ? deny : refund;
          mut.mutate(confirm.row.id, { onSettled: () => setConfirm(null) });
        }}
      />
```

- [ ] **Step 6: Run the affected suites**

Run: `cd frontend && pnpm vitest run src/features/events src/features/tournaments src/shared/ui`
Expected: PASS. A test asserting the dialog closes synchronously after
clicking confirm must be updated to await the mutation settling — the old
behaviour was the bug.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/shared/ui/confirm-dialog.tsx \
  frontend/src/shared/ui/confirm-dialog.test.tsx \
  frontend/src/features/events/components/RegistrationsTable.tsx \
  frontend/src/features/events/components/ManageEventPage.tsx \
  frontend/src/features/events/components/RegistrationPanel.tsx \
  frontend/src/features/tournaments/components/TournamentSection.tsx
git commit -m "refactor(shared): extract ConfirmDialog from four copies"
```

### Task 7: `removed` status and the reclaim guard

**Files:**
- Create: `backend/migrations/00009_registration_removed.sql`
- Modify: `backend/internal/events/service.go:1069-1096` (the
  `default:` late-payment branch of `handleCheckoutCompleted`)
- Modify: `backend/internal/platform/httpapi/events.go:21` (status enum)
- Test: `backend/internal/events/service_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: the `removed` registration status, accepted by the DB check
  constraint and the wire enum, and **refused** by the late-payment
  reclaim branch.

Why a new status rather than reusing `cancelled`: `cancelled` means *the
player withdrew*, and `handleCheckoutCompleted`'s `default:` branch
legitimately **reclaims** such a row to `paid` when the event is still
published and a spot is free — they wanted in. `removed` means *the
organizer ejected them* and must never be reclaimable, or a late checkout
would silently reinstate a removed player.

`removed` is deliberately **absent** from `registrations_one_active_idx`,
so it holds no capacity and blocks no re-registration — no index change.
Accepted limitation: a removed player can re-register and be removed
again.

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
-- 'removed' = organizer ejected the player. Distinct from 'cancelled'
-- (player withdrew) because a late Stripe payment may reclaim a
-- cancelled row but must never reinstate a removed one.
alter table registrations drop constraint registrations_status_check;
alter table registrations add constraint registrations_status_check
    check (status in (
        'pending_payment', 'paid', 'waitlisted',
        'cancelled', 'refund_requested', 'refunded', 'expired', 'removed'));

-- +goose Down
alter table registrations drop constraint registrations_status_check;
alter table registrations add constraint registrations_status_check
    check (status in (
        'pending_payment', 'paid', 'waitlisted',
        'cancelled', 'refund_requested', 'refunded', 'expired'));
```

Verify the real constraint name first — Postgres auto-names inline column
checks, so it may not be `registrations_status_check`:

```bash
make up
docker compose exec -T postgres psql -U postgres -d cube_planner \
  -c "\d registrations" | grep -i check
```

Use the exact name reported. Confirm the migration number is next in
sequence with `ls backend/migrations/`.

- [ ] **Step 2: Write the failing test**

```go
// ---- PR 5: removed status ----

func TestLatePaymentOnRemovedRowRefundsInsteadOfReclaiming(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	user := e.seedUser(t, "player@example.com")
	ev := e.createEvent(t, org, 2000, 8)
	e.publish(t, ev.ID)

	reg, err := e.svc.Register(context.Background(), ev.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	// Organizer removes the player while their checkout is still open.
	if _, err := e.q.SetRegistrationTerminal(context.Background(), db.SetRegistrationTerminalParams{
		ID: reg.ID, Status: "removed",
	}); err != nil {
		t.Fatal(err)
	}

	// The checkout completes anyway.
	err = e.svc.HandleWebhookEvent(context.Background(), WebhookEvent{
		ID:                "evt_late_1",
		Type:              "checkout.session.completed",
		ClientReferenceID: reg.ID.String(),
		PaymentIntentID:   "pi_late_1",
	})
	if err != nil {
		t.Fatal(err)
	}

	after, err := e.q.GetRegistration(context.Background(), reg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status == "paid" {
		t.Fatal("a removed registration must never be reclaimed by a late payment")
	}
	if len(e.stripe.refunds) != 1 || e.stripe.refunds[0] != "pi_late_1" {
		t.Fatalf("want the late charge auto-refunded, got refunds=%v", e.stripe.refunds)
	}
}

func TestLatePaymentOnCancelledRowStillReclaims(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	user := e.seedUser(t, "player@example.com")
	ev := e.createEvent(t, org, 2000, 8)
	e.publish(t, ev.ID)

	reg, err := e.svc.Register(context.Background(), ev.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.q.SetRegistrationTerminal(context.Background(), db.SetRegistrationTerminalParams{
		ID: reg.ID, Status: "cancelled",
	}); err != nil {
		t.Fatal(err)
	}
	err = e.svc.HandleWebhookEvent(context.Background(), WebhookEvent{
		ID:                "evt_late_2",
		Type:              "checkout.session.completed",
		ClientReferenceID: reg.ID.String(),
		PaymentIntentID:   "pi_late_2",
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := e.q.GetRegistration(context.Background(), reg.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Regression guard: self-cancellation stays reclaimable — they wanted in.
	if after.Status != "paid" {
		t.Fatalf("want cancelled row reclaimed to paid, got %s", after.Status)
	}
}
```

Check `Register`'s real signature before writing these
(`grep -n 'func (s \*Service) Register' backend/internal/events/service.go`)
and match the argument order.

- [ ] **Step 3: Run the tests to verify the first fails**

Run: `cd backend && go test ./internal/events/ -run 'LatePaymentOn' -v`
Expected: `TestLatePaymentOnRemovedRowRefundsInsteadOfReclaiming` FAILS
(the row is reclaimed to `paid`);
`TestLatePaymentOnCancelledRowStillReclaims` PASSES already.

- [ ] **Step 4: Add the guard**

In `handleCheckoutCompleted`'s `default:` branch, extend the reclaim
condition:

```go
			// A removed row is never reclaimable: the organizer ejected this
			// player, so a payment that lands afterwards is refunded rather
			// than silently reinstating them.
			if ev.Status == "published" && occupied < int64(ev.MaxParticipants) &&
				!hasOtherActive && reg.Status != "removed" {
```

- [ ] **Step 5: Extend the wire enum**

In `backend/internal/platform/httpapi/events.go:21`:

```go
	Status      string     `json:"status" enum:"pending_payment,paid,waitlisted,cancelled,refund_requested,refunded,expired,removed"`
```

- [ ] **Step 6: Run the tests to verify both pass**

Run: `cd backend && go test ./internal/events/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/00009_registration_removed.sql \
  backend/internal/events/service.go \
  backend/internal/platform/httpapi/events.go \
  backend/internal/events/service_test.go
git commit -m "feat(events): removed registration status, never reclaimed by late payment"
```

### Task 8: `ExpireCheckoutSession` on the Stripe client

**Files:**
- Modify: `backend/internal/events/stripe.go:33-37` (interface + stub)
- Modify: `backend/internal/events/stripe_client.go`
- Modify: `backend/internal/events/service_test.go` (extend `fakeStripe`)
- Modify: `backend/internal/platform/httpapi/*_test.go` (any other
  `StripeClient` fake)

**Interfaces:**
- Consumes: nothing.
- Produces: `StripeClient.ExpireCheckoutSession(ctx context.Context, sessionID string) error`.
  `fakeStripe` records calls in a new `expired []string` field.

This is the **fast path only**. It shrinks the window in which a removed
player can complete a checkout; it is not the guarantee, because a user
who already submitted their card cannot be stopped. Task 7's reclaim
guard is the guarantee.

- [ ] **Step 1: Extend the interface and the unconfigured stub**

```go
type StripeClient interface {
	Configured() bool
	CreateCheckoutSession(ctx context.Context, p CheckoutParams) (*CheckoutSession, error)
	RefundPaymentIntent(ctx context.Context, paymentIntentID string) error
	// ExpireCheckoutSession kills a live Checkout session so a removed
	// participant cannot complete it. Best-effort: callers log and continue.
	ExpireCheckoutSession(ctx context.Context, sessionID string) error
}

func (unconfiguredStripe) ExpireCheckoutSession(context.Context, string) error {
	return ErrPaymentsUnconfigured
}
```

- [ ] **Step 2: Implement it on the real client**

```go
func (c *stripeClient) ExpireCheckoutSession(ctx context.Context, sessionID string) error {
	_, err := c.sc.V1CheckoutSessions.Expire(ctx, sessionID,
		&stripe.CheckoutSessionExpireParams{})
	return err
}
```

Verify the exact method and params type against the installed SDK before
writing it:

```bash
cd backend && go doc github.com/stripe/stripe-go/v86/checkout/session 2>/dev/null | head -30
grep -rn "V1CheckoutSessions" $(go env GOMODCACHE)/github.com/stripe/stripe-go/v86*/client.go | head
```

- [ ] **Step 3: Extend `fakeStripe`**

```go
type fakeStripe struct {
	configured bool
	mu         sync.Mutex
	sessions   []CheckoutParams
	refunds    []string
	expired    []string
	refundErr  error
	expireErr  error
}

func (f *fakeStripe) ExpireCheckoutSession(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.expireErr != nil {
		return f.expireErr
	}
	f.expired = append(f.expired, id)
	return nil
}
```

- [ ] **Step 4: Build to find every other fake**

Run: `cd backend && go build ./... && go vet ./...`
Expected: compile errors naming every other type implementing
`StripeClient` (at least `endpointFakeStripe` in
`internal/platform/httpapi/`). Add the same method to each.

- [ ] **Step 5: Run the backend suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/events/stripe.go backend/internal/events/stripe_client.go \
  backend/internal/events/service_test.go backend/internal/platform/httpapi/
git commit -m "feat(events): ExpireCheckoutSession on the Stripe client"
```

### Task 9: `RemoveRegistration` service method

**Files:**
- Modify: `backend/internal/events/service.go` (new method after
  `OrganizerRefund` at `:784-801`)
- Modify: `backend/internal/events/emails.go` (new email builder)
- Test: `backend/internal/events/service_test.go`

**Interfaces:**
- Consumes: `removed` status (Task 7), `ExpireCheckoutSession` (Task 8).
- Produces:

```go
func (s *Service) RemoveRegistration(ctx context.Context, eventID, registrationID uuid.UUID, keepPayment bool) (*db.Registration, error)
```

and `ErrRemovePaidNeedsDecision = errors.New("paid registration needs refund or explicit keep")`,
plus `participantRemovedEmail(u db.User, ev db.Event, feeKept bool, baseURL string) pendingEmail`.

Behaviour, all decided under the `GetEventForUpdate` lock — never from
the client's view of the row:

| Row status | Intent present | `keepPayment` | Outcome |
|---|---|---|---|
| `pending_payment` / `waitlisted` | n/a | ignored | → `removed`; expire the session; free the spot; promote |
| `paid` | no | ignored | → `removed`; free the spot; promote |
| `paid` | yes | `false` | `ErrRemovePaidNeedsDecision` → 409 |
| `paid` | yes | `true` | → `removed`, intent **preserved**; free the spot; promote |
| `refund_requested` | — | — | `ErrInvalidTransition` → 409 (use Refund or Deny) |
| `cancelled` / `refunded` / `expired` / `removed` | — | — | `ErrInvalidTransition` → 409 |

Preserving `stripe_payment_intent_id` on a kept-payment removal is
load-bearing: `handleChargeRefunded` resolves a later dashboard refund by
that intent, and nulling it would strand the row.

- [ ] **Step 1: Write the failing tests**

```go
// ---- PR 5: RemoveRegistration ----

func TestRemoveFreeEventParticipant(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	user := e.seedUser(t, "player@example.com")
	ev := e.createEvent(t, org, 0, 8)
	e.publish(t, ev.ID)
	reg, err := e.svc.Register(context.Background(), ev.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Status != "paid" {
		t.Fatalf("free registration should be paid, got %s", reg.Status)
	}

	out, err := e.svc.RemoveRegistration(context.Background(), ev.ID, reg.ID, false)
	if err != nil {
		t.Fatalf("removing a free participant must succeed: %v", err)
	}
	if out.Status != "removed" {
		t.Fatalf("want removed, got %s", out.Status)
	}
	if len(e.stripe.refunds) != 0 {
		t.Fatalf("no refund may be attempted for a free event, got %v", e.stripe.refunds)
	}
	if !hasMailSubject(e.mailer, "Removed") {
		t.Fatal("the removed player must be notified")
	}
}

func TestRemovePaidRequiresExplicitKeepPayment(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	user := e.seedUser(t, "player@example.com")
	ev := e.createEvent(t, org, 2000, 8)
	e.publish(t, ev.ID)
	reg := e.registerAndPay(t, ev.ID, user, "pi_keep_1")

	_, err := e.svc.RemoveRegistration(context.Background(), ev.ID, reg.ID, false)
	if !errors.Is(err, ErrRemovePaidNeedsDecision) {
		t.Fatalf("want ErrRemovePaidNeedsDecision, got %v", err)
	}

	out, err := e.svc.RemoveRegistration(context.Background(), ev.ID, reg.ID, true)
	if err != nil {
		t.Fatalf("keepPayment removal must succeed: %v", err)
	}
	if out.Status != "removed" {
		t.Fatalf("want removed, got %s", out.Status)
	}
	if len(e.stripe.refunds) != 0 {
		t.Fatalf("keepPayment must not refund, got %v", e.stripe.refunds)
	}
	if out.StripePaymentIntentID == nil || *out.StripePaymentIntentID != "pi_keep_1" {
		t.Fatal("the payment intent must be preserved so a dashboard refund still resolves")
	}
}

func TestRemoveFreesTheSpotAndPromotes(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	first := e.seedUser(t, "first@example.com")
	second := e.seedUser(t, "second@example.com")
	ev := e.createEvent(t, org, 0, 1)
	e.publish(t, ev.ID)

	held, err := e.svc.Register(context.Background(), ev.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := e.svc.Register(context.Background(), ev.ID, second)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != "waitlisted" {
		t.Fatalf("second registrant should be waitlisted, got %s", waiting.Status)
	}

	if _, err := e.svc.RemoveRegistration(context.Background(), ev.ID, held.ID, false); err != nil {
		t.Fatal(err)
	}
	after, err := e.q.GetRegistration(context.Background(), waiting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status == "waitlisted" {
		t.Fatal("removing the spot-holder must promote the waitlist")
	}
}

func TestRemoveExpiresLiveCheckoutSession(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	user := e.seedUser(t, "player@example.com")
	ev := e.createEvent(t, org, 2000, 8)
	e.publish(t, ev.ID)
	reg, err := e.svc.Register(context.Background(), ev.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Pay(context.Background(), ev.ID, user); err != nil {
		t.Fatal(err)
	}

	if _, err := e.svc.RemoveRegistration(context.Background(), ev.ID, reg.ID, false); err != nil {
		t.Fatal(err)
	}
	if len(e.stripe.expired) != 1 {
		t.Fatalf("the live checkout session must be expired, got %v", e.stripe.expired)
	}
}

func TestRemoveRejectsRefundQueueAndTerminalRows(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	user := e.seedUser(t, "player@example.com")
	ev := e.createEvent(t, org, 2000, 8)
	e.publish(t, ev.ID)
	reg := e.registerAndPay(t, ev.ID, user, "pi_queue_1")

	if _, err := e.q.SetRegistrationTerminal(context.Background(), db.SetRegistrationTerminalParams{
		ID: reg.ID, Status: "refund_requested",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RemoveRegistration(context.Background(), ev.ID, reg.ID, true); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("refund_requested must be refused, got %v", err)
	}

	if _, err := e.q.SetRegistrationTerminal(context.Background(), db.SetRegistrationTerminalParams{
		ID: reg.ID, Status: "removed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RemoveRegistration(context.Background(), ev.ID, reg.ID, true); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("an already-removed row must be refused, got %v", err)
	}
}

func TestRemoveWrongEventIsNotFound(t *testing.T) {
	e := newTestEnv(t)
	org := e.seedUser(t, "org@example.com")
	user := e.seedUser(t, "player@example.com")
	ev := e.createEvent(t, org, 0, 8)
	other := e.createEvent(t, org, 0, 8)
	e.publish(t, ev.ID)
	reg, err := e.svc.Register(context.Background(), ev.ID, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RemoveRegistration(context.Background(), other.ID, reg.ID, false); !errors.Is(err, ErrRegistrationNotFound) {
		t.Fatalf("cross-event id must 404, got %v", err)
	}
}
```

Two test helpers these need — add them beside `publish` if absent:

```go
func (e *testEnv) registerAndPay(t *testing.T, eventID, userID uuid.UUID, intentID string) *db.Registration {
	t.Helper()
	reg, err := e.svc.Register(context.Background(), eventID, userID)
	if err != nil {
		t.Fatal(err)
	}
	paidAt := e.clock.Now()
	pi := intentID
	out, err := e.q.MarkRegistrationPaid(context.Background(), db.MarkRegistrationPaidParams{
		ID: reg.ID, PaidAt: &paidAt, PaymentIntentID: &pi,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &out
}

func hasMailSubject(m *recordMailer, substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sent {
		if strings.Contains(s.subject, substr) {
			return true
		}
	}
	return false
}
```

Check whether an equivalent mail helper already exists in
`service_test.go` (it imports `strings`, so one probably does) and reuse
it rather than adding a duplicate.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/events/ -run 'TestRemove' -v`
Expected: FAIL — `RemoveRegistration` and `ErrRemovePaidNeedsDecision`
undefined.

- [ ] **Step 3: Add the sentinel and the email**

In the `var (...)` block:

```go
	// ErrRemovePaidNeedsDecision: removing a paid registration that has a
	// Stripe charge is ambiguous — refund it (/refund) or state explicitly
	// that the fee is kept. Never inferred, so a stale client cannot pocket
	// someone's money by omission.
	ErrRemovePaidNeedsDecision = errors.New("paid registration needs a refund decision")
```

In `emails.go`, following the bilingual style of the existing builders:

```go
func participantRemovedEmail(u db.User, ev db.Event, feeKept bool, baseURL string) pendingEmail {
	plNote := "Opłata nie została pobrana."
	enNote := "No fee was charged."
	if feeKept {
		plNote = "Opłata nie zostanie zwrócona — skontaktuj się z organizatorem, jeśli to pomyłka."
		enNote = "Your fee will not be refunded — contact the organizer if this is a mistake."
	}
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Usunięcie z wydarzenia / Removed from event: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nOrganizator usunął Cię z %s. %s\n\n---\n\nHi %s,\n\nThe organizer removed you from %s. %s",
			u.DisplayName, ev.Name, plNote, u.DisplayName, ev.Name, enNote,
		),
	}
}
```

- [ ] **Step 4: Implement `RemoveRegistration`**

Model it on `CancelRegistration` (`service.go:645-720`) — same
lock-then-branch shape, same `promoteLocked` + `sendEmails` tail.

```go
// RemoveRegistration ejects a participant without touching money.
// Distinct from OrganizerRefund: free events have no payment intent at
// all, and a paid participant may be removed without a refund (a no-show
// or DQ) provided the caller says so explicitly.
func (s *Service) RemoveRegistration(
	ctx context.Context, eventID, registrationID uuid.UUID, keepPayment bool,
) (*db.Registration, error) {
	var out db.Registration
	var emails []pendingEmail
	sessionToExpire := ""
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		reg, err := qtx.GetRegistration(ctx, registrationID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && reg.EventID != eventID) {
			return ErrRegistrationNotFound
		}
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		// Re-read under the lock: a paid webhook may have landed between the
		// organizer's page load and this call.
		reg, err = qtx.GetRegistration(ctx, registrationID)
		if err != nil {
			return err
		}
		switch reg.Status {
		case "pending_payment", "waitlisted":
			// Nothing captured yet; the session (if any) is killed below.
		case "paid":
			if reg.StripePaymentIntentID != nil && !keepPayment {
				return ErrRemovePaidNeedsDecision
			}
		default:
			// refund_requested belongs to the refund queue (money in limbo),
			// and terminal rows are already gone.
			return fmt.Errorf("%w: remove on %s registration", ErrInvalidTransition, reg.Status)
		}
		if reg.StripeCheckoutSessionID != nil {
			sessionToExpire = *reg.StripeCheckoutSessionID
		}
		// Status only — the payment intent is deliberately preserved so a
		// later dashboard refund still resolves to this row.
		out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: reg.ID, Status: "removed",
		})
		if err != nil {
			return err
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		emails = append(emails, participantRemovedEmail(u, ev, reg.StripePaymentIntentID != nil, s.baseURL))
		more, err := s.promoteLocked(ctx, qtx, ev)
		emails = append(emails, more...)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	if sessionToExpire != "" {
		// Best-effort: shrinks the window for a late completion. The
		// authoritative protection is the reclaim guard in
		// handleCheckoutCompleted, which refuses to reinstate a removed row.
		if err := s.stripe.ExpireCheckoutSession(ctx, sessionToExpire); err != nil {
			s.log.Warn("events: expiring checkout session on removal failed",
				"registration", out.ID, "error", err)
		}
	}
	return &out, nil
}
```

`promoteLocked` already no-ops when no spot is free, so calling it
unconditionally is correct — including for a `waitlisted` removal, which
frees nothing.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/events/ -run 'TestRemove' -v`
Expected: PASS (6 tests).

- [ ] **Step 6: Run the full backend suite**

Run: `make test`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/events/service.go backend/internal/events/emails.go \
  backend/internal/events/service_test.go
git commit -m "feat(events): RemoveRegistration for free and no-refund removals"
```

### Task 10: `remove` endpoint and `hasPayment` on the wire

**Files:**
- Modify: `backend/internal/platform/httpapi/events.go` (new endpoint
  after `refundRegistration` at `:528-551`; `RegistrationInfo` at `:19-32`;
  `listEventRegistrations` at `:496-525`; `mapEventErr` at `:84-110`)
- Test: `backend/internal/platform/httpapi/events_endpoints_test.go`

**Interfaces:**
- Consumes: `RemoveRegistration`, `ErrRemovePaidNeedsDecision` (Task 9).
- Produces: `POST /api/events/{eventId}/registrations/{registrationId}/remove`
  with body `{ keepPayment: bool }`, and `RegistrationInfo.HasPayment bool`
  (`json:"hasPayment"`, always present).

`hasPayment` is derived (`r.StripePaymentIntentID != nil`) rather than
exposing the intent id — the frontend needs only to choose which buttons
to show, and the id itself is not the client's business.

- [ ] **Step 1: Write the failing endpoint test**

Follow the existing harness in
`backend/internal/platform/httpapi/events_endpoints_test.go` (see the
pattern near `:271`). Cover:

```go
// PR 5: remove endpoint
// 1. Free event, admin removes a paid-without-intent row → 200, status "removed".
// 2. Paid event with an intent, keepPayment=false → 409, type "remove-needs-decision".
// 3. Same row, keepPayment=true → 200, status "removed".
// 4. Non-admin caller → 403 (requireAdmin), asserted before any state change.
// 5. Anonymous caller → 401.
// 6. listEventRegistrations exposes hasPayment=false for the free row and
//    hasPayment=true for the charged one.
```

Write each as its own `func Test…` in the file's existing style, asserting
on `resp.StatusCode` and the decoded `ErrorModel.Type` for the failure
cases.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/platform/httpapi/ -run 'Remove|HasPayment' -v`
Expected: FAIL — 404, the route does not exist.

- [ ] **Step 3: Add `HasPayment` to the wire type**

```go
type RegistrationInfo struct {
	ID          uuid.UUID  `json:"id"`
	Status      string     `json:"status" enum:"pending_payment,paid,waitlisted,cancelled,refund_requested,refunded,expired,removed"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	WaitlistPos *int64     `json:"waitlistPos,omitempty"`
	PaidAt      *time.Time `json:"paidAt,omitempty"`
	// HasPayment: a Stripe charge exists on this row, so removing it needs
	// an explicit refund decision. Required (never omitted) so the client
	// can always branch on it.
	HasPayment bool `json:"hasPayment"`
}

func registrationInfoFrom(r db.Registration) RegistrationInfo {
	return RegistrationInfo{
		ID: r.ID, Status: r.Status, ExpiresAt: r.ExpiresAt,
		WaitlistPos: r.WaitlistPos, PaidAt: r.PaidAt,
		HasPayment: r.StripePaymentIntentID != nil,
	}
}
```

Also set it in the `listEventRegistrations` loop, which builds
`RegistrationInfo` inline rather than through the helper:

```go
				RegistrationInfo: RegistrationInfo{
					ID: r.ID, Status: r.Status, ExpiresAt: r.ExpiresAt,
					WaitlistPos: r.WaitlistPos, PaidAt: r.PaidAt,
					HasPayment: r.StripePaymentIntentID != nil,
				},
```

- [ ] **Step 4: Register the endpoint**

```go
	huma.Register(api, huma.Operation{
		OperationID: "removeRegistration",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/registrations/{registrationId}/remove",
		Summary:     "Remove a participant without touching money (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *struct {
		registrationActionInput
		Body struct {
			// KeepPayment must be set explicitly to remove a participant whose
			// fee was charged; the fee is then NOT refunded.
			KeepPayment bool `json:"keepPayment"`
		}
	}) (*registrationOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		eventID, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		regID, err := uuid.Parse(in.RegistrationID)
		if err != nil {
			return nil, eventProblem(http.StatusNotFound, "registration-not-found", "no such registration")
		}
		reg, err := deps.Events.RemoveRegistration(ctx, eventID, regID, in.Body.KeepPayment)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})
```

Confirm that embedding `registrationActionInput` alongside a `Body`
resolves the path params correctly; if huma rejects the embedded shape,
declare a flat input struct that repeats the two `path` fields with the
same tags as `registrationActionInput`.

- [ ] **Step 5: Map the new sentinel**

```go
	case errors.Is(err, events.ErrRemovePaidNeedsDecision):
		return eventProblem(http.StatusConflict, "remove-needs-decision", err.Error())
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/platform/httpapi/ -v`
Expected: PASS.

- [ ] **Step 7: Regenerate the OpenAPI client**

Run: `make api-generate`
Expected: `frontend/src/shared/api/` gains the `remove` path,
`hasPayment` on `RegistrationInfo`, and `removed` in both status enums.
Never hand-edit these files; commit them.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/platform/httpapi/ frontend/src/shared/api/
git commit -m "feat(events): remove-registration endpoint, hasPayment on the wire"
```

### Task 11: Remove buttons in the registrations table

**Files:**
- Modify: `frontend/src/features/events/api.ts` (new mutation)
- Modify: `frontend/src/features/events/components/RegistrationsTable.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/events/components/RegistrationsTable.test.tsx` (extend)

**Interfaces:**
- Consumes: `hasPayment` and the `remove` path from Task 10;
  `ConfirmDialog` from Task 6; the `status` prop from Task 5.
- Produces: `useRemoveRegistration(eventId)` — a mutation taking
  `{ registrationId: string; keepPayment: boolean }`.

Buttons per row:
- free / unpaid rows (`hasPayment === false`) → `[Remove]`
- paid rows (`hasPayment === true`) → `[Refund]` `[Remove — no refund]`,
  the second danger-styled, its confirm naming the fee and stating plainly
  that it will not be returned.

- [ ] **Step 1: Add the message keys**

To `frontend/messages/en.json`, beside the existing `regs_*` keys:

```json
  "regs_remove": "Remove",
  "regs_remove_confirm": "Remove {name} from this event? No fee was charged.",
  "regs_remove_keep": "Remove — no refund",
  "regs_remove_keep_confirm": "Remove {name} without refunding their entry fee? The fee is kept and this cannot be undone.",
```

To `frontend/messages/pl.json`, the same keys:

```json
  "regs_remove": "Usuń",
  "regs_remove_confirm": "Usunąć {name} z wydarzenia? Opłata nie została pobrana.",
  "regs_remove_keep": "Usuń — bez zwrotu",
  "regs_remove_keep_confirm": "Usunąć {name} bez zwrotu opłaty? Opłata zostanie zatrzymana i nie można tego cofnąć.",
```

- [ ] **Step 2: Write the failing test**

Append to `RegistrationsTable.test.tsx`:

```tsx
test("free rows get Remove, paid rows get Refund plus Remove-no-refund", async () => {
  renderTable("published", [
    row({ status: "paid", displayName: "Ann", hasPayment: false }),
    row({ status: "paid", displayName: "Bob", hasPayment: true }),
  ]);
  await screen.findByText("Ann");

  const annRow = screen.getByText("Ann").closest("li");
  const bobRow = screen.getByText("Bob").closest("li");
  expect(annRow).not.toBeNull();
  expect(bobRow).not.toBeNull();

  expect(within(annRow as HTMLElement).getByRole("button", { name: "Remove" })).toBeInTheDocument();
  expect(within(annRow as HTMLElement).queryByRole("button", { name: "Refund" })).not.toBeInTheDocument();

  expect(within(bobRow as HTMLElement).getByRole("button", { name: "Refund" })).toBeInTheDocument();
  expect(
    within(bobRow as HTMLElement).getByRole("button", { name: "Remove — no refund" }),
  ).toBeInTheDocument();
});
```

Add `within` to the `@testing-library/react` import.

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd frontend && pnpm vitest run src/features/events/components/RegistrationsTable.test.tsx`
Expected: FAIL — no Remove buttons exist.

- [ ] **Step 4: Add the mutation**

In `frontend/src/features/events/api.ts`, following `useRefundRegistration`:

```ts
export function useRemoveRegistration(eventId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { registrationId: string; keepPayment: boolean }) => {
      const { data, error } = await client.POST(
        "/api/events/{eventId}/registrations/{registrationId}/remove",
        {
          params: { path: { eventId, registrationId: vars.registrationId } },
          body: { keepPayment: vars.keepPayment },
        },
      );
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["events"] }),
  });
}
```

- [ ] **Step 5: Render the buttons**

Widen the confirm state and add the two button branches. `Confirm` becomes:

```tsx
type Confirm =
  | { kind: "refund" | "deny"; row: EventRegistrationRow }
  | { kind: "remove"; row: EventRegistrationRow; keepPayment: boolean };
```

Inside the row's action span, replace the single refund branch:

```tsx
                      {(r.status === "refund_requested" ||
                        (r.status === "paid" && r.hasPayment)) && (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          loading={refundingId === r.id}
                          onClick={() => setConfirm({ kind: "refund", row: r })}
                        >
                          {m.regs_refund()}
                        </Button>
                      )}
                      {(r.status === "paid" ||
                        r.status === "pending_payment" ||
                        r.status === "waitlisted") && (
                        <Button
                          type="button"
                          size="sm"
                          variant={r.hasPayment ? "danger" : "outline"}
                          loading={removingId === r.id}
                          onClick={() =>
                            setConfirm({ kind: "remove", row: r, keepPayment: r.hasPayment })
                          }
                        >
                          {r.hasPayment ? m.regs_remove_keep() : m.regs_remove()}
                        </Button>
                      )}
```

with the row-scoped spinner derived from in-flight variables per the
a11y convention:

```tsx
  const remove = useRemoveRegistration(eventId);
  const removingId = remove.isPending ? remove.variables.registrationId : null;
```

Extend the `ConfirmDialog` wiring from Task 6 to handle the third kind,
selecting the message by `keepPayment`:

```tsx
        onConfirm={() => {
          if (confirm == null) return;
          if (confirm.kind === "remove") {
            remove.mutate(
              { registrationId: confirm.row.id, keepPayment: confirm.keepPayment },
              { onSettled: () => setConfirm(null) },
            );
            return;
          }
          const mut = confirm.kind === "deny" ? deny : refund;
          mut.mutate(confirm.row.id, { onSettled: () => setConfirm(null) });
        }}
```

and include `remove.error` in the displayed error: `refund.error ?? deny.error ?? remove.error`.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd frontend && pnpm vitest run src/features/events`
Expected: PASS.

- [ ] **Step 7: Verify end to end**

Run `make up`, then as an admin: create a **free** event, publish it,
register a second account, and remove them from the manage page —
expect success, the row moving to history, and a removal email in
Mailpit (http://localhost:8025). Repeat on a **paid** event with
`make stripe-listen` running: confirm the paid row shows both buttons and
that "Remove — no refund" keeps the charge (no refund in the Stripe test
dashboard). Check both at 360px.

- [ ] **Step 8: Run the full suites and commit**

Run: `make test && cd frontend && pnpm vitest run`
Expected: PASS.

```bash
git add frontend/src/features/events/ frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(events): organizer can remove participants"
```

---
## PR 6 — Tournament "not created yet", rounds prefill, flicker (branch `feature/tournament-empty-state`)

Spec: PR 6. Closes findings #4 and #6.

Both findings share one root cause. `GET /tournament` 404s with
`tournament-not-found` when no tournament exists, so the organizer's
rounds input has nothing to prefill from and the section has an error
state to flicker through. Meanwhile `defaultRounds(n) = ceil(log2(n))`
(`internal/tournaments/service.go:230-235`) already computes the right
answer and is never exposed.

### Task 12: `Get` returns an empty aggregate instead of 404

**Files:**
- Modify: `backend/internal/tournaments/service.go:125-131` (`Detail`),
  `:133-196` (`Get`)
- Modify: `backend/internal/platform/httpapi/tournaments.go:50-57`
  (`TournamentInfo`), `:59-93` (`tournamentInfoFrom`)
- Test: `backend/internal/tournaments/service_test.go`
- Test: `backend/internal/platform/httpapi/tournaments_endpoints_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Detail` gains `Exists bool`, `RecommendedRounds int32`,
  `PaidPlayerCount int32`; `TournamentInfo` gains
  `exists bool`, `recommendedRounds int32`, `paidPlayerCount int32`
  (all required, never omitted). `Get` no longer returns
  `ErrTournamentNotFound` for a missing tournament — only for a missing
  or draft **event**.

This also fixes a latent bug: the frontend keys off the raw HTTP 404
(`api.ts:39`), so "no tournament yet" and "event not found" are
currently indistinguishable.

Keep `ErrTournamentNotFound` — the mutation paths
(`lockTournament`, `:220-226`) still use it, and `mapTournamentErr` still
maps it.

- [ ] **Step 1: Write the failing tests**

```go
// ---- PR 6: empty tournament aggregate ----

func TestGetBeforeTournamentExistsReportsRecommendation(t *testing.T) {
	e := newTournamentEnv(t)
	ev := e.startedEventWithPaidPlayers(t, 12)

	d, err := e.svc.Get(context.Background(), ev.ID, true)
	if err != nil {
		t.Fatalf("Get must succeed before a tournament exists: %v", err)
	}
	if d.Exists {
		t.Fatal("Exists must be false when no tournament row exists")
	}
	// ceil(log2(12)) = 4
	if d.RecommendedRounds != 4 {
		t.Fatalf("want RecommendedRounds 4 for 12 players, got %d", d.RecommendedRounds)
	}
	if d.PaidPlayerCount != 12 {
		t.Fatalf("want PaidPlayerCount 12, got %d", d.PaidPlayerCount)
	}
	if len(d.Rounds) != 0 {
		t.Fatalf("want no rounds, got %d", len(d.Rounds))
	}
}

func TestGetAfterTournamentExistsSetsExists(t *testing.T) {
	e := newTournamentEnv(t)
	ev := e.startedEventWithPaidPlayers(t, 4)
	if _, err := e.svc.Upsert(context.Background(), ev.ID, nil); err != nil {
		t.Fatal(err)
	}
	d, err := e.svc.Get(context.Background(), ev.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Exists {
		t.Fatal("Exists must be true once the tournament row exists")
	}
	if d.RecommendedRounds != 2 {
		t.Fatalf("want RecommendedRounds 2 for 4 players, got %d", d.RecommendedRounds)
	}
}

func TestGetUnknownEventStillNotFound(t *testing.T) {
	e := newTournamentEnv(t)
	if _, err := e.svc.Get(context.Background(), uuid.New(), true); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("an unknown event must still be not-found, got %v", err)
	}
}
```

Match the existing harness: check the real helper names with
`grep -n 'func newTournamentEnv\|func (e \*tournamentEnv)' backend/internal/tournaments/service_test.go`
and the real name of the planned-rounds upsert method with
`grep -n 'func (s \*Service) Upsert' backend/internal/tournaments/service.go`.
If no "started event with N paid players" helper exists, write one beside
the others rather than inlining the seeding in each test.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/tournaments/ -run 'TestGet' -v`
Expected: FAIL — `Exists` undefined; the first test errors with
`ErrTournamentNotFound`.

- [ ] **Step 3: Extend `Detail` and rework `Get`**

```go
type Detail struct {
	EventID uuid.UUID
	// Exists is false before the organizer creates the tournament. The
	// endpoint returns 200 with an empty aggregate rather than 404 so the
	// client has no error state to flicker through, and so a genuinely
	// missing event stays distinguishable from "no tournament yet".
	Exists            bool
	PlannedRounds     int32
	RecommendedRounds int32
	PaidPlayerCount   int32
	Players           []PlayerDetail
	Rounds            []RoundDetail
	Standings         []swiss.Standing
}
```

At the top of `Get`, replace the `pgx.ErrNoRows → ErrTournamentNotFound`
early return. The event must still be validated so unknown and draft
events 404 as before:

```go
func (s *Service) Get(ctx context.Context, eventID uuid.UUID, admin bool) (*Detail, error) {
	ev, err := s.queries.GetEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	// Drafts are invisible (same convention as internal/events).
	if ev.Status == "draft" {
		return nil, ErrEventNotFound
	}
	// The recommendation tracks the live paid roster, so it stays useful
	// while registrations are still moving.
	roster, err := s.queries.ListPaidRegistrationUsers(ctx, eventID)
	if err != nil {
		return nil, err
	}
	recommended := defaultRounds(len(roster))

	tour, err := s.queries.GetTournamentByEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &Detail{
			EventID:           eventID,
			Exists:            false,
			RecommendedRounds: recommended,
			PaidPlayerCount:   int32(len(roster)),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	// …existing body unchanged from here…
```

Then set the three new fields where `d` is constructed:

```go
	d := &Detail{
		EventID: eventID, Exists: true, PlannedRounds: tour.PlannedRounds,
		RecommendedRounds: recommended, PaidPlayerCount: int32(len(roster)),
	}
```

Check that `GetEvent` exists on the tournaments `Queries` handle
(`grep -n 'GetEvent' backend/internal/db/queries/events.sql`) — it is
shared sqlc output, so it does. Confirm `ErrEventNotFound` is already
declared in the tournaments package (it is — `mapTournamentErr` maps it).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/tournaments/ -v`
Expected: PASS. Any existing test asserting
`Get` → `ErrTournamentNotFound` for a missing tournament must be updated
to assert `Exists == false` — that is the intended change.

- [ ] **Step 5: Extend the wire type**

```go
type TournamentInfo struct {
	EventID uuid.UUID `json:"eventId"`
	// Exists is false before the organizer creates the tournament.
	Exists            bool                     `json:"exists"`
	PlannedRounds     int32                    `json:"plannedRounds"`
	RecommendedRounds int32                    `json:"recommendedRounds"`
	PaidPlayerCount   int32                    `json:"paidPlayerCount"`
	CurrentRound      *int32                   `json:"currentRound,omitempty"`
	Players           []TournamentPlayerInfo   `json:"players"`
	Rounds            []TournamentRoundInfo    `json:"rounds"`
	Standings         []TournamentStandingInfo `json:"standings"`
}
```

and carry them in `tournamentInfoFrom`:

```go
	out := TournamentInfo{
		EventID: d.EventID, Exists: d.Exists, PlannedRounds: d.PlannedRounds,
		RecommendedRounds: d.RecommendedRounds, PaidPlayerCount: d.PaidPlayerCount,
		Players:   make([]TournamentPlayerInfo, len(d.Players)),
		Rounds:    make([]TournamentRoundInfo, len(d.Rounds)),
		Standings: make([]TournamentStandingInfo, len(d.Standings)),
	}
```

- [ ] **Step 6: Update the endpoint test**

In `backend/internal/platform/httpapi/`, change the test asserting a 404
`tournament-not-found` body from `GET /api/events/{id}/tournament` to
assert **200** with `exists: false` and a `recommendedRounds` matching the
seeded roster. Keep (or add) a case asserting an unknown event id still
returns 404.

- [ ] **Step 7: Run the backend suite and regenerate the client**

Run: `make test && make api-generate`
Expected: PASS; `frontend/src/shared/api/schema.d.ts` gains the three
fields on `TournamentInfo`.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/tournaments/ backend/internal/platform/httpapi/ frontend/src/shared/api/
git commit -m "feat(tournaments): 200 with recommendation instead of 404 when none exists"
```

### Task 13: Prefill the rounds input and stop the flicker

**Files:**
- Modify: `frontend/src/features/tournaments/api.ts:31-45` (`useTournament`)
- Modify: `frontend/src/features/tournaments/components/TournamentPanel.tsx:43-146`
- Modify: `frontend/src/features/tournaments/components/TournamentSection.tsx:38-49`
- Test: `frontend/src/features/tournaments/components/TournamentPanel.test.tsx`
- Test: `frontend/src/features/tournaments/components/TournamentSection.test.tsx`

**Interfaces:**
- Consumes: `exists`, `recommendedRounds` from Task 12.
- Produces: no new exports. `NotFoundError` and the `response.status === 404`
  branch are **deleted** from `useTournament`.

Three distinct flicker causes, all fixed here:

1. `useTournament` has no `placeholderData: keepPreviousData` — unlike
   every other query in the app (`features/cubes/api.ts:21,73`,
   `features/collection/api.ts:21`, `shared/cards/api.ts:17`).
2. The `isPending`/`isError` branches are evaluated **before** `data`, so
   a single failed 10-second poll blanks a section whose cache is still
   warm.
3. `isPending → null` makes a hard refresh pop the section in with a
   layout shift, and it waits on a *second* query (`useEventStatus`)
   before `relevant` is even true, so it arrives in two steps.

Explicitly **not** in scope: global `QueryClient` defaults. `main.tsx:13`
constructs a bare `new QueryClient()`, and changing that would alter
refetch behaviour app-wide to fix a localised bug.

- [ ] **Step 1: Write the failing tests**

```tsx
test("prefills the planned-rounds input from the server recommendation", async () => {
  // Mock GET /tournament with exists:false, recommendedRounds:4.
  renderPanel({ exists: false, recommendedRounds: 4, paidPlayerCount: 12, rounds: [] });
  const input = await screen.findByLabelText(/planned rounds/i);
  expect(input).toHaveValue(4);
});

test("prefers the stored plannedRounds once a tournament exists", async () => {
  renderPanel({
    exists: true,
    plannedRounds: 5,
    recommendedRounds: 4,
    paidPlayerCount: 12,
    rounds: [],
  });
  const input = await screen.findByLabelText(/planned rounds/i);
  expect(input).toHaveValue(5);
});

test("keeps the section rendered while a background refetch is in flight", async () => {
  // Render with data, then make the next fetch reject; the previously
  // rendered round/standings content must stay on screen.
  const { rerenderWithFailingFetch } = renderPanelWithRounds();
  expect(await screen.findByText(/standings/i)).toBeInTheDocument();
  await rerenderWithFailingFetch();
  expect(screen.getByText(/standings/i)).toBeInTheDocument();
});
```

Build the `renderPanel` helper on the same `QueryClientProvider` +
`vi.spyOn(globalThis, "fetch")` shape used by the existing tournament
tests; check them first with
`ls frontend/src/features/tournaments/components/*.test.tsx` and follow
whichever harness is already there rather than inventing a second one.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && pnpm vitest run src/features/tournaments`
Expected: FAIL — the input renders empty, and the section disappears on a
failed refetch.

- [ ] **Step 3: Simplify `useTournament`**

```ts
export function useTournament(eventId: string, opts?: { refetchInterval?: number | false }) {
  return useQuery({
    queryKey: ["tournaments", eventId],
    retry: false,
    refetchInterval: opts?.refetchInterval ?? false,
    // Keep the previous aggregate on screen while polling refetches, so a
    // single failed poll cannot blank the section.
    placeholderData: keepPreviousData,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/events/{eventId}/tournament", {
        params: { path: { eventId } },
      });
      return unwrap(data, error);
    },
  });
}
```

Add `keepPreviousData` to the `@tanstack/react-query` import and **delete**
the `NotFoundError` class plus the `m.tournament_none_yet()` 404 branch.
Then remove the now-dangling `NotFoundError` imports and
`error instanceof NotFoundError` checks from `TournamentSection.tsx:39`
and `TournamentPanel.tsx`.

Note `m.tournament_none_yet()` was never actually rendered (the
NotFound branch returned `null`). Repurpose the key for the real empty
state in Step 5 rather than deleting it.

- [ ] **Step 4: Prefill the input in `TournamentPanel`**

```tsx
          <input
            id="planned-rounds"
            type="number"
            min={1}
            max={30}
            value={plannedRounds ?? t?.plannedRounds ?? t?.recommendedRounds ?? ""}
            onChange={(e) => setPlannedRounds(e.target.value)}
          />
```

and make an empty submit impossible rather than a silent no-op:

```tsx
              onSubmit={(e) => {
                e.preventDefault();
                const raw = plannedRounds ?? String(t?.plannedRounds ?? t?.recommendedRounds ?? "");
                const v = Number(raw);
                if (!Number.isInteger(v) || v < 1 || v > 30) return;
                upsert.mutate(v);
              }}
```

The `noTournament` derivation at `TournamentPanel.tsx:51-58` was built on
the 404; re-derive it from the new field — `const noTournament = t?.exists !== true;`
— and keep `t` itself non-null now that the endpoint always returns a body.

- [ ] **Step 5: Fix the render order in both components**

In `TournamentSection.tsx`, evaluate `data` first and `error` last, and
render a skeleton instead of `null` while genuinely loading:

```tsx
  if (!relevant) return null;
  const t = tournament.data;
  if (t === undefined) {
    // First load only: keepPreviousData means refetches keep the old body.
    return tournament.error ? (
      <p role="alert" className="text-danger">
        {tournament.error.message}
      </p>
    ) : (
      <p className="text-sm text-fg-muted">{m.loading()}</p>
    );
  }
  if (!t.exists) return null;
  const rounds = (t.rounds ?? []).filter((r) => r.status !== "draft");
  if (rounds.length === 0) return null;
```

Apply the same ordering in `TournamentPanel.tsx`, using
`m.tournament_none_yet()` for the organizer-facing "no tournament yet"
copy so the existing message key earns its keep.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd frontend && pnpm vitest run src/features/tournaments`
Expected: PASS.

- [ ] **Step 7: Verify the flicker is gone**

Run `make up`, open a **started** event's manage page, and watch the
tournament section across several 10-second poll cycles. Expected: no
blanking, no layout shift. Then stop the backend (`make down` in another
shell) and confirm the section keeps its last content rather than
vanishing. Check the rounds input is prefilled on a `started` event with
no tournament yet.

- [ ] **Step 8: Run the full frontend suite and commit**

Run: `cd frontend && pnpm vitest run`
Expected: PASS.

```bash
git add frontend/src/features/tournaments/
git commit -m "fix(tournaments): prefill planned rounds, stop section flicker"
```

---

## PR 7 — Drop the drawn-games input (branch `feature/tournament-drop-draws`)

Spec: PR 7. Closes finding #8.

A separate draws field expresses no extra information: a drawn match is
either 1-1 in games or 0-0 (termination in game one), and both are
already read as a draw by `p1Games == p2Games` in
`swiss/standings.go:65-78` and `swiss/pair.go:35-44`.

**The DB column stays.** No migration: existing rows keep their `draws`
value and keep computing identically. Only the request field and the UI
input go away, with the server writing `0` for new reports.

### Task 14: Remove `draws` from the request and the form

**Files:**
- Modify: `backend/internal/platform/httpapi/tournaments.go:190-198`
  (`reportResultInput` body)
- Modify: `backend/internal/tournaments/service.go:639-709`
  (`Result` / `valid()` / `ReportResult`)
- Modify: `frontend/src/features/tournaments/components/ResultForm.tsx`
- Modify: `frontend/src/features/tournaments/api.ts:112-124`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `backend/internal/tournaments/swiss/standings_test.go`
- Test: `backend/internal/tournaments/service_test.go`
- Test: `frontend/src/features/tournaments/components/ResultForm.test.tsx`

**Interfaces:**
- Consumes: nothing.
- Produces: the report-result request body becomes
  `{ p1Games: int32, p2Games: int32 }`. `tournaments.Result` keeps its
  `Draws` field (the DB column and standings still read it) but
  `ReportResult` always persists `0` for it.

- [ ] **Step 1: Write the failing tests**

Backend, in `swiss/standings_test.go` — the UI can now reach `0-0`
easily, so pin the behaviour:

```go
func TestZeroZeroCountsAsADrawWithoutGamePoints(t *testing.T) {
	p1, p2 := uuid.New(), uuid.New()
	players := []Player{{ID: p1, DisplayName: "Ann"}, {ID: p2, DisplayName: "Bob"}}
	matches := []Match{{
		Player1: p1, Player2: &p2,
		Result:  &Result{P1Games: 0, P2Games: 0, Draws: 0},
	}}
	got := ComputeStandings(players, matches)
	for _, s := range got {
		if s.MatchPoints != 1 {
			t.Fatalf("%s: a 0-0 match is a draw worth 1 MP, got %d", s.DisplayName, s.MatchPoints)
		}
		// games == 0 for this player, so GW% must not divide by zero.
		if s.GWPercent != 0 {
			t.Fatalf("%s: want GW%% 0 with no games played, got %v", s.DisplayName, s.GWPercent)
		}
	}
}
```

Backend, in `service_test.go`:

```go
func TestReportResultPersistsZeroDraws(t *testing.T) {
	// Seed a started event with a published round, then report 1-1 and
	// assert the stored row has draws == 0 and standings read it as a draw
	// worth 1 MP each. Match the seeding helpers already in this file.
}
```

Frontend, in `ResultForm.test.tsx`:

```tsx
test("has no draws input and hints that equal games are a draw", async () => {
  renderForm({ id: "m1", tableNumber: 1, p1Games: null, p2Games: null });
  expect(screen.queryByLabelText(/draw/i)).not.toBeInTheDocument();

  await userEvent.clear(screen.getByLabelText(/player 1/i));
  await userEvent.type(screen.getByLabelText(/player 1/i), "1");
  await userEvent.clear(screen.getByLabelText(/player 2/i));
  await userEvent.type(screen.getByLabelText(/player 2/i), "1");
  expect(screen.getByText(/recorded as a draw/i)).toBeInTheDocument();
});

test("still rejects 2-2", async () => {
  renderForm({ id: "m1", tableNumber: 1, p1Games: null, p2Games: null });
  await userEvent.type(screen.getByLabelText(/player 1/i), "2");
  await userEvent.type(screen.getByLabelText(/player 2/i), "2");
  await userEvent.click(screen.getByRole("button", { name: /save|report/i }));
  expect(screen.getByRole("alert")).toBeInTheDocument();
});
```

Read `ResultForm.tsx` first and match the real label text — the labels
come from `m.*` keys, so assert against the same messages the component
renders rather than guessing.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/tournaments/... -run 'ZeroZero|ZeroDraws' -v`
Run: `cd frontend && pnpm vitest run src/features/tournaments/components/ResultForm.test.tsx`
Expected: FAIL — the draws input still exists; the hint does not.

- [ ] **Step 3: Drop `draws` from the request body**

```go
type reportResultInput struct {
	EventID string `path:"eventId"`
	MatchID string `path:"matchId"`
	Body    struct {
		P1Games int32 `json:"p1Games" minimum:"0" maximum:"2"`
		P2Games int32 `json:"p2Games" minimum:"0" maximum:"2"`
	}
}
```

and at the call site build the service `Result` with `Draws: 0`:

```go
		res := tournaments.Result{P1Games: in.Body.P1Games, P2Games: in.Body.P2Games, Draws: 0}
```

Keep `Result.Draws` and its `valid()` bounds check: the field still
persists to a live column, and `swiss` still reads it for historical rows.

- [ ] **Step 4: Update `ResultForm`**

Delete the third `GamesField` (the draws one) and its state key, then add
the draw hint below the two fields:

```tsx
      {result.p1Games === result.p2Games && (
        <p className="text-sm text-fg-muted">{m.tournament_result_draw_hint()}</p>
      )}
```

Keep the existing client-side validation, minus the draws bound: each
value 0..2, sum ≤ 3, and not 2-2.

- [ ] **Step 5: Update the mutation**

```ts
export function useReportResult(eventId: string) {
  return useTournamentMutation(
    eventId,
    async (vars: { matchId: string; result: { p1Games: number; p2Games: number } }) => {
      const { data, error } = await client.PUT(
        "/api/events/{eventId}/tournament/matches/{matchId}/result",
        {
          params: { path: { eventId, matchId: vars.matchId } },
          body: vars.result,
        },
      );
      return unwrap(data, error);
    },
  );
}
```

Match the existing signature shape in `api.ts:112-124` — only the body
type changes.

- [ ] **Step 6: Add the message keys**

`frontend/messages/en.json`:

```json
  "tournament_result_draw_hint": "Equal game counts are recorded as a draw.",
```

`frontend/messages/pl.json`:

```json
  "tournament_result_draw_hint": "Równa liczba gier jest zapisywana jako remis.",
```

Remove the now-unused draws-label key from **both** files if it is not
referenced anywhere else (`grep -rn tournament_result_draws frontend/src`).

- [ ] **Step 7: Run everything**

Run: `make api-generate && make test && cd frontend && pnpm vitest run`
Expected: PASS. The regenerated schema drops `draws` from the request
body while keeping it on `TournamentMatchInfo` (the response still
reports stored values).

- [ ] **Step 8: Commit**

```bash
git add backend/internal/ frontend/src/features/tournaments/ frontend/src/shared/api/ \
  frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(tournaments): drop the drawn-games input"
```

---

## PR 8 — Result disputes, organizer lock, discard guard (branch `feature/tournament-result-disputes`)

Spec: PR 8. Closes findings #5 and #7.

Results are overwritten in place today: `UpdateMatchResult`
(`queries/tournaments.sql:107-112`) is last-writer-wins, and
`reported_by` is not even exposed on the wire, so an overwrite leaves no
trace for the organizer.

### Task 15: Append-only `match_result_reports`

**Files:**
- Create: `backend/migrations/00010_match_result_reports.sql`
- Modify: `backend/internal/db/queries/tournaments.sql`
- Modify: `backend/internal/tournaments/service.go:639-709` (`ReportResult`)
- Test: `backend/internal/tournaments/service_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: table `match_result_reports`; sqlc queries
  `InsertMatchResultReport`, `ListMatchResultReportsForTournament`.
  `ReportResult` inserts a report row **before** updating the match.

The authoritative result stays exactly as it is — last write wins. The
log is additive.

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
-- Append-only log of every reported result. The authoritative result stays
-- on matches (last write wins); this exists so the organizer can see that
-- a result was overwritten and by whom, which is the score-dispute signal.
create table match_result_reports (
    id uuid primary key default gen_random_uuid(),
    match_id uuid not null references matches (id) on delete cascade,
    reported_by uuid not null references users (id),
    -- Denormalized at write time: whether the reporter acted as organizer.
    -- An organizer report locks the match against further player reports,
    -- and that must not change retroactively if roles ever change.
    is_organizer boolean not null,
    p1_games int not null check (p1_games between 0 and 2),
    p2_games int not null check (p2_games between 0 and 2),
    reported_at timestamptz not null default now()
);

create index match_result_reports_match_idx
    on match_result_reports (match_id, reported_at);

-- +goose Down
drop table match_result_reports;
```

- [ ] **Step 2: Add the queries**

Append to `backend/internal/db/queries/tournaments.sql`:

```sql
-- name: InsertMatchResultReport :one
insert into match_result_reports (
    match_id, reported_by, is_organizer, p1_games, p2_games)
values (
    sqlc.arg(match_id), sqlc.arg(reported_by), sqlc.arg(is_organizer),
    sqlc.arg(p1_games), sqlc.arg(p2_games))
returning *;

-- Every report for one tournament, oldest first, with the reporter's name
-- for the organizer's dispute history.
-- name: ListMatchResultReportsForTournament :many
select rr.*, u.display_name
from match_result_reports rr
join matches mt on mt.id = rr.match_id
join rounds rd on rd.id = mt.round_id
join users u on u.id = rr.reported_by
where rd.tournament_id = sqlc.arg(tournament_id)
order by rr.match_id, rr.reported_at;
```

Regenerate: `cd backend && sqlc generate` (or `make api-generate` if the
Makefile chains it — check `make help`).

- [ ] **Step 3: Write the failing test**

```go
func TestReportResultAppendsToTheLog(t *testing.T) {
	// Seed a started event, published round 1, two players.
	// 1. Player A reports 2-1 → one report row, is_organizer=false.
	// 2. Player B reports 1-2 → two report rows, both preserved.
	// 3. The match itself holds B's result (last write wins) — unchanged behaviour.
	// Assert with ListMatchResultReportsForTournament.
}
```

Write it out against the helpers already in `service_test.go`.

- [ ] **Step 4: Run it to verify it fails**

Run: `cd backend && go test ./internal/tournaments/ -run 'AppendsToTheLog' -v`
Expected: FAIL — no rows logged.

- [ ] **Step 5: Insert the report in `ReportResult`**

Inside the existing transaction, after the permission and lock checks and
immediately before `UpdateMatchResult`:

```go
		if _, err := qtx.InsertMatchResultReport(ctx, db.InsertMatchResultReportParams{
			MatchID: m.ID, ReportedBy: callerID, IsOrganizer: admin,
			P1Games: res.P1Games, P2Games: res.P2Games,
		}); err != nil {
			return err
		}
```

Match the real parameter names sqlc generated, and the real local variable
names in that function (`m`, `res`, `callerID`, `admin`).

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd backend && go test ./internal/tournaments/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/00010_match_result_reports.sql \
  backend/internal/db/ backend/internal/tournaments/
git commit -m "feat(tournaments): append-only match result report log"
```

### Task 16: Organizer lock and the two dispute signals

**Files:**
- Modify: `backend/internal/tournaments/service.go` (`ReportResult`
  guard; `Get` derivation; `MatchDetail`)
- Modify: `backend/internal/platform/httpapi/tournaments.go:22-31`
  (`TournamentMatchInfo`), `:59-93` (`tournamentInfoFrom`)
- Test: `backend/internal/tournaments/service_test.go`

**Interfaces:**
- Consumes: the report log (Task 15).
- Produces: `MatchDetail` gains
  `Disputed bool`, `HadDispute bool`, `Reports []ResultReportDetail`,
  where

```go
type ResultReportDetail struct {
	ReporterName string
	IsOrganizer  bool
	P1Games      int32
	P2Games      int32
	ReportedAt   time.Time
}
```

`TournamentMatchInfo` gains `disputed bool`, `hadDispute bool`, and
`reports []TournamentResultReportInfo` (required arrays, empty not
omitted).

The derivation — no dispute state column, so nothing to keep in sync:

```
latest_per_player = one row per reporting player, most recent wins
disputed   = count(distinct (p1,p2) in latest_per_player) > 1
             AND no organizer report exists
hadDispute = latest_per_player scores ever disagreed   (sticky)
```

`disputed` **clears when the players agree**: Ann reports 2-1, Bob
reports 1-2 → disputed; Bob corrects to 2-1 → latest-per-player agree →
badge clears, and the stored result stays consistent because last write
wins and the last write *is* the agreeing correction. Deriving from all
reports instead of latest-per-player would brand the match disputed
forever — that is exactly the bug this shape avoids.

`hadDispute` is sticky and organizer-only: a player who claims a win and
then quietly backs down is the cheating pattern worth catching, and a
badge that vanishes would hide it.

The organizer lock: once the organizer reports, no ordinary player may
overwrite. "Let players do the work until the owner steps in."

- [ ] **Step 1: Write the failing tests**

```go
func TestOrganizerReportLocksOutPlayers(t *testing.T) {
	// Started event, published round, two players, admin organizer.
	// 1. Organizer reports 2-0 → ok.
	// 2. Player 1 reports 0-2 → ErrResultLocked, and the stored result is
	//    still 2-0.
	// 3. The organizer may re-report (2-1) → ok.
}

func TestDisputeClearsWhenPlayersAgree(t *testing.T) {
	// 1. Ann reports 2-1 → disputed=false (only one reporter).
	// 2. Bob reports 1-2 → disputed=true, hadDispute=true.
	// 3. Bob re-reports 2-1 → disputed=false, hadDispute stays true,
	//    stored result is 2-1.
}

func TestOrganizerReportResolvesDispute() {
	// Ann 2-1, Bob 1-2 → disputed. Organizer reports 2-0 → disputed=false
	// (an organizer report exists), hadDispute stays true.
}

func TestRepeatedIdenticalReportsAreNotADispute(t *testing.T) {
	// Ann reports 2-1 twice → disputed=false, hadDispute=false.
}
```

Assert through `svc.Get(...)` so the derivation is tested where it is
consumed. Fill each in against the file's existing helpers.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && go test ./internal/tournaments/ -run 'Dispute|LocksOut' -v`
Expected: FAIL — `Disputed` undefined.

- [ ] **Step 3: Add the lock to `ReportResult`**

After loading the match and before the existing non-admin checks:

```go
		organizerReported, err := qtx.MatchHasOrganizerReport(ctx, m.ID)
		if err != nil {
			return err
		}
		// Players do the reporting until the organizer steps in; from then on
		// the organizer's result is final and only they may change it.
		if !admin && organizerReported {
			return ErrResultLocked
		}
```

with the query:

```sql
-- name: MatchHasOrganizerReport :one
select exists (
    select 1 from match_result_reports
    where match_id = sqlc.arg(match_id) and is_organizer
);
```

`ErrResultLocked` already exists and already maps to 409 `result-locked`
(`httpapi/tournaments.go:126-131`), so no new error type or mapping.

- [ ] **Step 4: Derive the signals in `Get`**

Load the log once per aggregate (one query, not per match) and fold it in:

```go
	reports, err := s.queries.ListMatchResultReportsForTournament(ctx, tour.ID)
	if err != nil {
		return nil, err
	}
	// latest[matchID][reporterID] = that reporter's most recent score.
	// Rows arrive ordered by (match_id, reported_at), so a plain overwrite
	// leaves the latest in place.
	latest := map[uuid.UUID]map[uuid.UUID]reportScore{}
	organizerReported := map[uuid.UUID]bool{}
	hadDispute := map[uuid.UUID]bool{}
	byMatch := map[uuid.UUID][]ResultReportDetail{}
	for _, r := range reports {
		byMatch[r.MatchID] = append(byMatch[r.MatchID], ResultReportDetail{
			ReporterName: r.DisplayName, IsOrganizer: r.IsOrganizer,
			P1Games: r.P1Games, P2Games: r.P2Games, ReportedAt: r.ReportedAt,
		})
		if r.IsOrganizer {
			organizerReported[r.MatchID] = true
		}
		per, ok := latest[r.MatchID]
		if !ok {
			per = map[uuid.UUID]reportScore{}
			latest[r.MatchID] = per
		}
		per[r.ReportedBy] = reportScore{r.P1Games, r.P2Games}
		// Sticky: evaluated after every report, so a later agreement cannot
		// erase the fact that the players once disagreed.
		if distinctScores(per) > 1 {
			hadDispute[r.MatchID] = true
		}
	}
```

with the type and helper at package scope, beside `defaultRounds`:

```go
// reportScore is the comparable key that makes "did the players agree?"
// a set-cardinality question.
type reportScore struct{ p1, p2 int32 }

func distinctScores(per map[uuid.UUID]reportScore) int {
	seen := map[reportScore]struct{}{}
	for _, s := range per {
		seen[s] = struct{}{}
	}
	return len(seen)
}
```

Then when building each `MatchDetail`:

```go
			Disputed: distinctScores(latest[m.ID]) > 1 && !organizerReported[m.ID],
			HadDispute: hadDispute[m.ID],
			Reports:    byMatch[m.ID],
```

- [ ] **Step 5: Expose them on the wire**

```go
type TournamentResultReportInfo struct {
	ReporterName string    `json:"reporterName"`
	IsOrganizer  bool      `json:"isOrganizer"`
	P1Games      int32     `json:"p1Games"`
	P2Games      int32     `json:"p2Games"`
	ReportedAt   time.Time `json:"reportedAt"`
}
```

and on `TournamentMatchInfo`:

```go
	// Disputed: the players' latest reports disagree and no organizer has
	// ruled. Clears when they agree. HadDispute is sticky and organizer-only.
	Disputed   bool                         `json:"disputed"`
	HadDispute bool                         `json:"hadDispute"`
	Reports    []TournamentResultReportInfo `json:"reports"`
```

Populate all three in `tournamentInfoFrom`, allocating `Reports` with
`make([]TournamentResultReportInfo, len(m.Reports))` so it serializes as
`[]` rather than `null`.

**Only send `hadDispute` and `reports` to admins.** `Get` already takes
an `admin bool`; blank both for non-admins so a player cannot mine the
history of other tables:

```go
	if !admin {
		// Players see the live badge only; the audit trail is organizer-only.
		md.HadDispute = false
		md.Reports = nil
	}
```

- [ ] **Step 6: Run the tests and regenerate**

Run: `cd backend && go test ./internal/tournaments/ -v && make api-generate`
Expected: PASS; the schema gains the three match fields and the new
report schema.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/ frontend/src/shared/api/
git commit -m "feat(tournaments): derive result disputes, lock matches after organizer reports"
```

### Task 17: Dispute UI and the discard guard

**Files:**
- Modify: `frontend/src/features/tournaments/components/TournamentSection.tsx`
- Modify: `frontend/src/features/tournaments/components/TournamentPanel.tsx:45,246-269`
- Modify: `frontend/src/features/tournaments/components/ResultForm.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/tournaments/components/TournamentPanel.test.tsx`
- Test: `frontend/src/features/tournaments/components/ResultForm.test.tsx`

**Interfaces:**
- Consumes: `disputed`, `hadDispute`, `reports` (Task 16);
  `ConfirmDialog` (Task 6).
- Produces: `ResultForm` gains `onDirtyChange?: (dirty: boolean) => void`,
  called whenever the entered result diverges from the match's stored
  result.

The discard guard (#7): `TournamentPanel.tsx:45` holds a single-slot
`editingMatch`, so opening another row silently discards unsaved edits.

- [ ] **Step 1: Add the message keys**

`frontend/messages/en.json`:

```json
  "tournament_result_disputed": "Disputed",
  "tournament_result_disputed_player": "Your opponent reported a different result.",
  "tournament_result_had_dispute": "Resolved after disagreement",
  "tournament_result_locked_by_organizer": "The organizer recorded this result.",
  "tournament_result_reported_by": "{name} reported {p1}–{p2}",
  "tournament_discard_title": "Discard unsaved result?",
  "tournament_discard_message": "The result you entered for table {table} has not been submitted. Discard it?",
  "tournament_discard_confirm": "Discard",
```

`frontend/messages/pl.json`:

```json
  "tournament_result_disputed": "Sporny",
  "tournament_result_disputed_player": "Przeciwnik zgłosił inny wynik.",
  "tournament_result_had_dispute": "Rozstrzygnięty po rozbieżności",
  "tournament_result_locked_by_organizer": "Organizator zapisał ten wynik.",
  "tournament_result_reported_by": "{name} zgłosił {p1}–{p2}",
  "tournament_discard_title": "Odrzucić niezapisany wynik?",
  "tournament_discard_message": "Wynik wpisany dla stołu {table} nie został zapisany. Odrzucić go?",
  "tournament_discard_confirm": "Odrzuć",
```

- [ ] **Step 2: Write the failing tests**

```tsx
test("shows a disputed badge and the report history to the organizer", async () => {
  renderPanelWithMatch({
    id: "m1",
    tableNumber: 3,
    disputed: true,
    hadDispute: true,
    reports: [
      { reporterName: "Ann", isOrganizer: false, p1Games: 2, p2Games: 1, reportedAt: "2026-09-03T14:02:00Z" },
      { reporterName: "Bob", isOrganizer: false, p1Games: 1, p2Games: 2, reportedAt: "2026-09-03T14:05:00Z" },
    ],
  });
  expect(await screen.findByText("Disputed")).toBeInTheDocument();
  expect(screen.getByText(/Ann reported 2–1/)).toBeInTheDocument();
  expect(screen.getByText(/Bob reported 1–2/)).toBeInTheDocument();
});

test("confirms before discarding an unsaved result when opening another table", async () => {
  renderPanelWithTwoMatches();
  await userEvent.click(screen.getAllByRole("button", { name: /report result/i })[0]);
  await userEvent.type(screen.getByLabelText(/player 1/i), "2");
  await userEvent.click(screen.getAllByRole("button", { name: /report result/i })[1]);

  expect(screen.getByText(/has not been submitted/i)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Discard" }));
  // The second table's form is now the open one.
  expect(screen.getByLabelText(/player 1/i)).toHaveValue(null);
});

test("opening another table without edits does not prompt", async () => {
  renderPanelWithTwoMatches();
  await userEvent.click(screen.getAllByRole("button", { name: /report result/i })[0]);
  await userEvent.click(screen.getAllByRole("button", { name: /report result/i })[1]);
  expect(screen.queryByText(/has not been submitted/i)).not.toBeInTheDocument();
});
```

- [ ] **Step 3: Run them to verify they fail**

Run: `cd frontend && pnpm vitest run src/features/tournaments`
Expected: FAIL — no badge, no prompt.

- [ ] **Step 4: Report dirty state from `ResultForm`**

```tsx
export function ResultForm({
  match,
  onSubmit,
  pending,
  error,
  onDirtyChange,
}: {
  /* …existing props… */
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const [result, setResult] = useState<ResultInput>({
    p1Games: match.p1Games ?? 0,
    p2Games: match.p2Games ?? 0,
  });

  const dirty =
    result.p1Games !== (match.p1Games ?? 0) || result.p2Games !== (match.p2Games ?? 0);
  useEffect(() => {
    onDirtyChange?.(dirty);
    // Clear the flag when this form goes away, or the panel would keep
    // prompting about a form that no longer exists.
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);
```

Wrap the `onDirtyChange` the panel passes in `useCallback` so this effect
does not re-run every render.

- [ ] **Step 5: Guard the switch in `TournamentPanel`**

```tsx
  const [editingMatch, setEditingMatch] = useState<string | null>(null);
  const [dirtyMatch, setDirtyMatch] = useState<string | null>(null);
  const [pendingSwitch, setPendingSwitch] = useState<string | null>(null);

  const handleDirty = useCallback(
    (matchId: string) => (dirty: boolean) => setDirtyMatch(dirty ? matchId : null),
    [],
  );

  function requestOpen(matchId: string) {
    const next = editingMatch === matchId ? null : matchId;
    // Only prompt when the form being replaced actually has unsaved edits.
    if (dirtyMatch !== null && dirtyMatch !== matchId) {
      setPendingSwitch(next);
      return;
    }
    setEditingMatch(next);
  }
```

Point the row's toggle at `requestOpen(mt.id)`, pass
`onDirtyChange={handleDirty(mt.id)}` to each `ResultForm`, and render the
confirm:

```tsx
      <ConfirmDialog
        open={pendingSwitch !== null}
        onClose={() => setPendingSwitch(null)}
        onConfirm={() => {
          setDirtyMatch(null);
          setEditingMatch(pendingSwitch);
          setPendingSwitch(null);
        }}
        title={m.tournament_discard_title()}
        message={m.tournament_discard_message({
          table: rounds.flatMap((r) => r.matches).find((x) => x.id === dirtyMatch)?.tableNumber ?? 0,
        })}
        confirmLabel={m.tournament_discard_confirm()}
        danger
      />
```

Derive `table` from whatever variable actually holds the round's matches
in that component — read the file and use the real one rather than
copying `rounds.flatMap` blindly.

- [ ] **Step 6: Render the dispute signals**

In `TournamentPanel` (organizer), on each match row: the `Disputed`
badge when `mt.disputed`, the quiet `hadDispute` marker when
`mt.hadDispute && !mt.disputed`, and the report list from `mt.reports`
using `m.tournament_result_reported_by({ name, p1, p2 })` with a
locale-formatted timestamp (`new Date(r.reportedAt).toLocaleTimeString(getLocale())`,
matching `RegistrationsTable`'s use of `getLocale()`).

In `TournamentSection` (player), on their own match: the badge plus
`m.tournament_result_disputed_player()`. When the round's result is locked
by an organizer report, replace the form with
`m.tournament_result_locked_by_organizer()`. The client cannot see
`isOrganizer` on other players' reports (Task 16 blanks `reports` for
non-admins), so derive "locked" from the 409 `result-locked` response
**and** hide the form when `mt.reports` is empty but the match is
reported — simplest correct signal: show the form only while
`canReportMine` holds, and surface the 409 detail in the existing
`<Alert>` when the server refuses.

- [ ] **Step 7: Run the tests and the full suite**

Run: `cd frontend && pnpm vitest run`
Expected: PASS.

- [ ] **Step 8: Verify end to end**

Run `make up`. With three accounts (one admin, two players) on a started
event with a published round: have both players report conflicting
results and confirm the badge appears for both and the history appears for
the organizer. Have the losing player re-report the agreeing score and
confirm the badge clears while the organizer still sees "Resolved after
disagreement". Then have the organizer report and confirm a player's
further attempt is refused. Check the discard prompt by typing a result
and clicking another table's Report result. Verify at 360px.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/features/tournaments/ frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(tournaments): surface result disputes and guard unsaved reports"
```

---
## PR 9 — Shared card-list import, and cube bulk import (branch `feature/cube-bulk-import`)

Spec: PR 9. Closes finding #9.

The commit side already exists: `POST /cubes/{cubeId}/changes` accepts up
to 1000 adds. What is missing is *resolution and review*, which exists
but is trapped in the collection feature — and `features/cubes` may not
import `features/collection` (Global Constraints, rule 1).

### Task 18: Move parse + resolve into `internal/cards`

**Files:**
- Create: `backend/internal/cards/list.go` (moved from
  `backend/internal/collections/parse.go` + the resolve half of
  `backend/internal/collections/service.go:187-306`)
- Create: `backend/internal/cards/list_test.go` (moved parse tests)
- Modify: `backend/internal/collections/service.go` (delete
  `ResolveImport`, `suggest`, `CardRef`, `ResolvedLine`, the status
  consts, and `parse.go`; keep `ApplyImport` and `Wantlist`)
- Modify: `backend/internal/platform/httpapi/collections.go:266-296`
  (retire the endpoint) and `backend/internal/platform/httpapi/cards.go`
  (add the new one)
- Modify: `backend/cmd/server/main.go` (wire the resolver)
- Test: `backend/internal/cards/list_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces, in package `cards`:

```go
const (
	StatusMatched   = "matched"
	StatusAmbiguous = "ambiguous"
	StatusUnmatched = "unmatched"
)

const (
	MaxImportLines  = 500
	MaxItemQuantity = 999
)

var ErrTooManyLines = errors.New("import exceeds 500 lines")

type CardRef struct { /* unchanged from collections.CardRef */ }
type ResolvedLine struct { /* unchanged from collections.ResolvedLine */ }
type ParsedLine struct { /* unchanged from collections.ParsedLine */ }

func ParseImportText(text string) ([]ParsedLine, error)
func (s *Service) ResolveList(ctx context.Context, text string) ([]ResolvedLine, error)
```

Endpoint: `POST /api/cards/resolve-list`, body `{ text: string }`
(1–65536 chars), response `{ lines: [...] }` — same shape as the retired
`POST /api/collection/import/resolve`.

This is pure name → printing resolution touching only `cards` tables.
Leaving it in place would permanently serve a cubes feature from an
`/api/collection/` URL. The cost is one generated-client churn on a
single-consumer API, accepted to avoid the organizational debt.

`MaxItemQuantity` stays exported from `collections` too if `ApplyImport`
still references it — check with
`grep -rn 'MaxItemQuantity' backend/` and keep whichever package each
caller needs, without duplicating the constant's value in two places
(re-export: `const MaxItemQuantity = cards.MaxItemQuantity`).

- [ ] **Step 1: Move the files and adjust package/receiver names**

```bash
cd /Users/mateusz/projects/cube_planner/backend
git mv internal/collections/parse.go internal/cards/list.go
```

Change the package clause to `package cards`, drop the now-self-referential
`cards.NormalizeName` qualifier to plain `NormalizeName`, and move
`CardRef`, `ResolvedLine`, the status consts, `ResolveImport` (renamed
`ResolveList`) and `suggest` out of `internal/collections/service.go` into
`internal/cards/list.go`.

The `cards.Service` must have a `queries` handle with
`GetCardsByNormalizedNames` and `SuggestCardsByName`. Check its shape
first (`grep -n 'type Service struct' -A 8 backend/internal/cards/*.go`);
if those queries live in `queries/collections.sql`, move them to
`queries/cards.sql` so the SQL sits with the package that owns it, then
`sqlc generate`.

- [ ] **Step 2: Move the parse tests and run them**

```bash
git mv internal/collections/parse_test.go internal/cards/list_test.go
```

Adjust the package clause, then:

Run: `cd backend && go test ./internal/cards/ -v`
Expected: PASS — behaviour is unchanged by a move.

- [ ] **Step 3: Retire the old endpoint, add the new one**

Delete the `resolveImport` registration from
`internal/platform/httpapi/collections.go:266-296` and add to
`internal/platform/httpapi/cards.go`:

```go
	huma.Register(api, huma.Operation{
		OperationID: "resolveCardList",
		Method:      http.MethodPost,
		Path:        "/api/cards/resolve-list",
		Summary:     "Resolve a pasted card list to printings (no side effects)",
		Tags:        []string{"cards"},
	}, func(ctx context.Context, in *struct {
		Body struct {
			Text string `json:"text" minLength:"1" maxLength:"65536"`
		}
	}) (*resolveListOutput, error) {
		if _, err := requireUser(ctx, deps); err != nil {
			return nil, err
		}
		lines, err := deps.Cards.ResolveList(ctx, in.Body.Text)
		if err != nil {
			return nil, mapCardsErr(err)
		}
		out := &resolveListOutput{}
		out.Body.Lines = resolveLinesFrom(lines)
		return out, nil
	})
```

Move the `ImportResolveLine` / `CardSummary`-shaped wire structs and the
`resolveLinesFrom` mapper across from `collections.go` unchanged, and add
a `mapCardsErr` case for `cards.ErrTooManyLines` → 422 `invalid-import`
mirroring the retired `mapCollectionErr` behaviour. Keep the auth
requirement identical to the old endpoint (check whether it used
`requireUser` or was anonymous, and match it).

- [ ] **Step 4: Wire it in `main.go`**

`deps.Cards` must be the `cards.Service`. Check whether `Deps` already
carries it (`grep -n 'Cards' backend/internal/platform/httpapi/api.go`)
and add the field plus the `main.go` assignment if not.

- [ ] **Step 5: Build, test, regenerate**

Run: `cd backend && go build ./... && go vet ./... && cd .. && make test`
Expected: PASS.

Run: `make api-generate`
Expected: `/api/cards/resolve-list` appears and
`/api/collection/import/resolve` disappears.

- [ ] **Step 6: Re-point the frontend collection hook**

In `frontend/src/features/collection/api.ts`, change `useResolveImport`'s
path to `/api/cards/resolve-list`. Behaviour and response shape are
identical, so no component changes yet.

Run: `cd frontend && pnpm vitest run src/features/collection`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/ backend/cmd/ frontend/src/shared/api/ frontend/src/features/collection/api.ts
git commit -m "refactor(cards): move list resolution out of collections"
```

### Task 19: Promote the import dialog to `shared/cards`

**Files:**
- Create: `frontend/src/shared/cards/CardListImportDialog.tsx` (from
  `frontend/src/features/collection/components/ImportDialog.tsx`)
- Create: `frontend/src/shared/cards/listImportReview.ts` (from
  `frontend/src/features/collection/lib/importReview.ts`)
- Create: `frontend/src/shared/cards/useResolveCardList.ts`
- Delete: the two collection files above
- Modify: `frontend/src/features/collection/components/CollectionPage.tsx:93-95`
- Test: `frontend/src/shared/cards/CardListImportDialog.test.tsx`

**Interfaces:**
- Consumes: `/api/cards/resolve-list` (Task 18).
- Produces:

```tsx
// The card, not just its id: the resolved lines already carry a full CardRef,
// and the cube editor's pending diff needs the card to render staged rows.
// Callers that only need an id (collection's commit) map down themselves.
export type ResolvedItem = { card: CardSummary; quantity: number };

export function CardListImportDialog({
  open,
  onClose,
  onApply,
  applying,
  applyError,
  result,
  initialLines,
}: {
  open: boolean;
  onClose: () => void;
  onApply: (items: ResolvedItem[]) => void;
  applying?: boolean;
  applyError?: Error | null;
  result?: { added: number; updated: number } | null;
  /** Seed the review phase directly, skipping the paste phase — used by the
   *  create-cube flow, which resolves before the cube exists (Task 21). */
  initialLines?: ImportResolveLine[];
}): ReactNode
```

The dialog keeps phases 1 (paste) and 2 (review) and owns the resolve
mutation. It no longer owns the **commit**: `onApply` hands the resolved
items to the caller, so collection keeps its existing mutation while the
cube editor stages them in its pending diff. `result` is passed in rather
than derived, so each caller renders its own success copy.

Also promote the message keys: they currently live under
`collection_import_*`. Keep the existing key names (renaming them would
churn `pl.json` for no behavioural gain) and add cube-specific ones only
where the copy must differ.

- [ ] **Step 1: Move the files**

```bash
cd /Users/mateusz/projects/cube_planner/frontend
git mv src/features/collection/components/ImportDialog.tsx src/shared/cards/CardListImportDialog.tsx
git mv src/features/collection/lib/importReview.ts src/shared/cards/listImportReview.ts
```

Rename the component to `CardListImportDialog`, move `useResolveImport`
out of `features/collection/api.ts` into
`src/shared/cards/useResolveCardList.ts` (shared code may not import a
feature), and fix all import paths.

- [ ] **Step 2: Replace the commit with `onApply`**

Delete the `useImportItems()` call and the internal `result` state; take
both from props. The phase-2 submit becomes:

```tsx
          <Button
            type="button"
            loading={applying === true}
            onClick={() => onApply(items)}
          >
            {m.collection_import_apply()}
          </Button>
```

Use the real label key from the current file rather than inventing one —
read it before editing.

- [ ] **Step 3: Update `CollectionPage`**

```tsx
      <CardListImportDialog
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onApply={(items) =>
          importItems.mutate(
            { items: items.map((i) => ({ scryfallId: i.card.scryfallId, quantity: i.quantity })) },
            { onSuccess: (r) => setImportResult(r) },
          )
        }
        applying={importItems.isPending}
        applyError={importItems.error}
        result={importResult}
      />
```

`useImportItems` stays in `features/collection/api.ts` — the commit is
collection-specific, and it maps the richer items down to the
`{ scryfallId, quantity }` pairs its endpoint takes.

- [ ] **Step 4: Move and extend the tests**

```bash
git mv src/features/collection/components/ImportDialog.test.tsx \
  src/shared/cards/CardListImportDialog.test.tsx
```

(if such a test exists — check with `ls src/features/collection/components/`.)
Update imports, then add a test asserting `onApply` receives the built
items rather than the dialog issuing its own commit:

```tsx
test("hands resolved items to onApply instead of committing", async () => {
  const onApply = vi.fn();
  renderDialog({ onApply, resolved: [matchedLine("Lightning Bolt", 2)] });
  await userEvent.click(screen.getByRole("button", { name: /resolve|next|review/i }));
  await userEvent.click(await screen.findByRole("button", { name: /add|import|apply/i }));
  expect(onApply).toHaveBeenCalledWith([
    { card: expect.objectContaining({ name: "Lightning Bolt" }), quantity: 2 },
  ]);
});
```

- [ ] **Step 5: Run the suites**

Run: `cd frontend && pnpm vitest run src/shared src/features/collection`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/shared/cards/ frontend/src/features/collection/
git commit -m "refactor(cards): promote the list import dialog to shared"
```

### Task 20: Import into the cube editor

**Files:**
- Modify: `frontend/src/features/cubes/lib/pendingDiff.ts:12-21,41-59`
- Modify: `frontend/src/features/cubes/components/CubeEditorPage.tsx:150-153`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/cubes/lib/pendingDiff.test.ts`
- Test: `frontend/src/features/cubes/components/CubeEditorPage.test.tsx`

**Interfaces:**
- Consumes: `CardListImportDialog`, `ResolvedItem` (Task 19).
- Produces: a new reducer action
  `{ type: "addMany"; items: { card: CardSummary; quantity: number }[] }`.

The import must land in the **pending diff**, not in a direct commit, so
it is reviewed and committed as an ordinary change with a note — cube
history stays honest and there is no second commit path.

`ResolvedItem` (Task 19) is already `{ card: CardSummary; quantity: number }`,
which is exactly what `addMany` takes — so the dialog's `onApply` items
pass straight into `dispatch` with no mapping.

- [ ] **Step 1: Write the failing reducer test**

```ts
test("addMany stages several cards at once, capped at 99", () => {
  const bolt = card({ oracleId: "o1", scryfallId: "s1", name: "Lightning Bolt" });
  const storm = card({ oracleId: "o2", scryfallId: "s2", name: "Brainstorm" });
  const state = pendingReducer(emptyPending, {
    type: "addMany",
    items: [
      { card: bolt, quantity: 4 },
      { card: storm, quantity: 200 },
    ],
  });
  expect(state.adds.get("o1")?.quantity).toBe(4);
  expect(state.adds.get("o2")?.quantity).toBe(99);
});

test("addMany accumulates onto an existing pending add", () => {
  const bolt = card({ oracleId: "o1", scryfallId: "s1", name: "Lightning Bolt" });
  const once = pendingReducer(emptyPending, { type: "add", card: bolt });
  const state = pendingReducer(once, { type: "addMany", items: [{ card: bolt, quantity: 2 }] });
  expect(state.adds.get("o1")?.quantity).toBe(3);
});
```

Use the existing `card()` fixture helper in that test file; if none
exists, build a `CardSummary` literal with every required field
(`scryfallId, oracleId, name, manaCost, typeLine, colors, imageSmall`).

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && pnpm vitest run src/features/cubes/lib/pendingDiff.test.ts`
Expected: FAIL — unknown action type.

- [ ] **Step 3: Implement `addMany`**

Add to `PendingAction`:

```ts
  | { type: "addMany"; items: { card: CardSummary; quantity: number }[] }
```

and to the reducer, reusing `bumpAdd` so the add/remove cancellation and
the 99 cap behave exactly as single adds do:

```ts
    case "addMany":
      return action.items.reduce((acc, i) => bumpAdd(acc, i.card, i.quantity), state);
```

- [ ] **Step 4: Add the Import button to the editor**

```tsx
      <div className="flex flex-wrap items-end gap-2">
        <div className="flex flex-col gap-1">
          <Label htmlFor="editor-add">{m.cubes_editor_add_label()}</Label>
          <CardAutocomplete id="editor-add" onSelect={(card) => dispatch({ type: "add", card })} />
        </div>
        <Button type="button" variant="outline" onClick={() => setImportOpen(true)}>
          {m.cubes_import_open()}
        </Button>
      </div>
      <CardListImportDialog
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onApply={(items) => {
          dispatch({ type: "addMany", items });
          setImportOpen(false);
        }}
      />
```

No `applying`/`result` props: staging is synchronous and local, and the
existing `PendingChangesBar` is the feedback.

- [ ] **Step 5: Add the message keys**

`frontend/messages/en.json`:

```json
  "cubes_import_open": "Import list",
```

`frontend/messages/pl.json`:

```json
  "cubes_import_open": "Importuj listę",
```

- [ ] **Step 6: Write the editor test**

```tsx
test("imported cards land in the pending diff, not straight into the cube", async () => {
  // Render the editor, open the import dialog, resolve a two-card list,
  // apply, then assert both cards appear in the pending-changes list and
  // that no POST /changes request was made.
});
```

- [ ] **Step 7: Run the suites and commit**

Run: `cd frontend && pnpm vitest run src/features/cubes`
Expected: PASS.

```bash
git add frontend/src/features/cubes/ frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(cubes): bulk import into the editor's pending diff"
```

### Task 21: Paste a list while creating a cube

**Files:**
- Modify: `frontend/src/features/cubes/components/CreateCubePage.tsx:38-82`
- Modify: `frontend/src/features/cubes/components/CubeEditorPage.tsx`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/cubes/components/CreateCubePage.test.tsx`

**Interfaces:**
- Consumes: `useResolveCardList` (Task 19), the `addMany` action (Task 20).
- Produces: no new exports. Navigation carries the resolved items to the
  editor.

Flow: **resolve first** (side-effect-free) → create the cube → navigate to
the editor with the resolved items staged as change #1. Resolving before
creating means a hopeless list does not leave an orphan empty cube.

Carry the items through TanStack Router's navigation `state`, not a
query string (a 500-card list will not fit in a URL):

```tsx
navigate({
  to: "/cubes/$cubeId/edit",
  params: { cubeId: created.id },
  state: { importedItems: items } as never,
});
```

and in `CubeEditorPage`, consume it once on mount:

```tsx
  const routerState = useRouterState({ select: (s) => s.location.state });
  const imported = (routerState as { importedItems?: { card: CardSummary; quantity: number }[] })
    .importedItems;
  useEffect(() => {
    if (imported === undefined || imported.length === 0) return;
    dispatch({ type: "addMany", items: imported });
    // Replace the history entry so a refresh or Back does not re-stage.
    navigate({ to: ".", replace: true, state: {} as never });
  }, [imported, dispatch, navigate]);
```

Verify the router's typed-state API before writing this —
`grep -rn 'useRouterState' frontend/src` and the TanStack Router version
in `frontend/package.json`. If typed state proves awkward under strict
TS, the fallback is a module-scoped handoff in
`features/cubes/lib/importHandoff.ts` (a `let pending: … | null` set by
the create page and drained by the editor on mount) — simpler to type,
same single-consumption semantics, and it must be drained in an effect so
a stale value cannot leak into a later visit.

- [ ] **Step 1: Add the message keys**

`frontend/messages/en.json`:

```json
  "cubes_create_cards_label": "Cards (optional)",
  "cubes_create_cards_hint": "One card per line, e.g. “4 Lightning Bolt”. You can review them before saving.",
```

`frontend/messages/pl.json`:

```json
  "cubes_create_cards_label": "Karty (opcjonalnie)",
  "cubes_create_cards_hint": "Jedna karta na linię, np. „4 Lightning Bolt”. Przed zapisem będzie można je sprawdzić.",
```

- [ ] **Step 2: Write the failing test**

```tsx
test("a pasted list is resolved before the cube is created", async () => {
  // Mock POST /api/cards/resolve-list and POST /api/cubes.
  // Type a name, paste two lines, submit.
  // Assert: resolve-list was called BEFORE /api/cubes, and navigation
  // targeted the editor.
});

test("a hopeless list does not create a cube", async () => {
  // Mock resolve-list to return every line unmatched.
  // Submit, choose to go back / cancel in the review step.
  // Assert POST /api/cubes was never called.
});
```

- [ ] **Step 3: Add the textarea**

Add an optional `<textarea>` to the create form with a `<Label htmlFor>`
matching its `id`, `rows={8}`, and a mono font like the import dialog's.
When it is empty, submit behaves exactly as today.

- [ ] **Step 4: Resolve, then create, then navigate**

```tsx
        onSubmit={(e) => {
          e.preventDefault();
          if (cardText.trim() === "") {
            create.mutate(body, {
              onSuccess: (c) => navigate({ to: "/cubes/$cubeId", params: { cubeId: c.id } }),
            });
            return;
          }
          // Resolve first: a list that resolves to nothing must not leave an
          // orphan empty cube behind.
          resolve.mutate(
            { text: cardText },
            { onSuccess: (lines) => setReview(lines) },
          );
        }}
```

Render the review step with the same `CardListImportDialog` (in review
mode, seeded with `review`), whose `onApply` then creates the cube and
navigates with the items. Read the dialog after Task 19 and add a
`initialLines` prop if seeding it externally is cleaner than re-resolving.

- [ ] **Step 5: Run the suites**

Run: `cd frontend && pnpm vitest run src/features/cubes`
Expected: PASS.

- [ ] **Step 6: Verify end to end**

Run `make up`. Create a cube with ~30 pasted lines including one
misspelling: expect the review step to flag it, the cube to be created
only after applying, and the editor to open with the cards staged and the
`PendingChangesBar` showing the count. Commit them with a note and check
the cube history shows one change. Verify at 360px.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/features/cubes/ frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(cubes): paste a card list while creating a cube"
```

---

## PR 10 — Set-aware wantlist (branch `feature/wantlist-set-aware`)

Spec: PR 10. Closes finding #11.

`cube_cards` is keyed `(cube_id, oracle_id)` with a single `scryfall_id`
per row (`migrations/00004_cubes.sql:23-32`), so "the printing used in the
cube" is unambiguous. `collection_items` is already printing-level
(`(user_id, scryfall_id)`), so both modes are a `group by` swap.

### Task 22: `match=oracle|printing` on the wantlist

**Files:**
- Modify: `backend/internal/db/queries/collections.sql:99-118`
  (`GetCubeWantlist`; add a printing-mode sibling)
- Modify: `backend/internal/collections/service.go:308-353` (`Wantlist`)
- Modify: `backend/internal/platform/httpapi/collections.go:164-174`
  (`WantlistEntry`), `:323-355` (the endpoint)
- Test: `backend/internal/collections/service_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Wantlist(ctx, cubeID, userID uuid.UUID, matchPrinting bool)`
  — note the **new fourth parameter**; every caller must pass it.
  `WantlistItem` and the wire `WantlistEntry` gain `SetCode`, `SetName`,
  `CollectorNumber`. Query string: `?match=oracle` (default) or
  `?match=printing`.

- [ ] **Step 1: Write the failing test**

```go
func TestWantlistPrintingModeIgnoresOtherPrintings(t *testing.T) {
	e := newCollectionsEnv(t)
	owner := e.seedUser(t, "owner@example.com")
	// Two printings of one oracle card.
	leb := e.seedCard(t, "Lightning Bolt", "leb", "162")
	mm2 := e.seedCard(t, "Lightning Bolt", "mm2", "138")

	cube := e.seedCube(t, owner, "Vintage", "public")
	e.seedCubeCard(t, cube, mm2, 1) // the cube calls for the MM2 printing
	e.seedCollectionItem(t, owner, leb, 1) // the user owns only the LEB one

	// Oracle mode: any printing satisfies the slot.
	_, items, total, err := e.svc.Wantlist(context.Background(), cube, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("oracle mode should be satisfied by any printing, got %d missing", total)
	}

	// Printing mode: the exact printing is missing.
	_, items, total, err = e.svc.Wantlist(context.Background(), cube, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("printing mode should want the MM2 copy, got %d missing", total)
	}
	if items[0].SetCode != "mm2" || items[0].CollectorNumber != "138" {
		t.Fatalf("want mm2/138 on the entry, got %s/%s", items[0].SetCode, items[0].CollectorNumber)
	}
}
```

Check the real helper names in that test file first; add `seedCard`,
`seedCubeCard`, `seedCollectionItem` beside the existing helpers if they
are missing.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/collections/ -run 'PrintingMode' -v`
Expected: FAIL — `Wantlist` takes three arguments.

- [ ] **Step 3: Add the printing-mode query**

Append to `backend/internal/db/queries/collections.sql`:

```sql
-- Printing-aware wantlist: ownership counts only the exact printing the
-- cube calls for, so owning Lightning Bolt (LEB) does not satisfy a slot
-- asking for (MM2). Same columns as GetCubeWantlist so the service maps
-- both identically.
-- name: GetCubeWantlistByPrinting :many
select cc.oracle_id, cc.scryfall_id,
    cc.quantity as cube_quantity,
    coalesce(own.quantity, 0)::int as owned_quantity,
    (cc.quantity - coalesce(own.quantity, 0))::int as missing_quantity,
    ca.name, ca.mana_cost, ca.image_small, ca.image_normal,
    ca.set_code, ca.set_name, ca.collector_number
from cube_cards cc
join cards ca on ca.scryfall_id = cc.scryfall_id
left join collection_items own
    on own.user_id = sqlc.arg(user_id) and own.scryfall_id = cc.scryfall_id
where cc.cube_id = sqlc.arg(cube_id)
  and cc.quantity > coalesce(own.quantity, 0)
order by ca.name;
```

and add the same three `ca.*` columns to the existing `GetCubeWantlist`
select list so both modes return identical shapes.

Run: `cd backend && sqlc generate`

- [ ] **Step 4: Branch in the service**

```go
// Wantlist computes cube-minus-collection on demand, never stored.
// matchPrinting switches ownership from oracle level (any printing
// satisfies a slot) to printing level (the cube's exact printing must be
// owned). Cube visibility follows the cubes rule: private cubes 404 for
// non-owners.
func (s *Service) Wantlist(
	ctx context.Context, cubeID, userID uuid.UUID, matchPrinting bool,
) (string, []WantlistItem, int64, error) {
```

Keep the existing visibility check, then select the query by mode and map
both row types into `WantlistItem` (the generated row structs differ, so
either write two small mapping loops or convert through a tiny local
struct — do **not** duplicate the visibility logic).

Add the three fields to `WantlistItem`:

```go
	SetCode         string
	SetName         string
	CollectorNumber string
```

- [ ] **Step 5: Add the query param and the wire fields**

```go
type wantlistInput struct {
	CubeID string `path:"cubeId"`
	Match  string `query:"match" enum:"oracle,printing" default:"oracle"`
}
```

and on `WantlistEntry`:

```go
	SetCode         string `json:"setCode"`
	SetName         string `json:"setName"`
	CollectorNumber string `json:"collectorNumber"`
```

passing `in.Match == "printing"` into the service call.

- [ ] **Step 6: Run the tests and regenerate**

Run: `cd backend && go test ./internal/collections/ -v && cd .. && make test && make api-generate`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/ frontend/src/shared/api/
git commit -m "feat(collection): printing-aware wantlist mode"
```

### Task 23: Set column, mode toggle, and a second download

**Files:**
- Modify: `frontend/src/routes/cubes.$cubeId.wantlist.tsx` (`validateSearch`)
- Modify: `frontend/src/features/collection/components/WantlistPage.tsx`
- Modify: `frontend/src/features/collection/api.ts` (pass `match`)
- Modify: `frontend/src/features/collection/lib/cardmarket.ts`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`
- Test: `frontend/src/features/collection/lib/cardmarket.test.ts`
- Test: `frontend/src/features/collection/components/WantlistPage.test.tsx`

**Interfaces:**
- Consumes: `setCode`, `collectorNumber`, `?match=` (Task 22).
- Produces: `wantlistToSetAnnotatedText(items)` beside the existing
  `wantlistToCardmarketText(items)`; the route's search schema gains
  `match: "oracle" | "printing"` defaulting to `"oracle"`.

**Two download buttons, because the existing one must not break.**
`wantlistToCardmarketText` stays byte-identical: Cardmarket's Wants
import accepts only `<qty> <name>`, so appending set codes there risks
silently breaking a working import. The new button emits
`<qty> <name> (SET)`, the format Moxfield and Archidekt accept.

Per Global Constraints, the mode toggle lives in the URL via
`validateSearch` so it is shareable and survives refresh, and the
component reads it with `getRouteApi("/cubes/$cubeId/wantlist")` rather
than the route file holding logic.

- [ ] **Step 1: Write the failing export test**

```ts
import { expect, test } from "vitest";
import { wantlistToCardmarketText, wantlistToSetAnnotatedText } from "./cardmarket";

const items = [
  { missingQuantity: 1, name: "Lightning Bolt", setCode: "leb" },
  { missingQuantity: 2, name: "Brainstorm", setCode: "mmq" },
];

test("the Cardmarket export stays quantity + name only", () => {
  expect(wantlistToCardmarketText(items)).toBe("1 Lightning Bolt\n2 Brainstorm");
});

test("the set-annotated export appends an uppercased set code", () => {
  expect(wantlistToSetAnnotatedText(items)).toBe(
    "1 Lightning Bolt (LEB)\n2 Brainstorm (MMQ)",
  );
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd frontend && pnpm vitest run src/features/collection/lib/cardmarket.test.ts`
Expected: FAIL — `wantlistToSetAnnotatedText` is not exported.

- [ ] **Step 3: Add the second formatter**

```ts
// Moxfield/Archidekt accept a trailing "(SET)"; Cardmarket does not, which
// is why this is a separate export rather than a change to the one above.
export function wantlistToSetAnnotatedText(
  items: readonly { missingQuantity: number; name: string; setCode: string }[],
): string {
  return items
    .map((i) => `${i.missingQuantity} ${i.name} (${i.setCode.toUpperCase()})`)
    .join("\n");
}
```

- [ ] **Step 4: Add the search param to the route**

```tsx
export const Route = createFileRoute("/cubes/$cubeId/wantlist")({
  beforeLoad: requireAuth,
  validateSearch: (search: Record<string, unknown>): { match: "oracle" | "printing" } => ({
    match: search.match === "printing" ? "printing" : "oracle",
  }),
  component: WantlistPage,
});
```

Match the existing route file's shape (it already declares `beforeLoad`);
add only `validateSearch`.

- [ ] **Step 5: Add the message keys**

`frontend/messages/en.json`:

```json
  "wantlist_set": "Set",
  "wantlist_match_label": "Match printings",
  "wantlist_match_oracle": "Any printing",
  "wantlist_match_printing": "Exact printing",
  "wantlist_download_sets": "Download with sets",
```

`frontend/messages/pl.json`:

```json
  "wantlist_set": "Dodatek",
  "wantlist_match_label": "Dopasowanie wydań",
  "wantlist_match_oracle": "Dowolne wydanie",
  "wantlist_match_printing": "Konkretne wydanie",
  "wantlist_download_sets": "Pobierz z dodatkami",
```

- [ ] **Step 6: Wire the page**

Read `match` from the route API, pass it to the query, add a `Set` column
rendering `setCode.toUpperCase()` with `collectorNumber` as muted
secondary text, and add the toggle as a two-radio fieldset (each label
tied to its input) that navigates rather than holding local state:

```tsx
  const { match } = routeApi.useSearch();
  const navigate = routeApi.useNavigate();
  const wantlist = useWantlist(cubeId, match);
```

Keep the table inside its existing `overflow-x-auto` wrapper — it gains a
column and must not introduce horizontal page scroll at 360px.

- [ ] **Step 7: Write the page test**

```tsx
test("switching to exact-printing mode refetches and shows the set column", async () => {
  // Render at ?match=oracle, assert the Set column header and a row.
  // Click "Exact printing", assert the request carried match=printing.
});
```

- [ ] **Step 8: Run everything and verify**

Run: `cd frontend && pnpm vitest run`
Expected: PASS.

Then `make up`: with a cube calling for one printing and the collection
holding another, confirm oracle mode reports nothing missing and exact
mode reports the card with its set. Download both files and confirm the
Cardmarket one has no set codes. Check 360px.

- [ ] **Step 9: Commit**

```bash
git add frontend/src/routes/cubes.\$cubeId.wantlist.tsx frontend/src/features/collection/ \
  frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(collection): set-aware wantlist with set-annotated export"
```

---

## PR 11 — Set and printing selectors in the import grammar (branch `feature/import-printing-selectors`)

Spec: PR 11. Closes finding #14.

Today an import line is quantity + name, and resolution picks a
*representative* printing per oracle card
(`distinct on (oracle_id) … order by oracle_id, promo, released_at desc`).
A user who wants Urza's Mine (ATQ) 83a specifically — one of four
variants in Antiquities — must import and then click through the printing
picker for each card, which is exactly the manual work bulk import should
remove.

### Task 24: Parse `(SET)` and collector numbers

**Files:**
- Modify: `backend/internal/cards/list.go` (from Task 18)
- Test: `backend/internal/cards/list_test.go`

**Interfaces:**
- Consumes: the moved parser (Task 18).
- Produces: `ParsedLine` gains `SetCode string` and `CollectorNumber string`
  (empty = not specified), plus the exported helper
  `func NameWithoutQuantity(line string) string` — the line with any
  leading quantity token stripped, needed by Task 25's whole-line retry.

Grammar:

```
[<qty>[x]] <name> [(<set>)] [<collector-number>]
```

- **Set:** parenthesized, 2–5 characters of `[a-z0-9]`, case-insensitive.
  Our mirror stores Scryfall's `set` verbatim (lowercase), so lower both
  sides before comparing.
- **Collector number:** accepted **only** when a set was given — a bare
  trailing number is far more likely part of a name. Charset
  `[a-z0-9★†-]`, case-insensitive.

This is deliberately the Moxfield/Archidekt convention
(`1 Lightning Bolt (LEB) 123`), so lists exported from those tools paste
in directly and PR 10's "Download with sets" round-trips.

**Names containing parentheses are real** (`B.F.M. (Big Furry Monster)`,
`Erase (Not the Urza's Legacy One)`). Guard: the parenthesized token must
satisfy the charset **and** the 2–5 length bound, which both examples fail
on length. Task 25 adds the second, belt-and-braces guard.

- [ ] **Step 1: Write the failing tests**

```go
func TestParseSetAndCollectorNumber(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantQty int32
		wantNm  string
		wantSet string
		wantCN  string
		wantOK  bool
	}{
		{"bare name", "Lightning Bolt", 1, "Lightning Bolt", "", "", true},
		{"qty and name", "4 Lightning Bolt", 4, "Lightning Bolt", "", "", true},
		{"qty x and name", "4x Lightning Bolt", 4, "Lightning Bolt", "", "", true},
		{"name and set", "Lightning Bolt (LEB)", 1, "Lightning Bolt", "leb", "", true},
		{"qty name set", "4 Lightning Bolt (leb)", 4, "Lightning Bolt", "leb", "", true},
		{"full", "1 Urza's Mine (ATQ) 83a", 1, "Urza's Mine", "atq", "83a", true},
		{"numeric set", "1 Lightning Bolt (2X2) 117", 1, "Lightning Bolt", "2x2", "117", true},
		{"star collector number", "1 Arcane Signet (SLD) ★12", 1, "Arcane Signet", "sld", "★12", true},
		// A parenthesized token that is not set-shaped stays part of the name.
		{"paren name unglued", "B.F.M. (Big Furry Monster)", 1, "B.F.M. (Big Furry Monster)", "", "", true},
		{"paren name unhinged", "Erase (Not the Urza's Legacy One)", 1, "Erase (Not the Urza's Legacy One)", "", "", true},
		// A bare trailing number is part of the name, not a collector number.
		{"no set means no collector number", "Fire // Ice 128", 1, "Fire // Ice 128", "", "", true},
		{"quantity over cap", "1000 Lightning Bolt", 0, "", "", "", false},
		// Regression: a name that begins with a big number is not a quantity.
		{"numeric name", "1996 World Champion", 1, "1996 World Champion", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLine(1, tt.line)
			if got.OK != tt.wantOK {
				t.Fatalf("OK: want %v, got %v", tt.wantOK, got.OK)
			}
			if !tt.wantOK {
				return
			}
			if got.Quantity != tt.wantQty || got.Name != tt.wantNm ||
				got.SetCode != tt.wantSet || got.CollectorNumber != tt.wantCN {
				t.Fatalf("want qty=%d name=%q set=%q cn=%q, got qty=%d name=%q set=%q cn=%q",
					tt.wantQty, tt.wantNm, tt.wantSet, tt.wantCN,
					got.Quantity, got.Name, got.SetCode, got.CollectorNumber)
			}
		})
	}
}
```

Note the `1996 World Champion` case: today it parses as quantity 1996,
blows the 999 cap and is reported unmatched. Step 3 fixes that as a
drive-by.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/cards/ -run 'ParseSetAndCollector' -v`
Expected: FAIL — `SetCode` undefined.

- [ ] **Step 3: Extend the parser**

Add the two fields to `ParsedLine`, then parse suffix-first (so the name
is whatever remains):

```go
var (
	setCodeRe    = regexp.MustCompile(`^\(([A-Za-z0-9]{2,5})\)$`)
	collectorRe  = regexp.MustCompile(`^[A-Za-z0-9★†-]+$`)
)

// splitSelectors peels an optional trailing "(SET)" and, only when a set
// was found, an optional trailing collector number. Card names really do
// contain parentheses ("B.F.M. (Big Furry Monster)"), so the token must
// look like a set code — 2–5 alphanumerics — to be treated as one.
func splitSelectors(name string) (rest, setCode, collectorNumber string) {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return name, "", ""
	}
	last := len(fields) - 1
	// Case: "... (SET) 83a"
	if last >= 1 && collectorRe.MatchString(fields[last]) {
		if mt := setCodeRe.FindStringSubmatch(fields[last-1]); mt != nil {
			return strings.Join(fields[:last-1], " "), strings.ToLower(mt[1]), strings.ToLower(fields[last])
		}
	}
	// Case: "... (SET)"
	if mt := setCodeRe.FindStringSubmatch(fields[last]); mt != nil {
		return strings.Join(fields[:last], " "), strings.ToLower(mt[1]), ""
	}
	return name, "", ""
}
```

Extract the quantity-stripping so Task 25's whole-line retry can reuse it
instead of re-tokenizing:

```go
// NameWithoutQuantity strips a leading quantity token ("4", "4x") if one is
// present. Exported because list resolution retries a line as a bare name
// when the set-aware reading finds nothing, and it must strip the quantity
// the same way parseLine does.
func NameWithoutQuantity(line string) string {
	first, rest := line, ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		first, rest = line[:i], strings.TrimSpace(line[i+1:])
	}
	tok := strings.TrimSuffix(strings.TrimSuffix(first, "x"), "X")
	qty, err := strconv.Atoi(tok)
	if err != nil || rest == "" || qty < 1 || qty > MaxItemQuantity {
		return line
	}
	return rest
}
```

In `parseLine`, after resolving quantity and the raw name, apply the
selector split — and guard against a selector-only line:

```go
	rest, setCode, collectorNumber := splitSelectors(nameText)
	if rest == "" {
		// The whole line was a selector; treat it as unparsable rather than
		// producing an empty name.
		return p
	}
	p.Quantity, p.Name, p.SetCode, p.CollectorNumber, p.OK =
		qty, rest, setCode, collectorNumber, true
```

Also fix the numeric-name regression: when the leading token parses as a
number **above** `MaxItemQuantity` and a non-empty remainder follows,
treat the whole line as a name rather than rejecting it:

```go
	qty, err := strconv.Atoi(qtyToken)
	switch {
	case err != nil:
		// No leading quantity — the whole line is the name.
		qty = 1
		nameText = line
	case qty > MaxItemQuantity && rest != "":
		// "1996 World Champion" is a card, not 1996 copies of something.
		qty = 1
		nameText = line
	case qty < 1 || rest == "":
		return p
	default:
		nameText = rest
	}
```

Add `regexp` to the imports. Note `strings.ToLower` on a `★` prefix is a
no-op, which is fine — comparison in Task 25 lowers both sides.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/cards/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/cards/
git commit -m "feat(cards): parse (SET) and collector-number selectors"
```

### Task 25: Resolve by set and printing

**Files:**
- Modify: `backend/internal/db/queries/cards.sql`
- Modify: `backend/internal/cards/list.go` (`ResolveList`)
- Modify: `backend/internal/platform/httpapi/cards.go` (wire fields +
  status enum)
- Test: `backend/internal/cards/list_resolve_test.go`

**Interfaces:**
- Consumes: the parser fields (Task 24).
- Produces: new status `StatusPrintingNotFound = "printing-not-found"`;
  `ResolvedLine` gains `SetCode string` and `CollectorNumber string`
  (echoes of what was parsed). Two new batch queries:
  `GetCardsBySetAndCollectorNumbers`, `GetCardsByNameAndSet`.

Resolution ladder, first match wins:

| Input | Resolution | Miss |
|---|---|---|
| set + collector number | exact printing | `printing-not-found`, suggesting every printing of that name |
| set only | printings of that name in that set — one → `matched`; several (Urza's Mine) → `ambiguous` with those variants as suggestions | `printing-not-found` |
| neither | today's behaviour: oracle representative → `ambiguous` → per-line fuzzy → `unmatched` | unchanged |

`printing-not-found` is worth distinguishing from `unmatched`: the name is
real and only the printing is wrong, so the fix is picking a different
printing, not correcting a typo — and the suggestions give the user that
choice in the existing per-line `<select>`, so no new UI is needed.

**Resolution must stay batched** — 500 lines cannot become 500 round
trips. Three constant-count batch queries plus the existing per-line
fuzzy fallback for lines that reach it.

**Determinism does not depend on `(set_code, collector_number)` being
unique.** Scryfall's `default-cards` bulk file is one object per printing
so in practice it is, but there is no unique index. Every new lookup
therefore carries the same
`order by promo, released_at desc, (image_small is null)` tie-break the
existing queries use.

- [ ] **Step 1: Write the failing tests**

```go
func TestResolveListExactPrinting(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Urza's Mine", "atq", "83a")
	e.seedCard(t, "Urza's Mine", "atq", "83b")

	lines, err := e.svc.ResolveList(context.Background(), "1 Urza's Mine (ATQ) 83a")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched {
		t.Fatalf("want matched, got %s", lines[0].Status)
	}
	if lines[0].Match.CollectorNumber != "83a" {
		t.Fatalf("want the 83a printing, got %s", lines[0].Match.CollectorNumber)
	}
}

func TestResolveListSetWithVariantsIsAmbiguous(t *testing.T) {
	e := newCardsEnv(t)
	for _, cn := range []string{"83a", "83b", "83c", "83d"} {
		e.seedCard(t, "Urza's Mine", "atq", cn)
	}
	lines, err := e.svc.ResolveList(context.Background(), "1 Urza's Mine (ATQ)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusAmbiguous {
		t.Fatalf("want ambiguous, got %s", lines[0].Status)
	}
	if len(lines[0].Suggestions) != 4 {
		t.Fatalf("want all 4 variants offered, got %d", len(lines[0].Suggestions))
	}
}

func TestResolveListSetWithOnePrintingMatches(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Lightning Bolt", "leb", "162")
	e.seedCard(t, "Lightning Bolt", "mm2", "138")
	lines, err := e.svc.ResolveList(context.Background(), "1 Lightning Bolt (MM2)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched || lines[0].Match.SetCode != "mm2" {
		t.Fatalf("want the mm2 printing matched, got %s / %+v", lines[0].Status, lines[0].Match)
	}
}

func TestResolveListUnknownPrintingSuggestsOthers(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Lightning Bolt", "leb", "162")
	e.seedCard(t, "Lightning Bolt", "mm2", "138")
	lines, err := e.svc.ResolveList(context.Background(), "1 Lightning Bolt (XYZ) 999")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusPrintingNotFound {
		t.Fatalf("want printing-not-found, got %s", lines[0].Status)
	}
	if len(lines[0].Suggestions) != 2 {
		t.Fatalf("want both real printings suggested, got %d", len(lines[0].Suggestions))
	}
}

func TestResolveListParenthesisedNameStillResolves(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "B.F.M. (Big Furry Monster)", "ugl", "28")
	lines, err := e.svc.ResolveList(context.Background(), "1 B.F.M. (Big Furry Monster)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched {
		t.Fatalf("a parenthesised card name must still resolve, got %s", lines[0].Status)
	}
}

func TestResolveListSetAwareLineFallsBackToWholeLineAsName(t *testing.T) {
	e := newCardsEnv(t)
	// A card whose name ends in a set-code-shaped parenthesis.
	e.seedCard(t, "Sift Through Sands (FOO)", "xyz", "1")
	lines, err := e.svc.ResolveList(context.Background(), "1 Sift Through Sands (FOO)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched {
		t.Fatalf("the whole-line retry must rescue this, got %s", lines[0].Status)
	}
}
```

Add a `newCardsEnv` harness with a `seedCard(t, name, setCode, collectorNumber)`
helper if the cards package has no integration harness yet — model it on
`internal/collections`' testcontainers setup. `seedCard` must set
`normalized_name` with `NormalizeName` and share one `oracle_id` per
distinct name.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd backend && go test ./internal/cards/ -run 'TestResolveList' -v`
Expected: FAIL — `StatusPrintingNotFound` undefined.

- [ ] **Step 3: Add the batch queries**

```sql
-- Exact printings for import lines carrying "(SET) <collector-number>".
-- Batched over parallel arrays so 500 lines stay one round trip. The
-- tie-break mirrors the other card lookups: (set_code, collector_number)
-- has no unique index, so a duplicate pair must resolve stably.
-- name: GetCardsBySetAndCollectorNumbers :many
select distinct on (lower(set_code), lower(collector_number))
    scryfall_id, oracle_id, name, normalized_name, mana_cost, type_line,
    set_code, set_name, collector_number, image_small, image_normal
from cards
where (lower(set_code), lower(collector_number)) in (
    select lower(s), lower(c)
    from unnest(sqlc.arg(set_codes)::text[], sqlc.arg(collector_numbers)::text[]) as t(s, c)
)
order by lower(set_code), lower(collector_number), promo, released_at desc, (image_small is null);

-- Every printing of a name within one set: one row → matched, several →
-- ambiguous (e.g. the four Antiquities Urza's Mine variants).
-- name: GetCardsByNameAndSet :many
select scryfall_id, oracle_id, name, normalized_name, mana_cost, type_line,
    set_code, set_name, collector_number, image_small, image_normal
from cards
where (normalized_name, lower(set_code)) in (
    select n, lower(s)
    from unnest(sqlc.arg(names)::text[], sqlc.arg(set_codes)::text[]) as t(n, s)
)
order by normalized_name, lower(set_code), collector_number;
```

Run `cd backend && sqlc generate` and check the generated param struct
names before using them.

Also confirm `GetPrintingsByOracleID` (`queries/cards.sql:134-137`) is
reachable from this service — it is reused unchanged for
`printing-not-found` suggestions.

- [ ] **Step 4: Implement the ladder in `ResolveList`**

Add the status and the echo fields:

```go
const (
	StatusMatched   = "matched"
	StatusAmbiguous = "ambiguous"
	StatusUnmatched = "unmatched"
	// StatusPrintingNotFound: the name is real but the requested printing
	// is not. Distinct from unmatched because the fix is choosing another
	// printing, not correcting a typo.
	StatusPrintingNotFound = "printing-not-found"
)
```

```go
type ResolvedLine struct {
	LineNumber      int32
	Raw             string
	Quantity        int32
	Status          string
	SetCode         string // echo of what was parsed, "" if absent
	CollectorNumber string
	Match           *CardRef
	Suggestions     []CardRef
}
```

Then, before the existing per-line loop, run the two extra batches over
the lines that carry selectors, keyed for lookup:

```go
	// Three constant-count batches, not one query per line.
	exactByPrinting := map[string]CardRef{}        // "set|cn"
	bySetAndName := map[string][]CardRef{}         // "normname|set"
```

and in the loop, ahead of the existing `default:` name branch:

```go
		switch {
		case !l.OK:
			rl.Status = StatusUnmatched
		case l.SetCode != "" && l.CollectorNumber != "":
			if ref, ok := exactByPrinting[l.SetCode+"|"+l.CollectorNumber]; ok {
				rl.Status, rl.Match = StatusMatched, &ref
				break
			}
			rl.Status = StatusPrintingNotFound
			rl.Suggestions, err = s.printingsForName(ctx, l.Name)
			if err != nil {
				return nil, err
			}
		case l.SetCode != "":
			refs := bySetAndName[NormalizeName(l.Name)+"|"+l.SetCode]
			switch len(refs) {
			case 1:
				rl.Status, rl.Match = StatusMatched, &refs[0]
			case 0:
				rl.Status = StatusPrintingNotFound
				rl.Suggestions, err = s.printingsForName(ctx, l.Name)
				if err != nil {
					return nil, err
				}
			default:
				rl.Status, rl.Suggestions = StatusAmbiguous, refs
			}
		default:
			// …existing name-only branch, unchanged…
		}
```

`printingsForName` looks the name up in the already-batched `exact` map
to get an oracle id, then calls `GetPrintingsByOracleID`. Only misses pay
for it, matching how `suggest` is already used.

**The belt-and-braces guard**, applied after the switch: a line whose
set-aware reading resolved to nothing gets retried with the entire raw
line as a bare name, so no pathological card name becomes unimportable.

```go
		// A card name may itself end in something set-code-shaped. If the
		// set-aware reading found nothing, retry the whole line as a name
		// before giving up.
		if (rl.Status == StatusPrintingNotFound || rl.Status == StatusUnmatched) &&
			l.SetCode != "" {
			if retry := exact[NormalizeName(NameWithoutQuantity(l.Raw))]; len(retry) == 1 {
				rl.Status, rl.Match = StatusMatched, &retry[0]
				rl.SetCode, rl.CollectorNumber = "", ""
				rl.Suggestions = nil
			}
		}
```

For that retry to hit, the **whole raw line** must already be in the
batched name set — the retry must not issue its own query. So in the
name-collection pass, for any line carrying a set selector, also collect
`NormalizeName(NameWithoutQuantity(l.Raw))` alongside
`NormalizeName(l.Name)`, and key the retry lookup the same way:

```go
			if retry := exact[NormalizeName(NameWithoutQuantity(l.Raw))]; len(retry) == 1 {
```

`NameWithoutQuantity` comes from Task 24; using it in both places keeps
the quantity-stripping rule in exactly one function.

- [ ] **Step 5: Extend the wire type and status enum**

In `internal/platform/httpapi/cards.go`, add `setCode` and
`collectorNumber` to the resolved-line schema and add
`printing-not-found` to its status `enum:` tag. Keep both new fields
required (empty string when absent) so the client can always read them.

- [ ] **Step 6: Run the tests, regenerate, run everything**

Run: `cd backend && go test ./internal/cards/ -v && cd .. && make test && make api-generate`
Expected: PASS.

- [ ] **Step 7: Show the parsed selectors in the review dialog**

In `frontend/src/shared/cards/CardListImportDialog.tsx`, render a
`printing-not-found` group between `ambiguous` and `unmatched`, reusing
the ambiguous group's `<select>` (the suggestions are the alternative
printings). Show the parsed selector on the line so the user can see what
was understood, e.g. `Urza's Mine (ATQ) 83a`.

Add the message keys to **both** message files:

```json
  "collection_import_group_printing_not_found": "Printing not found — pick another",
```

```json
  "collection_import_group_printing_not_found": "Nie znaleziono wydania — wybierz inne",
```

Also update `frontend/src/shared/cards/listImportReview.ts` so
`defaultChoices` pre-selects the first suggestion for
`printing-not-found` lines (matching how it treats `ambiguous`), and add
a unit test for that.

- [ ] **Step 8: Run the frontend suite and verify end to end**

Run: `cd frontend && pnpm vitest run`
Expected: PASS.

Then `make up` and paste into a cube:

```
1 Urza's Mine (ATQ) 83a
1 Urza's Mine (ATQ)
1 Lightning Bolt (LEB)
1 Lightning Bolt
1 B.F.M. (Big Furry Monster)
1 Lightning Bolt (XYZ) 999
```

Expected: line 1 matched to 83a; line 2 ambiguous with four variants;
line 3 matched to the LEB printing; line 4 matched to the representative
printing; line 5 matched despite the parentheses; line 6
printing-not-found with the real printings offered. Commit them and
confirm the cube rows carry the chosen printings — no printing-picker
pass needed. Finally, export a wantlist with sets (PR 10) and paste it
back in to confirm the round-trip.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/ frontend/src/shared/ frontend/src/shared/api/ \
  frontend/messages/en.json frontend/messages/pl.json
git commit -m "feat(cards): resolve import lines by set and exact printing"
```

---

## Wrap-up

After all 11 PRs are merged:

- [ ] **Run the full suite on `master`**

```bash
git checkout master && git pull
make test
cd frontend && pnpm vitest run && pnpm lint && pnpm typecheck
```

- [ ] **Update `docs/architecture/structure.md`** if any convention
  shifted in practice. Two candidates from this batch: overlays now lock
  body scroll via `shared/lib/useScrollLock` (worth a line in the
  responsive section, since the reason is iOS Safari), and
  `shared/ui/confirm-dialog.tsx` is now the way to confirm a mutation
  (worth a line in the a11y section beside the existing "defer closing
  until it settles" rule).

- [ ] **Verify in production** after deploy: create a free event and
  remove a participant; check a started event's tournament section does
  not flicker; import a list with printing selectors into a cube.
