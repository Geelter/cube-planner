# Events, Registration & Payments — Sub-project 5a Design

**Date:** 2026-07-13
**Status:** Approved. Implements the first half of master design §7.5
(`2026-07-10-cube-planner-master-design.md`). The tournament engine
(swiss pairings, result entry, standings) is split into sub-project 5b
with its own spec.

## 1. Scope

Event CRUD + organizer panel, publishing lifecycle, paid registration
via Stripe Checkout, webhook handling, capacity enforcement, waitlist
with automatic promotion, self-cancellation with a refund policy, and
transactional emails.

**Decisions made during brainstorming:**

| Decision | Choice |
|---|---|
| Split | 5a = events + registration + payments (this spec); 5b = swiss pairings, results, standings (separate spec/plan/branch) |
| Organizer | `role` column on `users` (`user` \| `admin`), flipped via SQL once for the owner; organizer endpoints require `admin` |
| Stripe in dev/tests | Real Stripe **test mode**; `stripe listen` forwards webhooks to localhost; integration tests use recorded webhook fixtures with real signature verification |
| Payment window | 24 h for `pending_payment` (registration and waitlist promotion alike) |
| Refund policy | Self-cancel with automatic refund until the per-event `refund_deadline`; after it, self-cancel still frees the spot immediately but the refund goes to an organizer decision queue (refund / deny). Incentive is "I can lose my money", not "I can't cancel" |
| Emails | Waitlist promotion, payment confirmed, refund issued, refund denied (+ event cancelled) — a clear signal for every money/state change the user can't be expected to notice in-app |
| Free events | `fee_cents = 0` allowed: registration confirms instantly, no Stripe, no 24 h window; waitlist promotion also confirms instantly |
| Architecture | Approach 1: `registrations` table is the single source of truth; 1-minute in-process sweeper (same pattern as the Scryfall scheduler) expires and promotes; Checkout session created on pay-click; webhooks idempotent via a `stripe_events` table. Rejected: fully webhook-driven expiry (correctness would hinge on Stripe delivering `checkout.session.expired`, and free events need our own promotion logic anyway); event-sourced registration ledger (fights transactional capacity enforcement, overkill at this scale) |

**Non-goals (5b or later):** rounds, matches, pairings, result entry,
standings, drops mid-event; multi-organizer payouts / Stripe Connect
(master design explicitly excludes them); partial refunds; registration
transfer between users.

## 2. Data model

Migration `backend/migrations/00006_events.sql` (+ goose down).

**`users`** — add `role text not null default 'user'`
(CHECK `role in ('user','admin')`). `/api/me` starts returning it so
the frontend can gate organizer UI.

**`events`**
- `id uuid pk default gen_random_uuid()`
- `organizer_id uuid not null` FK → `users`
- `name text not null`, `description text not null default ''`,
  `location text not null default ''`
- `starts_at timestamptz not null`
- `fee_cents int not null` CHECK (`fee_cents >= 0`) — 0 = free event
- `currency text not null default 'pln'`
- `max_participants int not null` CHECK (`> 0`)
- `refund_deadline timestamptz` — nullable; null = auto-refund until
  the event starts
- `status text not null default 'draft'` CHECK in
  (`draft`, `published`, `started`, `finished`, `cancelled`)
- `waitlist_counter bigint not null default 0` — per-event monotonic
  source for `registrations.waitlist_pos`
- `created_at`, `updated_at timestamptz`

**`event_cubes`**
- `event_id uuid` FK → `events` ON DELETE CASCADE
- `cube_id uuid` FK → `cubes`
- `cube_change_id uuid` FK → `cube_changes`, nullable — pins the cube
  "as of this point in history" (master design §4); null = live cube
- PK `(event_id, cube_id)`
- Only public cubes or cubes owned by the organizer can be attached
  (service-level check); a pinned change must belong to that cube.

**`registrations`**
- `id uuid pk default gen_random_uuid()`
- `event_id uuid` FK → `events`, `user_id uuid` FK → `users`
- `status text not null` CHECK in (`pending_payment`, `paid`,
  `waitlisted`, `cancelled`, `refund_requested`, `refunded`, `expired`)
- `expires_at timestamptz` — set only while `pending_payment`
  (now + 24 h), null otherwise
- `waitlist_pos bigint` — set only while `waitlisted`; taken from
  `events.waitlist_counter` incremented under the event row lock, so
  promotion order is stable and positions are never reused
- `stripe_checkout_session_id text`,
  `stripe_session_expires_at timestamptz` (lets the sweeper skip
  registrations with a live checkout session, §3),
  `stripe_payment_intent_id text` (needed to issue refunds),
  `paid_at timestamptz`
- `created_at`, `updated_at timestamptz`
- **Partial unique index** on `(event_id, user_id)` WHERE
  `status in ('pending_payment','paid','waitlisted','refund_requested')`
  — one active registration per user per event; re-registering after
  cancel/expire/refund creates a new row (full audit trail preserved).
  `refund_requested` counts as "active" only for uniqueness (the user
  shouldn't re-register while money is in limbo), **not** for capacity.
- Indexes: `(event_id, status)`; `(user_id)`;
  unique `(stripe_checkout_session_id)` where not null (webhook lookup).

**`stripe_events`**
- `stripe_event_id text pk`, `type text not null`,
  `received_at timestamptz not null default now()`
- Webhook processing = `INSERT … ON CONFLICT DO NOTHING`; zero rows
  inserted → duplicate delivery → skip. Handler work happens in the
  same transaction as the insert.

**Capacity invariant:** an event is full when
`count(registrations where status in ('pending_payment','paid')) >=
max_participants`. Every path that consumes or frees a spot
(register, expire, promote, cancel, refund, webhook-completed) runs in
a transaction that first locks the event row (`SELECT … FOR UPDATE`),
making check-then-act safe under concurrency.

## 3. Registration state machine

States: `pending_payment`, `paid`, `waitlisted` (active) —
`cancelled`, `refunded`, `expired` (terminal) — `refund_requested`
(money-limbo: terminal for capacity, active for uniqueness).

**Register** (`POST /events/{id}/register`; event must be `published`;
any logged-in user; not already actively registered):
- Spot free, `fee_cents > 0` → `pending_payment`, `expires_at = now + 24 h`.
- Spot free, `fee_cents = 0` → `paid` immediately (`paid_at = now`),
  no Stripe involvement.
- Event full → `waitlisted` with the next `waitlist_pos`.

**Pay** (`POST /events/{id}/registration/pay`; own registration in
`pending_payment`): creates a Stripe Checkout session on demand and
returns its URL. If a live (non-expired) session already exists for
the registration, its URL is returned instead of creating another.
Session expiry is clamped to the registration's remaining window,
respecting Stripe's 30-minute minimum: if less than 30 min remain, the
session gets 30 min and the sweeper won't expire a registration whose
`stripe_session_expires_at` is still in the future (prevents paying
into a dead registration in the common case).

**Webhook `checkout.session.completed`** → registration `paid`,
`paid_at` set, `stripe_payment_intent_id` recorded, confirmation email.
Edge case — payment lands after the registration expired: re-claim a
spot if one is free (back to `paid`); otherwise refund the payment
immediately via the API and email the user an apology-with-refund.

**Webhook `checkout.session.expired`** → if the registration is still
`pending_payment` and has no newer session, treat as early expiry:
`expired` + promote. (The sweeper would catch it anyway; this is just
faster.)

**Webhook `charge.refunded`** → confirms refund state; if the
registration is not yet `refunded` (e.g. refund issued from the Stripe
dashboard manually), transition it and send the refund email.

**Sweeper** (`internal/events/scheduler.go`, 1-minute ticker in the
server process, same lifecycle pattern as `internal/cards/scheduler.go`;
clock injected for tests): per event with overdue registrations —
lock event row, flip overdue `pending_payment` → `expired`, then
**promote**: while capacity is free and the waitlist is non-empty, take
the lowest `waitlist_pos`: `fee_cents > 0` → `pending_payment`
(+24 h) + promotion email ("a spot opened, pay by …", link to the
event page); `fee_cents = 0` → `paid` + confirmation email. Promote is
one reusable service function; every spot-freeing path calls it in the
same transaction.

**Self-cancel** (`DELETE /events/{id}/registration`; own active
registration; event not `started`/`finished`/`cancelled`):
- `waitlisted` or `pending_payment` → `cancelled` (no money moved).
- `paid`, now ≤ refund window (before `refund_deadline`, or before
  `starts_at` when the deadline is null) → issue Stripe refund (full)
  → `refunded` + refund email + promote.
- `paid`, past the window → `refund_requested`: spot freed + promote
  immediately; the row enters the organizer's refund queue.

**Organizer resolution:**
- `POST …/registrations/{rid}/refund` — allowed on `refund_requested`
  (queue resolution) and on `paid` (organizer kick-with-refund; also
  frees the spot + promotes) → Stripe refund → `refunded` + email.
- `POST …/registrations/{rid}/deny-refund` — `refund_requested` →
  `cancelled` + refund-denied email.

**Event lifecycle** (organizer): `draft → published → started →
finished`; `cancelled` reachable from `published`/`started`.
- `publish` — event becomes visible and registrable.
- `start` — registration closes; remaining `pending_payment` →
  `expired`, `waitlisted` → `cancelled` (no promotions after start).
  Paid roster is now the 5b tournament roster.
- `finish` — display-only state change.
- `cancel` — every `paid` and open `refund_requested` gets an
  automatic full refund (`refunded`), all other active rows →
  `cancelled`; event-cancelled email to everyone affected. Stripe
  refund calls run per-registration; a failed call leaves that row
  untouched and is retried by re-invoking cancel (idempotent —
  already-`refunded` rows are skipped).
- Draft events are fully editable; after publish only `description`,
  `location`, and `refund_deadline` may change (fee, capacity, date
  and cubes are locked — people paid under those terms). Capacity
  increase post-publish is future work.

## 4. Stripe integration

- Official `stripe-go` SDK. Config (`platform/config`):
  `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`; both empty in
  environments without payments — creating a **paid** event or paying
  then fails with a clear 503 problem type (`payments-unconfigured`);
  free events work without Stripe entirely.
- Checkout session: `mode=payment`, single line item (event name,
  `currency`, `fee_cents`), `metadata.registration_id` +
  `client_reference_id` = registration id, success/cancel URLs →
  event page (`/events/{id}?checkout=success|cancelled`).
- **Webhook** `POST /api/stripe/webhook` mounted at **chi level**
  (outside huma — it needs the raw body for signature verification and
  is authenticated by signature, not session). Consumed:
  `checkout.session.completed`, `checkout.session.expired`,
  `charge.refunded`. Everything else: acknowledged + ignored. 200 only
  after the transaction commits; errors → 5xx so Stripe retries.
  Idempotency via `stripe_events` (§2).
- The webhook is the single source of truth for "paid" (master design
  §5); the redirect back shows only an optimistic "confirming…" state.
- **Dev:** `make stripe-listen` wraps
  `stripe listen --forward-to localhost:<port>/api/stripe/webhook`;
  README/CLAUDE.md document grabbing the printed webhook secret into
  `.env`. Test-mode keys from the owner's Stripe account.
- **Tests:** recorded webhook payload fixtures signed at test time
  with a known secret (the `webhook.ConstructEvent` path runs for
  real); refund/session API calls go through a thin `stripeClient`
  interface faked in integration tests (the sweeper and state machine
  never talk to real Stripe in CI).

## 5. API surface

huma handlers in `backend/internal/platform/httpapi/events.go`;
service in new package `backend/internal/events/` (mirrors
`internal/cubes`/`internal/collections`): `service.go`,
`scheduler.go` (sweeper), `stripe.go` (client interface + real impl),
`promote.go` if it earns its own file. sqlc queries in
`backend/internal/db/queries/events.sql`.

### User endpoints (session auth)

- `GET /events` → `{events: EventSummary[]}` — `published`/`started`/
  `finished` (+`cancelled` for transparency), upcoming first then past
  descending; includes fee, spots taken/total, caller's registration
  status if any.
- `GET /events/{id}` → `EventDetail` — full info, linked cubes (name +
  optional pinned change reference), paid-attendee display names,
  waitlist length, caller's registration (status, `expires_at`,
  waitlist position).
- `POST /events/{id}/register` → `RegistrationInfo`.
- `POST /events/{id}/registration/pay` → `{checkoutUrl}`.
- `DELETE /events/{id}/registration` → `RegistrationInfo` (the row is
  status-flipped, not deleted — the caller sees `cancelled`/`refunded`/
  `refund_requested` and the UI explains what that means).

Draft events are invisible to non-admins (404, no existence leak —
same convention as private cubes).

### Organizer endpoints (session auth + `admin` role, else 403 problem type `admin-required`)

- `POST /events` (created as `draft`), `PATCH /events/{id}` (field
  whitelist per lifecycle, §3), `GET /events/{id}` returns drafts too
  for admins.
- `POST /events/{id}/publish | start | finish | cancel` — legal
  transitions only, else 409 problem type `invalid-event-transition`.
- `PUT /events/{id}/cubes` `{cubes: [{cubeId, cubeChangeId?}]}` —
  replace-set semantics; validates visibility/ownership + pin
  belonging; locked after publish.
- `GET /events/{id}/registrations` → all rows, grouped by status:
  paid roster, pending (with expiry), waitlist in order, refund queue
  (`refund_requested`), history (terminal states).
- `POST /events/{id}/registrations/{rid}/refund`
- `POST /events/{id}/registrations/{rid}/deny-refund`

### Domain problem types

`already-registered`,
`registration-not-found`, `registration-not-payable`,
`event-registration-closed` (registering/cancelling on a
started/finished/cancelled event; drafts are a plain 404 — no existence
leak), `invalid-event-transition`, `event-locked` (PATCH touched a
field frozen post-publish), `event-cubes-locked`, `invalid-event-cube`,
`payments-unconfigured`, `admin-required`.

### Wire types

- `EventSummary`: id, name, startsAt, location, feeCents, currency,
  maxParticipants, paidCount, pendingCount, waitlistCount, status,
  myRegistrationStatus?
- `EventDetail`: summary fields + description, refundDeadline?,
  cubes: `EventCubeEntry[]` (cubeId, cubeName, cubeChangeId?),
  attendees: `{displayName}[]`, myRegistration?: `RegistrationInfo`
- `RegistrationInfo`: id, status, expiresAt?, waitlistPos?, paidAt?
- Organizer list rows add: user displayName + email, timestamps,
  stripe ids.

After the backend lands, `make api-generate` refreshes
`frontend/src/shared/api/` (CI enforces freshness).

## 6. Emails

Via the existing `platform/mail` `Mailer` (same plain-text pattern as
auth emails). Users have no stored locale, so each email is **bilingual
in one body: Polish first, then English** (decided during planning —
auth emails stay English-only as-is):

| Email | Trigger |
|---|---|
| Waitlist promotion | promote → `pending_payment` ("a spot opened — pay by {deadline}", event link); free events get the confirmation email instead |
| Payment confirmed | `checkout.session.completed` (and instant free-event confirmation) |
| Refund issued | any path reaching `refunded` |
| Refund denied | organizer deny-refund |
| Event cancelled | organizer cancels the event (everyone active; notes refund where applicable) |
| Payment-after-expiry apology | the §3 edge case, only when the spot could not be re-claimed |

Email sending must not break state transitions: send after commit,
log failures (same convention as auth mail).

## 7. Frontend structure & UX

### Routes (thin, per structure.md)

- `/events` — public-ish list (requires login like the rest of the app)
- `/events/$eventId` — detail
- `/events/new`, `/events/$eventId/manage` — admin-gated: route guard
  reads `role` from the `/me` query; non-admins get the 404 component
  (no existence leak); nav link hidden for non-admins

### features/events/

`api.ts` — TanStack Query hooks over the generated client; mutations
invalidate the event detail + list queries. Registration state changes
(register/cancel) are mutate-and-invalidate; no optimistic updates
(money is involved — show server truth only). Plus `components/`,
`lib/`.

### Events list

Upcoming events (published/started) as cards: name, date, location,
fee (formatted from cents + currency), spots `taken/total` with a
"waitlist" badge when full, caller's status badge (registered / paid /
waitlisted #N / payment pending). Past (finished) and cancelled events
in a collapsed section below.

### Event detail

- Info block: date, location, fee, refund-deadline note ("free
  cancellation until …"), spots, organizer.
- Linked cubes: cube name linking to the cube display page; pinned
  cubes link into the history view at that change and are labeled
  "as of {date}".
- Attendee list (paid, display names) + waitlist count.
- **CTA driven by my registration state:**
  - none → **Register** (or **Join waitlist** when full — the button
    says what will actually happen)
  - `pending_payment` → **Pay now** + live countdown to `expires_at`
  - `waitlisted` → "Waitlisted — position N" + **Leave waitlist**
  - `paid` → "You're in" + **Cancel registration**; past the refund
    deadline the confirm dialog warns "you will only be refunded if
    the organizer approves it"
  - `refund_requested` → "Cancelled — refund pending organizer review"
- Returning from Stripe (`?checkout=success`): optimistic
  "confirming payment…" panel that polls the event detail query
  (2 s interval, ~60 s cap) until the registration flips to `paid`;
  `?checkout=cancelled` returns to the Pay-now state with a hint.

### Organizer panel (`/manage`)

- Create/edit form (draft: everything; published: description /
  location / refund deadline only — locked fields disabled with an
  explanatory hint). Fee entered in PLN, stored as cents.
- Cube linking: search-select from public + own cubes; optional pin
  picker listing the cube's changelog entries (reuses history data
  from sub-project 3).
- Lifecycle buttons with confirm dialogs; cancel's dialog states the
  total that will be refunded.
- Registrations table grouped: paid roster, pending payments (expiry
  countdown), waitlist (ordered), **refund queue** with per-row
  Refund / Deny buttons (confirm dialogs), history.

### Conventions

All strings via `m.*()` (en + pl), semantic color tokens, cva
variants, a11y per structure.md, axe smoke test per screen
(`// @vitest-environment jsdom`), route-file test prefix `-`.

## 8. Testing & error handling

**Backend**
- Table-driven unit tests for the state machine service functions with
  a faked `stripeClient` and injected clock: register (free spot /
  full / free event / duplicate), expiry + promotion chains (paid and
  free events, multiple frees promote multiple), self-cancel matrix
  (waitlisted / pending / paid-before-deadline / paid-after-deadline /
  null deadline), organizer refund + deny, event cancel mass-refund
  (including refund-call failure leaving the row retryable), start
  closes registration.
- testcontainers integration tests: endpoints incl. **concurrent
  registration on the last spot** (two parallel registers → one
  pending + one waitlisted), partial unique index behavior
  (re-register after cancel), role gating (403 non-admin, draft 404),
  lifecycle transition validation, webhook endpoint with signed
  fixtures (completed / expired / refunded / duplicate delivery
  no-ops / bad signature 400), pay-after-expiry reclaim + refund path.
- Sweeper: injected clock, ticker loop tested like
  `cards/scheduler_test.go`.

**Frontend**
- RTL: CTA renders correctly for every registration state; countdown
  formatting; confirm-dialog warning past the refund deadline;
  organizer refund queue actions (mocked mutations); checkout-return
  polling flips to paid.
- Unit: fee formatting (cents → localized PLN), lifecycle
  field-locking helper.
- axe smoke per screen.

**Error handling** (RFC 7807, master design §5): problem types from §5
map to toasts / inline CTA errors; 409 transition conflicts surface the
problem `detail`; 401 → existing auth redirect; draft/forbidden → 404
component.

## 9. Order of work

1. Migration 00006 + sqlc queries + `internal/events` service with
   faked Stripe (TDD on the state machine) + sweeper.
2. Stripe client (real impl) + webhook endpoint + signed fixtures +
   httpapi endpoints + integration tests.
3. `make api-generate` → committed client.
4. `features/events`: list → detail + registration CTA flow → checkout
   return polling → organizer panel (form, cubes, lifecycle,
   registrations table, refund queue).
5. Emails wired into transitions.
6. Manual acceptance in the browser with Stripe test mode +
   `make stripe-listen` (Mateusz), as in prior sub-projects.

## 10. Future work (logged, not in scope)

- Sub-project 5b: swiss pairings, result entry, standings, drops.
- Capacity increase after publish (would trigger waitlist promotion).
- Registration transfer between users; partial refunds.
- Organizer-configurable payment window (fixed 24 h for now).
- iCal export / reminders before the event.
