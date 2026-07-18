# Mana Symbol Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render `mana_cost` strings like `{2}{W}{W}` as real mana-symbol SVGs at all three call sites that currently show them as plain text.

**Architecture:** A shared `ManaCost` component in `frontend/src/shared/cards/` builds a token→URL map at build time with `import.meta.glob` over the committed SVGs in `frontend/src/shared/cards/mana/`, parses the cost string, and renders one `<img>` per known symbol. Unknown tokens and text outside braces fall back to literal text.

**Tech Stack:** React 19, Vite (`import.meta.glob` with `?url`), Tailwind v4, Vitest + React Testing Library on happy-dom.

**Spec:** `docs/superpowers/specs/2026-07-18-mana-symbols-design.md`

## Global Constraints

- `docs/architecture/structure.md` is binding: dependency direction `app`/`routes` → `features` → `shared` (never feature → feature), semantic color tokens only.
- No hardcoded user-facing strings — but the `aria-label` here is the raw cost string (card data), so no Paraglide message is needed.
- Tooling: oxlint + oxfmt (lefthook runs both on pre-commit; never eslint/prettier). Strict tsconfig — do not loosen.
- Frontend tests: vitest + RTL on happy-dom (the default; no `@vitest-environment` pragma needed here).
- `pnpm` needs Node 24: run `nvm use 24` first if `pnpm` is missing from PATH.
- All commands below run from the repo root.

---

### Task 1: `ManaCost` component

**Files:**
- Create: `frontend/src/shared/cards/ManaCost.tsx`
- Test: `frontend/src/shared/cards/ManaCost.test.tsx`

**Interfaces:**
- Consumes: the 55 SVG files already committed at `frontend/src/shared/cards/mana/*.svg`, named by token text uppercased with slashes stripped (`W.svg`, `2.svg`, `WU.svg`, `GWP.svg`, …).
- Produces: `export function ManaCost({ cost }: { cost: string }): ReactNode` — renders `null` for `""`; otherwise a `<span role="img" aria-label={cost}>` containing `<img alt="">` per known symbol and literal text for everything else. Task 2 imports it as `import { ManaCost } from "@/shared/cards/ManaCost"`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/shared/cards/ManaCost.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ManaCost } from "./ManaCost";

function symbolImgs(wrapper: HTMLElement) {
  return [...wrapper.querySelectorAll("img")];
}

describe("ManaCost", () => {
  it("renders one img per symbol, in order", () => {
    render(<ManaCost cost="{2}{W}{W}" />);
    const wrapper = screen.getByRole("img", { name: "{2}{W}{W}" });
    const imgs = symbolImgs(wrapper);
    expect(imgs).toHaveLength(3);
    expect(imgs[0]?.src).toContain("/2.svg");
    expect(imgs[1]?.src).toContain("/W.svg");
    expect(imgs[2]?.src).toContain("/W.svg");
  });

  it("resolves hybrid and phyrexian tokens by stripping slashes", () => {
    render(<ManaCost cost="{W/U}{G/W/P}" />);
    const imgs = symbolImgs(screen.getByRole("img", { name: "{W/U}{G/W/P}" }));
    expect(imgs[0]?.src).toContain("/WU.svg");
    expect(imgs[1]?.src).toContain("/GWP.svg");
  });

  it("falls back to literal text for unknown tokens", () => {
    render(<ManaCost cost="{Q}{W}" />);
    const wrapper = screen.getByRole("img", { name: "{Q}{W}" });
    expect(wrapper).toHaveTextContent("{Q}");
    expect(symbolImgs(wrapper)).toHaveLength(1);
  });

  it("preserves text outside braces (split cards)", () => {
    render(<ManaCost cost="{1}{W} // {2}{U}" />);
    const wrapper = screen.getByRole("img", { name: "{1}{W} // {2}{U}" });
    expect(wrapper).toHaveTextContent("//");
    expect(symbolImgs(wrapper)).toHaveLength(4);
  });

  it("renders nothing for an empty cost", () => {
    const { container } = render(<ManaCost cost="" />);
    expect(container).toBeEmptyDOMElement();
  });
});
```

Note: the wrapper is the only element matched by `getByRole("img")` — the inner `<img alt="">` elements have empty alt and are treated as presentational, which is exactly the intended screen-reader behavior.

- [ ] **Step 2: Run the test to verify it fails**

Run: `pnpm -C frontend test -- ManaCost`
Expected: FAIL — cannot resolve `./ManaCost`.

- [ ] **Step 3: Write the component**

Create `frontend/src/shared/cards/ManaCost.tsx`:

```tsx
const symbolUrls = import.meta.glob<string>("./mana/*.svg", {
  eager: true,
  query: "?url",
  import: "default",
});

type Part = { key: number; symbolUrl: string | null; text: string };

function parseCost(cost: string): Part[] {
  const parts: Part[] = [];
  let last = 0;
  for (const match of cost.matchAll(/\{([^}]+)\}/g)) {
    if (match.index > last) {
      parts.push({ key: parts.length, symbolUrl: null, text: cost.slice(last, match.index) });
    }
    const stem = (match[1] ?? "").toUpperCase().replaceAll("/", "");
    const url = symbolUrls[`./mana/${stem}.svg`];
    parts.push({ key: parts.length, symbolUrl: url ?? null, text: match[0] });
    last = match.index + match[0].length;
  }
  if (last < cost.length) {
    parts.push({ key: parts.length, symbolUrl: null, text: cost.slice(last) });
  }
  return parts;
}

export function ManaCost({ cost }: { cost: string }) {
  if (cost === "") {
    return null;
  }
  return (
    <span role="img" aria-label={cost}>
      {parseCost(cost).map((part) =>
        part.symbolUrl === null ? (
          <span key={part.key}>{part.text}</span>
        ) : (
          <img
            key={part.key}
            src={part.symbolUrl}
            alt=""
            className="inline-block size-[1em] align-[-0.15em]"
          />
        ),
      )}
    </span>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `pnpm -C frontend test -- ManaCost`
Expected: PASS, 5 tests.

- [ ] **Step 5: Typecheck and commit**

Run: `pnpm -C frontend typecheck`
Expected: exit 0.

```bash
git add frontend/src/shared/cards/ManaCost.tsx frontend/src/shared/cards/ManaCost.test.tsx
git commit -m "feat(cards): shared ManaCost component rendering symbol SVGs"
```

---

### Task 2: Wire `ManaCost` into the three call sites

**Files:**
- Modify: `frontend/src/shared/cards/CardAutocomplete.tsx:31-34`
- Modify: `frontend/src/features/cards/components/CardSearchPage.tsx:25-28`
- Modify: `frontend/src/features/cubes/components/GroupedCardList.tsx:39`

**Interfaces:**
- Consumes: `import { ManaCost } from "@/shared/cards/ManaCost"` from Task 1 (props: `{ cost: string }`; renders `null` for `""`).
- Produces: nothing new — user-visible change only.

No new tests: no existing test asserts on rendered mana-cost text (fixtures merely carry `manaCost` values), and the component's behavior is covered by Task 1. The full suite run below guards against regressions.

- [ ] **Step 1: CardAutocomplete option rows**

In `frontend/src/shared/cards/CardAutocomplete.tsx`, add the import:

```tsx
import { ManaCost } from "./ManaCost";
```

(Same-directory relative import — this file already imports `./api` that way.)

Replace

```tsx
            <span className="text-xs text-fg-muted">
              {c.typeLine}
              {c.manaCost !== "" && ` · ${c.manaCost}`}
            </span>
```

with

```tsx
            <span className="text-xs text-fg-muted">
              {c.typeLine}
              {c.manaCost !== "" && (
                <>
                  {" · "}
                  <ManaCost cost={c.manaCost} />
                </>
              )}
            </span>
```

- [ ] **Step 2: CardSearchPage details line**

In `frontend/src/features/cards/components/CardSearchPage.tsx`, add the import (with the other `@/shared/cards/` imports):

```tsx
import { ManaCost } from "@/shared/cards/ManaCost";
```

Replace

```tsx
        <p className="text-sm text-fg-muted">
          {latest.typeLine}
          {latest.manaCost !== "" && ` · ${latest.manaCost}`}
        </p>
```

with

```tsx
        <p className="text-sm text-fg-muted">
          {latest.typeLine}
          {latest.manaCost !== "" && (
            <>
              {" · "}
              <ManaCost cost={latest.manaCost} />
            </>
          )}
        </p>
```

- [ ] **Step 3: GroupedCardList rows**

In `frontend/src/features/cubes/components/GroupedCardList.tsx`, add the import:

```tsx
import { ManaCost } from "@/shared/cards/ManaCost";
```

Replace

```tsx
                    <span className="shrink-0 text-fg-muted">{card.manaCost}</span>
```

with

```tsx
                    <span className="shrink-0">
                      <ManaCost cost={card.manaCost} />
                    </span>
```

(`text-fg-muted` is dropped deliberately — the symbols carry their own colors; keep `shrink-0` so long names truncate instead of squeezing the cost.)

- [ ] **Step 4: Run the full frontend suite, typecheck, lint**

Run: `pnpm -C frontend test && pnpm -C frontend typecheck && pnpm -C frontend lint`
Expected: all pass, exit 0. If a call-site test fails on changed markup, fix the assertion to match the new structure (query by accessible name, e.g. `screen.getByRole("img", { name: "{R}" })`) — do not weaken the component.

- [ ] **Step 5: Verify visually**

Run `make up` (or use the already-running dev stack), open http://localhost:5173, and check:
- `/cards`: autocomplete rows and the selected-card details line show symbol icons sized to the text.
- A cube editor list: costs render as symbols, right-aligned, no layout jump.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/shared/cards/CardAutocomplete.tsx frontend/src/features/cards/components/CardSearchPage.tsx frontend/src/features/cubes/components/GroupedCardList.tsx
git commit -m "feat(cards): render mana costs as symbol SVGs at all call sites"
```
