# Directory Structure & Code Organization

Source of truth for where code lives and what may depend on what. Reviews
enforce this document. Change the rules here first, then the code.

## Monorepo

```
cube_planner/
├── Makefile          # primary task runner — `make help`
├── compose.yml       # LOCAL-ONLY infra (Postgres + Mailpit)
├── backend/          # Go module
├── frontend/         # Vite + React SPA (pnpm workspace pkg)
├── deploy/           # production compose + Caddyfile
├── docs/             # specs, plans, architecture docs
└── .github/workflows # ci.yml, deploy.yml
```

## Backend (`backend/`)

Modular monolith. One package per bounded context under `internal/`
(`auth`, and per master design later: `cards`, `cubes`, `collections`,
`events`). `internal/platform/` holds shared infrastructure (db, config,
mail, httpapi) and contains **no business logic**.

- `cmd/server/main.go` is the sole composition root: config → db →
  services → router wiring happens there and nowhere else.
- Domain packages never import each other. Cross-domain needs are
  expressed as small interfaces, satisfied in `main.go`.
- Migrations: goose SQL files embedded in the binary, applied on startup.
  Queries: sqlc. Handlers: huma on chi.
- Tests: table-driven unit tests for pure logic; testcontainers
  (real Postgres) integration tests for handlers/repos.

## Frontend (`frontend/src/`)

Feature-sliced. Two axes that must not blur: `shared/` is domain-blind,
`features/<feature>/` are vertical slices.

```
src/
├── app/                  # bootstrap: main.tsx, styles.css, app-level tests
├── shared/               # domain-blind, feature-blind
│   ├── ui/               # primitives (button, input, card, …) — cva + cn
│   ├── api/              # GENERATED OpenAPI client + fetch wrapper (tracked)
│   ├── cards/            # promoted domain widgets (CardAutocomplete)
│   ├── i18n/             # locale UI glue (LanguageSwitcher)
│   └── lib/              # generic utilities (cn, theme)
├── features/<feature>/   # vertical slices: components/, api.ts, hooks
├── routes/               # TanStack file routes — THIN: compose feature components
├── paraglide/            # GENERATED messages + runtime (gitignored)
└── routeTree.gen.ts      # GENERATED route tree (gitignored)
```

### The rules

1. **Dependency direction:** `app`/`routes` → `features` → `shared`.
   Never feature → feature. Never shared → feature. If two features need
   the same thing, it moves down into `shared/` (promote, don't copy).
   Promoted domain widgets live in `shared/<domain>/` (e.g.
   `shared/cards/CardAutocomplete`): `shared/` stays *feature*-blind, but
   — like the generated `shared/api/` — not necessarily domain-blind.
2. **Routes are thin.** A route file declares path config
   (`validateSearch`, etc.) and points `component` at a feature component.
   No markup or logic in route files. Components read search params via
   `getRouteApi("/path")`, keeping them out of route files.
   User-scoped routes additionally declare `beforeLoad: requireAuth`
   (from `@/features/auth/guard`) so logged-out visitors are redirected
   to `/login`; ownership/role checks stay server-side. Tests that build
   a router from the real `routeTree.gen` must pass
   `context: { queryClient }` to `createRouter` (typecheck enforces it),
   and tests rendering a guarded route must serve `/api/me` a 200 — an
   unhandled 401 makes the guard silently render `/login` instead of the
   page under test.
3. **Variants are props, implemented with cva.** `ButtonPrimary.tsx` or
   `Button2.tsx` are defects. One file per component; variants are typed
   cva config. All conditional/merged classes go through `cn()`
   (`@/shared/lib/cn` = clsx + tailwind-merge) so consumer `className`
   overrides win.
4. **Semantic tokens only.** Components use `bg-surface`,
   `bg-surface-raised`, `text-fg`, `text-fg-muted`, `border-border`,
   `bg-accent`, `text-accent-fg`, `bg-danger`, `text-danger-fg`. Raw
   palette utilities (`bg-zinc-800`) are allowed only inside
   `src/app/styles.css`, where themes map primitives to roles. The `dark:`
   variant is a rare escape hatch, not the theming mechanism.
5. **No hardcoded user-facing strings.** Every display string is a typed
   message call: `m.some_key()` from `@/paraglide/messages`. English
   (`messages/en.json`) is the source of truth; `pl.json` must carry the
   same keys (the Paraglide compiler enforces parity). Exception: backend
   RFC 7807 `detail` strings are shown verbatim.
6. **Accessibility conventions.** Every input has a `<Label htmlFor>` with
   a matching `id`. Errors render in `<Alert>` (`role="alert"`); when an
   error belongs to one field, associate it via `aria-describedby`. Route
   changes focus `<main>` (handled by the shell). `<html lang>` follows
   the active locale. Screen-level components get a vitest-axe smoke test
   (with `// @vitest-environment jsdom` — axe needs jsdom). Buttons that
   trigger a network request show it in flight via
   `<Button loading>` (disabled + spinner + `aria-busy`) rather than a
   bare `disabled`, in one of three shapes:
   `loading={mutation.isPending}` for single-action buttons and form
   submits; row-scoped for one-mutation-many-rows lists — only the
   clicked row spins, derived from the in-flight variables
   (`const pendingId = mut.isPending ? mut.variables.<key> : null`, then
   `loading={pendingId === row.<key>}`) while other rows stay enabled;
   and `loading={query.isFetching && query.isPlaceholderData}` for
   pagination over `keepPreviousData` queries. Buttons that only change
   local state (filters, opening dialogs) get no loading prop. Dialogs
   that confirm a mutation defer closing until it settles
   (`mutate(vars, { onSettled: () => close() })`) so the spinner stays
   visible instead of the dialog vanishing mid-request. Multi-panel tab
   UIs (e.g. tournament rounds) implement the full APG tabs pattern:
   `role="tablist"`/`role="tab"`/`role="tabpanel"`, `aria-selected`,
   `aria-controls`/`aria-labelledby` (via `useId`), roving `tabIndex`
   (`0` on the selected tab, `-1` on the rest), and Arrow/Home/End key
   handling that moves both focus and selection.
7. **Test placement.** Tests sit next to what they test. Inside
   `src/routes/` a test file needs a `-` filename prefix so route
   generation skips it; elsewhere use plain `*.test.ts(x)`.
8. **Generated artifacts.**

   | Artifact | Tracked? | Regenerate |
   |---|---|---|
   | `src/shared/api/` (client) | yes — CI checks freshness | `make api-generate` |
   | `src/routeTree.gen.ts` | no (gitignored) | `pnpm gen` / any vite run |
   | `src/paraglide/` | no (gitignored) | `pnpm gen` / any vite run |

   Never hand-edit generated files; oxlint/oxfmt ignore them.
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
     targets adjacent without a gap. Icon-only affordances (dialog close,
     theme toggle) use `size="icon"` at `size-11`; when the visual icon
     must stay small, pull the hit area out with a negative margin
     (`-m-2`) rather than growing the icon itself.
   - **Use the established patterns.** Primary nav = header links on
     `md:`+ and a fixed bottom tab bar below `md:`
     (`<nav aria-label={m.nav_primary()}>`, Cards/Cubes/Events/Collection,
     `data-status="active"` styling from TanStack Router, safe-area
     padding); `<main>` reserves `pb-24 md:pb-8` for it, and floating
     bars like `PendingChangesBar` stack above it
     (`bottom-[calc(3.5rem+env(safe-area-inset-bottom))] md:bottom-0`).
     Secondary nav (My Cubes, account, logout, log in, language switcher)
     = the header drawer (`shared/ui/drawer.tsx`). Wide tables =
     `overflow-x-auto` wrapper; dialogs = `shared/ui/dialog.tsx` (handles
     mobile sizing); forms = single-column `max-w-md`. Inputs use ≥16px
     font on mobile (`text-base sm:text-sm`) so iOS Safari does not zoom
     on focus.

### Adding shadcn/ui components

`frontend/components.json` targets `@/shared/ui`. After
`pnpm dlx shadcn@latest add <component>`: reformat with `pnpm fmt`, fix
strict-TS complaints, and remap shadcn's stock color variables to our
semantic tokens:

| shadcn variable | our token |
|---|---|
| background | surface |
| card / popover | surface-raised |
| foreground / card-foreground | fg |
| muted-foreground | fg-muted |
| border / input | border |
| primary | accent |
| primary-foreground | accent-fg |
| destructive | danger |
| ring | accent |

Copied components are ours: they are reviewed and maintained like
hand-written code.
