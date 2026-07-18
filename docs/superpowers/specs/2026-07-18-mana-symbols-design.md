# Mana Symbol Rendering — Design

**Date:** 2026-07-18
**Issue:** #14 (item 1 of 3 — mana-symbol SVGs; printing picker and pricing links are out of scope here)
**Status:** Approved

## Goal

Render `mana_cost` strings such as `{2}{W}{W}` as real mana-symbol SVGs
instead of plain text, everywhere a mana cost is shown.

## Assets

User-provided SVGs live in `frontend/src/shared/cards/mana/` (committed).
One file per symbol, named after the token text uppercased with the slash
stripped: `W.svg`, `0.svg`–`20.svg`, `X.svg`, `Y.svg`, `C.svg`, `S.svg`,
`T.svg`, hybrids `WU.svg` … `RW.svg` (Scryfall canonical pair order),
mono-hybrids `2W.svg` …, phyrexians `WP.svg` … including the hybrid
four (`GUP.svg`, `GWP.svg`, `RGP.svg`, `RWP.svg`). Each SVG is
self-colored (fills baked in), viewBox `0 0 100 100`.

## Component

New shared component `frontend/src/shared/cards/ManaCost.tsx`:

```tsx
export function ManaCost({ cost }: { cost: string })
```

- **Symbol map:** built once at module level via
  `import.meta.glob("./mana/*.svg", { eager: true, query: "?url", import: "default" })`,
  keyed by filename stem. Vite emits hashed asset URLs; only symbols that
  actually appear on a page are fetched by the browser.
- **Parsing:** `cost.matchAll(/\{([^}]+)\}/g)`; each captured token is
  uppercased and has `/` removed, then looked up in the map.
- **Known token →** `<img src={url} alt="" className="inline-block size-[1em] align-[-0.15em]" />`
  so symbols track the surrounding font size at every call site.
- **Unknown token →** the raw `{token}` rendered as text — future or
  unexpected symbols degrade to exactly the pre-feature behavior.
- **Empty string →** renders nothing (`null`).

## Accessibility

Wrapper is `<span role="img" aria-label={cost}>`; all inner `<img>`
elements have `alt=""`. Screen readers announce the cost once as a
single unit rather than one announcement per symbol. The label is the
raw cost string — card data, not UI copy, so no Paraglide message.

## Call sites

Replace the plain-text cost in the three existing render sites:

1. `frontend/src/shared/cards/CardAutocomplete.tsx:33` — option rows.
2. `frontend/src/features/cards/components/CardSearchPage.tsx:27` —
   card details line; the ` · ` separator stays as text.
3. `frontend/src/features/cubes/components/GroupedCardList.tsx:39` —
   cube editor rows; drop `text-fg-muted` on the cost span (symbols
   carry their own colors).

No backend, API, or i18n changes.

## Testing

Vitest + RTL, `frontend/src/shared/cards/ManaCost.test.tsx`:

- Multi-token cost renders the right number of `<img>`s in order.
- Hybrid (`{W/U}`) and phyrexian (`{G/W/P}`) tokens resolve (slash
  stripped, correct file stem).
- Unknown token (e.g. `{Q}`) falls back to literal text.
- Empty string renders nothing.
- Wrapper exposes `role="img"` with `aria-label` equal to the raw cost.

Existing tests for the three call sites are updated where they assert on
text mana costs.
