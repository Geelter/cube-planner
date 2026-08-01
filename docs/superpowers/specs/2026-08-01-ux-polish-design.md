# UX Polish: Logout Redirect, Form Pending Feedback, Auth-Gated UI — Design

**Date:** 2026-08-01
**Status:** Approved

Four small frontend improvements that tighten the logged-out experience and
make in-flight requests visible. Frontend-only; no backend or API changes.

## 1. Logout redirects to `/login`

**Problem:** `useLogout` (`frontend/src/features/auth/api.ts`) only
invalidates the `me` query. The user stays on the current route after
logging out, which is awkward on user-scoped screens (e.g. collection
edit).

**Design:** Centralize the redirect in the hook — both call sites (desktop
header and mobile drawer in `routes/__root.tsx`) share `useLogout`.

- `useLogout` gains `useNavigate()`; `onSuccess` becomes: invalidate the
  **entire** query cache (`qc.invalidateQueries()` with no filter, so stale
  user-scoped data like collection and my-cubes doesn't linger), then
  `navigate({ to: "/login" })`.
- No call-site changes needed.

## 2. Pending-request feedback in forms

**Problem:** 7 of 10 form submit buttons disable on `isPending` but show no
visual change beyond opacity; 3 auth pages (Register, Forgot Password,
Reset Password) give no feedback at all. Requests in flight feel like
nothing is happening.

**Design:** Follow the shadcn spinner-in-button pattern
(https://ui.shadcn.com/docs/components/base/button#spinner), adapted to
this codebase's conventions.

- **Add shadcn's `Spinner` component** via
  `pnpm dlx shadcn@latest add spinner` (per the structure.md workflow:
  `components.json` already targets `@/shared/ui`; afterwards run
  `pnpm fmt`, fix strict-TS complaints, remap any stock color variables).
  One repo-convention adaptation: replace the hardcoded
  `aria-label="Loading"` with a Paraglide message (en + pl).
- **`Button` gains `loading?: boolean`:** when true the button is
  `disabled`, gets `aria-busy`, and renders `<Spinner />` before its
  children. Ignored when `asChild` is set (links can't be pending).
  Existing `gap-2` in `buttonVariants` handles spacing.
- **Port shadcn's svg-sizing classes to `buttonVariants`:** the base class
  string gains `[&_svg]:pointer-events-none [&_svg]:shrink-0
  [&_svg:not([class*='size-'])]:size-4` (present in upstream shadcn button,
  dropped when the component was trimmed) so icons inside buttons size
  and behave consistently.
- **Apply `loading={mutation.isPending}`** to every form submit button,
  replacing bare `disabled={isPending}` where present and adding coverage
  where missing:
  - auth: LoginPage, RegisterPage, ForgotPasswordPage, ResetPasswordPage
  - cubes: CreateCubePage, CubeSettingsSection
  - events: EventForm (serves new + edit)
  - collection: ImportDialog
  - tournaments: TournamentPanel, ResultForm

Non-form mutation buttons (logout, registration actions, etc.) are out of
scope; the `loading` prop makes adding them later trivial.

## 3. Hide "New cube" on `/cubes` when logged out

**Problem:** `CubeBrowserPage` renders the "New cube" button
unconditionally; only logged-in users can create cubes.

**Design:** `CubeBrowserPage` calls `useMe()` and renders the button only
when `me.data` is truthy — the same pattern `routes/__root.tsx` and
`TournamentPanel` already use.

## 4. Auth route guards

**Problem:** No route has a guard; a logged-out user can open `/cubes/new`
(or any user-scoped route) directly and only fails at submit with a 401.

**Design:** TanStack Router `beforeLoad` guards backed by the React Query
cache.

- **Router context:** `app/main.tsx` passes
  `context: { queryClient }` to `createRouter`; `routes/__root.tsx`
  switches to `createRootRouteWithContext<{ queryClient: QueryClient }>()`.
- **Shared query options:** extract `meQueryOptions` (the `queryOptions`
  helper: key `["me"]`, existing 401→null queryFn, `retry: false`) in
  `features/auth/api.ts`; `useMe` consumes it, so hook and guard share one
  cache entry.
- **Guard helper** exported from `features/auth` (e.g. `requireAuth`):

  ```ts
  async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQueryOptions);
    if (!me) throw redirect({ to: "/login" });
  }
  ```

- **Guarded routes:** `/account`, `/collection`, `/cubes/mine`,
  `/cubes/new`, `/cubes/$cubeId/edit`, `/cubes/$cubeId/wantlist`,
  `/events/new`, `/events/$eventId/manage`. Each route file adds
  `beforeLoad: requireAuth`.
- Ownership/role checks (cube edit, event manage) stay server-side —
  guards only require *a* session.
- No return-to-origin (`redirect` search param) — YAGNI for now; login
  already navigates home on success.
- Note: `ensureQueryData` serves cached data. A session that expires
  server-side mid-visit still passes the guard until the cache updates —
  acceptable; the API's 401s remain the source of truth.

## Error handling

- Logout failure: mutation error leaves the user where they are (current
  behavior); no new handling.
- Guard redirect throws are TanStack Router's normal control flow.

## Testing

- `button.test.tsx`: `loading` renders spinner + `aria-busy` + disabled;
  no spinner when false.
- New `spinner` coverage via the button tests (no separate file needed).
- `CubeBrowserPage.test.tsx`: mock `useMe` (as tournament tests do);
  button present when logged in, absent when logged out.
- `features/auth/api.test.tsx`: logout invalidates cache and navigates to
  `/login` (mock `useNavigate`).
- Route guard: unit-test `requireAuth` with a real `QueryClient` and a
  mocked fetch — redirects when `me` resolves null, passes when a user is
  returned.
- Existing form tests keep passing (buttons stay `type="submit"`, label
  text unchanged).
