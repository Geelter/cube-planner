# Events, Registration & Payments (Sub-project 5a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Organizer-run events with Stripe Checkout paid registration, capacity + waitlist with automatic promotion, a cutoff-based refund policy with an organizer refund queue, and bilingual transactional emails, per `docs/superpowers/specs/2026-07-13-events-registration-payments-design.md`.

**Architecture:** Migration 00006 adds `users.role`, `events`, `event_cubes`, `registrations`, `stripe_events`. New backend package `internal/events`: a state-machine `Service` (single source of truth = `registrations` rows; every spot-consuming/freeing path locks the event row), a `StripeClient` interface (real impl on stripe-go v86; fake in tests), a 1-minute sweeper (`RunSweeper`, same lifecycle as the cards scheduler), and post-commit bilingual emails. huma endpoints in `platform/httpapi/events.go`; the Stripe webhook is a raw chi route (signature-verified, idempotent via `stripe_events`). Frontend: new `features/events` slice with list/detail/organizer screens.

**Tech Stack:** Go + huma v2 + chi + sqlc (pgx/v5) + goose + stripe-go/v86; React + TanStack Router/Query + Tailwind v4 + Paraglide; vitest + RTL + vitest-axe; testcontainers-go.

## Global Constraints

- Go module path: `github.com/mjabloniec/cube-planner/backend`. Frontend alias `@/` = `frontend/src/`.
- Format/lint: `gofumpt -w` on touched Go files; `pnpm --filter @cube-planner/frontend fmt` (oxfmt) and `lint` (oxlint). Never eslint/prettier. lefthook runs these pre-commit — if a commit fails on formatting, run the formatter and retry the commit.
- Backend tests: `cd backend && go test ./...` (integration tests need Docker running). Frontend: `pnpm --filter @cube-planner/frontend test`, typecheck via `pnpm --filter @cube-planner/frontend typecheck`.
- Every user-facing string is `m.key()` from `@/paraglide/messages`; `frontend/messages/en.json` and `frontend/messages/pl.json` must carry identical key sets. After editing messages run `pnpm --filter @cube-planner/frontend gen` if tests complain about missing message functions.
- Semantic color tokens only (`bg-surface`, `bg-surface-raised`, `text-fg`, `text-fg-muted`, `border-border`, `bg-accent`, `text-danger`, …). No raw palette classes.
- huma v2.38 panics on pointer-typed **query** params — optional query params use value types. Pointer fields in JSON bodies are fine (the PATCH input uses them).
- Generated artifacts: `frontend/src/shared/api/` is regenerated ONLY via `make api-generate` and committed; `src/routeTree.gen.ts` + `src/paraglide/` are gitignored build output (`pnpm gen` recreates). `backend/internal/db/*.sql.go` regenerated via `cd backend && sqlc generate` and committed. Never hand-edit any of them.
- Registration statuses: `pending_payment | paid | waitlisted | cancelled | refund_requested | refunded | expired`. Capacity = count of `pending_payment` + `paid`. `refund_requested` occupies NO capacity but blocks re-registering (partial unique index).
- Event statuses: `draft → published → started → finished`, `cancelled` reachable from `published`/`started`.
- Payment window: 24 h (`PaymentWindow = 24 * time.Hour`). Stripe Checkout session expiry is clamped to the registration window but never below Stripe's 30-minute minimum.
- All money is integer cents (`fee_cents int`), currency lowercase ISO (`pln` default). Fee 0 = free event (no Stripe involvement anywhere).
- Emails are bilingual, one body: Polish paragraph first, then English. Sent AFTER the transaction commits; failures are logged, never fail the request.
- Stripe is test-mode in dev; `STRIPE_SECRET_KEY` + `STRIPE_WEBHOOK_SECRET` empty = payments unconfigured (creating/publishing a PAID event and paying return 503 `payments-unconfigured`; free events fully work).
- The webhook is the single source of truth for "paid" (master design §5). Webhook handlers are idempotent via `stripe_events` insert-or-skip.
- Lock ordering: always `GetEventForUpdate` FIRST, then registration rows. Every path that consumes or frees a spot runs inside such a transaction.
- Commit messages: conventional prefixes (`feat(events): …`), imperative mood, trailer:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Existing httpapi test helpers (package `httpapi_test`, shared across files): `newTestServer(t)`, `seedCard(t, pool, testCard{…})`, `loggedInClient(t, srv, q, email)`, `newCookieClient(t, srv)`, `c.do(t, method, path, body)`, `decode[T](t, resp)`, `getJSON(t, srv, path, out)`. `testdb.New(t)` gives a migrated testcontainers pool.
- No test files in `src/routes/` (they'd need a `-` prefix) — frontend tests live in `features/` or `shared/`.

---

### Task 1: Migration 00006 (role, events, event_cubes, registrations, stripe_events)

**Files:**
- Create: `backend/migrations/00006_events.sql`
- Generated: `cd backend && sqlc generate` (models pick up the new tables)

**Interfaces:**
- Produces: tables exactly as below — every later backend task builds on them. `users.role` (`'user' | 'admin'`).

- [ ] **Step 1: Write the migration** `backend/migrations/00006_events.sql`:

```sql
-- +goose Up
-- Single-organizer model (master design): the site owner flips their own
-- row to 'admin' via SQL once; organizer endpoints require it.
alter table users add column role text not null default 'user'
    check (role in ('user', 'admin'));

create table events (
    id uuid primary key default gen_random_uuid(),
    organizer_id uuid not null references users (id),
    name text not null check (char_length(name) between 1 and 200),
    description text not null default '',
    location text not null default '' check (char_length(location) <= 200),
    starts_at timestamptz not null,
    fee_cents int not null check (fee_cents >= 0),
    currency text not null default 'pln',
    max_participants int not null check (max_participants > 0),
    -- Self-cancel auto-refunds until this moment; null = until starts_at.
    refund_deadline timestamptz,
    status text not null default 'draft'
        check (status in ('draft', 'published', 'started', 'finished', 'cancelled')),
    -- Monotonic source for registrations.waitlist_pos, bumped under the
    -- event row lock; positions are never reused.
    waitlist_counter bigint not null default 0,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index events_status_starts_idx on events (status, starts_at);

create table event_cubes (
    event_id uuid not null references events (id) on delete cascade,
    cube_id uuid not null references cubes (id),
    -- Optional pin: "play the cube as of this change". Null = live cube.
    cube_change_id uuid references cube_changes (id),
    primary key (event_id, cube_id)
);

create table registrations (
    id uuid primary key default gen_random_uuid(),
    event_id uuid not null references events (id),
    user_id uuid not null references users (id),
    status text not null check (status in (
        'pending_payment', 'paid', 'waitlisted',
        'cancelled', 'refund_requested', 'refunded', 'expired')),
    -- Set only while pending_payment (now + 24h).
    expires_at timestamptz,
    -- Set only while waitlisted.
    waitlist_pos bigint,
    stripe_checkout_session_id text,
    stripe_checkout_session_url text,
    -- Lets the sweeper skip rows whose Checkout session is still live.
    stripe_session_expires_at timestamptz,
    -- Needed to issue refunds.
    stripe_payment_intent_id text,
    paid_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- One active registration per user per event. refund_requested blocks
-- re-registering (money in limbo) but occupies no capacity.
create unique index registrations_one_active_idx
    on registrations (event_id, user_id)
    where status in ('pending_payment', 'paid', 'waitlisted', 'refund_requested');
create index registrations_event_status_idx on registrations (event_id, status);
create index registrations_user_idx on registrations (user_id);
create unique index registrations_checkout_session_idx
    on registrations (stripe_checkout_session_id)
    where stripe_checkout_session_id is not null;
create index registrations_pending_expiry_idx
    on registrations (expires_at) where status = 'pending_payment';

-- Webhook idempotency: insert-or-skip keyed on Stripe's event id.
create table stripe_events (
    stripe_event_id text primary key,
    type text not null,
    received_at timestamptz not null default now()
);

-- +goose Down
drop table stripe_events;
drop table registrations;
drop table event_cubes;
drop table events;
alter table users drop column role;
```

- [ ] **Step 2: Regenerate sqlc models + build**

Run: `cd backend && sqlc generate && go build ./...`
Expected: clean build; `internal/db/models.go` now contains `Event`, `EventCube`, `Registration`, `StripeEvent`, and `User.Role`.

- [ ] **Step 3: Sanity-check migration up/down against testdb**

Run: `cd backend && go test ./internal/platform/testdb/ ./internal/cards/ -run TestSync -count=1 2>&1 | tail -5`
Expected: PASS (testdb migrates through 00006 without error). If there is no testdb test, any testcontainers-backed package test exercises the migration — `go test ./internal/collections/ -run . -count=1` also works.

- [ ] **Step 4: Commit**

```bash
git add backend/migrations/00006_events.sql backend/internal/db
git commit -m "feat(events): migration 00006 — role, events, event_cubes, registrations, stripe_events"
```

---

### Task 2: Events sqlc queries

**Files:**
- Create: `backend/internal/db/queries/events.sql`
- Generated: `backend/internal/db/events.sql.go` via `sqlc generate`

**Interfaces:**
- Produces (generated Go, used by Tasks 3–7): `CreateEvent`, `GetEvent`, `GetEventForUpdate`, `UpdateEventMeta`, `SetEventStatus`, `ListEvents`, `GetEventCounts`, `DeleteEventCubes`, `InsertEventCube`, `ListEventCubes`, `GetActiveRegistration`, `CountOccupiedSpots`, `CreateRegistration`, `GetRegistration`, `GetRegistrationByCheckoutSession`, `GetRegistrationByPaymentIntent`, `SetRegistrationCheckoutSession`, `MarkRegistrationPaid`, `SetRegistrationTerminal`, `PromoteToPendingPayment`, `NextWaitlisted`, `IncrementWaitlistCounter`, `ListEventIDsWithOverduePending`, `ExpireOverduePendingForEvent`, `ListPaidAttendees`, `ListEventRegistrations`, `ListActiveRegistrationsForEvent`, `InsertStripeEvent`, `SetRegistrationPaymentIntent`.

- [ ] **Step 1: Write the queries** `backend/internal/db/queries/events.sql`:

```sql
-- name: CreateEvent :one
insert into events (organizer_id, name, description, location, starts_at,
    fee_cents, currency, max_participants, refund_deadline)
values (sqlc.arg(organizer_id), sqlc.arg(name), sqlc.arg(description),
    sqlc.arg(location), sqlc.arg(starts_at), sqlc.arg(fee_cents),
    sqlc.arg(currency), sqlc.arg(max_participants), sqlc.narg(refund_deadline))
returning *;

-- name: GetEvent :one
select * from events where id = sqlc.arg(id);

-- name: GetEventForUpdate :one
select * from events where id = sqlc.arg(id) for update;

-- PATCH semantics: omitted field = unchanged. Clearing refund_deadline to
-- null is not supported (YAGNI, spec §3 locks most fields post-publish).
-- name: UpdateEventMeta :one
update events set
    name = coalesce(sqlc.narg(name), name),
    description = coalesce(sqlc.narg(description), description),
    location = coalesce(sqlc.narg(location), location),
    starts_at = coalesce(sqlc.narg(starts_at), starts_at),
    fee_cents = coalesce(sqlc.narg(fee_cents), fee_cents),
    currency = coalesce(sqlc.narg(currency), currency),
    max_participants = coalesce(sqlc.narg(max_participants), max_participants),
    refund_deadline = coalesce(sqlc.narg(refund_deadline), refund_deadline),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: SetEventStatus :one
update events set status = sqlc.arg(status), updated_at = now()
where id = sqlc.arg(id)
returning *;

-- Active events (published/started) first by soonest start; then past
-- (finished/cancelled) newest first. Drafts only when include_drafts
-- (admin). my_status = the caller's active registration, if any.
-- name: ListEvents :many
select e.*,
    coalesce(c.paid, 0)::int as paid_count,
    coalesce(c.pending, 0)::int as pending_count,
    coalesce(c.waitlisted, 0)::int as waitlist_count,
    my.status as my_status
from events e
left join lateral (
    select count(*) filter (where r.status = 'paid') as paid,
        count(*) filter (where r.status = 'pending_payment') as pending,
        count(*) filter (where r.status = 'waitlisted') as waitlisted
    from registrations r where r.event_id = e.id
) c on true
left join registrations my on my.event_id = e.id
    and my.user_id = sqlc.arg(user_id)
    and my.status in ('pending_payment', 'paid', 'waitlisted', 'refund_requested')
where e.status <> 'draft' or sqlc.arg(include_drafts)::bool
order by (case when e.status in ('draft', 'published', 'started') then 0 else 1 end),
    (case when e.status in ('draft', 'published', 'started') then e.starts_at end) asc,
    e.starts_at desc;

-- name: GetEventCounts :one
select count(*) filter (where status = 'paid')::int as paid_count,
    count(*) filter (where status = 'pending_payment')::int as pending_count,
    count(*) filter (where status = 'waitlisted')::int as waitlist_count
from registrations where event_id = sqlc.arg(event_id);

-- name: DeleteEventCubes :exec
delete from event_cubes where event_id = sqlc.arg(event_id);

-- name: InsertEventCube :exec
insert into event_cubes (event_id, cube_id, cube_change_id)
values (sqlc.arg(event_id), sqlc.arg(cube_id), sqlc.narg(cube_change_id));

-- name: ListEventCubes :many
select ec.cube_id, ec.cube_change_id, cu.name as cube_name,
    ch.version as pinned_version, ch.created_at as pinned_at
from event_cubes ec
join cubes cu on cu.id = ec.cube_id
left join cube_changes ch on ch.id = ec.cube_change_id
where ec.event_id = sqlc.arg(event_id)
order by cu.name;

-- name: GetActiveRegistration :one
select * from registrations
where event_id = sqlc.arg(event_id) and user_id = sqlc.arg(user_id)
    and status in ('pending_payment', 'paid', 'waitlisted', 'refund_requested');

-- Capacity = pending_payment + paid (spec §2).
-- name: CountOccupiedSpots :one
select count(*) from registrations
where event_id = sqlc.arg(event_id) and status in ('pending_payment', 'paid');

-- name: CreateRegistration :one
insert into registrations (event_id, user_id, status, expires_at, waitlist_pos, paid_at)
values (sqlc.arg(event_id), sqlc.arg(user_id), sqlc.arg(status),
    sqlc.narg(expires_at), sqlc.narg(waitlist_pos), sqlc.narg(paid_at))
returning *;

-- name: GetRegistration :one
select * from registrations where id = sqlc.arg(id);

-- name: GetRegistrationByCheckoutSession :one
select * from registrations
where stripe_checkout_session_id = sqlc.arg(session_id);

-- charge.refunded arrives with a payment intent, not a session id.
-- name: GetRegistrationByPaymentIntent :one
select * from registrations
where stripe_payment_intent_id = sqlc.arg(payment_intent_id);

-- name: SetRegistrationCheckoutSession :exec
update registrations set
    stripe_checkout_session_id = sqlc.arg(session_id),
    stripe_checkout_session_url = sqlc.arg(session_url),
    stripe_session_expires_at = sqlc.arg(session_expires_at),
    updated_at = now()
where id = sqlc.arg(id);

-- name: SetRegistrationPaymentIntent :exec
update registrations set stripe_payment_intent_id = sqlc.arg(payment_intent_id),
    updated_at = now()
where id = sqlc.arg(id);

-- name: MarkRegistrationPaid :one
update registrations set status = 'paid', paid_at = sqlc.arg(paid_at),
    stripe_payment_intent_id = coalesce(sqlc.narg(payment_intent_id), stripe_payment_intent_id),
    expires_at = null, waitlist_pos = null, updated_at = now()
where id = sqlc.arg(id)
returning *;

-- All transitions into cancelled / refund_requested / refunded / expired:
-- the row leaves the capacity/waitlist domain, so both columns null out.
-- name: SetRegistrationTerminal :one
update registrations set status = sqlc.arg(status),
    expires_at = null, waitlist_pos = null, updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: PromoteToPendingPayment :one
update registrations set status = 'pending_payment',
    expires_at = sqlc.arg(expires_at), waitlist_pos = null, updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: NextWaitlisted :one
select * from registrations
where event_id = sqlc.arg(event_id) and status = 'waitlisted'
order by waitlist_pos
limit 1;

-- name: IncrementWaitlistCounter :one
update events set waitlist_counter = waitlist_counter + 1
where id = sqlc.arg(id)
returning waitlist_counter;

-- Sweeper scan: which events have overdue pending rows (skipping rows
-- whose Checkout session is still live — the webhook owns those).
-- name: ListEventIDsWithOverduePending :many
select distinct event_id from registrations
where status = 'pending_payment' and expires_at <= sqlc.arg(now)
    and (stripe_session_expires_at is null or stripe_session_expires_at <= sqlc.arg(now));

-- name: ExpireOverduePendingForEvent :many
update registrations set status = 'expired',
    expires_at = null, waitlist_pos = null, updated_at = now()
where event_id = sqlc.arg(event_id) and status = 'pending_payment'
    and expires_at <= sqlc.arg(now)
    and (stripe_session_expires_at is null or stripe_session_expires_at <= sqlc.arg(now))
returning *;

-- name: ListPaidAttendees :many
select u.display_name from registrations r
join users u on u.id = r.user_id
where r.event_id = sqlc.arg(event_id) and r.status = 'paid'
order by r.paid_at;

-- Organizer panel: every row with user identity.
-- name: ListEventRegistrations :many
select r.*, u.display_name, u.email from registrations r
join users u on u.id = r.user_id
where r.event_id = sqlc.arg(event_id)
order by r.created_at;

-- Event cancel: everything that needs a transition or refund.
-- name: ListActiveRegistrationsForEvent :many
select r.*, u.display_name, u.email from registrations r
join users u on u.id = r.user_id
where r.event_id = sqlc.arg(event_id)
    and r.status in ('pending_payment', 'paid', 'waitlisted', 'refund_requested')
order by r.created_at;

-- name: InsertStripeEvent :execrows
insert into stripe_events (stripe_event_id, type)
values (sqlc.arg(stripe_event_id), sqlc.arg(type))
on conflict do nothing;

-- SetCubes validation: which cube does a pinned change belong to?
-- name: GetCubeChangeCubeID :one
select cube_id from cube_changes where id = sqlc.arg(id);
```

- [ ] **Step 2: Regenerate + build**

Run: `cd backend && sqlc generate && go build ./...`
Expected: clean build; `internal/db/events.sql.go` exists.

Note for all later tasks: sqlc derives parameter/field nullability from the column it touches — params on nullable columns (e.g. `expires_at`, `stripe_*`) generate as pointers (`*time.Time`, `*string`). Where plan code passes a value into a generated pointer field (or vice versa), follow the compiler: take the address or dereference. The semantics in this plan are authoritative; exact pointer-ness comes from the generated code.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/db
git commit -m "feat(events): sqlc queries for events, registrations, waitlist, stripe_events"
```

---

### Task 3: `internal/events` package — errors, StripeClient interface, event CRUD + lifecycle + cubes

Service skeleton and everything organizer-side that does NOT touch registrations yet. TDD against a real migrated Postgres (`testdb.New`), fake Stripe + recording mailer + injected clock — the same fakes carry through Tasks 4–7.

**Files:**
- Create: `backend/internal/events/service.go`
- Create: `backend/internal/events/stripe.go` (interface + `unconfiguredStripe`; real impl comes in Task 5)
- Create: `backend/internal/events/service_test.go`

**Interfaces:**
- Consumes: Task 2 queries; `testdb.New(t)`; `mail.Mailer`.
- Produces (used by Tasks 4–11):
  - `NewService(queries *db.Queries, pool *pgxpool.Pool, stripe StripeClient, mailer mail.Mailer, baseURL string, log *slog.Logger) *Service` (field `now func() time.Time`, defaults `time.Now`, overridable in tests)
  - errors: `ErrEventNotFound`, `ErrInvalidTransition`, `ErrEventLocked`, `ErrCubesLocked`, `ErrInvalidEventCube`, `ErrPaymentsUnconfigured`, `ErrAlreadyRegistered`, `ErrRegistrationNotFound`, `ErrRegistrationNotPayable`, `ErrRegistrationClosed`
  - `StripeClient` interface: `Configured() bool`, `CreateCheckoutSession(ctx, CheckoutParams) (*CheckoutSession, error)`, `RefundPaymentIntent(ctx, paymentIntentID string) error`; types `CheckoutParams{RegistrationID uuid.UUID; EventName, Currency string; AmountCents int64; ExpiresAt time.Time; SuccessURL, CancelURL string}`, `CheckoutSession{ID, URL string; ExpiresAt time.Time}`
  - methods: `Create(ctx, organizerID, CreateEventParams) (*db.Event, error)`, `Update(ctx, eventID, UpdateEventParams) (*db.Event, error)`, `Transition(ctx, eventID uuid.UUID, action string) (*db.Event, error)` (actions `publish|start|finish|cancel`; `start` is finished in Task 4, `cancel` in Task 6), `SetCubes(ctx, eventID uuid.UUID, links []CubeLinkInput) error`, `List(ctx, callerID uuid.UUID, isAdmin bool) ([]EventInfo, error)`, `GetDetail(ctx, eventID, callerID uuid.UUID, isAdmin bool) (*EventDetail, error)`
  - types: `CreateEventParams{Name, Description, Location string; StartsAt time.Time; FeeCents int32; Currency string; MaxParticipants int32; RefundDeadline *time.Time}`, `UpdateEventParams{Name, Description, Location, Currency *string; StartsAt, RefundDeadline *time.Time; FeeCents, MaxParticipants *int32}`, `CubeLinkInput{CubeID uuid.UUID; CubeChangeID *uuid.UUID}`, `EventInfo{Event db.Event; PaidCount, PendingCount, WaitlistCount int32; MyStatus *string}`, `CubeLink{CubeID uuid.UUID; CubeName string; CubeChangeID *uuid.UUID; PinnedVersion *int32; PinnedAt *time.Time}`, `EventDetail{EventInfo; Cubes []CubeLink; Attendees []string; MyRegistration *db.Registration}`

- [ ] **Step 1: Write `backend/internal/events/stripe.go`** (interface + unconfigured stub only):

```go
package events

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrPaymentsUnconfigured: Stripe keys are absent. Creating/publishing a
// PAID event and paying map this to 503 payments-unconfigured; free
// events never hit it.
var ErrPaymentsUnconfigured = errors.New("payments not configured")

type CheckoutParams struct {
	RegistrationID uuid.UUID
	EventName      string
	Currency       string
	AmountCents    int64
	ExpiresAt      time.Time
	SuccessURL     string
	CancelURL      string
}

type CheckoutSession struct {
	ID        string
	URL       string
	ExpiresAt time.Time
}

// StripeClient isolates the SDK so the state machine tests never talk to
// real Stripe. The real implementation lands in stripe_client.go (Task 5).
type StripeClient interface {
	Configured() bool
	CreateCheckoutSession(ctx context.Context, p CheckoutParams) (*CheckoutSession, error)
	RefundPaymentIntent(ctx context.Context, paymentIntentID string) error
}

type unconfiguredStripe struct{}

func (unconfiguredStripe) Configured() bool { return false }

func (unconfiguredStripe) CreateCheckoutSession(context.Context, CheckoutParams) (*CheckoutSession, error) {
	return nil, ErrPaymentsUnconfigured
}

func (unconfiguredStripe) RefundPaymentIntent(context.Context, string) error {
	return ErrPaymentsUnconfigured
}
```

- [ ] **Step 2: Write `backend/internal/events/service.go`:**

```go
// Package events owns the event lifecycle, paid registration state
// machine, waitlist promotion, and Stripe integration.
package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
)

// PaymentWindow is how long a pending_payment registration holds a spot
// (registration and waitlist promotion alike). Matches Stripe Checkout's
// default session expiry so one mechanism covers both.
const PaymentWindow = 24 * time.Hour

// stripeMinSessionTTL is Stripe's minimum Checkout session lifetime.
const stripeMinSessionTTL = 30 * time.Minute

var (
	// ErrEventNotFound also hides drafts from non-admins (no existence leak,
	// same convention as private cubes).
	ErrEventNotFound     = errors.New("event not found")
	ErrInvalidTransition = errors.New("invalid event transition")
	// ErrEventLocked: a PATCH touched a field that is frozen post-publish
	// (people paid under those terms).
	ErrEventLocked  = errors.New("event field locked after publish")
	ErrCubesLocked  = errors.New("event cubes locked after publish")
	ErrInvalidEventCube = errors.New("invalid event cube")
	ErrAlreadyRegistered      = errors.New("already registered")
	ErrRegistrationNotFound   = errors.New("registration not found")
	ErrRegistrationNotPayable = errors.New("registration not payable")
	ErrRegistrationClosed     = errors.New("registration closed")
)

type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
	stripe  StripeClient
	mailer  mail.Mailer
	baseURL string
	log     *slog.Logger
	now     func() time.Time
}

func NewService(queries *db.Queries, pool *pgxpool.Pool, stripe StripeClient, mailer mail.Mailer, baseURL string, log *slog.Logger) *Service {
	return &Service{
		queries: queries, pool: pool, stripe: stripe, mailer: mailer,
		baseURL: baseURL, log: log, now: time.Now,
	}
}

func (s *Service) withTx(ctx context.Context, fn func(qtx *db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type CreateEventParams struct {
	Name            string
	Description     string
	Location        string
	StartsAt        time.Time
	FeeCents        int32
	Currency        string
	MaxParticipants int32
	RefundDeadline  *time.Time
}

func (s *Service) Create(ctx context.Context, organizerID uuid.UUID, p CreateEventParams) (*db.Event, error) {
	if p.FeeCents > 0 && !s.stripe.Configured() {
		return nil, ErrPaymentsUnconfigured
	}
	if p.Currency == "" {
		p.Currency = "pln"
	}
	ev, err := s.queries.CreateEvent(ctx, db.CreateEventParams{
		OrganizerID: organizerID, Name: p.Name, Description: p.Description,
		Location: p.Location, StartsAt: p.StartsAt, FeeCents: p.FeeCents,
		Currency: p.Currency, MaxParticipants: p.MaxParticipants,
		RefundDeadline: p.RefundDeadline,
	})
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

type UpdateEventParams struct {
	Name            *string
	Description     *string
	Location        *string
	StartsAt        *time.Time
	FeeCents        *int32
	Currency        *string
	MaxParticipants *int32
	RefundDeadline  *time.Time
}

// Update enforces the lifecycle field whitelist: drafts are fully
// editable; from publish on, only description, location, and
// refund_deadline may change.
func (s *Service) Update(ctx context.Context, eventID uuid.UUID, p UpdateEventParams) (*db.Event, error) {
	var out db.Event
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		if ev.Status != "draft" {
			if p.Name != nil || p.StartsAt != nil || p.FeeCents != nil ||
				p.Currency != nil || p.MaxParticipants != nil {
				return ErrEventLocked
			}
		}
		if p.FeeCents != nil && *p.FeeCents > 0 && !s.stripe.Configured() {
			return ErrPaymentsUnconfigured
		}
		out, err = qtx.UpdateEventMeta(ctx, db.UpdateEventMetaParams{
			ID: eventID, Name: p.Name, Description: p.Description,
			Location: p.Location, StartsAt: p.StartsAt, FeeCents: p.FeeCents,
			Currency: p.Currency, MaxParticipants: p.MaxParticipants,
			RefundDeadline: p.RefundDeadline,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// legalTransitions: action → required current status.
var legalTransitions = map[string][]string{
	"publish": {"draft"},
	"start":   {"published"},
	"finish":  {"started"},
	"cancel":  {"published", "started"},
}

// Transition validates and applies a lifecycle action. start additionally
// closes registration (Task 4) and cancel mass-refunds (Task 6).
func (s *Service) Transition(ctx context.Context, eventID uuid.UUID, action string) (*db.Event, error) {
	allowed, ok := legalTransitions[action]
	if !ok {
		return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidTransition, action)
	}
	target := map[string]string{
		"publish": "published", "start": "started",
		"finish": "finished", "cancel": "cancelled",
	}[action]
	var out db.Event
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		legal := false
		for _, st := range allowed {
			legal = legal || ev.Status == st
		}
		if !legal {
			return fmt.Errorf("%w: %s from %s", ErrInvalidTransition, action, ev.Status)
		}
		if action == "publish" && ev.FeeCents > 0 && !s.stripe.Configured() {
			return ErrPaymentsUnconfigured
		}
		if action == "start" {
			if err := s.closeRegistrationLocked(ctx, qtx, ev); err != nil {
				return err
			}
		}
		if action == "cancel" {
			emails, err = s.cancelEventLocked(ctx, qtx, ev)
			if err != nil {
				return err
			}
		}
		out, err = qtx.SetEventStatus(ctx, db.SetEventStatusParams{ID: eventID, Status: target})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	return &out, nil
}

type CubeLinkInput struct {
	CubeID       uuid.UUID
	CubeChangeID *uuid.UUID
}

// SetCubes replaces the event's cube links. Draft-only (locked after
// publish). Each cube must be public or owned by the event's organizer;
// a pin must belong to its cube.
func (s *Service) SetCubes(ctx context.Context, eventID uuid.UUID, links []CubeLinkInput) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		if ev.Status != "draft" {
			return ErrCubesLocked
		}
		for _, l := range links {
			cube, err := qtx.GetCube(ctx, l.CubeID)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: unknown cube", ErrInvalidEventCube)
			}
			if err != nil {
				return err
			}
			if cube.Visibility != "public" && cube.OwnerID != ev.OrganizerID {
				return fmt.Errorf("%w: cube not accessible", ErrInvalidEventCube)
			}
			if l.CubeChangeID != nil {
				cubeID, err := qtx.GetCubeChangeCubeID(ctx, *l.CubeChangeID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && cubeID != l.CubeID) {
					return fmt.Errorf("%w: pin does not belong to cube", ErrInvalidEventCube)
				}
				if err != nil {
					return err
				}
			}
		}
		if err := qtx.DeleteEventCubes(ctx, eventID); err != nil {
			return err
		}
		for _, l := range links {
			if err := qtx.InsertEventCube(ctx, db.InsertEventCubeParams{
				EventID: eventID, CubeID: l.CubeID, CubeChangeID: l.CubeChangeID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

type EventInfo struct {
	Event         db.Event
	PaidCount     int32
	PendingCount  int32
	WaitlistCount int32
	MyStatus      *string
}

func (s *Service) List(ctx context.Context, callerID uuid.UUID, isAdmin bool) ([]EventInfo, error) {
	rows, err := s.queries.ListEvents(ctx, db.ListEventsParams{
		UserID: callerID, IncludeDrafts: isAdmin,
	})
	if err != nil {
		return nil, err
	}
	out := make([]EventInfo, len(rows))
	for i, r := range rows {
		out[i] = EventInfo{
			Event: db.Event{
				ID: r.ID, OrganizerID: r.OrganizerID, Name: r.Name,
				Description: r.Description, Location: r.Location,
				StartsAt: r.StartsAt, FeeCents: r.FeeCents, Currency: r.Currency,
				MaxParticipants: r.MaxParticipants, RefundDeadline: r.RefundDeadline,
				Status: r.Status, WaitlistCounter: r.WaitlistCounter,
				CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			},
			PaidCount: r.PaidCount, PendingCount: r.PendingCount,
			WaitlistCount: r.WaitlistCount, MyStatus: r.MyStatus,
		}
	}
	return out, nil
}

type CubeLink struct {
	CubeID        uuid.UUID
	CubeName      string
	CubeChangeID  *uuid.UUID
	PinnedVersion *int32
	PinnedAt      *time.Time
}

type EventDetail struct {
	EventInfo
	Cubes          []CubeLink
	Attendees      []string
	MyRegistration *db.Registration
}

func (s *Service) GetDetail(ctx context.Context, eventID, callerID uuid.UUID, isAdmin bool) (*EventDetail, error) {
	ev, err := s.queries.GetEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	if ev.Status == "draft" && !isAdmin {
		return nil, ErrEventNotFound
	}
	counts, err := s.queries.GetEventCounts(ctx, eventID)
	if err != nil {
		return nil, err
	}
	cubes, err := s.queries.ListEventCubes(ctx, eventID)
	if err != nil {
		return nil, err
	}
	attendees, err := s.queries.ListPaidAttendees(ctx, eventID)
	if err != nil {
		return nil, err
	}
	detail := &EventDetail{
		EventInfo: EventInfo{
			Event: ev, PaidCount: counts.PaidCount,
			PendingCount: counts.PendingCount, WaitlistCount: counts.WaitlistCount,
		},
		Cubes:     make([]CubeLink, len(cubes)),
		Attendees: attendees,
	}
	for i, c := range cubes {
		detail.Cubes[i] = CubeLink{
			CubeID: c.CubeID, CubeName: c.CubeName, CubeChangeID: c.CubeChangeID,
			PinnedVersion: c.PinnedVersion, PinnedAt: c.PinnedAt,
		}
	}
	reg, err := s.queries.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
		EventID: eventID, UserID: callerID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// no active registration — fine
	case err != nil:
		return nil, err
	default:
		st := reg.Status
		detail.MyRegistration = &reg
		detail.MyStatus = &st
	}
	return detail, nil
}
```

Note: `closeRegistrationLocked`, `cancelEventLocked`, `pendingEmail`, and `sendEmails` do not exist yet — Step 3 stubs the email plumbing minimally so this task compiles; Task 4 implements `closeRegistrationLocked` and the real email bodies; Task 6 implements `cancelEventLocked`.

- [ ] **Step 3: Write `backend/internal/events/emails.go`** (plumbing now, more bodies in Tasks 4/6):

```go
package events

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// pendingEmail is composed inside a transaction and sent only after
// commit — mail must never fail or delay a state transition.
type pendingEmail struct {
	to      string
	subject string
	body    string
}

func (s *Service) sendEmails(ctx context.Context, emails []pendingEmail) {
	for _, e := range emails {
		if err := s.mailer.Send(ctx, e.to, e.subject, e.body); err != nil {
			s.log.Error("events: send mail", "to", e.to, "subject", e.subject, "error", err)
		}
	}
}

// closeRegistrationLocked and cancelEventLocked are implemented in
// Tasks 4 and 6; these compile-time stubs keep Transition total.
func (s *Service) closeRegistrationLocked(ctx context.Context, qtx *db.Queries, ev db.Event) error {
	_ = pgx.ErrNoRows
	return nil
}

func (s *Service) cancelEventLocked(ctx context.Context, qtx *db.Queries, ev db.Event) ([]pendingEmail, error) {
	return nil, nil
}
```

- [ ] **Step 4: Write the failing tests** `backend/internal/events/service_test.go`:

```go
package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// ---- shared fakes (extended by Tasks 4–7) ----

type fakeStripe struct {
	configured bool
	mu         sync.Mutex
	sessions   []CheckoutParams
	refunds    []string
	refundErr  error
}

func (f *fakeStripe) Configured() bool { return f.configured }

func (f *fakeStripe) CreateCheckoutSession(_ context.Context, p CheckoutParams) (*CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, p)
	return &CheckoutSession{
		ID:  "cs_test_" + p.RegistrationID.String(),
		URL: "https://checkout.stripe.test/" + p.RegistrationID.String(),
		ExpiresAt: p.ExpiresAt,
	}, nil
}

func (f *fakeStripe) RefundPaymentIntent(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refundErr != nil {
		return f.refundErr
	}
	f.refunds = append(f.refunds, id)
	return nil
}

type sentMail struct{ to, subject, body string }

type recordMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

func (m *recordMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{to, subject, body})
	return nil
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type testEnv struct {
	svc    *Service
	pool   *pgxpool.Pool
	q      *db.Queries
	stripe *fakeStripe
	mailer *recordMailer
	clock  *fakeClock
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	st := &fakeStripe{configured: true}
	m := &recordMailer{}
	clock := &fakeClock{t: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	svc := NewService(q, pool, st, m, "http://localhost:5173", slog.Default())
	svc.now = clock.Now
	return &testEnv{svc: svc, pool: pool, q: q, stripe: st, mailer: m, clock: clock}
}

func (e *testEnv) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.pool.QueryRow(context.Background(),
		`insert into users (email, display_name, email_verified_at)
		 values ($1, split_part($1, '@', 1), now()) returning id`, email).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *testEnv) seedCube(t *testing.T, ownerID uuid.UUID, name, visibility string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.pool.QueryRow(context.Background(),
		`insert into cubes (owner_id, name, visibility) values ($1, $2, $3) returning id`,
		ownerID, name, visibility).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *testEnv) createEvent(t *testing.T, organizerID uuid.UUID, feeCents int32, maxParticipants int32) *db.Event {
	t.Helper()
	ev, err := e.svc.Create(context.Background(), organizerID, CreateEventParams{
		Name: "Cube Night", StartsAt: e.clock.Now().Add(7 * 24 * time.Hour),
		FeeCents: feeCents, MaxParticipants: maxParticipants,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func (e *testEnv) publish(t *testing.T, eventID uuid.UUID) {
	t.Helper()
	if _, err := e.svc.Transition(context.Background(), eventID, "publish"); err != nil {
		t.Fatal(err)
	}
}

// ---- Task 3 tests ----

func TestCreatePaidEventRequiresStripe(t *testing.T) {
	env := newTestEnv(t)
	env.stripe.configured = false
	org := env.seedUser(t, "org@test")
	_, err := env.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Paid Night", StartsAt: env.clock.Now(), FeeCents: 5000, MaxParticipants: 8,
	})
	if !errors.Is(err, ErrPaymentsUnconfigured) {
		t.Fatalf("want ErrPaymentsUnconfigured, got %v", err)
	}
	// Free events work without Stripe.
	if _, err := env.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Free Night", StartsAt: env.clock.Now(), FeeCents: 0, MaxParticipants: 8,
	}); err != nil {
		t.Fatalf("free event should not need stripe: %v", err)
	}
}

func TestUpdateFieldWhitelist(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 8)

	// Draft: everything editable.
	newFee := int32(6000)
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{FeeCents: &newFee}); err != nil {
		t.Fatalf("draft fee edit: %v", err)
	}

	env.publish(t, ev.ID)

	// Published: fee locked, description fine.
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{FeeCents: &newFee}); !errors.Is(err, ErrEventLocked) {
		t.Fatalf("want ErrEventLocked, got %v", err)
	}
	desc := "bring sleeves"
	got, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{Description: &desc})
	if err != nil || got.Description != desc {
		t.Fatalf("published description edit: %v (%+v)", err, got)
	}
}

func TestTransitions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 0, 8)

	if _, err := env.svc.Transition(ctx, ev.ID, "start"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("draft→started must fail, got %v", err)
	}
	if _, err := env.svc.Transition(ctx, ev.ID, "publish"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Transition(ctx, ev.ID, "publish"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("double publish must fail, got %v", err)
	}
	if _, err := env.svc.Transition(ctx, ev.ID, "start"); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.Transition(ctx, ev.ID, "finish")
	if err != nil || got.Status != "finished" {
		t.Fatalf("finish: %v (%+v)", err, got)
	}
}

func TestPublishPaidEventRequiresStripe(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 8)
	env.stripe.configured = false
	if _, err := env.svc.Transition(context.Background(), ev.ID, "publish"); !errors.Is(err, ErrPaymentsUnconfigured) {
		t.Fatalf("want ErrPaymentsUnconfigured, got %v", err)
	}
}

func TestSetCubesValidation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	other := env.seedUser(t, "other@test")
	ev := env.createEvent(t, org, 0, 8)

	pubCube := env.seedCube(t, other, "Public Cube", "public")
	privOther := env.seedCube(t, other, "Private Other", "private")
	privOwn := env.seedCube(t, org, "Private Own", "private")

	// Public + organizer-owned private are fine; foreign private is not.
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: pubCube}, {CubeID: privOwn}}); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: privOther}}); !errors.Is(err, ErrInvalidEventCube) {
		t.Fatalf("want ErrInvalidEventCube, got %v", err)
	}

	// A pin must belong to its cube.
	var changeID uuid.UUID
	if err := env.pool.QueryRow(ctx,
		`insert into cube_changes (cube_id, version, author_id) values ($1, 1, $2) returning id`,
		pubCube, other).Scan(&changeID); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: privOwn, CubeChangeID: &changeID}}); !errors.Is(err, ErrInvalidEventCube) {
		t.Fatalf("cross-cube pin must fail, got %v", err)
	}
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: pubCube, CubeChangeID: &changeID}}); err != nil {
		t.Fatal(err)
	}

	// Replace-set semantics: the previous two links are gone.
	detail, err := env.svc.GetDetail(ctx, ev.ID, org, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Cubes) != 1 || detail.Cubes[0].CubeID != pubCube {
		t.Fatalf("expected exactly the pinned pubCube link, got %+v", detail.Cubes)
	}

	// Locked after publish.
	env.publish(t, ev.ID)
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: pubCube}}); !errors.Is(err, ErrCubesLocked) {
		t.Fatalf("want ErrCubesLocked, got %v", err)
	}
}

func TestDraftHiddenFromNonAdmins(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	user := env.seedUser(t, "user@test")
	ev := env.createEvent(t, org, 0, 8)

	if _, err := env.svc.GetDetail(ctx, ev.ID, user, false); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("draft must 404 for non-admin, got %v", err)
	}
	if _, err := env.svc.GetDetail(ctx, ev.ID, org, true); err != nil {
		t.Fatal(err)
	}
	list, err := env.svc.List(ctx, user, false)
	if err != nil || len(list) != 0 {
		t.Fatalf("draft must be absent from non-admin list: %v %+v", err, list)
	}
	list, err = env.svc.List(ctx, org, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("admin list must include draft: %v %+v", err, list)
	}
}
```

- [ ] **Step 5: Run tests to verify they fail, then compile-fix to green**

Run: `cd backend && go test ./internal/events/ -v -count=1`
Expected: first FAIL (missing code), then after Steps 1–3 all Task 3 tests PASS. (Integration tests need Docker.)

- [ ] **Step 6: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/events/ && cd ..
git add backend/internal/events
git commit -m "feat(events): service — event CRUD, lifecycle transitions, cube links"
```

---

### Task 4: Registration, waitlist, promotion, expiry, start-closes-registration

The heart of the state machine. Everything runs under the event row lock; promotion is ONE reusable function every spot-freeing path calls. Free events confirm instantly. Promotion and free-confirmation emails land here.

**Files:**
- Modify: `backend/internal/events/service.go` (add registration methods)
- Modify: `backend/internal/events/emails.go` (real bodies + real `closeRegistrationLocked`)
- Modify: `backend/internal/events/service_test.go`

**Interfaces:**
- Consumes: Task 2 queries, Task 3 service + fakes.
- Produces:
  - `Register(ctx, eventID, userID uuid.UUID) (*db.Registration, error)`
  - `expireAndPromote(ctx, eventID uuid.UUID) error` (sweeper entry, per event)
  - `promoteLocked(ctx, qtx *db.Queries, ev db.Event) ([]pendingEmail, error)`
  - real `closeRegistrationLocked` (start: pending → expired, waitlisted → cancelled, NO promotion)
  - email builders: `promotionEmail`, `confirmationEmail` (bilingual PL-then-EN, plain text)

- [ ] **Step 1: Write the failing tests.** Append to `service_test.go`:

```go
func (e *testEnv) register(t *testing.T, eventID, userID uuid.UUID) *db.Registration {
	t.Helper()
	reg, err := e.svc.Register(context.Background(), eventID, userID)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestRegisterPaidEvent(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)

	reg := env.register(t, ev.ID, alice)
	if reg.Status != "pending_payment" {
		t.Fatalf("want pending_payment, got %s", reg.Status)
	}
	wantExpiry := env.clock.Now().Add(PaymentWindow)
	if reg.ExpiresAt == nil || !reg.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("want expires_at %v, got %v", wantExpiry, reg.ExpiresAt)
	}
	// Duplicate active registration is rejected.
	if _, err := env.svc.Register(context.Background(), ev.ID, alice); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("want ErrAlreadyRegistered, got %v", err)
	}
}

func TestRegisterFreeEventConfirmsInstantly(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 0, 2)
	env.publish(t, ev.ID)

	reg := env.register(t, ev.ID, alice)
	if reg.Status != "paid" || reg.PaidAt == nil {
		t.Fatalf("free event must confirm instantly, got %+v", reg)
	}
	if len(env.mailer.sent) != 1 || env.mailer.sent[0].to != "alice@test" {
		t.Fatalf("want one confirmation email to alice, got %+v", env.mailer.sent)
	}
}

func TestRegisterFullEventWaitlists(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	c := env.seedUser(t, "c@test")
	env.register(t, ev.ID, a) // takes the spot (pending_payment)
	rb := env.register(t, ev.ID, b)
	rc := env.register(t, ev.ID, c)
	if rb.Status != "waitlisted" || rb.WaitlistPos == nil || *rb.WaitlistPos != 1 {
		t.Fatalf("b should be waitlist #1, got %+v", rb)
	}
	if rc.Status != "waitlisted" || rc.WaitlistPos == nil || *rc.WaitlistPos != 2 {
		t.Fatalf("c should be waitlist #2, got %+v", rc)
	}
}

func TestExpiryPromotesWaitlist(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a)
	env.register(t, ev.ID, b)

	env.clock.Advance(PaymentWindow + time.Minute)
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}

	gotA, err := env.q.GetRegistration(ctx, ra.ID)
	if err != nil || gotA.Status != "expired" {
		t.Fatalf("a should be expired: %v %+v", err, gotA)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" || gotB.ExpiresAt == nil {
		t.Fatalf("b should be promoted to pending_payment: %v %+v", err, gotB)
	}
	// Promotion email with a pay-by deadline went to b.
	found := false
	for _, m := range env.mailer.sent {
		found = found || m.to == "b@test"
	}
	if !found {
		t.Fatalf("want promotion email to b, got %+v", env.mailer.sent)
	}

	// a can re-register after expiry (partial unique index only covers active rows).
	if _, err := env.svc.Register(ctx, ev.ID, a); err != nil {
		t.Fatalf("re-register after expiry: %v", err)
	}
}

func TestFreeEventPromotionConfirmsInstantly(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 0, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.register(t, ev.ID, a) // paid instantly (free event)
	rb := env.register(t, ev.ID, b)
	if rb.Status != "waitlisted" {
		t.Fatalf("want waitlisted, got %+v", rb)
	}

	// Free the spot via start-agnostic path: directly cancel a's row in Task 6;
	// here simulate by expiring via SQL is impossible (paid rows don't expire),
	// so use the sweeper path after flipping a to pending manually:
	if _, err := env.pool.Exec(ctx,
		`update registrations set status = 'pending_payment', expires_at = now() - interval '1 hour', paid_at = null
		 where event_id = $1 and user_id = $2`, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "paid" || gotB.PaidAt == nil {
		t.Fatalf("free-event promotion must confirm instantly: %v %+v", err, gotB)
	}
}

func TestMultipleFreedSpotsPromoteMultiple(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)

	users := make([]uuid.UUID, 4)
	for i, email := range []string{"a@test", "b@test", "c@test", "d@test"} {
		users[i] = env.seedUser(t, email)
		env.register(t, ev.ID, users[i])
	}
	// a, b hold spots; c, d waitlisted. Both spots expire → both promote in order.
	env.clock.Advance(PaymentWindow + time.Minute)
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	for _, u := range users[2:] {
		got, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: u})
		if err != nil || got.Status != "pending_payment" {
			t.Fatalf("waitlisted user should be promoted: %v %+v", err, got)
		}
	}
}

func TestRegisterRequiresPublished(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")

	draft := env.createEvent(t, org, 0, 8)
	if _, err := env.svc.Register(ctx, draft.ID, alice); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("draft register must 404 (no leak), got %v", err)
	}

	started := env.createEvent(t, org, 0, 8)
	env.publish(t, started.ID)
	if _, err := env.svc.Transition(ctx, started.ID, "start"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Register(ctx, started.ID, alice); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("started register must be closed, got %v", err)
	}
}

func TestStartClosesRegistration(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a) // pending_payment
	rb := env.register(t, ev.ID, b) // waitlisted

	if _, err := env.svc.Transition(ctx, ev.ID, "start"); err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	gotB, _ := env.q.GetRegistration(ctx, rb.ID)
	if gotA.Status != "expired" || gotB.Status != "cancelled" {
		t.Fatalf("start must expire pending and cancel waitlist, got %s / %s", gotA.Status, gotB.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/events/ -run 'TestRegister|TestExpiry|TestFreeEvent|TestMultiple|TestStart' -v -count=1`
Expected: FAIL — `Register` undefined etc.

- [ ] **Step 3: Implement.** Append to `service.go`:

```go
// Register creates the caller's registration for a published event:
// spot free + paid event → pending_payment (24h window); spot free +
// free event → paid instantly; event full → waitlisted.
func (s *Service) Register(ctx context.Context, eventID, userID uuid.UUID) (*db.Registration, error) {
	var reg db.Registration
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		switch ev.Status {
		case "published":
			// registrable
		case "draft":
			return ErrEventNotFound // no existence leak
		default:
			return ErrRegistrationClosed
		}
		if _, err := qtx.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
			EventID: eventID, UserID: userID,
		}); err == nil {
			return ErrAlreadyRegistered
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		occupied, err := qtx.CountOccupiedSpots(ctx, eventID)
		if err != nil {
			return err
		}
		now := s.now()
		switch {
		case occupied >= int64(ev.MaxParticipants):
			pos, err := qtx.IncrementWaitlistCounter(ctx, eventID)
			if err != nil {
				return err
			}
			reg, err = qtx.CreateRegistration(ctx, db.CreateRegistrationParams{
				EventID: eventID, UserID: userID, Status: "waitlisted", WaitlistPos: &pos,
			})
			return err
		case ev.FeeCents > 0:
			expires := now.Add(PaymentWindow)
			reg, err = qtx.CreateRegistration(ctx, db.CreateRegistrationParams{
				EventID: eventID, UserID: userID, Status: "pending_payment", ExpiresAt: &expires,
			})
			return err
		default:
			reg, err = qtx.CreateRegistration(ctx, db.CreateRegistrationParams{
				EventID: eventID, UserID: userID, Status: "paid", PaidAt: &now,
			})
			if err != nil {
				return err
			}
			u, err := qtx.GetUserByID(ctx, userID)
			if err != nil {
				return err
			}
			emails = append(emails, confirmationEmail(u, ev, s.baseURL))
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	return &reg, nil
}

// promoteLocked fills free capacity from the waitlist. MUST run inside a
// transaction holding the event row lock. Every spot-freeing path
// (expiry, cancel, refund, webhook) calls it.
func (s *Service) promoteLocked(ctx context.Context, qtx *db.Queries, ev db.Event) ([]pendingEmail, error) {
	var emails []pendingEmail
	for {
		occupied, err := qtx.CountOccupiedSpots(ctx, ev.ID)
		if err != nil {
			return nil, err
		}
		if occupied >= int64(ev.MaxParticipants) {
			return emails, nil
		}
		next, err := qtx.NextWaitlisted(ctx, ev.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return emails, nil
		}
		if err != nil {
			return nil, err
		}
		u, err := qtx.GetUserByID(ctx, next.UserID)
		if err != nil {
			return nil, err
		}
		now := s.now()
		if ev.FeeCents > 0 {
			expires := now.Add(PaymentWindow)
			if _, err := qtx.PromoteToPendingPayment(ctx, db.PromoteToPendingPaymentParams{
				ID: next.ID, ExpiresAt: expires,
			}); err != nil {
				return nil, err
			}
			emails = append(emails, promotionEmail(u, ev, expires, s.baseURL))
		} else {
			if _, err := qtx.MarkRegistrationPaid(ctx, db.MarkRegistrationPaidParams{
				ID: next.ID, PaidAt: now,
			}); err != nil {
				return nil, err
			}
			emails = append(emails, confirmationEmail(u, ev, s.baseURL))
		}
	}
}

// expireAndPromote is the per-event sweeper body: flip overdue
// pending_payment rows to expired, then promote.
func (s *Service) expireAndPromote(ctx context.Context, eventID uuid.UUID) error {
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if err != nil {
			return err
		}
		if _, err := qtx.ExpireOverduePendingForEvent(ctx, db.ExpireOverduePendingForEventParams{
			EventID: eventID, Now: s.now(),
		}); err != nil {
			return err
		}
		emails, err = s.promoteLocked(ctx, qtx, ev)
		return err
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	return nil
}
```

Replace the `closeRegistrationLocked` stub in `emails.go` with the real one (move it to `service.go`, delete the stub):

```go
// closeRegistrationLocked (start): registration closes — remaining
// pending_payment rows expire, the waitlist is cancelled, and nothing
// promotes. The paid roster is the 5b tournament roster.
func (s *Service) closeRegistrationLocked(ctx context.Context, qtx *db.Queries, ev db.Event) error {
	rows, err := qtx.ListActiveRegistrationsForEvent(ctx, ev.ID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		var target string
		switch r.Status {
		case "pending_payment":
			target = "expired"
		case "waitlisted":
			target = "cancelled"
		default:
			continue
		}
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: r.ID, Status: target,
		}); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Write the email bodies.** Replace `emails.go`'s remaining content (keep `pendingEmail` + `sendEmails`) and add:

```go
// Emails are bilingual in one body — Polish first, then English — because
// users have no stored locale (decided in the spec §6).

func eventLink(baseURL string, eventID uuid.UUID) string {
	return fmt.Sprintf("%s/events/%s", baseURL, eventID)
}

func payByLabel(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func confirmationEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Potwierdzenie zapisu / Registration confirmed: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nTwoje miejsce na %s jest potwierdzone.\n%s\n\n---\n\nHi %s,\n\nYour spot at %s is confirmed.\n%s",
			u.DisplayName, ev.Name, eventLink(baseURL, ev.ID),
			u.DisplayName, ev.Name, eventLink(baseURL, ev.ID)),
	}
}

func promotionEmail(u db.User, ev db.Event, expiresAt time.Time, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Zwolniło się miejsce / A spot opened: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nZwolniło się miejsce na %s. Zapłać do %s, żeby je zająć:\n%s\n\n---\n\nHi %s,\n\nA spot opened at %s. Pay by %s to claim it:\n%s",
			u.DisplayName, ev.Name, payByLabel(expiresAt), eventLink(baseURL, ev.ID),
			u.DisplayName, ev.Name, payByLabel(expiresAt), eventLink(baseURL, ev.ID)),
	}
}
```

(`emails.go` needs imports `fmt`, `time`, `github.com/google/uuid` now; drop the `pgx` stub import.)

- [ ] **Step 5: Run all events tests**

Run: `cd backend && go test ./internal/events/ -v -count=1`
Expected: ALL PASS (Tasks 3 + 4 tests).

- [ ] **Step 6: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/events/ && cd ..
git add backend/internal/events
git commit -m "feat(events): registration state machine — capacity, waitlist, promotion, expiry"
```

---

### Task 5: Real Stripe client + Pay (Checkout session)

**Files:**
- Create: `backend/internal/events/stripe_client.go`
- Modify: `backend/internal/events/service.go` (add `Pay`)
- Modify: `backend/internal/events/service_test.go`
- Modify: `backend/go.mod` (+ stripe-go)

**Interfaces:**
- Consumes: `StripeClient` interface (Task 3).
- Produces:
  - `NewStripeClient(secretKey string) StripeClient` — real SDK impl, or `unconfiguredStripe{}` when the key is empty
  - `Pay(ctx, eventID, userID uuid.UUID) (checkoutURL string, err error)`

- [ ] **Step 1: Add the dependency**

Run: `cd backend && go get github.com/stripe/stripe-go/v86@latest`
Expected: go.mod/go.sum updated. (If v86 is not the current major anymore, use the latest major and adjust import paths consistently — the API shape used below is the `stripe.NewClient` + `V1CheckoutSessions`/`V1Refunds` services pattern, stable since v82.)

- [ ] **Step 2: Write the failing Pay tests.** Append to `service_test.go`:

```go
func TestPayCreatesCheckoutSession(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	reg := env.register(t, ev.ID, alice)

	url, err := env.svc.Pay(ctx, ev.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if url == "" || len(env.stripe.sessions) != 1 {
		t.Fatalf("want one checkout session, got url=%q sessions=%+v", url, env.stripe.sessions)
	}
	p := env.stripe.sessions[0]
	if p.AmountCents != 5000 || p.Currency != "pln" || p.RegistrationID != reg.ID {
		t.Fatalf("bad checkout params: %+v", p)
	}
	if !p.ExpiresAt.Equal(env.clock.Now().Add(PaymentWindow)) {
		t.Fatalf("session expiry should match the registration window, got %v", p.ExpiresAt)
	}

	// Second Pay reuses the live session — no new Stripe call.
	url2, err := env.svc.Pay(ctx, ev.ID, alice)
	if err != nil || url2 != url {
		t.Fatalf("want reused session url, got %q err=%v", url2, err)
	}
	if len(env.stripe.sessions) != 1 {
		t.Fatalf("no second session should be created, got %d", len(env.stripe.sessions))
	}
}

func TestPaySessionExpiryClampedToStripeMinimum(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	env.register(t, ev.ID, alice)

	// 10 minutes left on the registration — Stripe minimum is 30.
	env.clock.Advance(PaymentWindow - 10*time.Minute)
	if _, err := env.svc.Pay(ctx, ev.ID, alice); err != nil {
		t.Fatal(err)
	}
	want := env.clock.Now().Add(30 * time.Minute)
	if got := env.stripe.sessions[0].ExpiresAt; !got.Equal(want) {
		t.Fatalf("want clamped expiry %v, got %v", want, got)
	}
}

func TestPayValidation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.register(t, ev.ID, a)
	env.register(t, ev.ID, b) // waitlisted

	// Waitlisted can't pay.
	if _, err := env.svc.Pay(ctx, ev.ID, b); !errors.Is(err, ErrRegistrationNotPayable) {
		t.Fatalf("want ErrRegistrationNotPayable, got %v", err)
	}
	// No registration at all.
	nobody := env.seedUser(t, "nobody@test")
	if _, err := env.svc.Pay(ctx, ev.ID, nobody); !errors.Is(err, ErrRegistrationNotFound) {
		t.Fatalf("want ErrRegistrationNotFound, got %v", err)
	}
	// Expired window can't pay.
	env.clock.Advance(PaymentWindow + time.Minute)
	if _, err := env.svc.Pay(ctx, ev.ID, a); !errors.Is(err, ErrRegistrationNotPayable) {
		t.Fatalf("want ErrRegistrationNotPayable after expiry, got %v", err)
	}
}
```

- [ ] **Step 3: Run to verify FAIL** — `cd backend && go test ./internal/events/ -run TestPay -v -count=1` → `Pay` undefined.

- [ ] **Step 4: Implement Pay.** Append to `service.go`:

```go
// Pay returns a Stripe Checkout URL for the caller's pending_payment
// registration, creating the session on demand and reusing a live one.
// The session expiry is clamped to the registration's remaining window,
// respecting Stripe's 30-minute minimum.
func (s *Service) Pay(ctx context.Context, eventID, userID uuid.UUID) (string, error) {
	ev, err := s.queries.GetEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrEventNotFound
	}
	if err != nil {
		return "", err
	}
	reg, err := s.queries.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
		EventID: eventID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRegistrationNotFound
	}
	if err != nil {
		return "", err
	}
	now := s.now()
	if reg.Status != "pending_payment" || reg.ExpiresAt == nil || !reg.ExpiresAt.After(now) {
		return "", ErrRegistrationNotPayable
	}
	if reg.StripeCheckoutSessionUrl != nil && reg.StripeSessionExpiresAt != nil &&
		reg.StripeSessionExpiresAt.After(now) {
		return *reg.StripeCheckoutSessionUrl, nil
	}
	if !s.stripe.Configured() {
		return "", ErrPaymentsUnconfigured
	}
	sessionExpiry := *reg.ExpiresAt
	if min := now.Add(stripeMinSessionTTL); sessionExpiry.Before(min) {
		sessionExpiry = min
	}
	cs, err := s.stripe.CreateCheckoutSession(ctx, CheckoutParams{
		RegistrationID: reg.ID,
		EventName:      ev.Name,
		Currency:       ev.Currency,
		AmountCents:    int64(ev.FeeCents),
		ExpiresAt:      sessionExpiry,
		SuccessURL:     eventLink(s.baseURL, ev.ID) + "?checkout=success",
		CancelURL:      eventLink(s.baseURL, ev.ID) + "?checkout=cancelled",
	})
	if err != nil {
		return "", err
	}
	if err := s.queries.SetRegistrationCheckoutSession(ctx, db.SetRegistrationCheckoutSessionParams{
		ID: reg.ID, SessionID: &cs.ID, SessionUrl: &cs.URL, SessionExpiresAt: &cs.ExpiresAt,
	}); err != nil {
		return "", err
	}
	return cs.URL, nil
}
```

- [ ] **Step 5: Write the real Stripe client** `backend/internal/events/stripe_client.go`:

```go
package events

import (
	"context"
	"time"

	stripe "github.com/stripe/stripe-go/v86"
)

// NewStripeClient returns the real SDK-backed client, or the unconfigured
// stub when no secret key is set (dev without payments, free events only).
func NewStripeClient(secretKey string) StripeClient {
	if secretKey == "" {
		return unconfiguredStripe{}
	}
	return &stripeClient{sc: stripe.NewClient(secretKey)}
}

type stripeClient struct{ sc *stripe.Client }

func (c *stripeClient) Configured() bool { return true }

func (c *stripeClient) CreateCheckoutSession(ctx context.Context, p CheckoutParams) (*CheckoutSession, error) {
	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String(stripe.CheckoutSessionModePayment),
		SuccessURL:        stripe.String(p.SuccessURL),
		CancelURL:         stripe.String(p.CancelURL),
		ClientReferenceID: stripe.String(p.RegistrationID.String()),
		ExpiresAt:         stripe.Int64(p.ExpiresAt.Unix()),
		Metadata:          map[string]string{"registration_id": p.RegistrationID.String()},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionCreateLineItemPriceDataParams{
				Currency:   stripe.String(p.Currency),
				UnitAmount: stripe.Int64(p.AmountCents),
				ProductData: &stripe.CheckoutSessionCreateLineItemPriceDataProductDataParams{
					Name: stripe.String(p.EventName),
				},
			},
		}},
	}
	sess, err := c.sc.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return nil, err
	}
	return &CheckoutSession{ID: sess.ID, URL: sess.URL, ExpiresAt: time.Unix(sess.ExpiresAt, 0)}, nil
}

func (c *stripeClient) RefundPaymentIntent(ctx context.Context, paymentIntentID string) error {
	_, err := c.sc.V1Refunds.Create(ctx, &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(paymentIntentID),
	})
	return err
}
```

If any struct/field name differs in the installed major version, fix by compiler error — the semantic content (mode=payment, line item with product name + unit amount, expires_at, metadata, refund by payment intent) is fixed by the spec.

- [ ] **Step 6: Run all events tests** — `cd backend && go test ./internal/events/ -v -count=1` → ALL PASS.

- [ ] **Step 7: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/events/ && go mod tidy && cd ..
git add backend/internal/events backend/go.mod backend/go.sum
git commit -m "feat(events): Stripe Checkout client and Pay with session reuse + expiry clamp"
```

---

### Task 6: Refund paths, self-cancel, organizer queue, event cancel, webhook processing

All money movement. Stripe refund calls happen OUTSIDE row-lock transactions; state finalizes after a successful call; the `charge.refunded` webhook independently syncs state for dashboard-issued refunds, so a crash between "refund succeeded" and "row updated" self-heals.

**Files:**
- Modify: `backend/internal/events/service.go`
- Modify: `backend/internal/events/emails.go`
- Modify: `backend/internal/events/service_test.go`

**Interfaces:**
- Consumes: Tasks 3–5.
- Produces (used by Tasks 8–10):
  - `CancelRegistration(ctx, eventID, userID uuid.UUID) (*db.Registration, error)`
  - `OrganizerRefund(ctx, eventID, registrationID uuid.UUID) (*db.Registration, error)`
  - `DenyRefund(ctx, eventID, registrationID uuid.UUID) (*db.Registration, error)`
  - `HandleWebhookEvent(ctx, WebhookEvent) error` with `WebhookEvent{ID, Type, CheckoutSessionID, PaymentIntentID string}`
  - `Transition(…, "cancel")` becomes a mass-refunding, idempotently re-runnable action (`legalTransitions["cancel"]` gains `"cancelled"`)
  - email builders: `refundIssuedEmail`, `refundDeniedEmail`, `eventCancelledEmail`, `paymentAfterExpiryEmail`

- [ ] **Step 1: Write the failing tests.** Append to `service_test.go`:

```go
// paidRegistration drives a registration through register → Pay →
// checkout.session.completed, returning the paid row.
func (e *testEnv) paidRegistration(t *testing.T, eventID, userID uuid.UUID) *db.Registration {
	t.Helper()
	ctx := context.Background()
	reg := e.register(t, eventID, userID)
	if reg.Status != "pending_payment" {
		t.Fatalf("expected pending_payment before pay, got %s", reg.Status)
	}
	if _, err := e.svc.Pay(ctx, eventID, userID); err != nil {
		t.Fatal(err)
	}
	err := e.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID:   "evt_completed_" + reg.ID.String(),
		Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + reg.ID.String(),
		PaymentIntentID:   "pi_" + reg.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.q.GetRegistration(ctx, reg.ID)
	if err != nil || got.Status != "paid" {
		t.Fatalf("webhook should mark paid: %v %+v", err, got)
	}
	return &got
}

func TestWebhookCompletedIsIdempotent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	reg := env.paidRegistration(t, ev.ID, alice)

	mails := len(env.mailer.sent)
	// Redelivery of the same Stripe event id must no-op.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_completed_" + reg.ID.String(), Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + reg.ID.String(), PaymentIntentID: "pi_" + reg.ID.String(),
	})
	if err != nil || len(env.mailer.sent) != mails {
		t.Fatalf("duplicate delivery must no-op: %v (mails %d→%d)", err, mails, len(env.mailer.sent))
	}
}

func TestSelfCancelWaitlistedAndPending(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.register(t, ev.ID, a) // pending, holds the spot
	env.register(t, ev.ID, b) // waitlisted

	// Waitlisted cancel: no promotion side effects, no refund.
	got, err := env.svc.CancelRegistration(ctx, ev.ID, b)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("waitlist cancel: %v %+v", err, got)
	}
	// Re-waitlist b, then cancel the pending holder → b promotes.
	env.register(t, ev.ID, b)
	got, err = env.svc.CancelRegistration(ctx, ev.ID, a)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("pending cancel: %v %+v", err, got)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted after pending cancel: %v %+v", err, gotB)
	}
	if len(env.stripe.refunds) != 0 {
		t.Fatalf("no refunds expected, got %v", env.stripe.refunds)
	}
}

func TestSelfCancelPaidBeforeDeadlineRefunds(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b) // waitlisted

	got, err := env.svc.CancelRegistration(ctx, ev.ID, a)
	if err != nil || got.Status != "refunded" {
		t.Fatalf("in-window paid cancel: %v %+v", err, got)
	}
	if len(env.stripe.refunds) != 1 || env.stripe.refunds[0] != "pi_"+ra.ID.String() {
		t.Fatalf("want refund of a's payment intent, got %v", env.stripe.refunds)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
}

func TestSelfCancelPaidAfterDeadlineQueuesRefund(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	deadline := env.clock.Now().Add(time.Hour)
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{RefundDeadline: &deadline}); err != nil {
		t.Fatal(err)
	}
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b)

	env.clock.Advance(2 * time.Hour) // past the refund deadline, event not started

	got, err := env.svc.CancelRegistration(ctx, ev.ID, a)
	if err != nil || got.Status != "refund_requested" {
		t.Fatalf("post-deadline cancel must queue: %v %+v", err, got)
	}
	if len(env.stripe.refunds) != 0 {
		t.Fatalf("no automatic refund past deadline, got %v", env.stripe.refunds)
	}
	// Spot freed immediately: b promoted.
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
	// refund_requested blocks re-registering.
	if _, err := env.svc.Register(ctx, ev.ID, a); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("refund_requested must block re-register, got %v", err)
	}
}

func TestOrganizerRefundAndDeny(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 2)
	deadline := env.clock.Now().Add(time.Hour)
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{RefundDeadline: &deadline}); err != nil {
		t.Fatal(err)
	}
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	rb := env.paidRegistration(t, ev.ID, b)
	env.clock.Advance(2 * time.Hour)

	// Both self-cancel post-deadline → queue.
	if _, err := env.svc.CancelRegistration(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.CancelRegistration(ctx, ev.ID, b); err != nil {
		t.Fatal(err)
	}

	// Organizer approves a → refunded + refund email.
	got, err := env.svc.OrganizerRefund(ctx, ev.ID, ra.ID)
	if err != nil || got.Status != "refunded" {
		t.Fatalf("organizer refund: %v %+v", err, got)
	}
	if len(env.stripe.refunds) != 1 {
		t.Fatalf("want 1 refund, got %v", env.stripe.refunds)
	}
	// Organizer denies b → cancelled + denied email.
	got, err = env.svc.DenyRefund(ctx, ev.ID, rb.ID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("deny refund: %v %+v", err, got)
	}
	deniedMail := env.mailer.sent[len(env.mailer.sent)-1]
	if deniedMail.to != "b@test" {
		t.Fatalf("denied email should go to b, got %+v", deniedMail)
	}
	// Deny on a non-queued row is invalid.
	if _, err := env.svc.DenyRefund(ctx, ev.ID, ra.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("deny on refunded row must fail, got %v", err)
	}
}

func TestOrganizerKickRefundsPaid(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b) // waitlisted

	got, err := env.svc.OrganizerRefund(ctx, ev.ID, ra.ID)
	if err != nil || got.Status != "refunded" {
		t.Fatalf("kick+refund: %v %+v", err, got)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted after kick: %v %+v", err, gotB)
	}
}

func TestEventCancelMassRefundsAndRetries(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	c := env.seedUser(t, "c@test")
	env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b) // pending_payment
	env.register(t, ev.ID, c) // waitlisted

	// First cancel: the refund call fails → a's row stays paid, retryable.
	env.stripe.refundErr = errors.New("stripe is down")
	got, err := env.svc.Transition(ctx, ev.ID, "cancel")
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("cancel: %v %+v", err, got)
	}
	rows, err := env.q.ListEventRegistrations(ctx, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	statusByUser := map[uuid.UUID]string{}
	for _, r := range rows {
		statusByUser[r.UserID] = r.Status
	}
	if statusByUser[b] != "cancelled" || statusByUser[c] != "cancelled" {
		t.Fatalf("pending+waitlisted must be cancelled: %+v", statusByUser)
	}
	if statusByUser[a] != "paid" {
		t.Fatalf("failed refund must leave the row untouched: %+v", statusByUser)
	}

	// Retry: cancel again with Stripe healthy → a refunded.
	env.stripe.refundErr = nil
	if _, err := env.svc.Transition(ctx, ev.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	rows, _ = env.q.ListEventRegistrations(ctx, ev.ID)
	for _, r := range rows {
		if r.UserID == a && r.Status != "refunded" {
			t.Fatalf("retry must refund a, got %s", r.Status)
		}
	}
	if len(env.stripe.refunds) != 1 {
		t.Fatalf("want exactly one successful refund, got %v", env.stripe.refunds)
	}
}

func TestWebhookExpiredPromotesEarly(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	env.register(t, ev.ID, b)

	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_expired_1", Type: "checkout.session.expired",
		CheckoutSessionID: "cs_test_" + ra.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "expired" {
		t.Fatalf("session expiry must expire the registration, got %s", gotA.Status)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
}

func TestWebhookLatePaymentRefundsWhenFull(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	env.register(t, ev.ID, b)

	// a's registration expires; b promotes and pays — the event is full again.
	env.clock.Advance(PaymentWindow + 25*time.Hour) // past a's session expiry too
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	env.paidRegistration(t, ev.ID, b)

	// a's payment lands anyway → immediate refund + apology email.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_late_1", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + ra.ID.String(), PaymentIntentID: "pi_late",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "refunded" {
		t.Fatalf("late payment with no spot must refund, got %s", gotA.Status)
	}
	if len(env.stripe.refunds) != 1 || env.stripe.refunds[0] != "pi_late" {
		t.Fatalf("want pi_late refunded, got %v", env.stripe.refunds)
	}
}

func TestWebhookLatePaymentReclaimsFreeSpot(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	ra := env.register(t, ev.ID, a)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	env.clock.Advance(PaymentWindow + 25*time.Hour)
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	// No waitlist — the spot is still free when the late payment lands.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_late_2", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + ra.ID.String(), PaymentIntentID: "pi_late2",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "paid" {
		t.Fatalf("late payment with a free spot must reclaim it, got %s", gotA.Status)
	}
}

func TestWebhookChargeRefundedSyncsDashboardRefunds(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b)

	// Organizer refunds from the Stripe dashboard: only the webhook arrives.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_refund_1", Type: "charge.refunded",
		PaymentIntentID: "pi_" + ra.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "refunded" {
		t.Fatalf("dashboard refund must sync, got %s", gotA.Status)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
}
```

- [ ] **Step 2: Run to verify FAIL** — `cd backend && go test ./internal/events/ -run 'TestSelfCancel|TestOrganizer|TestEventCancel|TestWebhook' -v -count=1` → undefined methods.

- [ ] **Step 3: Implement self-cancel + organizer actions.** Append to `service.go`:

```go
// refundDeadlineFor: explicit deadline, else the event start.
func refundDeadlineFor(ev db.Event) time.Time {
	if ev.RefundDeadline != nil {
		return *ev.RefundDeadline
	}
	return ev.StartsAt
}

// CancelRegistration is self-cancel. Cancelling always frees the spot
// (waitlist promotion is never blocked); only the MONEY depends on the
// deadline: in-window paid → automatic refund; past it → refund_requested
// (organizer decides). Free-event rows never touch Stripe.
func (s *Service) CancelRegistration(ctx context.Context, eventID, userID uuid.UUID) (*db.Registration, error) {
	var out db.Registration
	var emails []pendingEmail
	refundIntent := ""
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		if ev.Status == "draft" {
			return ErrEventNotFound
		}
		if ev.Status != "published" {
			return ErrRegistrationClosed
		}
		reg, err := qtx.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
			EventID: eventID, UserID: userID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRegistrationNotFound
		}
		if err != nil {
			return err
		}
		switch reg.Status {
		case "waitlisted":
			out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
				ID: reg.ID, Status: "cancelled",
			})
			return err
		case "pending_payment":
			out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
				ID: reg.ID, Status: "cancelled",
			})
			if err != nil {
				return err
			}
			emails, err = s.promoteLocked(ctx, qtx, ev)
			return err
		case "paid":
			switch {
			case reg.StripePaymentIntentID == nil:
				// Free event: nothing to refund.
				out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
					ID: reg.ID, Status: "cancelled",
				})
			case s.now().Before(refundDeadlineFor(ev)):
				// Refund first, finalize after the call succeeds (below).
				refundIntent = *reg.StripePaymentIntentID
				out = reg
				return nil
			default:
				out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
					ID: reg.ID, Status: "refund_requested",
				})
			}
			if err != nil {
				return err
			}
			more, err := s.promoteLocked(ctx, qtx, ev)
			emails = append(emails, more...)
			return err
		default:
			return ErrRegistrationNotFound
		}
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	if refundIntent != "" {
		return s.refundRegistration(ctx, out.ID, refundIntent, false)
	}
	return &out, nil
}

// refundRegistration calls Stripe, then finalizes the row to 'refunded'
// under the event lock, promoting if the row still held a spot. Races
// with the charge.refunded webhook are resolved by the status check.
// apology selects the late-payment email instead of the plain refund one.
func (s *Service) refundRegistration(ctx context.Context, regID uuid.UUID, paymentIntentID string, apology bool) (*db.Registration, error) {
	if err := s.stripe.RefundPaymentIntent(ctx, paymentIntentID); err != nil {
		return nil, err
	}
	var out db.Registration
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		reg, err := qtx.GetRegistration(ctx, regID)
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		reg, err = qtx.GetRegistration(ctx, regID) // re-read under the lock
		if err != nil {
			return err
		}
		if reg.Status == "refunded" {
			out = reg
			return nil
		}
		wasHolding := reg.Status == "paid" || reg.Status == "pending_payment"
		out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: regID, Status: "refunded",
		})
		if err != nil {
			return err
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		if apology {
			emails = append(emails, paymentAfterExpiryEmail(u, ev, s.baseURL))
		} else {
			emails = append(emails, refundIssuedEmail(u, ev, s.baseURL))
		}
		if wasHolding {
			more, err := s.promoteLocked(ctx, qtx, ev)
			if err != nil {
				return err
			}
			emails = append(emails, more...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	return &out, nil
}

// OrganizerRefund resolves a refund_requested queue entry, or
// kick-refunds a paid registration.
func (s *Service) OrganizerRefund(ctx context.Context, eventID, registrationID uuid.UUID) (*db.Registration, error) {
	reg, err := s.queries.GetRegistration(ctx, registrationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && reg.EventID != eventID) {
		return nil, ErrRegistrationNotFound
	}
	if err != nil {
		return nil, err
	}
	if reg.Status != "refund_requested" && reg.Status != "paid" {
		return nil, fmt.Errorf("%w: refund on %s registration", ErrInvalidTransition, reg.Status)
	}
	if reg.StripePaymentIntentID == nil {
		return nil, fmt.Errorf("%w: nothing was paid", ErrInvalidTransition)
	}
	return s.refundRegistration(ctx, registrationID, *reg.StripePaymentIntentID, false)
}

// DenyRefund resolves a refund_requested entry as denied: the row is
// cancelled, the money stays.
func (s *Service) DenyRefund(ctx context.Context, eventID, registrationID uuid.UUID) (*db.Registration, error) {
	var out db.Registration
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		reg, err := qtx.GetRegistration(ctx, registrationID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && reg.EventID != eventID) {
			return ErrRegistrationNotFound
		}
		if err != nil {
			return err
		}
		if reg.Status != "refund_requested" {
			return fmt.Errorf("%w: deny-refund on %s registration", ErrInvalidTransition, reg.Status)
		}
		ev, err := qtx.GetEvent(ctx, reg.EventID)
		if err != nil {
			return err
		}
		out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: registrationID, Status: "cancelled",
		})
		if err != nil {
			return err
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		emails = append(emails, refundDeniedEmail(u, ev, s.baseURL))
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	return &out, nil
}
```

- [ ] **Step 4: Implement event cancel.** In `service.go`, change `legalTransitions` (`cancel` becomes idempotently re-runnable for refund retries):

```go
var legalTransitions = map[string][]string{
	"publish": {"draft"},
	"start":   {"published"},
	"finish":  {"started"},
	// cancelled → cancel is a refund-retry no-op re-run (spec §3).
	"cancel": {"published", "started", "cancelled"},
}
```

Replace the `cancelEventLocked` stub with (and adjust `Transition`'s cancel branch to collect + post-process):

```go
// cancelEventLocked transitions every no-money row to cancelled and
// returns the rows that need a Stripe refund (processed AFTER commit —
// no IO under the event lock).
func (s *Service) cancelEventLocked(ctx context.Context, qtx *db.Queries, ev db.Event) ([]pendingEmail, []db.ListActiveRegistrationsForEventRow, error) {
	rows, err := qtx.ListActiveRegistrationsForEvent(ctx, ev.ID)
	if err != nil {
		return nil, nil, err
	}
	var emails []pendingEmail
	var refunds []db.ListActiveRegistrationsForEventRow
	for _, r := range rows {
		if (r.Status == "paid" || r.Status == "refund_requested") && r.StripePaymentIntentID != nil {
			refunds = append(refunds, r)
			continue
		}
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: r.ID, Status: "cancelled",
		}); err != nil {
			return nil, nil, err
		}
		u := db.User{Email: r.Email, DisplayName: r.DisplayName}
		emails = append(emails, eventCancelledEmail(u, ev, false, s.baseURL))
	}
	return emails, refunds, nil
}

// refundCancelledEventRows runs after the cancel transaction commits.
// A failed Stripe call leaves that row untouched; re-running the cancel
// action retries exactly the leftovers.
func (s *Service) refundCancelledEventRows(ctx context.Context, ev db.Event, rows []db.ListActiveRegistrationsForEventRow) {
	for _, r := range rows {
		if err := s.stripe.RefundPaymentIntent(ctx, *r.StripePaymentIntentID); err != nil {
			s.log.Error("events: cancel refund failed, re-run cancel to retry",
				"registration", r.ID, "error", err)
			continue
		}
		err := s.withTx(ctx, func(qtx *db.Queries) error {
			_, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
				ID: r.ID, Status: "refunded",
			})
			return err
		})
		if err != nil {
			s.log.Error("events: mark refunded after cancel", "registration", r.ID, "error", err)
			continue
		}
		u := db.User{Email: r.Email, DisplayName: r.DisplayName}
		s.sendEmails(ctx, []pendingEmail{eventCancelledEmail(u, ev, true, s.baseURL)})
	}
}
```

In `Transition`, the cancel branch becomes:

```go
		var refundRows []db.ListActiveRegistrationsForEventRow
		// (declare next to `emails` before the withTx closure)
		if action == "cancel" {
			emails, refundRows, err = s.cancelEventLocked(ctx, qtx, ev)
			if err != nil {
				return err
			}
		}
```

and after `s.sendEmails(ctx, emails)`:

```go
	if action == "cancel" {
		s.refundCancelledEventRows(ctx, out, refundRows)
	}
```

- [ ] **Step 5: Implement webhook processing.** Append to `service.go`:

```go
// WebhookEvent is the minimal, SDK-independent shape the chi handler
// extracts from a verified Stripe event.
type WebhookEvent struct {
	ID                string
	Type              string
	CheckoutSessionID string
	PaymentIntentID   string
}

// errAlreadySeen: duplicate delivery — acknowledged, never an error.
var errAlreadySeen = errors.New("stripe event already processed")

// HandleWebhookEvent processes a signature-verified Stripe event
// idempotently (stripe_events insert-or-skip in the same transaction as
// the state change). Unknown types are recorded and acknowledged.
func (s *Service) HandleWebhookEvent(ctx context.Context, we WebhookEvent) error {
	var err error
	switch we.Type {
	case "checkout.session.completed":
		err = s.handleCheckoutCompleted(ctx, we)
	case "checkout.session.expired":
		err = s.handleCheckoutExpired(ctx, we)
	case "charge.refunded":
		err = s.handleChargeRefunded(ctx, we)
	default:
		err = s.withTx(ctx, func(qtx *db.Queries) error {
			_, e := qtx.InsertStripeEvent(ctx, db.InsertStripeEventParams{
				StripeEventID: we.ID, Type: we.Type,
			})
			return e
		})
	}
	if errors.Is(err, errAlreadySeen) {
		return nil
	}
	return err
}

func markSeen(ctx context.Context, qtx *db.Queries, we WebhookEvent) error {
	n, err := qtx.InsertStripeEvent(ctx, db.InsertStripeEventParams{
		StripeEventID: we.ID, Type: we.Type,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errAlreadySeen
	}
	return nil
}

func (s *Service) handleCheckoutCompleted(ctx context.Context, we WebhookEvent) error {
	var emails []pendingEmail
	lateRefund := ""
	var lateRegID uuid.UUID
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		if err := markSeen(ctx, qtx, we); err != nil {
			return err
		}
		reg, err := qtx.GetRegistrationByCheckoutSession(ctx, we.CheckoutSessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			s.log.Warn("stripe webhook: unknown checkout session", "session", we.CheckoutSessionID)
			return nil // acknowledge; nothing to do
		}
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		reg, err = qtx.GetRegistration(ctx, reg.ID) // re-read under the lock
		if err != nil {
			return err
		}
		// Always record the intent so dashboard refunds stay traceable.
		if we.PaymentIntentID != "" {
			if err := qtx.SetRegistrationPaymentIntent(ctx, db.SetRegistrationPaymentIntentParams{
				ID: reg.ID, PaymentIntentID: &we.PaymentIntentID,
			}); err != nil {
				return err
			}
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		switch reg.Status {
		case "pending_payment":
			pi := we.PaymentIntentID
			if _, err := qtx.MarkRegistrationPaid(ctx, db.MarkRegistrationPaidParams{
				ID: reg.ID, PaidAt: s.now(), PaymentIntentID: &pi,
			}); err != nil {
				return err
			}
			emails = append(emails, confirmationEmail(u, ev, s.baseURL))
			return nil
		case "paid":
			return nil // redundant delivery of a different event id
		default:
			// Late payment: the registration already expired/cancelled.
			occupied, err := qtx.CountOccupiedSpots(ctx, ev.ID)
			if err != nil {
				return err
			}
			if ev.Status == "published" && occupied < int64(ev.MaxParticipants) {
				pi := we.PaymentIntentID
				if _, err := qtx.MarkRegistrationPaid(ctx, db.MarkRegistrationPaidParams{
					ID: reg.ID, PaidAt: s.now(), PaymentIntentID: &pi,
				}); err != nil {
					return err
				}
				emails = append(emails, confirmationEmail(u, ev, s.baseURL))
				return nil
			}
			lateRefund = we.PaymentIntentID
			lateRegID = reg.ID
			return nil
		}
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	if lateRefund != "" {
		if _, err := s.refundRegistration(ctx, lateRegID, lateRefund, true); err != nil {
			// The charge stays; a dashboard refund + charge.refunded webhook
			// will sync state. Log loudly.
			s.log.Error("events: late-payment auto-refund failed", "registration", lateRegID, "error", err)
		}
	}
	return nil
}

func (s *Service) handleCheckoutExpired(ctx context.Context, we WebhookEvent) error {
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		if err := markSeen(ctx, qtx, we); err != nil {
			return err
		}
		reg, err := qtx.GetRegistrationByCheckoutSession(ctx, we.CheckoutSessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		reg, err = qtx.GetRegistration(ctx, reg.ID)
		if err != nil {
			return err
		}
		if reg.Status != "pending_payment" {
			return nil
		}
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: reg.ID, Status: "expired",
		}); err != nil {
			return err
		}
		emails, err = s.promoteLocked(ctx, qtx, ev)
		return err
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	return nil
}

func (s *Service) handleChargeRefunded(ctx context.Context, we WebhookEvent) error {
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		if err := markSeen(ctx, qtx, we); err != nil {
			return err
		}
		reg, err := qtx.GetRegistrationByPaymentIntent(ctx, &we.PaymentIntentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		reg, err = qtx.GetRegistration(ctx, reg.ID)
		if err != nil {
			return err
		}
		if reg.Status == "refunded" {
			return nil // we issued this refund ourselves
		}
		wasHolding := reg.Status == "paid" || reg.Status == "pending_payment"
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: reg.ID, Status: "refunded",
		}); err != nil {
			return err
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		emails = append(emails, refundIssuedEmail(u, ev, s.baseURL))
		if wasHolding {
			more, err := s.promoteLocked(ctx, qtx, ev)
			if err != nil {
				return err
			}
			emails = append(emails, more...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	return nil
}
```

Note: `GetRegistrationByPaymentIntent` takes `*string` if sqlc generated the nullable-column param as a pointer — match whatever `events.sql.go` generated.

- [ ] **Step 6: Add the remaining email builders.** Append to `emails.go`:

```go
func refundIssuedEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Zwrot środków / Refund issued: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nZwróciliśmy Twoją opłatę za %s. Środki wrócą na Twoją kartę w ciągu kilku dni.\n\n---\n\nHi %s,\n\nYour fee for %s has been refunded. The money should reach your card within a few days.",
			u.DisplayName, ev.Name, u.DisplayName, ev.Name),
	}
}

func refundDeniedEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Odmowa zwrotu / Refund denied: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nOrganizator odrzucił zwrot opłaty za %s (rezygnacja po terminie).\n\n---\n\nHi %s,\n\nThe organizer declined the refund for %s (cancellation after the deadline).",
			u.DisplayName, ev.Name, u.DisplayName, ev.Name),
	}
}

func eventCancelledEmail(u db.User, ev db.Event, refunded bool, baseURL string) pendingEmail {
	refundNotePL, refundNoteEN := "", ""
	if refunded {
		refundNotePL = " Twoja opłata została zwrócona."
		refundNoteEN = " Your fee has been refunded."
	}
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Wydarzenie odwołane / Event cancelled: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nWydarzenie %s zostało odwołane.%s\n\n---\n\nHi %s,\n\nThe event %s has been cancelled.%s",
			u.DisplayName, ev.Name, refundNotePL, u.DisplayName, ev.Name, refundNoteEN),
	}
}

func paymentAfterExpiryEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Płatność po terminie — zwrot / Late payment refunded: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nTwoja płatność za %s dotarła po wygaśnięciu rezerwacji, a miejsce zostało już zajęte. Zwróciliśmy pełną kwotę.\n\n---\n\nHi %s,\n\nYour payment for %s arrived after your reservation expired and the spot was already taken. We refunded the full amount.",
			u.DisplayName, ev.Name, u.DisplayName, ev.Name),
	}
}
```

- [ ] **Step 7: Run the full events suite** — `cd backend && go test ./internal/events/ -v -count=1` → ALL PASS.

- [ ] **Step 8: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/events/ && cd ..
git add backend/internal/events
git commit -m "feat(events): refunds, organizer queue, event cancel, idempotent Stripe webhooks"
```

---

### Task 7: Sweeper (1-minute in-process ticker)

**Files:**
- Create: `backend/internal/events/scheduler.go`
- Create: `backend/internal/events/scheduler_test.go`

**Interfaces:**
- Consumes: `expireAndPromote` (Task 4), `ListEventIDsWithOverduePending` (Task 2).
- Produces: `DefaultSweepInterval = time.Minute`; `(s *Service) RunSweeper(ctx, interval)` (blocks; run in a goroutine — Task 11 wires it in main.go); `(s *Service) Sweep(ctx) error` (one pass, exported for tests).

- [ ] **Step 1: Write the failing tests** `backend/internal/events/scheduler_test.go`:

```go
package events

import (
	"context"
	"testing"
	"time"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

func TestSweepExpiresAcrossEvents(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev1 := env.createEvent(t, org, 5000, 1)
	ev2 := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev1.ID)
	env.publish(t, ev2.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	r1 := env.register(t, ev1.ID, a)
	r2 := env.register(t, ev2.ID, b)

	env.clock.Advance(PaymentWindow + time.Minute)
	if err := env.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{r1.ID, r2.ID} {
		got, err := env.q.GetRegistration(ctx, id)
		if err != nil || got.Status != "expired" {
			t.Fatalf("sweep must expire overdue rows in every event: %v %+v", err, got)
		}
	}
}

func TestSweepSkipsLiveCheckoutSessions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	ra := env.register(t, ev.ID, a)

	// Pay with 10 minutes left: the session gets the 30-minute Stripe
	// minimum, outliving the registration window.
	env.clock.Advance(PaymentWindow - 10*time.Minute)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}

	// 15 minutes later the registration window is past but the session
	// is live → the sweeper must NOT expire (the webhook owns this row).
	env.clock.Advance(15 * time.Minute)
	if err := env.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := env.q.GetRegistration(ctx, ra.ID)
	if got.Status != "pending_payment" {
		t.Fatalf("sweep must skip live checkout sessions, got %s", got.Status)
	}

	// Once the session is dead too, the sweeper expires the row.
	env.clock.Advance(20 * time.Minute)
	if err := env.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = env.q.GetRegistration(ctx, ra.ID)
	if got.Status != "expired" {
		t.Fatalf("sweep must expire once the session died, got %s", got.Status)
	}
}

func TestRunSweeperStopsOnCancel(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		env.svc.RunSweeper(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunSweeper did not stop on context cancel")
	}
	_ = db.Queries{} // keep the db import if otherwise unused
}
```

(Adjust imports: `github.com/google/uuid` is needed; drop the `db` underscore trick if the import is genuinely used.)

- [ ] **Step 2: Run to verify FAIL** — `cd backend && go test ./internal/events/ -run TestSweep -v -count=1` → `Sweep` undefined.

- [ ] **Step 3: Implement** `backend/internal/events/scheduler.go`:

```go
package events

import (
	"context"
	"errors"
	"time"
)

// DefaultSweepInterval: the payment window is 24h, so minute-level
// resolution is far more than enough.
const DefaultSweepInterval = time.Minute

// RunSweeper expires overdue pending_payment registrations and promotes
// waitlists on a ticker. Blocks until ctx is cancelled — run in a
// goroutine (same lifecycle as cards.RunScheduler).
func (s *Service) RunSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("events sweep failed", "error", err)
			}
		}
	}
}

// Sweep runs one pass over every event with overdue pending rows. Each
// event is its own transaction; one failure doesn't block the others.
func (s *Service) Sweep(ctx context.Context) error {
	ids, err := s.queries.ListEventIDsWithOverduePending(ctx, s.now())
	if err != nil {
		return err
	}
	var firstErr error
	for _, id := range ids {
		if err := s.expireAndPromote(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
```

- [ ] **Step 4: Run** — `cd backend && go test ./internal/events/ -v -count=1` → ALL PASS.

- [ ] **Step 5: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/events/ && cd ..
git add backend/internal/events
git commit -m "feat(events): 1-minute sweeper for expiry and waitlist promotion"
```

---

### Task 8: User-facing huma endpoints

**Files:**
- Create: `backend/internal/platform/httpapi/events.go`
- Create: `backend/internal/platform/httpapi/events_endpoints_test.go`
- Modify: `backend/internal/platform/httpapi/api.go` (Deps + registration)

**Interfaces:**
- Consumes: `events.Service` (Tasks 3–6).
- Produces:
  - `Deps.Events *events.Service`
  - operations `listEvents` (GET `/api/events`), `getEvent` (GET `/api/events/{eventId}`), `registerForEvent` (POST `/api/events/{eventId}/register`), `payRegistration` (POST `/api/events/{eventId}/registration/pay`), `cancelMyRegistration` (DELETE `/api/events/{eventId}/registration`)
  - wire types `EventSummary`, `EventDetailBody`, `EventCubeEntry`, `RegistrationInfo` (exported — they name the generated TS types)
  - helpers `isAdmin(ctx, deps) bool`, `mapEventErr(err) error`, `parseEventID(raw) (uuid.UUID, error)` — reused by Tasks 9–10

- [ ] **Step 1: Register the module.** In `api.go`, add to `Deps`:

```go
	Events      *events.Service
```

(+ import `"github.com/mjabloniec/cube-planner/backend/internal/events"`), and in `Build` after `registerCollections(api, deps)`:

```go
	registerEvents(api, deps)
```

- [ ] **Step 2: Write wire types + user endpoints** `backend/internal/platform/httpapi/events.go`:

```go
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/events"
)

// Wire types are exported: huma derives OpenAPI schema names (and the
// generated TS type names) from Go type names.

type RegistrationInfo struct {
	ID          uuid.UUID  `json:"id"`
	Status      string     `json:"status" enum:"pending_payment,paid,waitlisted,cancelled,refund_requested,refunded,expired"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	WaitlistPos *int64     `json:"waitlistPos,omitempty"`
	PaidAt      *time.Time `json:"paidAt,omitempty"`
}

func registrationInfoFrom(r db.Registration) RegistrationInfo {
	return RegistrationInfo{
		ID: r.ID, Status: r.Status, ExpiresAt: r.ExpiresAt,
		WaitlistPos: r.WaitlistPos, PaidAt: r.PaidAt,
	}
}

type EventSummary struct {
	ID                   uuid.UUID  `json:"id"`
	Name                 string     `json:"name"`
	StartsAt             time.Time  `json:"startsAt"`
	Location             string     `json:"location"`
	FeeCents             int32      `json:"feeCents"`
	Currency             string     `json:"currency"`
	MaxParticipants      int32      `json:"maxParticipants"`
	PaidCount            int32      `json:"paidCount"`
	PendingCount         int32      `json:"pendingCount"`
	WaitlistCount        int32      `json:"waitlistCount"`
	Status               string     `json:"status" enum:"draft,published,started,finished,cancelled"`
	MyRegistrationStatus *string    `json:"myRegistrationStatus,omitempty"`
}

func eventSummaryFrom(e events.EventInfo) EventSummary {
	return EventSummary{
		ID: e.Event.ID, Name: e.Event.Name, StartsAt: e.Event.StartsAt,
		Location: e.Event.Location, FeeCents: e.Event.FeeCents,
		Currency: e.Event.Currency, MaxParticipants: e.Event.MaxParticipants,
		PaidCount: e.PaidCount, PendingCount: e.PendingCount,
		WaitlistCount: e.WaitlistCount, Status: e.Event.Status,
		MyRegistrationStatus: e.MyStatus,
	}
}

type EventCubeEntry struct {
	CubeID        uuid.UUID  `json:"cubeId"`
	CubeName      string     `json:"cubeName"`
	CubeChangeID  *uuid.UUID `json:"cubeChangeId,omitempty"`
	PinnedVersion *int32     `json:"pinnedVersion,omitempty"`
	PinnedAt      *time.Time `json:"pinnedAt,omitempty"`
}

type EventDetailBody struct {
	EventSummary
	Description    string            `json:"description"`
	RefundDeadline *time.Time        `json:"refundDeadline,omitempty"`
	OrganizerName  string            `json:"organizerName"`
	Cubes          []EventCubeEntry  `json:"cubes"`
	Attendees      []string          `json:"attendees"`
	MyRegistration *RegistrationInfo `json:"myRegistration,omitempty"`
}

func eventProblem(status int, ptype, detail string) error {
	return &huma.ErrorModel{
		Status: status, Type: ptype, Title: http.StatusText(status), Detail: detail,
	}
}

func mapEventErr(err error) error {
	switch {
	case errors.Is(err, events.ErrEventNotFound):
		return huma.Error404NotFound("event not found")
	case errors.Is(err, events.ErrRegistrationNotFound):
		return eventProblem(http.StatusNotFound, "registration-not-found", "no active registration")
	case errors.Is(err, events.ErrAlreadyRegistered):
		return eventProblem(http.StatusConflict, "already-registered", err.Error())
	case errors.Is(err, events.ErrRegistrationClosed):
		return eventProblem(http.StatusConflict, "event-registration-closed", err.Error())
	case errors.Is(err, events.ErrRegistrationNotPayable):
		return eventProblem(http.StatusConflict, "registration-not-payable", err.Error())
	case errors.Is(err, events.ErrInvalidTransition):
		return eventProblem(http.StatusConflict, "invalid-event-transition", err.Error())
	case errors.Is(err, events.ErrEventLocked):
		return eventProblem(http.StatusConflict, "event-locked", err.Error())
	case errors.Is(err, events.ErrCubesLocked):
		return eventProblem(http.StatusConflict, "event-cubes-locked", err.Error())
	case errors.Is(err, events.ErrInvalidEventCube):
		return eventProblem(http.StatusUnprocessableEntity, "invalid-event-cube", err.Error())
	case errors.Is(err, events.ErrPaymentsUnconfigured):
		return eventProblem(http.StatusServiceUnavailable, "payments-unconfigured", "payments are not configured")
	default:
		return err
	}
}

func parseEventID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, huma.Error404NotFound("event not found")
	}
	return id, nil
}

// isAdmin resolves the session user's role. Missing session or lookup
// failure simply reads as non-admin.
func isAdmin(ctx context.Context, deps Deps) bool {
	uid, ok := CurrentUserID(ctx)
	if !ok {
		return false
	}
	u, err := deps.Queries.GetUserByID(ctx, uid)
	return err == nil && u.Role == "admin"
}

type eventIDInput struct {
	EventID string `path:"eventId"`
}

type listEventsOutput struct {
	Body struct {
		Events []EventSummary `json:"events"`
	}
}

type eventDetailOutput struct {
	Body EventDetailBody
}

type registrationOutput struct {
	Body RegistrationInfo
}

type payOutput struct {
	Body struct {
		CheckoutUrl string `json:"checkoutUrl"`
	}
}

func (deps Deps) eventDetailBody(ctx context.Context, eventID, callerID uuid.UUID, admin bool) (*EventDetailBody, error) {
	d, err := deps.Events.GetDetail(ctx, eventID, callerID, admin)
	if err != nil {
		return nil, mapEventErr(err)
	}
	organizer, err := deps.Queries.GetUserByID(ctx, d.Event.OrganizerID)
	if err != nil {
		return nil, err
	}
	body := &EventDetailBody{
		EventSummary:   eventSummaryFrom(d.EventInfo),
		Description:    d.Event.Description,
		RefundDeadline: d.Event.RefundDeadline,
		OrganizerName:  organizer.DisplayName,
		Cubes:          make([]EventCubeEntry, len(d.Cubes)),
		Attendees:      d.Attendees,
	}
	for i, c := range d.Cubes {
		body.Cubes[i] = EventCubeEntry{
			CubeID: c.CubeID, CubeName: c.CubeName, CubeChangeID: c.CubeChangeID,
			PinnedVersion: c.PinnedVersion, PinnedAt: c.PinnedAt,
		}
	}
	if d.MyRegistration != nil {
		info := registrationInfoFrom(*d.MyRegistration)
		body.MyRegistration = &info
	}
	return body, nil
}

func registerEvents(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "listEvents",
		Method:      http.MethodGet,
		Path:        "/api/events",
		Summary:     "All visible events (admins also see drafts)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, _ *struct{}) (*listEventsOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		infos, err := deps.Events.List(ctx, uid, isAdmin(ctx, deps))
		if err != nil {
			return nil, mapEventErr(err)
		}
		out := &listEventsOutput{}
		out.Body.Events = make([]EventSummary, len(infos))
		for i, e := range infos {
			out.Body.Events[i] = eventSummaryFrom(e)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getEvent",
		Method:      http.MethodGet,
		Path:        "/api/events/{eventId}",
		Summary:     "Event detail with cubes, attendees, and the caller's registration",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*eventDetailOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		body, err := deps.eventDetailBody(ctx, id, uid, isAdmin(ctx, deps))
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "registerForEvent",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/register",
		Summary:     "Register (or join the waitlist when full)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*registrationOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		reg, err := deps.Events.Register(ctx, id, uid)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "payRegistration",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/registration/pay",
		Summary:     "Get a Stripe Checkout URL for the pending registration",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*payOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		url, err := deps.Events.Pay(ctx, id, uid)
		if err != nil {
			return nil, mapEventErr(err)
		}
		out := &payOutput{}
		out.Body.CheckoutUrl = url
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancelMyRegistration",
		Method:      http.MethodDelete,
		Path:        "/api/events/{eventId}/registration",
		Summary:     "Cancel own registration (refund policy applies)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*registrationOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		reg, err := deps.Events.CancelRegistration(ctx, id, uid)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})
}
```

(Admin operations are appended to `registerEvents` in Task 9.)

- [ ] **Step 3: Write the integration tests** `backend/internal/platform/httpapi/events_endpoints_test.go`:

```go
package httpapi_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/events"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// endpointFakeStripe implements events.StripeClient for endpoint tests.
type endpointFakeStripe struct {
	mu       sync.Mutex
	sessions []events.CheckoutParams
	refunds  []string
}

func (f *endpointFakeStripe) Configured() bool { return true }

func (f *endpointFakeStripe) CreateCheckoutSession(_ context.Context, p events.CheckoutParams) (*events.CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, p)
	return &events.CheckoutSession{
		ID:  "cs_test_" + p.RegistrationID.String(),
		URL: "https://checkout.stripe.test/" + p.RegistrationID.String(),
		ExpiresAt: p.ExpiresAt,
	}, nil
}

func (f *endpointFakeStripe) RefundPaymentIntent(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunds = append(f.refunds, id)
	return nil
}

func newEventsServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries, *events.Service, *endpointFakeStripe) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	fake := &endpointFakeStripe{}
	svc := events.NewService(q, pool, fake, noopMailer{}, "http://test", slog.Default())
	deps := httpapi.Deps{
		Auth:     auth.NewService(q, noopMailer{}, "http://test"),
		Sessions: auth.NewSessions(q, false),
		Queries:  q,
		Events:   svc,
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q, svc, fake
}

func makeAdmin(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`update users set role = 'admin' where email = $1`, email); err != nil {
		t.Fatal(err)
	}
}

// seedPublishedEvent creates + publishes an event directly via the service.
func seedPublishedEvent(t *testing.T, pool *pgxpool.Pool, svc *events.Service, feeCents, maxParticipants int32) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var orgID uuid.UUID
	err := pool.QueryRow(ctx,
		`insert into users (email, display_name, email_verified_at, role)
		 values ('organizer+' || gen_random_uuid()::text || '@test', 'Organizer', now(), 'admin')
		 returning id`).Scan(&orgID)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := svc.Create(ctx, orgID, events.CreateEventParams{
		Name: "Cube Night", StartsAt: time.Now().Add(7 * 24 * time.Hour),
		FeeCents: feeCents, MaxParticipants: maxParticipants,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, ev.ID, "publish"); err != nil {
		t.Fatal(err)
	}
	return ev.ID
}

type registrationInfoBody struct {
	ID          string  `json:"id"`
	Status      string  `json:"status"`
	WaitlistPos *int64  `json:"waitlistPos"`
}

func TestEventsRequireAuth(t *testing.T) {
	srv, _, _, _, _ := newEventsServer(t)
	c := newCookieClient(t, srv)
	for _, probe := range []struct{ method, path string }{
		{"GET", "/api/events"},
		{"GET", "/api/events/" + uuid.NewString()},
		{"POST", "/api/events/" + uuid.NewString() + "/register"},
		{"POST", "/api/events/" + uuid.NewString() + "/registration/pay"},
		{"DELETE", "/api/events/" + uuid.NewString() + "/registration"},
	} {
		resp := c.do(t, probe.method, probe.path, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s: want 401, got %d", probe.method, probe.path, resp.StatusCode)
		}
	}
}

func TestRegisterPayCancelFlow(t *testing.T) {
	srv, pool, q, svc, fake := newEventsServer(t)
	evID := seedPublishedEvent(t, pool, svc, 5000, 2)
	alice := loggedInClient(t, srv, q, "alice@test")

	// Register → pending_payment.
	resp := alice.do(t, "POST", fmt.Sprintf("/api/events/%s/register", evID), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	reg := decode[registrationInfoBody](t, resp)
	if reg.Status != "pending_payment" {
		t.Fatalf("want pending_payment, got %s", reg.Status)
	}

	// Pay → checkout url from the (fake) Stripe session.
	resp = alice.do(t, "POST", fmt.Sprintf("/api/events/%s/registration/pay", evID), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pay: %d", resp.StatusCode)
	}
	pay := decode[struct {
		CheckoutUrl string `json:"checkoutUrl"`
	}](t, resp)
	if pay.CheckoutUrl == "" || len(fake.sessions) != 1 {
		t.Fatalf("want a checkout session, got %+v / %d sessions", pay, len(fake.sessions))
	}

	// Detail shows my registration.
	resp = alice.do(t, "GET", fmt.Sprintf("/api/events/%s", evID), "")
	detail := decode[struct {
		MyRegistration *registrationInfoBody `json:"myRegistration"`
		PendingCount   int32                 `json:"pendingCount"`
	}](t, resp)
	if detail.MyRegistration == nil || detail.MyRegistration.Status != "pending_payment" || detail.PendingCount != 1 {
		t.Fatalf("detail: %+v", detail)
	}

	// Cancel → cancelled (pending, no money moved).
	resp = alice.do(t, "DELETE", fmt.Sprintf("/api/events/%s/registration", evID), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel: %d", resp.StatusCode)
	}
	reg = decode[registrationInfoBody](t, resp)
	if reg.Status != "cancelled" || len(fake.refunds) != 0 {
		t.Fatalf("pending cancel must not refund: %+v %v", reg, fake.refunds)
	}
}

func TestDraftEventHiddenOverHTTP(t *testing.T) {
	srv, pool, q, svc, _ := newEventsServer(t)
	ctx := context.Background()
	var orgID uuid.UUID
	if err := pool.QueryRow(ctx,
		`insert into users (email, display_name, email_verified_at, role)
		 values ('org@test', 'Org', now(), 'admin') returning id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	ev, err := svc.Create(ctx, orgID, events.CreateEventParams{
		Name: "Secret Draft", StartsAt: time.Now().Add(24 * time.Hour),
		FeeCents: 0, MaxParticipants: 8,
	})
	if err != nil {
		t.Fatal(err)
	}

	user := loggedInClient(t, srv, q, "user@test")
	if resp := user.do(t, "GET", fmt.Sprintf("/api/events/%s", ev.ID), ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("draft must 404 for non-admin, got %d", resp.StatusCode)
	}

	admin := loggedInClient(t, srv, q, "admin@test")
	makeAdmin(t, pool, "admin@test")
	if resp := admin.do(t, "GET", fmt.Sprintf("/api/events/%s", ev.ID), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin must see the draft, got %d", resp.StatusCode)
	}
}

func TestConcurrentRegistrationOnLastSpot(t *testing.T) {
	srv, pool, q, svc, _ := newEventsServer(t)
	evID := seedPublishedEvent(t, pool, svc, 5000, 1)
	a := loggedInClient(t, srv, q, "a@test")
	b := loggedInClient(t, srv, q, "b@test")

	var wg sync.WaitGroup
	statuses := make([]string, 2)
	for i, c := range []*cookieClient{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := c.do(t, "POST", fmt.Sprintf("/api/events/%s/register", evID), "")
			if resp.StatusCode != http.StatusOK {
				t.Errorf("register %d: %d", i, resp.StatusCode)
				return
			}
			statuses[i] = decode[registrationInfoBody](t, resp).Status
		}()
	}
	wg.Wait()
	got := map[string]int{}
	for _, s := range statuses {
		got[s]++
	}
	if got["pending_payment"] != 1 || got["waitlisted"] != 1 {
		t.Fatalf("exactly one spot + one waitlist expected, got %v", got)
	}
}
```

- [ ] **Step 4: Run** — `cd backend && go test ./internal/platform/httpapi/ -run 'TestEvents|TestRegisterPay|TestDraftEvent|TestConcurrent' -v -count=1` → after implementation, ALL PASS. Also `go build ./...` (main.go doesn't pass `Events` yet — that's Task 11; `Build` tolerates a nil service because no handler dereferences it at registration time, and the OpenAPI command keeps working).

- [ ] **Step 5: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/platform/httpapi/ && cd ..
git add backend/internal/platform/httpapi backend/internal/events
git commit -m "feat(events): user API — list, detail, register, pay, self-cancel"
```

---

### Task 9: Organizer endpoints + `role` in `/api/me`

**Files:**
- Modify: `backend/internal/platform/httpapi/session.go` (UserBody + role)
- Modify: `backend/internal/platform/httpapi/events.go` (admin ops)
- Modify: `backend/internal/platform/httpapi/events_endpoints_test.go`

**Interfaces:**
- Consumes: Task 8 helpers, `events.Service` organizer methods.
- Produces:
  - `UserBody.Role string` — the frontend gates organizer UI on it (Task 13+)
  - `requireAdmin(ctx, deps) (uuid.UUID, error)` (401 / 403 `admin-required`)
  - operations: `createEvent` (POST `/api/events`), `updateEvent` (PATCH `/api/events/{eventId}`), `publishEvent|startEvent|finishEvent|cancelEvent` (POST `/api/events/{eventId}/publish|start|finish|cancel`), `setEventCubes` (PUT `/api/events/{eventId}/cubes`), `listEventRegistrations` (GET `/api/events/{eventId}/registrations`), `refundRegistration` (POST `/api/events/{eventId}/registrations/{registrationId}/refund`), `denyRefund` (POST `/api/events/{eventId}/registrations/{registrationId}/deny-refund`)
  - wire type `EventRegistrationRow` (RegistrationInfo + displayName, email, createdAt)

- [ ] **Step 1: role in `/api/me`.** In `session.go`, add `Role string \`json:"role" enum:"user,admin"\`` to `UserBody` and set `Role: u.Role` inside `userBodyFor`. Extend `TestLoginMeLogout` minimally: decode the `/api/me` body and assert `role == "user"`.

- [ ] **Step 2: Admin ops.** Append to `events.go`:

```go
func requireAdmin(ctx context.Context, deps Deps) (uuid.UUID, error) {
	uid, ok := CurrentUserID(ctx)
	if !ok {
		return uuid.Nil, huma.Error401Unauthorized("authentication required")
	}
	u, err := deps.Queries.GetUserByID(ctx, uid)
	if err != nil {
		return uuid.Nil, err
	}
	if u.Role != "admin" {
		return uuid.Nil, eventProblem(http.StatusForbidden, "admin-required", "organizer access required")
	}
	return uid, nil
}

type createEventInput struct {
	Body struct {
		Name            string     `json:"name" minLength:"1" maxLength:"200"`
		Description     string     `json:"description,omitempty" maxLength:"5000"`
		Location        string     `json:"location,omitempty" maxLength:"200"`
		StartsAt        time.Time  `json:"startsAt"`
		FeeCents        int32      `json:"feeCents" minimum:"0" maximum:"10000000"`
		Currency        string     `json:"currency,omitempty" pattern:"^[a-z]{3}$"`
		MaxParticipants int32      `json:"maxParticipants" minimum:"1" maximum:"1000"`
		RefundDeadline  *time.Time `json:"refundDeadline,omitempty"`
	}
}

type updateEventInput struct {
	EventID string `path:"eventId"`
	Body    struct {
		Name            *string    `json:"name,omitempty" minLength:"1" maxLength:"200"`
		Description     *string    `json:"description,omitempty" maxLength:"5000"`
		Location        *string    `json:"location,omitempty" maxLength:"200"`
		StartsAt        *time.Time `json:"startsAt,omitempty"`
		FeeCents        *int32     `json:"feeCents,omitempty" minimum:"0" maximum:"10000000"`
		Currency        *string    `json:"currency,omitempty" pattern:"^[a-z]{3}$"`
		MaxParticipants *int32     `json:"maxParticipants,omitempty" minimum:"1" maximum:"1000"`
		RefundDeadline  *time.Time `json:"refundDeadline,omitempty"`
	}
}

type setEventCubesInput struct {
	EventID string `path:"eventId"`
	Body    struct {
		Cubes []struct {
			CubeID       uuid.UUID  `json:"cubeId"`
			CubeChangeID *uuid.UUID `json:"cubeChangeId,omitempty"`
		} `json:"cubes" maxItems:"20"`
	}
}

type EventRegistrationRow struct {
	RegistrationInfo
	DisplayName string    `json:"displayName"`
	Email       string    `json:"email"`
	CreatedAt   time.Time `json:"createdAt"`
}

type listRegistrationsOutput struct {
	Body struct {
		Registrations []EventRegistrationRow `json:"registrations"`
	}
}

type registrationActionInput struct {
	EventID        string `path:"eventId"`
	RegistrationID string `path:"registrationId"`
}

func registerEventAdmin(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "createEvent",
		Method:      http.MethodPost,
		Path:        "/api/events",
		Summary:     "Create a draft event (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *createEventInput) (*eventDetailOutput, error) {
		uid, err := requireAdmin(ctx, deps)
		if err != nil {
			return nil, err
		}
		ev, err := deps.Events.Create(ctx, uid, events.CreateEventParams{
			Name: in.Body.Name, Description: in.Body.Description,
			Location: in.Body.Location, StartsAt: in.Body.StartsAt,
			FeeCents: in.Body.FeeCents, Currency: in.Body.Currency,
			MaxParticipants: in.Body.MaxParticipants,
			RefundDeadline:  in.Body.RefundDeadline,
		})
		if err != nil {
			return nil, mapEventErr(err)
		}
		body, err := deps.eventDetailBody(ctx, ev.ID, uid, true)
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateEvent",
		Method:      http.MethodPatch,
		Path:        "/api/events/{eventId}",
		Summary:     "Edit an event (field whitelist depends on lifecycle)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *updateEventInput) (*eventDetailOutput, error) {
		uid, err := requireAdmin(ctx, deps)
		if err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		if _, err := deps.Events.Update(ctx, id, events.UpdateEventParams{
			Name: in.Body.Name, Description: in.Body.Description,
			Location: in.Body.Location, StartsAt: in.Body.StartsAt,
			FeeCents: in.Body.FeeCents, Currency: in.Body.Currency,
			MaxParticipants: in.Body.MaxParticipants,
			RefundDeadline:  in.Body.RefundDeadline,
		}); err != nil {
			return nil, mapEventErr(err)
		}
		body, err := deps.eventDetailBody(ctx, id, uid, true)
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	for _, action := range []string{"publish", "start", "finish", "cancel"} {
		huma.Register(api, huma.Operation{
			OperationID: action + "Event",
			Method:      http.MethodPost,
			Path:        "/api/events/{eventId}/" + action,
			Summary:     "Lifecycle: " + action,
			Tags:        []string{"events"},
		}, func(ctx context.Context, in *eventIDInput) (*eventDetailOutput, error) {
			uid, err := requireAdmin(ctx, deps)
			if err != nil {
				return nil, err
			}
			id, err := parseEventID(in.EventID)
			if err != nil {
				return nil, err
			}
			if _, err := deps.Events.Transition(ctx, id, action); err != nil {
				return nil, mapEventErr(err)
			}
			body, err := deps.eventDetailBody(ctx, id, uid, true)
			if err != nil {
				return nil, err
			}
			return &eventDetailOutput{Body: *body}, nil
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "setEventCubes",
		Method:      http.MethodPut,
		Path:        "/api/events/{eventId}/cubes",
		Summary:     "Replace the event's linked cubes (draft only)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *setEventCubesInput) (*eventDetailOutput, error) {
		uid, err := requireAdmin(ctx, deps)
		if err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		links := make([]events.CubeLinkInput, len(in.Body.Cubes))
		for i, c := range in.Body.Cubes {
			links[i] = events.CubeLinkInput{CubeID: c.CubeID, CubeChangeID: c.CubeChangeID}
		}
		if err := deps.Events.SetCubes(ctx, id, links); err != nil {
			return nil, mapEventErr(err)
		}
		body, err := deps.eventDetailBody(ctx, id, uid, true)
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listEventRegistrations",
		Method:      http.MethodGet,
		Path:        "/api/events/{eventId}/registrations",
		Summary:     "Every registration incl. waitlist order and refund queue (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*listRegistrationsOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		rows, err := deps.Queries.ListEventRegistrations(ctx, id)
		if err != nil {
			return nil, err
		}
		out := &listRegistrationsOutput{}
		out.Body.Registrations = make([]EventRegistrationRow, len(rows))
		for i, r := range rows {
			out.Body.Registrations[i] = EventRegistrationRow{
				RegistrationInfo: RegistrationInfo{
					ID: r.ID, Status: r.Status, ExpiresAt: r.ExpiresAt,
					WaitlistPos: r.WaitlistPos, PaidAt: r.PaidAt,
				},
				DisplayName: r.DisplayName, Email: r.Email, CreatedAt: r.CreatedAt,
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "refundRegistration",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/registrations/{registrationId}/refund",
		Summary:     "Refund a queued or paid registration (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *registrationActionInput) (*registrationOutput, error) {
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
		reg, err := deps.Events.OrganizerRefund(ctx, eventID, regID)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "denyRefund",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/registrations/{registrationId}/deny-refund",
		Summary:     "Deny a queued refund request (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *registrationActionInput) (*registrationOutput, error) {
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
		reg, err := deps.Events.DenyRefund(ctx, eventID, regID)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})
}
```

and call `registerEventAdmin(api, deps)` at the end of `registerEvents`.

- [ ] **Step 3: Tests.** Append to `events_endpoints_test.go`:

```go
func TestAdminGating(t *testing.T) {
	srv, _, q, _, _ := newEventsServer(t)
	user := loggedInClient(t, srv, q, "pleb@test")
	resp := user.do(t, "POST", "/api/events",
		`{"name":"Nope","startsAt":"2026-08-01T18:00:00Z","feeCents":0,"maxParticipants":8}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin create: want 403, got %d", resp.StatusCode)
	}
}

func TestOrganizerLifecycleOverHTTP(t *testing.T) {
	srv, pool, q, _, fake := newEventsServer(t)
	admin := loggedInClient(t, srv, q, "boss@test")
	makeAdmin(t, pool, "boss@test")

	// Create draft.
	resp := admin.do(t, "POST", "/api/events",
		`{"name":"Vintage Cube Night","startsAt":"2026-08-01T18:00:00Z","feeCents":5000,"maxParticipants":1,"refundDeadline":"2026-07-30T18:00:00Z"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	ev := decode[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}](t, resp)
	if ev.Status != "draft" {
		t.Fatalf("want draft, got %s", ev.Status)
	}

	// Publish, then a locked-field PATCH must 409.
	if resp := admin.do(t, "POST", "/api/events/"+ev.ID+"/publish", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("publish: %d", resp.StatusCode)
	}
	if resp := admin.do(t, "PATCH", "/api/events/"+ev.ID, `{"feeCents":9900}`); resp.StatusCode != http.StatusConflict {
		t.Fatalf("locked-field patch: want 409, got %d", resp.StatusCode)
	}
	if resp := admin.do(t, "PATCH", "/api/events/"+ev.ID, `{"description":"bring snacks"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("description patch: %d", resp.StatusCode)
	}

	// A user registers + pays (via service-level webhook simulation is
	// Task 6-tested; over HTTP we just check the organizer panel list).
	user := loggedInClient(t, srv, q, "player@test")
	if resp := user.do(t, "POST", "/api/events/"+ev.ID+"/register", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	resp = admin.do(t, "GET", "/api/events/"+ev.ID+"/registrations", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registrations list: %d", resp.StatusCode)
	}
	regs := decode[struct {
		Registrations []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Email  string `json:"email"`
		} `json:"registrations"`
	}](t, resp)
	if len(regs.Registrations) != 1 || regs.Registrations[0].Email != "player@test" {
		t.Fatalf("organizer list: %+v", regs)
	}
	if resp := user.do(t, "GET", "/api/events/"+ev.ID+"/registrations", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin registrations list: want 403, got %d", resp.StatusCode)
	}
	_ = fake
}
```

- [ ] **Step 4: Run** — `cd backend && go test ./internal/platform/httpapi/ -v -count=1` → ALL PASS (including pre-existing session tests with the extended UserBody).

- [ ] **Step 5: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/platform/httpapi/ && cd ..
git add backend/internal/platform/httpapi
git commit -m "feat(events): organizer API + role-gated admin access, role in /api/me"
```

---

### Task 10: Stripe webhook endpoint (raw chi route, signed-fixture tests)

**Files:**
- Create: `backend/internal/platform/httpapi/stripe.go`
- Create: `backend/internal/platform/httpapi/stripe_webhook_test.go`
- Modify: `backend/internal/platform/httpapi/api.go`

**Interfaces:**
- Consumes: `events.Service.HandleWebhookEvent`, stripe-go `webhook` package.
- Produces: `Deps.StripeWebhookSecret string`; POST `/api/stripe/webhook` mounted at chi level (raw body + signature auth, outside huma — it must never require a session).

- [ ] **Step 1: Handler** `backend/internal/platform/httpapi/stripe.go`:

```go
package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	stripe "github.com/stripe/stripe-go/v86"
	"github.com/stripe/stripe-go/v86/webhook"

	"github.com/mjabloniec/cube-planner/backend/internal/events"
)

// stripeWebhookHandler verifies the Stripe-Signature header against the
// raw body (which is why this lives outside huma) and forwards a minimal
// event shape to the service. 200 only after the DB transaction commits;
// 5xx makes Stripe retry.
func stripeWebhookHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			http.Error(w, "body too large", http.StatusBadRequest)
			return
		}
		ev, err := webhook.ConstructEventWithOptions(payload,
			r.Header.Get("Stripe-Signature"), deps.StripeWebhookSecret,
			webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
		if err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
		we := events.WebhookEvent{ID: ev.ID, Type: string(ev.Type)}
		switch ev.Type {
		case "checkout.session.completed", "checkout.session.expired":
			var s stripe.CheckoutSession
			if err := json.Unmarshal(ev.Data.Raw, &s); err != nil {
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			we.CheckoutSessionID = s.ID
			if s.PaymentIntent != nil {
				we.PaymentIntentID = s.PaymentIntent.ID
			}
		case "charge.refunded":
			var c stripe.Charge
			if err := json.Unmarshal(ev.Data.Raw, &c); err != nil {
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			if c.PaymentIntent != nil {
				we.PaymentIntentID = c.PaymentIntent.ID
			}
		}
		if err := deps.Events.HandleWebhookEvent(r.Context(), we); err != nil {
			slog.Error("stripe webhook processing failed", "event", ev.ID, "type", ev.Type, "error", err)
			http.Error(w, "processing failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
```

In `api.go`: add `StripeWebhookSecret string` to `Deps`, and in `Build` (next to the OAuth mount):

```go
	if deps.Events != nil && deps.StripeWebhookSecret != "" {
		router.Post("/api/stripe/webhook", stripeWebhookHandler(deps))
	}
```

- [ ] **Step 2: Signed-fixture tests** `backend/internal/platform/httpapi/stripe_webhook_test.go`:

```go
package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/events"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

const testWebhookSecret = "whsec_testsecret"

func newWebhookServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries, *events.Service, *endpointFakeStripe) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	fake := &endpointFakeStripe{}
	svc := events.NewService(q, pool, fake, noopMailer{}, "http://test", slog.Default())
	deps := httpapi.Deps{
		Auth:                auth.NewService(q, noopMailer{}, "http://test"),
		Sessions:            auth.NewSessions(q, false),
		Queries:             q,
		Events:              svc,
		StripeWebhookSecret: testWebhookSecret,
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q, svc, fake
}

// signStripePayload reproduces Stripe's v1 signature scheme:
// HMAC-SHA256 over "<timestamp>.<payload>".
func signStripePayload(payload []byte, secret string) string {
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func postWebhook(t *testing.T, srv *httptest.Server, payload []byte, sig string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", srv.URL+"/api/stripe/webhook", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Stripe-Signature", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestWebhookEndpointCompletesPayment(t *testing.T) {
	srv, pool, q, svc, _ := newWebhookServer(t)
	ctx := context.Background()
	evID := seedPublishedEvent(t, pool, svc, 5000, 2)
	alice := loggedInClient(t, srv, q, "alice@test")

	resp := alice.do(t, "POST", fmt.Sprintf("/api/events/%s/register", evID), "")
	reg := decode[registrationInfoBody](t, resp)
	if resp := alice.do(t, "POST", fmt.Sprintf("/api/events/%s/registration/pay", evID), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("pay: %d", resp.StatusCode)
	}

	sessionID := "cs_test_" + reg.ID
	payload := []byte(fmt.Sprintf(`{
		"id": "evt_fixture_1",
		"type": "checkout.session.completed",
		"data": {"object": {"id": %q, "object": "checkout.session", "payment_intent": "pi_fixture_1"}}
	}`, sessionID))

	// Bad signature → 400, nothing changes.
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, "whsec_wrong")); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad signature: want 400, got %d", resp.StatusCode)
	}

	// Good signature → 200 and the registration is paid.
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, testWebhookSecret)); resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: want 200, got %d", resp.StatusCode)
	}
	var status string
	if err := pool.QueryRow(ctx,
		`select status from registrations where id = $1`, reg.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paid" {
		t.Fatalf("want paid, got %s", status)
	}

	// Duplicate delivery of the same event id → 200, still one paid row.
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, testWebhookSecret)); resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate delivery: want 200, got %d", resp.StatusCode)
	}
}

func TestWebhookEndpointUnknownTypeAcknowledged(t *testing.T) {
	srv, _, _, _, _ := newWebhookServer(t)
	payload := []byte(`{"id": "evt_other_1", "type": "invoice.paid", "data": {"object": {}}}`)
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, testWebhookSecret)); resp.StatusCode != http.StatusOK {
		t.Fatalf("unknown type must be acknowledged, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 3: Run** — `cd backend && go test ./internal/platform/httpapi/ -run TestWebhookEndpoint -v -count=1` → PASS. (If `ConstructEventWithOptions` rejects the fixture for a missing `api_version` field, add `"api_version": "2026-01-01"` to the payloads — the option `IgnoreAPIVersionMismatch: true` skips the comparison.)

- [ ] **Step 4: gofumpt + commit**

```bash
cd backend && gofumpt -w internal/platform/httpapi/ && cd ..
git add backend/internal/platform/httpapi
git commit -m "feat(events): Stripe webhook endpoint with signature verification"
```

---

### Task 11: Wiring — config, main.go, Makefile `stripe-listen`, docs

**Files:**
- Modify: `backend/internal/platform/config/config.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `Makefile`
- Modify: `CLAUDE.md` (Gotchas)

**Interfaces:**
- Consumes: everything backend.
- Produces: `Config.StripeSecretKey`, `Config.StripeWebhookSecret`; a running sweeper; `make stripe-listen`.

- [ ] **Step 1: Config.** In `config.go` add to `Config`:

```go
	StripeSecretKey     string
	StripeWebhookSecret string
```

and in `Load()`:

```go
		StripeSecretKey:     env("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: env("STRIPE_WEBHOOK_SECRET", ""),
```

- [ ] **Step 2: main.go.** Inside `hooks.OnStart`, after the cards sync block:

```go
			mailer := mail.FromConfig(cfg)
			eventsSvc := events.NewService(queries, pool,
				events.NewStripeClient(cfg.StripeSecretKey), mailer,
				cfg.BaseURL, slog.Default())
			go eventsSvc.RunSweeper(ctx, events.DefaultSweepInterval)
```

reuse `mailer` for auth (`Auth: auth.NewService(queries, mailer, cfg.BaseURL)`), and extend `deps`:

```go
				Events:              eventsSvc,
				StripeWebhookSecret: cfg.StripeWebhookSecret,
```

(+ import `"github.com/mjabloniec/cube-planner/backend/internal/events"`).

- [ ] **Step 3: Makefile.** Add next to the other dev targets:

```make
.PHONY: stripe-listen
stripe-listen: ## Forward Stripe test-mode webhooks to the local backend (requires stripe CLI, prints the whsec_ secret for .env)
	stripe listen --forward-to localhost:8080/api/stripe/webhook
```

If the backend dev port in `make up` is not 8080, match whatever `PORT` the dev stack uses (check the `up` target / `.env` — the config default is 8080).

- [ ] **Step 4: CLAUDE.md.** Add to the Gotchas list:

```markdown
- Stripe dev: test-mode keys in `.env` (`STRIPE_SECRET_KEY`,
  `STRIPE_WEBHOOK_SECRET`); run `make stripe-listen` alongside `make up`
  and paste the printed `whsec_…` into `.env`. Without keys, paid events
  return 503 `payments-unconfigured`; free events work fully.
```

- [ ] **Step 5: Verify** — `cd backend && go build ./... && go test ./... -count=1` (full suite, Docker running) → PASS. Then `go run ./cmd/server openapi > /dev/null` → no panic (OpenAPI generation with nil deps still works).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/platform/config backend/cmd/server Makefile CLAUDE.md
git commit -m "feat(events): wire events service, sweeper, and Stripe config; make stripe-listen"
```

---

### Task 12: Regenerate the TS client

**Files:**
- Generated: `frontend/src/shared/api/` via `make api-generate` (committed; CI enforces freshness)

- [ ] **Step 1:** Run `make api-generate`
Expected: `frontend/src/shared/api/schema.d.ts` gains `EventSummary`, `EventDetailBody`, `EventCubeEntry`, `RegistrationInfo`, `EventRegistrationRow`, `UserBody.role`, and the new paths.

- [ ] **Step 2:** `pnpm --filter @cube-planner/frontend typecheck`
Expected: clean (nothing consumes the new types yet).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/shared/api
git commit -m "feat(events): regenerate API client with events endpoints"
```

---

### Task 13: `features/events` — API hooks, money/countdown libs, messages

**Files:**
- Create: `frontend/src/features/events/api.ts`
- Create: `frontend/src/features/events/lib/money.ts`, `frontend/src/features/events/lib/money.test.ts`
- Create: `frontend/src/features/events/lib/countdown.ts`, `frontend/src/features/events/lib/countdown.test.ts`
- Modify: `frontend/messages/en.json`, `frontend/messages/pl.json`

**Interfaces:**
- Consumes: generated client (Task 12).
- Produces (used by Tasks 14–17): hooks `useEvents`, `useEvent(eventId, {refetchInterval})`, `useRegister(eventId)`, `usePay(eventId)`, `useCancelRegistration(eventId)`, `useCreateEvent`, `useUpdateEvent(eventId)`, `useEventAction(eventId)`, `useSetEventCubes(eventId)`, `useEventRegistrations(eventId)`, `useRefundRegistration(eventId)`, `useDenyRefund(eventId)`, `useLinkableCubes()`, `useCubeChangelog(cubeId)`; libs `formatFee(feeCents, currency)`, `remainingLabel(expiresAt, now?)`; types re-exported from the schema.

- [ ] **Step 1: Messages.** Add to `frontend/messages/en.json` (keep alphabetical-ish grouping with the other domains):

```json
{
  "nav_events": "Events",
  "events_title": "Events",
  "events_upcoming": "Upcoming",
  "events_past": "Past & cancelled",
  "events_drafts": "Drafts",
  "events_empty": "No events yet.",
  "events_login_required": "Log in to see events.",
  "events_not_found": "Event not found.",
  "events_free": "Free",
  "events_spots": "{taken}/{total} spots",
  "events_waitlist_count": "{count} on the waitlist",
  "events_new_button": "New event",
  "events_status_draft": "Draft",
  "events_status_published": "Open",
  "events_status_started": "In progress",
  "events_status_finished": "Finished",
  "events_status_cancelled": "Cancelled",
  "events_my_pending": "Payment pending",
  "events_my_paid": "You're in",
  "events_my_waitlisted": "Waitlisted #{pos}",
  "events_my_waitlisted_nopos": "Waitlisted",
  "events_my_refund_requested": "Cancelled — refund pending organizer review",
  "event_date": "Date",
  "event_location": "Location",
  "event_fee": "Entry fee",
  "event_organizer": "Organizer",
  "event_refund_until": "Free cancellation until {date}",
  "event_refund_until_start": "Free cancellation until the event starts",
  "event_attendees": "Attendees ({count})",
  "event_attendees_empty": "No one yet — be the first!",
  "event_cubes": "Cubes",
  "event_cube_pinned": "as of v{version} ({date})",
  "event_register": "Register",
  "event_join_waitlist": "Join the waitlist",
  "event_pay_now": "Pay now",
  "event_pay_time_left": "Time left to pay: {remaining}",
  "event_cancel_registration": "Cancel registration",
  "event_leave_waitlist": "Leave the waitlist",
  "event_cancel_confirm": "Cancel your registration for {name}?",
  "event_cancel_confirm_late": "The free-cancellation deadline has passed. Your spot frees up immediately, but you will only get your money back if the organizer approves the refund. Cancel anyway?",
  "event_confirming_payment": "Confirming your payment…",
  "event_confirming_timeout": "This is taking longer than usual. The confirmation email will arrive once the payment lands — you can safely leave this page.",
  "event_checkout_cancelled": "Payment cancelled — you can retry below.",
  "event_manage": "Manage",
  "events_new_title": "New event",
  "event_form_name": "Name",
  "event_form_description": "Description",
  "event_form_location": "Location",
  "event_form_starts_at": "Starts at",
  "event_form_fee": "Entry fee (PLN)",
  "event_form_fee_hint": "0 makes the event free",
  "event_form_max_participants": "Max participants",
  "event_form_refund_deadline": "Free-cancellation deadline (optional)",
  "event_form_locked_hint": "Locked after publish",
  "event_form_create": "Create draft",
  "event_form_save": "Save",
  "event_form_saved": "Saved.",
  "event_lifecycle_publish": "Publish",
  "event_lifecycle_start": "Start event",
  "event_lifecycle_finish": "Finish event",
  "event_lifecycle_cancel": "Cancel event",
  "event_publish_confirm": "Publish {name}? It becomes visible and open for registration.",
  "event_start_confirm": "Start {name}? Registration closes; pending payments expire and the waitlist is cancelled.",
  "event_finish_confirm": "Mark {name} as finished?",
  "event_cancel_event_confirm": "Cancel {name}? Every paid registration will be refunded automatically.",
  "event_cubes_editor_title": "Linked cubes",
  "event_cubes_add": "Add cube",
  "event_cubes_remove": "Remove",
  "event_cubes_pin_label": "Pinned version",
  "event_cubes_pin_live": "Live (latest)",
  "regs_title": "Registrations",
  "regs_group_paid": "Paid roster",
  "regs_group_pending": "Pending payment",
  "regs_group_waitlist": "Waitlist",
  "regs_group_refund_queue": "Refund queue",
  "regs_group_history": "History",
  "regs_refund": "Refund",
  "regs_deny": "Deny",
  "regs_refund_confirm": "Refund {name}'s entry fee?",
  "regs_deny_confirm": "Deny the refund for {name}? They keep nothing; the row closes.",
  "regs_empty": "Nobody here.",
  "regs_expires": "expires {date}",
  "regs_paid_at": "paid {date}"
}
```

and the same keys to `frontend/messages/pl.json`:

```json
{
  "nav_events": "Wydarzenia",
  "events_title": "Wydarzenia",
  "events_upcoming": "Nadchodzące",
  "events_past": "Zakończone i odwołane",
  "events_drafts": "Szkice",
  "events_empty": "Na razie nie ma wydarzeń.",
  "events_login_required": "Zaloguj się, aby zobaczyć wydarzenia.",
  "events_not_found": "Nie znaleziono wydarzenia.",
  "events_free": "Za darmo",
  "events_spots": "{taken}/{total} miejsc",
  "events_waitlist_count": "{count} na liście rezerwowej",
  "events_new_button": "Nowe wydarzenie",
  "events_status_draft": "Szkic",
  "events_status_published": "Zapisy otwarte",
  "events_status_started": "W trakcie",
  "events_status_finished": "Zakończone",
  "events_status_cancelled": "Odwołane",
  "events_my_pending": "Oczekuje na płatność",
  "events_my_paid": "Jesteś na liście",
  "events_my_waitlisted": "Rezerwowa #{pos}",
  "events_my_waitlisted_nopos": "Na liście rezerwowej",
  "events_my_refund_requested": "Anulowano — zwrot czeka na decyzję organizatora",
  "event_date": "Termin",
  "event_location": "Miejsce",
  "event_fee": "Wpisowe",
  "event_organizer": "Organizator",
  "event_refund_until": "Bezpłatna rezygnacja do {date}",
  "event_refund_until_start": "Bezpłatna rezygnacja do rozpoczęcia wydarzenia",
  "event_attendees": "Uczestnicy ({count})",
  "event_attendees_empty": "Jeszcze nikogo nie ma — bądź pierwszy!",
  "event_cubes": "Cuby",
  "event_cube_pinned": "wg wersji v{version} ({date})",
  "event_register": "Zapisz się",
  "event_join_waitlist": "Dołącz do listy rezerwowej",
  "event_pay_now": "Zapłać teraz",
  "event_pay_time_left": "Czas na płatność: {remaining}",
  "event_cancel_registration": "Anuluj zapis",
  "event_leave_waitlist": "Opuść listę rezerwową",
  "event_cancel_confirm": "Anulować zapis na {name}?",
  "event_cancel_confirm_late": "Termin bezpłatnej rezygnacji minął. Miejsce zwolni się od razu, ale pieniądze odzyskasz tylko, jeśli organizator zatwierdzi zwrot. Anulować mimo to?",
  "event_confirming_payment": "Potwierdzamy Twoją płatność…",
  "event_confirming_timeout": "Trwa to dłużej niż zwykle. Gdy płatność dotrze, wyślemy e-mail z potwierdzeniem — możesz spokojnie opuścić tę stronę.",
  "event_checkout_cancelled": "Płatność anulowana — możesz spróbować ponownie poniżej.",
  "event_manage": "Zarządzaj",
  "events_new_title": "Nowe wydarzenie",
  "event_form_name": "Nazwa",
  "event_form_description": "Opis",
  "event_form_location": "Miejsce",
  "event_form_starts_at": "Rozpoczęcie",
  "event_form_fee": "Wpisowe (PLN)",
  "event_form_fee_hint": "0 oznacza darmowe wydarzenie",
  "event_form_max_participants": "Limit uczestników",
  "event_form_refund_deadline": "Termin bezpłatnej rezygnacji (opcjonalny)",
  "event_form_locked_hint": "Zablokowane po publikacji",
  "event_form_create": "Utwórz szkic",
  "event_form_save": "Zapisz",
  "event_form_saved": "Zapisano.",
  "event_lifecycle_publish": "Opublikuj",
  "event_lifecycle_start": "Rozpocznij wydarzenie",
  "event_lifecycle_finish": "Zakończ wydarzenie",
  "event_lifecycle_cancel": "Odwołaj wydarzenie",
  "event_publish_confirm": "Opublikować {name}? Wydarzenie stanie się widoczne i otwarte na zapisy.",
  "event_start_confirm": "Rozpocząć {name}? Zapisy zostaną zamknięte; nieopłacone rezerwacje wygasną, a lista rezerwowa zostanie anulowana.",
  "event_finish_confirm": "Oznaczyć {name} jako zakończone?",
  "event_cancel_event_confirm": "Odwołać {name}? Wszystkie opłacone zapisy zostaną automatycznie zwrócone.",
  "event_cubes_editor_title": "Powiązane cuby",
  "event_cubes_add": "Dodaj cube",
  "event_cubes_remove": "Usuń",
  "event_cubes_pin_label": "Przypięta wersja",
  "event_cubes_pin_live": "Bieżąca (najnowsza)",
  "regs_title": "Zapisy",
  "regs_group_paid": "Opłaceni",
  "regs_group_pending": "Oczekują na płatność",
  "regs_group_waitlist": "Lista rezerwowa",
  "regs_group_refund_queue": "Kolejka zwrotów",
  "regs_group_history": "Historia",
  "regs_refund": "Zwróć",
  "regs_deny": "Odmów",
  "regs_refund_confirm": "Zwrócić wpisowe dla {name}?",
  "regs_deny_confirm": "Odmówić zwrotu dla {name}? Wiersz zostanie zamknięty bez zwrotu.",
  "regs_empty": "Nikogo tu nie ma.",
  "regs_expires": "wygasa {date}",
  "regs_paid_at": "opłacono {date}"
}
```

Then run `pnpm --filter @cube-planner/frontend gen` to refresh Paraglide output.

- [ ] **Step 2: libs + failing tests.** `frontend/src/features/events/lib/money.ts`:

```ts
import { getLocale } from "@/paraglide/runtime";

/** feeCents → localized currency string ("50,00 zł" / "PLN 50.00"). */
export function formatFee(feeCents: number, currency: string): string {
  return new Intl.NumberFormat(getLocale(), {
    style: "currency",
    currency: currency.toUpperCase(),
  }).format(feeCents / 100);
}
```

`money.test.ts`:

```ts
import { expect, test, vi } from "vitest";

vi.mock("@/paraglide/runtime", () => ({ getLocale: () => "en" }));

import { formatFee } from "./money";

test("formats cents as localized currency", () => {
  expect(formatFee(5000, "pln")).toMatch(/50/);
  expect(formatFee(5000, "pln")).toMatch(/PLN|zł/);
  expect(formatFee(150, "pln")).toMatch(/1[.,]50/);
});
```

`countdown.ts`:

```ts
/** Human "time left" label for a pending_payment deadline. */
export function remainingLabel(expiresAt: string, now: number = Date.now()): string {
  const ms = new Date(expiresAt).getTime() - now;
  if (ms <= 0) return "0m";
  const totalMinutes = Math.ceil(ms / 60_000);
  const h = Math.floor(totalMinutes / 60);
  const min = totalMinutes % 60;
  return h > 0 ? `${h}h ${min}m` : `${min}m`;
}
```

`countdown.test.ts`:

```ts
import { expect, test } from "vitest";
import { remainingLabel } from "./countdown";

const base = Date.parse("2026-07-13T12:00:00Z");

test("hours and minutes", () => {
  expect(remainingLabel("2026-07-14T11:30:00Z", base)).toBe("23h 30m");
});
test("minutes only", () => {
  expect(remainingLabel("2026-07-13T12:45:00Z", base)).toBe("45m");
});
test("past deadline clamps to zero", () => {
  expect(remainingLabel("2026-07-13T11:00:00Z", base)).toBe("0m");
});
```

Run: `pnpm --filter @cube-planner/frontend test -- lib` → PASS after implementing.

- [ ] **Step 3: API hooks** `frontend/src/features/events/api.ts`:

```ts
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { m } from "@/paraglide/messages";
import { client } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type EventSummary = components["schemas"]["EventSummary"];
export type EventDetail = components["schemas"]["EventDetailBody"];
export type EventCubeEntry = components["schemas"]["EventCubeEntry"];
export type RegistrationInfo = components["schemas"]["RegistrationInfo"];
export type EventRegistrationRow = components["schemas"]["EventRegistrationRow"];

/** Thrown on 401 so pages can render a login prompt instead of a generic error. */
export class UnauthorizedError extends Error {}

function unwrap<T>(data: T | undefined, error: { detail?: string | null } | undefined): T {
  if (error) throw new Error(error.detail ?? m.error_generic());
  if (!data) throw new Error(m.error_generic());
  return data;
}

export function useEvents() {
  return useQuery({
    queryKey: ["events", "list"],
    retry: false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/events");
      if (response.status === 401) throw new UnauthorizedError(m.events_login_required());
      return unwrap(data, error).events ?? [];
    },
  });
}

export function useEvent(eventId: string, opts?: { refetchInterval?: number | false }) {
  return useQuery({
    queryKey: ["events", "detail", eventId],
    retry: false,
    refetchInterval: opts?.refetchInterval ?? false,
    queryFn: async () => {
      const { data, error, response } = await client.GET("/api/events/{eventId}", {
        params: { path: { eventId } },
      });
      if (response.status === 401) throw new UnauthorizedError(m.events_login_required());
      return unwrap(data, error);
    },
  });
}

function useEventMutation<TVars, TData>(fn: (vars: TVars) => Promise<TData>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["events"] }),
  });
}

export function useRegister(eventId: string) {
  return useEventMutation(async () => {
    const { data, error } = await client.POST("/api/events/{eventId}/register", {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

export function usePay(eventId: string) {
  // No invalidation: on success the browser leaves for Stripe Checkout.
  return useMutation({
    mutationFn: async () => {
      const { data, error } = await client.POST("/api/events/{eventId}/registration/pay", {
        params: { path: { eventId } },
      });
      return unwrap(data, error).checkoutUrl;
    },
    onSuccess: (url) => window.location.assign(url),
  });
}

export function useCancelRegistration(eventId: string) {
  return useEventMutation(async () => {
    const { data, error } = await client.DELETE("/api/events/{eventId}/registration", {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

// ---- organizer ----

export type EventFormValues = {
  name: string;
  description: string;
  location: string;
  startsAt: string; // RFC3339
  feeCents: number;
  maxParticipants: number;
  refundDeadline?: string;
};

export function useCreateEvent() {
  return useEventMutation(async (body: EventFormValues) => {
    const { data, error } = await client.POST("/api/events", { body });
    return unwrap(data, error);
  });
}

export function useUpdateEvent(eventId: string) {
  return useEventMutation(async (body: Partial<EventFormValues>) => {
    const { data, error } = await client.PATCH("/api/events/{eventId}", {
      params: { path: { eventId } },
      body,
    });
    return unwrap(data, error);
  });
}

export type EventAction = "publish" | "start" | "finish" | "cancel";

const ACTION_PATHS = {
  publish: "/api/events/{eventId}/publish",
  start: "/api/events/{eventId}/start",
  finish: "/api/events/{eventId}/finish",
  cancel: "/api/events/{eventId}/cancel",
} as const;

export function useEventAction(eventId: string) {
  return useEventMutation(async (action: EventAction) => {
    const { data, error } = await client.POST(ACTION_PATHS[action], {
      params: { path: { eventId } },
    });
    return unwrap(data, error);
  });
}

export function useSetEventCubes(eventId: string) {
  return useEventMutation(
    async (cubes: { cubeId: string; cubeChangeId?: string }[]) => {
      const { data, error } = await client.PUT("/api/events/{eventId}/cubes", {
        params: { path: { eventId } },
        body: { cubes },
      });
      return unwrap(data, error);
    },
  );
}

export function useEventRegistrations(eventId: string) {
  return useQuery({
    queryKey: ["events", "registrations", eventId],
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/events/{eventId}/registrations", {
        params: { path: { eventId } },
      });
      return unwrap(data, error).registrations ?? [];
    },
  });
}

export function useRefundRegistration(eventId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (registrationId: string) => {
      const { data, error } = await client.POST(
        "/api/events/{eventId}/registrations/{registrationId}/refund",
        { params: { path: { eventId, registrationId } } },
      );
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["events"] }),
  });
}

export function useDenyRefund(eventId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (registrationId: string) => {
      const { data, error } = await client.POST(
        "/api/events/{eventId}/registrations/{registrationId}/deny-refund",
        { params: { path: { eventId, registrationId } } },
      );
      return unwrap(data, error);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ["events"] }),
  });
}

// Cube linking sources. features must not import other features
// (structure.md), so events talks to the cubes API through the shared
// generated client directly.
export function useLinkableCubes() {
  return useQuery({
    queryKey: ["events", "linkable-cubes"],
    retry: false,
    queryFn: async () => {
      const [pub, mine] = await Promise.all([
        client.GET("/api/cubes", { params: { query: { limit: 100, offset: 0 } } }),
        client.GET("/api/cubes/mine"),
      ]);
      const cubes = [...(pub.data?.cubes ?? []), ...(mine.data?.cubes ?? [])];
      const seen = new Set<string>();
      return cubes.filter((c) => (seen.has(c.id) ? false : (seen.add(c.id), true)));
    },
  });
}

export function useCubeChangelog(cubeId: string | null) {
  return useQuery({
    queryKey: ["events", "cube-changes", cubeId],
    enabled: cubeId != null,
    retry: false,
    queryFn: async () => {
      const { data, error } = await client.GET("/api/cubes/{cubeId}/changes", {
        params: { path: { cubeId: cubeId! }, query: { limit: 50, offset: 0 } },
      });
      return unwrap(data, error).changes ?? [];
    },
  });
}
```

Adjust the two cube paths/response shapes to whatever `shared/api/schema.d.ts` actually names them (`useCubeList`/`useCubeChanges` in `features/cubes/api.ts` show the exact calls — mirror those, do NOT import them).

- [ ] **Step 4:** `pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend test -- features/events` → clean.

- [ ] **Step 5: Commit**

```bash
pnpm --filter @cube-planner/frontend fmt
git add frontend/src/features/events frontend/messages
git commit -m "feat(events): frontend api hooks, money/countdown libs, en+pl messages"
```

---

### Task 14: Events list page + route + nav

**Files:**
- Create: `frontend/src/features/events/components/EventsListPage.tsx`
- Create: `frontend/src/features/events/components/EventsListPage.test.tsx`
- Create: `frontend/src/routes/events.index.tsx`
- Modify: `frontend/src/routes/__root.tsx` (nav link)

**Interfaces:**
- Consumes: Task 13 hooks + libs, `useMe` (role), shared `Card`/`Button`.
- Produces: `/events` route; `EventStatusBadge` + `MyStatusBadge` small components exported from the file for reuse in Task 15.

- [ ] **Step 1: Route** `frontend/src/routes/events.index.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { EventsListPage } from "@/features/events/components/EventsListPage";

export const Route = createFileRoute("/events/")({ component: EventsListPage });
```

- [ ] **Step 2: Nav.** In `__root.tsx`, after the `/cubes` link:

```tsx
            <Link to="/events" className="text-sm text-fg-muted hover:text-fg">
              {m.nav_events()}
            </Link>
```

- [ ] **Step 3: Page** `frontend/src/features/events/components/EventsListPage.tsx`:

```tsx
import { Link } from "@tanstack/react-router";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { useEvents, UnauthorizedError, type EventSummary } from "../api";
import { formatFee } from "../lib/money";

export function EventStatusBadge({ status }: { status: EventSummary["status"] }) {
  const label = {
    draft: m.events_status_draft(),
    published: m.events_status_published(),
    started: m.events_status_started(),
    finished: m.events_status_finished(),
    cancelled: m.events_status_cancelled(),
  }[status];
  return (
    <span className="rounded-full border border-border px-2 py-0.5 text-xs text-fg-muted">
      {label}
    </span>
  );
}

export function MyStatusBadge({ status, pos }: { status?: string | null; pos?: number | null }) {
  if (!status) return null;
  const label =
    status === "pending_payment"
      ? m.events_my_pending()
      : status === "paid"
        ? m.events_my_paid()
        : status === "waitlisted"
          ? pos != null
            ? m.events_my_waitlisted({ pos })
            : m.events_my_waitlisted_nopos()
          : status === "refund_requested"
            ? m.events_my_refund_requested()
            : null;
  if (!label) return null;
  return (
    <span className="rounded-full bg-accent/10 px-2 py-0.5 text-xs font-medium text-accent">
      {label}
    </span>
  );
}

function EventRow({ event }: { event: EventSummary }) {
  const taken = event.paidCount + event.pendingCount;
  return (
    <li>
      <Link
        to="/events/$eventId"
        params={{ eventId: event.id }}
        className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-surface-raised p-4 hover:border-accent"
      >
        <div className="flex flex-col gap-1">
          <span className="flex items-center gap-2 font-medium text-fg">
            {event.name}
            <EventStatusBadge status={event.status} />
            <MyStatusBadge status={event.myRegistrationStatus} />
          </span>
          <span className="text-sm text-fg-muted">
            {new Date(event.startsAt).toLocaleString(getLocale())}
            {event.location && ` · ${event.location}`}
          </span>
        </div>
        <div className="flex flex-col items-end gap-1 text-sm">
          <span className="text-fg">
            {event.feeCents === 0 ? m.events_free() : formatFee(event.feeCents, event.currency)}
          </span>
          <span className="text-fg-muted">
            {m.events_spots({ taken, total: event.maxParticipants })}
            {event.waitlistCount > 0 && ` · ${m.events_waitlist_count({ count: event.waitlistCount })}`}
          </span>
        </div>
      </Link>
    </li>
  );
}

export function EventsListPage() {
  const me = useMe();
  const events = useEvents();

  if (events.error instanceof UnauthorizedError) {
    return <p className="text-fg-muted">{events.error.message}</p>;
  }
  if (events.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (events.error) return <p role="alert" className="text-danger">{events.error.message}</p>;

  const drafts = events.data.filter((e) => e.status === "draft");
  const upcoming = events.data.filter((e) => e.status === "published" || e.status === "started");
  const past = events.data.filter((e) => e.status === "finished" || e.status === "cancelled");
  const isAdmin = me.data?.role === "admin";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-fg">{m.events_title()}</h1>
        {isAdmin && (
          <Button asChild size="sm">
            <Link to="/events/new">{m.events_new_button()}</Link>
          </Button>
        )}
      </div>
      {events.data.length === 0 && <p className="text-fg-muted">{m.events_empty()}</p>}
      {isAdmin && drafts.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.events_drafts()}</h2>
          <ul className="flex flex-col gap-2">
            {drafts.map((e) => <EventRow key={e.id} event={e} />)}
          </ul>
        </section>
      )}
      {upcoming.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.events_upcoming()}</h2>
          <ul className="flex flex-col gap-2">
            {upcoming.map((e) => <EventRow key={e.id} event={e} />)}
          </ul>
        </section>
      )}
      {past.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.events_past()}</h2>
          <ul className="flex flex-col gap-2">
            {past.map((e) => <EventRow key={e.id} event={e} />)}
          </ul>
        </section>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Test** `EventsListPage.test.tsx` — same memory-router + fetch-stub pattern as `CubeBrowserPage.test.tsx` (register routes `/`, `/events/$eventId`, `/events/new`): stub `fetch` to return `/api/me` (role `user`) and `/api/events` with one published (fee 5000, my status `waitlisted`) and one finished event; assert the upcoming/past group headings, the fee rendering, the waitlist badge text, and that the "New event" button is absent for role `user`; a second test with role `admin` asserts the button is present.

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { EventsListPage } from "./EventsListPage";

afterEach(() => vi.unstubAllGlobals());

function renderPage() {
  const rootRoute = createRootRoute();
  const index = createRoute({ getParentRoute: () => rootRoute, path: "/", component: EventsListPage });
  const detail = createRoute({ getParentRoute: () => rootRoute, path: "/events/$eventId", component: () => null });
  const create = createRoute({ getParentRoute: () => rootRoute, path: "/events/new", component: () => null });
  const router = createRouter({
    routeTree: rootRoute.addChildren([index, detail, create]),
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

const eventsPayload = {
  events: [
    {
      id: "e1", name: "Vintage Night", startsAt: "2026-08-01T18:00:00Z", location: "LGS",
      feeCents: 5000, currency: "pln", maxParticipants: 8,
      paidCount: 8, pendingCount: 0, waitlistCount: 2,
      status: "published", myRegistrationStatus: "waitlisted",
    },
    {
      id: "e2", name: "Old Draft Night", startsAt: "2026-06-01T18:00:00Z", location: "",
      feeCents: 0, currency: "pln", maxParticipants: 8,
      paidCount: 6, pendingCount: 0, waitlistCount: 0,
      status: "finished",
    },
  ],
};

function stubFetch(role: string) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes("/api/me")) {
        return new Response(JSON.stringify({ id: "u1", email: "x@y", displayName: "X", providers: [], role }));
      }
      return new Response(JSON.stringify(eventsPayload));
    }),
  );
}

test("groups events and shows fee, spots, and my status", async () => {
  stubFetch("user");
  renderPage();
  expect(await screen.findByText("Vintage Night")).toBeInTheDocument();
  expect(screen.getByText("Upcoming")).toBeInTheDocument();
  expect(screen.getByText("Past & cancelled")).toBeInTheDocument();
  expect(screen.getByText(/8\/8 spots/)).toBeInTheDocument();
  // The list summary carries no waitlist position — the badge is generic.
  expect(screen.getByText("Waitlisted")).toBeInTheDocument();
  expect(screen.queryByText("New event")).not.toBeInTheDocument();
});

test("admins see the new-event button", async () => {
  stubFetch("admin");
  renderPage();
  expect(await screen.findByText("New event")).toBeInTheDocument();
});
```

- [ ] **Step 5:** `pnpm --filter @cube-planner/frontend test -- EventsListPage && pnpm --filter @cube-planner/frontend typecheck` → PASS.

- [ ] **Step 6: Commit**

```bash
pnpm --filter @cube-planner/frontend fmt && pnpm --filter @cube-planner/frontend lint
git add frontend/src/features/events frontend/src/routes/events.index.tsx frontend/src/routes/__root.tsx
git commit -m "feat(events): events list page, route, and nav entry"
```

---

### Task 15: Event detail page — registration CTA + checkout return polling

**Files:**
- Create: `frontend/src/features/events/components/EventDetailPage.tsx`
- Create: `frontend/src/features/events/components/RegistrationPanel.tsx`
- Create: `frontend/src/features/events/components/RegistrationPanel.test.tsx`
- Create: `frontend/src/routes/events.$eventId.index.tsx`

**Interfaces:**
- Consumes: Task 13 hooks/libs, Task 14 badges, shared `Dialog`, `Button`, `Card`.
- Produces: `/events/$eventId` route with `?checkout=success|cancelled` search handling; `RegistrationPanel` (all CTA states).

- [ ] **Step 1: Route** `frontend/src/routes/events.$eventId.index.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { EventDetailPage } from "@/features/events/components/EventDetailPage";

export const Route = createFileRoute("/events/$eventId/")({
  component: EventDetailPage,
  validateSearch: (s: Record<string, unknown>): { checkout?: "success" | "cancelled" } => ({
    ...(s.checkout === "success" || s.checkout === "cancelled"
      ? { checkout: s.checkout }
      : {}),
  }),
});
```

- [ ] **Step 2: RegistrationPanel** `frontend/src/features/events/components/RegistrationPanel.tsx`:

```tsx
import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import type { EventDetail } from "../api";
import { useCancelRegistration, usePay, useRegister } from "../api";
import { remainingLabel } from "../lib/countdown";

/**
 * The registration CTA block. Confirming state (checkout=success while
 * the webhook is pending) is rendered by the parent, which also drives
 * the polling — this component only renders server truth.
 */
export function RegistrationPanel({
  event,
  checkoutCancelled,
}: {
  event: EventDetail;
  checkoutCancelled: boolean;
}) {
  const register = useRegister(event.id);
  const pay = usePay(event.id);
  const cancel = useCancelRegistration(event.id);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const reg = event.myRegistration;
  const taken = event.paidCount + event.pendingCount;
  const full = taken >= event.maxParticipants;
  const registrable = event.status === "published";
  const err = register.error ?? pay.error ?? cancel.error;

  const deadline = event.refundDeadline ? new Date(event.refundDeadline) : new Date(event.startsAt);
  const pastDeadline = Date.now() > deadline.getTime();

  const confirmCancel = () => {
    setConfirmOpen(false);
    cancel.mutate(undefined);
  };

  if (!registrable && !reg) return null;

  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border bg-surface-raised p-4">
      {err && (
        <p role="alert" className="text-sm text-danger">
          {err.message}
        </p>
      )}
      {!reg && registrable && (
        <Button
          type="button"
          disabled={register.isPending}
          onClick={() => register.mutate(undefined)}
        >
          {full ? m.event_join_waitlist() : m.event_register()}
        </Button>
      )}
      {reg?.status === "pending_payment" && (
        <>
          {checkoutCancelled && <p className="text-sm text-fg-muted">{m.event_checkout_cancelled()}</p>}
          {reg.expiresAt && (
            <p className="text-sm text-fg-muted">
              {m.event_pay_time_left({ remaining: remainingLabel(reg.expiresAt) })}
            </p>
          )}
          <div className="flex gap-2">
            <Button type="button" disabled={pay.isPending} onClick={() => pay.mutate(undefined)}>
              {m.event_pay_now()}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={cancel.isPending}
              onClick={() => cancel.mutate(undefined)}
            >
              {m.event_cancel_registration()}
            </Button>
          </div>
        </>
      )}
      {reg?.status === "waitlisted" && (
        <>
          <p className="text-sm font-medium text-fg">
            {m.events_my_waitlisted({ pos: reg.waitlistPos ?? 0 })}
          </p>
          <Button
            type="button"
            variant="outline"
            disabled={cancel.isPending}
            onClick={() => cancel.mutate(undefined)}
          >
            {m.event_leave_waitlist()}
          </Button>
        </>
      )}
      {reg?.status === "paid" && (
        <>
          <p className="text-sm font-medium text-fg">{m.events_my_paid()}</p>
          {registrable && (
            <Button type="button" variant="outline" onClick={() => setConfirmOpen(true)}>
              {m.event_cancel_registration()}
            </Button>
          )}
          <Dialog
            open={confirmOpen}
            onClose={() => setConfirmOpen(false)}
            title={m.event_cancel_registration()}
          >
            <p className="text-sm text-fg">
              {pastDeadline
                ? m.event_cancel_confirm_late()
                : m.event_cancel_confirm({ name: event.name })}
            </p>
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" onClick={() => setConfirmOpen(false)}>
                {m.dialog_close()}
              </Button>
              <Button type="button" variant="danger" disabled={cancel.isPending} onClick={confirmCancel}>
                {m.event_cancel_registration()}
              </Button>
            </div>
          </Dialog>
        </>
      )}
      {reg?.status === "refund_requested" && (
        <p className="text-sm text-fg-muted">{m.events_my_refund_requested()}</p>
      )}
      {event.refundDeadline ? (
        <p className="text-xs text-fg-muted">
          {m.event_refund_until({ date: deadline.toLocaleString(getLocale()) })}
        </p>
      ) : (
        <p className="text-xs text-fg-muted">{m.event_refund_until_start()}</p>
      )}
    </div>
  );
}
```

(If `Button` has no `danger` variant, use the closest existing destructive variant — check `shared/ui/button.tsx` and reuse what the cube delete flow uses.)

- [ ] **Step 3: EventDetailPage** `frontend/src/features/events/components/EventDetailPage.tsx`:

```tsx
import { Link, useParams, useSearch } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { UnauthorizedError, useEvent } from "../api";
import { formatFee } from "../lib/money";
import { EventStatusBadge } from "./EventsListPage";
import { RegistrationPanel } from "./RegistrationPanel";

const CONFIRM_POLL_MS = 2_000;
const CONFIRM_CAP_MS = 60_000;

export function EventDetailPage() {
  const { eventId } = useParams({ from: "/events/$eventId/" });
  const search = useSearch({ from: "/events/$eventId/" });
  const me = useMe();

  // "confirming…" after the Stripe redirect: poll until the webhook flips
  // the registration to paid, capped at 60s. State-driven so the hook
  // options stay a plain number | false.
  const [confirming, setConfirming] = useState(search.checkout === "success");
  const [timedOut, setTimedOut] = useState(false);
  const event = useEvent(eventId, { refetchInterval: confirming ? CONFIRM_POLL_MS : false });

  // Stop confirming once the server shows anything other than a pending
  // payment (paid = success; absent/expired = nothing left to confirm).
  useEffect(() => {
    if (!confirming || !event.data) return;
    if (event.data.myRegistration?.status !== "pending_payment") setConfirming(false);
  }, [confirming, event.data]);

  useEffect(() => {
    if (!confirming) return;
    const t = setTimeout(() => {
      setConfirming(false);
      setTimedOut(true);
    }, CONFIRM_CAP_MS);
    return () => clearTimeout(t);
  }, [confirming]);

  if (event.error instanceof UnauthorizedError) {
    return <p className="text-fg-muted">{event.error.message}</p>;
  }
  if (event.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (event.error) return <p role="alert" className="text-danger">{m.events_not_found()}</p>;

  const e = event.data;
  const taken = e.paidCount + e.pendingCount;
  const isAdmin = me.data?.role === "admin";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-start justify-between gap-4">
        <h1 className="flex items-center gap-2 text-2xl font-semibold text-fg">
          {e.name}
          <EventStatusBadge status={e.status} />
        </h1>
        {isAdmin && (
          <Button asChild variant="outline" size="sm">
            <Link to="/events/$eventId/manage" params={{ eventId }}>
              {m.event_manage()}
            </Link>
          </Button>
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
        <div>
          <dt className="text-fg-muted">{m.event_date()}</dt>
          <dd className="text-fg">{new Date(e.startsAt).toLocaleString(getLocale())}</dd>
        </div>
        <div>
          <dt className="text-fg-muted">{m.event_location()}</dt>
          <dd className="text-fg">{e.location || "—"}</dd>
        </div>
        <div>
          <dt className="text-fg-muted">{m.event_fee()}</dt>
          <dd className="text-fg">
            {e.feeCents === 0 ? m.events_free() : formatFee(e.feeCents, e.currency)}
          </dd>
        </div>
        <div>
          <dt className="text-fg-muted">{m.event_organizer()}</dt>
          <dd className="text-fg">{e.organizerName}</dd>
        </div>
      </dl>

      {e.description && <p className="whitespace-pre-wrap text-fg">{e.description}</p>}

      {confirming ? (
        <div
          role="status"
          className="rounded-lg border border-border bg-surface-raised p-4 text-fg-muted"
        >
          {m.event_confirming_payment()}
        </div>
      ) : (
        <>
          {timedOut && e.myRegistration?.status === "pending_payment" && (
            <p className="text-sm text-fg-muted">{m.event_confirming_timeout()}</p>
          )}
          <RegistrationPanel event={e} checkoutCancelled={search.checkout === "cancelled"} />
        </>
      )}

      {e.cubes.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-medium text-fg">{m.event_cubes()}</h2>
          <ul className="flex flex-col gap-1">
            {e.cubes.map((c) => (
              <li key={c.cubeId} className="text-sm">
                <Link
                  to="/cubes/$cubeId"
                  params={{ cubeId: c.cubeId }}
                  className="text-accent hover:underline"
                >
                  {c.cubeName}
                </Link>
                {c.pinnedVersion != null && c.pinnedAt && (
                  <span className="text-fg-muted">
                    {" "}
                    ·{" "}
                    {m.event_cube_pinned({
                      version: c.pinnedVersion,
                      date: new Date(c.pinnedAt).toLocaleDateString(getLocale()),
                    })}
                  </span>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="flex flex-col gap-2">
        <h2 className="text-lg font-medium text-fg">
          {m.event_attendees({ count: e.paidCount })}
        </h2>
        <p className="text-sm text-fg-muted">
          {m.events_spots({ taken, total: e.maxParticipants })}
          {e.waitlistCount > 0 && ` · ${m.events_waitlist_count({ count: e.waitlistCount })}`}
        </p>
        {e.attendees.length === 0 ? (
          <p className="text-sm text-fg-muted">{m.event_attendees_empty()}</p>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {e.attendees.map((name, i) => (
              <li
                key={`${name}-${i}`}
                className="rounded-full border border-border px-3 py-1 text-sm text-fg"
              >
                {name}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
```

The polling contract: poll every 2 s while `checkout=success` and my registration is `pending_payment`, stop as soon as the status changes (or the registration disappears), hard-stop after 60 s with the timeout hint.

- [ ] **Step 4: RTL tests** `RegistrationPanel.test.tsx` — mock `../api` with `vi.mock`; render the panel directly with a `QueryClientProvider` (no router needed — the panel has no `Link`):

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import type { EventDetail } from "../api";

const register = vi.fn();
const pay = vi.fn();
const cancel = vi.fn();
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useRegister: () => ({ mutate: register, isPending: false, error: null }),
  usePay: () => ({ mutate: pay, isPending: false, error: null }),
  useCancelRegistration: () => ({ mutate: cancel, isPending: false, error: null }),
}));

import { RegistrationPanel } from "./RegistrationPanel";

function baseEvent(overrides: Partial<EventDetail>): EventDetail {
  return {
    id: "e1", name: "Cube Night", startsAt: "2026-08-01T18:00:00Z", location: "LGS",
    feeCents: 5000, currency: "pln", maxParticipants: 2,
    paidCount: 0, pendingCount: 0, waitlistCount: 0,
    status: "published", description: "", organizerName: "Org",
    cubes: [], attendees: [],
    ...overrides,
  } as EventDetail;
}

function renderPanel(event: EventDetail, checkoutCancelled = false) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationPanel event={event} checkoutCancelled={checkoutCancelled} />
    </QueryClientProvider>,
  );
}

test("no registration, spots free → Register", async () => {
  renderPanel(baseEvent({}));
  await userEvent.click(screen.getByRole("button", { name: "Register" }));
  expect(register).toHaveBeenCalled();
});

test("no registration, event full → Join the waitlist", () => {
  renderPanel(baseEvent({ paidCount: 2 }));
  expect(screen.getByRole("button", { name: "Join the waitlist" })).toBeInTheDocument();
});

test("pending payment → Pay now + countdown", () => {
  renderPanel(
    baseEvent({
      myRegistration: {
        id: "r1", status: "pending_payment",
        expiresAt: new Date(Date.now() + 3 * 3600_000).toISOString(),
      },
    }),
  );
  expect(screen.getByRole("button", { name: "Pay now" })).toBeInTheDocument();
  expect(screen.getByText(/Time left to pay/)).toBeInTheDocument();
});

test("paid past refund deadline → cancel warns about losing money", async () => {
  renderPanel(
    baseEvent({
      refundDeadline: new Date(Date.now() - 3600_000).toISOString(),
      myRegistration: { id: "r1", status: "paid" },
    }),
  );
  await userEvent.click(screen.getByRole("button", { name: "Cancel registration" }));
  expect(await screen.findByText(/only get your money back if the organizer approves/)).toBeInTheDocument();
});

test("refund_requested → status note, no buttons", () => {
  renderPanel(baseEvent({ myRegistration: { id: "r1", status: "refund_requested" } }));
  expect(screen.getByText(/refund pending organizer review/)).toBeInTheDocument();
  expect(screen.queryByRole("button")).not.toBeInTheDocument();
});
```

Add one page-level test for the checkout-return state, `EventDetailPage.test.tsx` — memory router at `/events/e1?checkout=success` (routes: detail as `$eventId` index, plus `/cubes/$cubeId`, `/events/$eventId/manage` stubs for the Links), fetch stubbed so `/api/me` returns a user and `/api/events/e1` returns a detail payload whose `myRegistration.status` is `"pending_payment"`:

```tsx
test("checkout=success with a pending registration shows the confirming panel", async () => {
  stubDetailFetch("pending_payment"); // helper mirroring Task 14's stubFetch
  renderAt("/events/e1?checkout=success");
  expect(await screen.findByText("Confirming your payment…")).toBeInTheDocument();
  // Server truth flips to paid on the next poll → panel switches to "You're in".
});

test("checkout=success with a paid registration renders server truth", async () => {
  stubDetailFetch("paid");
  renderAt("/events/e1?checkout=success");
  expect(await screen.findByText("You're in")).toBeInTheDocument();
  expect(screen.queryByText("Confirming your payment…")).not.toBeInTheDocument();
});
```

(`renderAt` = the Task 14 router scaffold with `initialEntries: [path]`; `stubDetailFetch(status)` returns the full `EventDetailBody` shape from Task 15's `baseEvent` plus `myRegistration: { id: "r1", status }`.)

- [ ] **Step 5:** `pnpm --filter @cube-planner/frontend test -- RegistrationPanel && pnpm --filter @cube-planner/frontend typecheck` → PASS.

- [ ] **Step 6: Commit**

```bash
pnpm --filter @cube-planner/frontend fmt && pnpm --filter @cube-planner/frontend lint
git add frontend/src/features/events frontend/src/routes/events.\$eventId.index.tsx
git commit -m "feat(events): event detail page with registration CTA and checkout polling"
```

---

### Task 16: Organizer — event form, new/manage routes, lifecycle, cube linking

**Files:**
- Create: `frontend/src/features/events/components/EventForm.tsx`
- Create: `frontend/src/features/events/components/NewEventPage.tsx`
- Create: `frontend/src/features/events/components/ManageEventPage.tsx`
- Create: `frontend/src/features/events/components/EventCubesEditor.tsx`
- Create: `frontend/src/routes/events.new.tsx`, `frontend/src/routes/events.$eventId.manage.tsx`

**Interfaces:**
- Consumes: Task 13 hooks (`useCreateEvent`, `useUpdateEvent`, `useEventAction`, `useSetEventCubes`, `useLinkableCubes`, `useCubeChangelog`), shared `Input`, `Label`, `Button`, `Dialog`.
- Produces: `/events/new` + `/events/$eventId/manage` (admin-gated in the component: non-admins get the not-found message — no existence leak); `EventForm` with lifecycle-aware field locking (`lockedAfterPublish` set: name, startsAt, fee, currency, maxParticipants).

- [ ] **Step 1: Routes.**

`frontend/src/routes/events.new.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { NewEventPage } from "@/features/events/components/NewEventPage";

export const Route = createFileRoute("/events/new")({ component: NewEventPage });
```

`frontend/src/routes/events.$eventId.manage.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { ManageEventPage } from "@/features/events/components/ManageEventPage";

export const Route = createFileRoute("/events/$eventId/manage")({ component: ManageEventPage });
```

- [ ] **Step 2: EventForm** — controlled form used by both pages. Full component:

```tsx
import { useState } from "react";
import type { FormEvent } from "react";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import type { EventDetail, EventFormValues } from "../api";

// datetime-local wants "YYYY-MM-DDTHH:mm" in local time.
function toLocalInput(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromLocalInput(v: string): string | undefined {
  return v ? new Date(v).toISOString() : undefined;
}

export function EventForm({
  initial,
  locked,
  submitLabel,
  onSubmit,
  pending,
  error,
}: {
  initial?: EventDetail;
  /** true once published: name/date/fee/participants are frozen. */
  locked: boolean;
  submitLabel: string;
  onSubmit: (values: EventFormValues) => void;
  pending: boolean;
  error: Error | null;
}) {
  const [name, setName] = useState(initial?.name ?? "");
  const [description, setDescription] = useState(initial?.description ?? "");
  const [location, setLocation] = useState(initial?.location ?? "");
  const [startsAt, setStartsAt] = useState(toLocalInput(initial?.startsAt));
  const [feePln, setFeePln] = useState(initial ? String(initial.feeCents / 100) : "0");
  const [maxParticipants, setMaxParticipants] = useState(String(initial?.maxParticipants ?? 8));
  const [refundDeadline, setRefundDeadline] = useState(toLocalInput(initial?.refundDeadline ?? undefined));

  const submit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit({
      name,
      description,
      location,
      startsAt: fromLocalInput(startsAt) ?? new Date().toISOString(),
      feeCents: Math.round(Number(feePln) * 100),
      maxParticipants: Number(maxParticipants),
      ...(fromLocalInput(refundDeadline) ? { refundDeadline: fromLocalInput(refundDeadline) } : {}),
    });
  };

  const lockedHint = locked ? (
    <span className="text-xs text-fg-muted"> ({m.event_form_locked_hint()})</span>
  ) : null;

  return (
    <form onSubmit={submit} className="flex max-w-lg flex-col gap-4">
      {error && (
        <p role="alert" className="text-sm text-danger">
          {error.message}
        </p>
      )}
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-name">
          {m.event_form_name()}
          {lockedHint}
        </Label>
        <Input id="ev-name" required maxLength={200} disabled={locked} value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-desc">{m.event_form_description()}</Label>
        <textarea
          id="ev-desc"
          className="rounded-md border border-border bg-surface px-3 py-2 text-fg"
          rows={4}
          maxLength={5000}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-location">{m.event_form_location()}</Label>
        <Input id="ev-location" maxLength={200} value={location} onChange={(e) => setLocation(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-starts">
          {m.event_form_starts_at()}
          {lockedHint}
        </Label>
        <Input id="ev-starts" type="datetime-local" required disabled={locked} value={startsAt} onChange={(e) => setStartsAt(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-fee">
          {m.event_form_fee()}
          {lockedHint}
        </Label>
        <Input
          id="ev-fee"
          type="number"
          min="0"
          step="0.01"
          required
          disabled={locked}
          value={feePln}
          onChange={(e) => setFeePln(e.target.value)}
          aria-describedby="ev-fee-hint"
        />
        <p id="ev-fee-hint" className="text-xs text-fg-muted">
          {m.event_form_fee_hint()}
        </p>
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-max">
          {m.event_form_max_participants()}
          {lockedHint}
        </Label>
        <Input id="ev-max" type="number" min="1" max="1000" required disabled={locked} value={maxParticipants} onChange={(e) => setMaxParticipants(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="ev-refund">{m.event_form_refund_deadline()}</Label>
        <Input id="ev-refund" type="datetime-local" value={refundDeadline} onChange={(e) => setRefundDeadline(e.target.value)} />
      </div>
      <Button type="submit" disabled={pending}>
        {submitLabel}
      </Button>
    </form>
  );
}
```

- [ ] **Step 3: NewEventPage** — admin gate + create → navigate to manage:

```tsx
import { useNavigate } from "@tanstack/react-router";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { useCreateEvent } from "../api";
import { EventForm } from "./EventForm";

export function NewEventPage() {
  const me = useMe();
  const navigate = useNavigate();
  const create = useCreateEvent();

  if (me.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (me.data?.role !== "admin") return <p className="text-fg-muted">{m.events_not_found()}</p>;

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold text-fg">{m.events_new_title()}</h1>
      <EventForm
        locked={false}
        submitLabel={m.event_form_create()}
        pending={create.isPending}
        error={create.error}
        onSubmit={(values) =>
          create.mutate(values, {
            onSuccess: (ev) =>
              navigate({ to: "/events/$eventId/manage", params: { eventId: ev.id } }),
          })
        }
      />
    </div>
  );
}
```

- [ ] **Step 4: EventCubesEditor** — replace-set editing of linked cubes with optional pins (draft only):

```tsx
import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Label } from "@/shared/ui/label";
import type { EventDetail } from "../api";
import { useCubeChangelog, useLinkableCubes, useSetEventCubes } from "../api";

type Draft = { cubeId: string; cubeName: string; cubeChangeId?: string };

export function EventCubesEditor({ event }: { event: EventDetail }) {
  const linkable = useLinkableCubes();
  const setCubes = useSetEventCubes(event.id);
  const [links, setLinks] = useState<Draft[]>(
    event.cubes.map((c) => ({
      cubeId: c.cubeId,
      cubeName: c.cubeName,
      ...(c.cubeChangeId ? { cubeChangeId: c.cubeChangeId } : {}),
    })),
  );
  const [adding, setAdding] = useState("");
  const editable = event.status === "draft";

  const save = (next: Draft[]) => {
    setLinks(next);
    setCubes.mutate(next.map(({ cubeId, cubeChangeId }) => ({ cubeId, ...(cubeChangeId ? { cubeChangeId } : {}) })));
  };

  return (
    <section className="flex flex-col gap-3">
      <h2 className="text-lg font-medium text-fg">{m.event_cubes_editor_title()}</h2>
      {setCubes.error && (
        <p role="alert" className="text-sm text-danger">
          {setCubes.error.message}
        </p>
      )}
      <ul className="flex flex-col gap-2">
        {links.map((l) => (
          <li key={l.cubeId} className="flex flex-wrap items-center gap-2 text-sm">
            <span className="font-medium text-fg">{l.cubeName}</span>
            {editable && (
              <>
                <PinPicker link={l} onChange={(cubeChangeId) =>
                  save(links.map((x) => (x.cubeId === l.cubeId ? { ...x, ...(cubeChangeId ? { cubeChangeId } : {}) } : x)))
                } />
                <Button type="button" variant="ghost" size="sm" onClick={() => save(links.filter((x) => x.cubeId !== l.cubeId))}>
                  {m.event_cubes_remove()}
                </Button>
              </>
            )}
          </li>
        ))}
      </ul>
      {editable && (
        <div className="flex items-end gap-2">
          <div className="flex flex-col gap-1">
            <Label htmlFor="cube-add">{m.event_cubes_add()}</Label>
            <select
              id="cube-add"
              className="rounded-md border border-border bg-surface px-3 py-2 text-fg"
              value={adding}
              onChange={(e) => setAdding(e.target.value)}
            >
              <option value="" />
              {(linkable.data ?? [])
                .filter((c) => !links.some((l) => l.cubeId === c.id))
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
            </select>
          </div>
          <Button
            type="button"
            size="sm"
            disabled={!adding}
            onClick={() => {
              const cube = linkable.data?.find((c) => c.id === adding);
              if (!cube) return;
              save([...links, { cubeId: cube.id, cubeName: cube.name }]);
              setAdding("");
            }}
          >
            {m.event_cubes_add()}
          </Button>
        </div>
      )}
    </section>
  );
}

function PinPicker({ link, onChange }: { link: Draft; onChange: (changeId?: string) => void }) {
  const changes = useCubeChangelog(link.cubeId);
  const id = `pin-${link.cubeId}`;
  return (
    <span className="flex items-center gap-1">
      <Label htmlFor={id} className="text-xs text-fg-muted">
        {m.event_cubes_pin_label()}
      </Label>
      <select
        id={id}
        className="rounded-md border border-border bg-surface px-2 py-1 text-xs text-fg"
        value={link.cubeChangeId ?? ""}
        onChange={(e) => onChange(e.target.value || undefined)}
      >
        <option value="">{m.event_cubes_pin_live()}</option>
        {(changes.data ?? []).map((ch) => (
          <option key={ch.id} value={ch.id}>
            v{ch.version} · {new Date(ch.createdAt).toLocaleDateString(getLocale())}
          </option>
        ))}
      </select>
    </span>
  );
}
```

(Field names of a changelog entry — `id`, `version`, `createdAt` — come from the generated `ChangelogEntry` type; adjust to the schema.)

- [ ] **Step 5: ManageEventPage** — form + lifecycle + cubes + registrations (table lands Task 17):

```tsx
import { Link, useParams } from "@tanstack/react-router";
import { useState } from "react";
import { m } from "@/paraglide/messages";
import { useMe } from "@/features/auth/api";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import type { EventAction } from "../api";
import { UnauthorizedError, useEvent, useEventAction, useUpdateEvent } from "../api";
import { EventStatusBadge } from "./EventsListPage";
import { EventCubesEditor } from "./EventCubesEditor";
import { EventForm } from "./EventForm";
import { RegistrationsTable } from "./RegistrationsTable";

const ACTIONS: { action: EventAction; label: () => string; confirm: (name: string) => string; from: string[] }[] = [
  { action: "publish", label: () => m.event_lifecycle_publish(), confirm: (name) => m.event_publish_confirm({ name }), from: ["draft"] },
  { action: "start", label: () => m.event_lifecycle_start(), confirm: (name) => m.event_start_confirm({ name }), from: ["published"] },
  { action: "finish", label: () => m.event_lifecycle_finish(), confirm: (name) => m.event_finish_confirm({ name }), from: ["started"] },
  { action: "cancel", label: () => m.event_lifecycle_cancel(), confirm: (name) => m.event_cancel_event_confirm({ name }), from: ["published", "started"] },
];

export function ManageEventPage() {
  const { eventId } = useParams({ from: "/events/$eventId/manage" });
  const me = useMe();
  const event = useEvent(eventId);
  const update = useUpdateEvent(eventId);
  const act = useEventAction(eventId);
  const [confirmAction, setConfirmAction] = useState<EventAction | null>(null);
  const [saved, setSaved] = useState(false);

  if (me.isPending || event.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (me.data?.role !== "admin") return <p className="text-fg-muted">{m.events_not_found()}</p>;
  if (event.error instanceof UnauthorizedError) return <p className="text-fg-muted">{event.error.message}</p>;
  if (event.error) return <p role="alert" className="text-danger">{m.events_not_found()}</p>;

  const e = event.data;
  const pendingConfirm = ACTIONS.find((a) => a.action === confirmAction);

  return (
    <div className="flex flex-col gap-8">
      <div className="flex items-center justify-between gap-4">
        <h1 className="flex items-center gap-2 text-2xl font-semibold text-fg">
          <Link to="/events/$eventId" params={{ eventId }} className="hover:text-accent">
            {e.name}
          </Link>
          <EventStatusBadge status={e.status} />
        </h1>
        <div className="flex gap-2">
          {ACTIONS.filter((a) => a.from.includes(e.status)).map((a) => (
            <Button key={a.action} type="button" size="sm" variant={a.action === "cancel" ? "outline" : "primary"} onClick={() => setConfirmAction(a.action)}>
              {a.label()}
            </Button>
          ))}
        </div>
      </div>

      {act.error && (
        <p role="alert" className="text-sm text-danger">
          {act.error.message}
        </p>
      )}

      <EventForm
        initial={e}
        locked={e.status !== "draft"}
        submitLabel={m.event_form_save()}
        pending={update.isPending}
        error={update.error}
        onSubmit={(values) => {
          const body =
            e.status === "draft"
              ? values
              : {
                  description: values.description,
                  location: values.location,
                  ...(values.refundDeadline ? { refundDeadline: values.refundDeadline } : {}),
                };
          update.mutate(body, { onSuccess: () => setSaved(true) });
        }}
      />
      {saved && <p role="status" className="text-sm text-fg-muted">{m.event_form_saved()}</p>}

      <EventCubesEditor event={e} />

      <RegistrationsTable eventId={eventId} />

      <Dialog
        open={confirmAction != null}
        onClose={() => setConfirmAction(null)}
        title={pendingConfirm?.label() ?? ""}
      >
        <p className="text-sm text-fg">{pendingConfirm?.confirm(e.name)}</p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => setConfirmAction(null)}>
            {m.dialog_close()}
          </Button>
          <Button
            type="button"
            disabled={act.isPending}
            onClick={() => {
              if (confirmAction) act.mutate(confirmAction);
              setConfirmAction(null);
            }}
          >
            {pendingConfirm?.label()}
          </Button>
        </div>
      </Dialog>
    </div>
  );
}
```

(If `Button` variants differ — `primary` may be the default with no explicit name — match `shared/ui/button.tsx`'s cva variants.)

- [ ] **Step 6:** `pnpm --filter @cube-planner/frontend typecheck` — will fail on the missing `RegistrationsTable` until Task 17; if executing tasks strictly in order, create a placeholder export in Task 16:

```tsx
// frontend/src/features/events/components/RegistrationsTable.tsx
export function RegistrationsTable(_props: { eventId: string }) {
  return null; // real table lands in the next task
}
```

then typecheck + `pnpm --filter @cube-planner/frontend test` → PASS.

- [ ] **Step 7: Commit**

```bash
pnpm --filter @cube-planner/frontend fmt && pnpm --filter @cube-planner/frontend lint
git add frontend/src/features/events frontend/src/routes/events.new.tsx frontend/src/routes/events.\$eventId.manage.tsx
git commit -m "feat(events): organizer pages — form, lifecycle actions, cube linking"
```

---

### Task 17: Registrations table + refund queue

**Files:**
- Modify: `frontend/src/features/events/components/RegistrationsTable.tsx` (replace the placeholder)
- Create: `frontend/src/features/events/components/RegistrationsTable.test.tsx`

**Interfaces:**
- Consumes: `useEventRegistrations`, `useRefundRegistration`, `useDenyRefund` (Task 13), shared `Dialog`/`Button`.
- Produces: grouped organizer table with per-row Refund / Deny actions and confirm dialogs.

- [ ] **Step 1: Component:**

```tsx
import { useState } from "react";
import { getLocale } from "@/paraglide/runtime";
import { m } from "@/paraglide/messages";
import { Button } from "@/shared/ui/button";
import { Dialog } from "@/shared/ui/dialog";
import type { EventRegistrationRow } from "../api";
import { useDenyRefund, useEventRegistrations, useRefundRegistration } from "../api";

const GROUPS: { key: string; title: () => string; statuses: string[] }[] = [
  { key: "paid", title: () => m.regs_group_paid(), statuses: ["paid"] },
  { key: "pending", title: () => m.regs_group_pending(), statuses: ["pending_payment"] },
  { key: "waitlist", title: () => m.regs_group_waitlist(), statuses: ["waitlisted"] },
  { key: "queue", title: () => m.regs_group_refund_queue(), statuses: ["refund_requested"] },
  { key: "history", title: () => m.regs_group_history(), statuses: ["cancelled", "refunded", "expired"] },
];

type Confirm = { kind: "refund" | "deny"; row: EventRegistrationRow };

export function RegistrationsTable({ eventId }: { eventId: string }) {
  const regs = useEventRegistrations(eventId);
  const refund = useRefundRegistration(eventId);
  const deny = useDenyRefund(eventId);
  const [confirm, setConfirm] = useState<Confirm | null>(null);

  if (regs.isPending) return <p className="text-fg-muted">{m.loading()}</p>;
  if (regs.error) return <p role="alert" className="text-danger">{regs.error.message}</p>;

  const err = refund.error ?? deny.error;
  const locale = getLocale();

  const rowMeta = (r: EventRegistrationRow) => {
    if (r.status === "pending_payment" && r.expiresAt) {
      return m.regs_expires({ date: new Date(r.expiresAt).toLocaleString(locale) });
    }
    if (r.status === "paid" && r.paidAt) {
      return m.regs_paid_at({ date: new Date(r.paidAt).toLocaleString(locale) });
    }
    if (r.status === "waitlisted" && r.waitlistPos != null) return `#${r.waitlistPos}`;
    return r.status;
  };

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-medium text-fg">{m.regs_title()}</h2>
      {err && (
        <p role="alert" className="text-sm text-danger">
          {err.message}
        </p>
      )}
      {GROUPS.map((g) => {
        const rows = (regs.data ?? [])
          .filter((r) => g.statuses.includes(r.status))
          .sort((a, b) => (a.waitlistPos ?? 0) - (b.waitlistPos ?? 0));
        return (
          <div key={g.key} className="flex flex-col gap-1">
            <h3 className="text-sm font-medium text-fg-muted">{g.title()}</h3>
            {rows.length === 0 ? (
              <p className="text-sm text-fg-muted">{m.regs_empty()}</p>
            ) : (
              <ul className="flex flex-col divide-y divide-border rounded-lg border border-border">
                {rows.map((r) => (
                  <li key={r.id} className="flex flex-wrap items-center justify-between gap-2 p-3 text-sm">
                    <span className="text-fg">
                      {r.displayName} <span className="text-fg-muted">({r.email})</span>
                    </span>
                    <span className="flex items-center gap-3">
                      <span className="text-fg-muted">{rowMeta(r)}</span>
                      {(r.status === "refund_requested" || r.status === "paid") && (
                        <Button type="button" size="sm" variant="outline" disabled={refund.isPending} onClick={() => setConfirm({ kind: "refund", row: r })}>
                          {m.regs_refund()}
                        </Button>
                      )}
                      {r.status === "refund_requested" && (
                        <Button type="button" size="sm" variant="ghost" disabled={deny.isPending} onClick={() => setConfirm({ kind: "deny", row: r })}>
                          {m.regs_deny()}
                        </Button>
                      )}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        );
      })}
      <Dialog
        open={confirm != null}
        onClose={() => setConfirm(null)}
        title={confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
      >
        <p className="text-sm text-fg">
          {confirm?.kind === "deny"
            ? m.regs_deny_confirm({ name: confirm.row.displayName })
            : confirm
              ? m.regs_refund_confirm({ name: confirm.row.displayName })
              : ""}
        </p>
        <div className="flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={() => setConfirm(null)}>
            {m.dialog_close()}
          </Button>
          <Button
            type="button"
            onClick={() => {
              if (confirm?.kind === "refund") refund.mutate(confirm.row.id);
              if (confirm?.kind === "deny") deny.mutate(confirm.row.id);
              setConfirm(null);
            }}
          >
            {confirm?.kind === "deny" ? m.regs_deny() : m.regs_refund()}
          </Button>
        </div>
      </Dialog>
    </section>
  );
}
```

- [ ] **Step 2: RTL test** `RegistrationsTable.test.tsx` — mock `../api` (`useEventRegistrations` returning one row per group, `useRefundRegistration`/`useDenyRefund` with spy `mutate`): assert group headings render, the refund-queue row shows both Refund and Deny, clicking Refund opens the confirm dialog and confirming calls `refund.mutate` with the row id; the waitlisted row shows `#1`; history rows show no action buttons.

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";

const refundMutate = vi.fn();
const denyMutate = vi.fn();
const rows = [
  { id: "r1", status: "paid", displayName: "Ala", email: "ala@t", createdAt: "2026-07-13T10:00:00Z", paidAt: "2026-07-13T10:05:00Z" },
  { id: "r2", status: "waitlisted", displayName: "Bea", email: "bea@t", createdAt: "2026-07-13T10:01:00Z", waitlistPos: 1 },
  { id: "r3", status: "refund_requested", displayName: "Cez", email: "cez@t", createdAt: "2026-07-13T10:02:00Z" },
  { id: "r4", status: "expired", displayName: "Dag", email: "dag@t", createdAt: "2026-07-13T10:03:00Z" },
];
vi.mock("../api", async (orig) => ({
  ...(await orig()),
  useEventRegistrations: () => ({ data: rows, isPending: false, error: null }),
  useRefundRegistration: () => ({ mutate: refundMutate, isPending: false, error: null }),
  useDenyRefund: () => ({ mutate: denyMutate, isPending: false, error: null }),
}));

import { RegistrationsTable } from "./RegistrationsTable";

function renderTable() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegistrationsTable eventId="e1" />
    </QueryClientProvider>,
  );
}

test("groups rows and gates actions by status", () => {
  renderTable();
  expect(screen.getByText("Paid roster")).toBeInTheDocument();
  expect(screen.getByText("Refund queue")).toBeInTheDocument();
  expect(screen.getByText("#1")).toBeInTheDocument();
  // paid + refund_requested rows each get a Refund button; only the
  // queued row gets Deny.
  expect(screen.getAllByRole("button", { name: "Refund" })).toHaveLength(2);
  expect(screen.getAllByRole("button", { name: "Deny" })).toHaveLength(1);
});

test("refund flows through the confirm dialog", async () => {
  renderTable();
  await userEvent.click(screen.getAllByRole("button", { name: "Refund" })[1]!);
  expect(await screen.findByText(/Refund Cez's entry fee\?/)).toBeInTheDocument();
  // The dialog's action button is the last "Refund" in the DOM.
  const buttons = screen.getAllByRole("button", { name: "Refund" });
  await userEvent.click(buttons[buttons.length - 1]!);
  expect(refundMutate).toHaveBeenCalledWith("r3");
});
```

- [ ] **Step 3:** `pnpm --filter @cube-planner/frontend test -- RegistrationsTable && pnpm --filter @cube-planner/frontend typecheck` → PASS.

- [ ] **Step 4: Commit**

```bash
pnpm --filter @cube-planner/frontend fmt && pnpm --filter @cube-planner/frontend lint
git add frontend/src/features/events
git commit -m "feat(events): organizer registrations table with refund queue"
```

---

### Task 18: a11y smoke tests, i18n parity, full verification

**Files:**
- Create: `frontend/src/features/events/components/a11y.test.tsx`

- [ ] **Step 1: Axe smoke tests** (jsdom opt-in, patterned on `features/cubes/components/a11y.test.tsx` — reuse its render/mock scaffolding style):

```tsx
// @vitest-environment jsdom
```

One axe pass per screen: `EventsListPage` (fetch-stubbed as in Task 14), `EventDetailPage` route render with a full detail payload (registration panel in the pending_payment state), `ManageEventPage` with an admin `/api/me` stub and a draft event. Assert `expect(await axe(container)).toHaveNoViolations()` per screen, matching how the cubes a11y file does it.

- [ ] **Step 2: i18n parity check**

Run: `jq -S 'keys' frontend/messages/en.json > /tmp/en.keys && jq -S 'keys' frontend/messages/pl.json > /tmp/pl.keys && diff /tmp/en.keys /tmp/pl.keys`
Expected: no diff.

- [ ] **Step 3: Full sweep**

Run: `make test` (Docker running) and `pnpm --filter @cube-planner/frontend typecheck && pnpm --filter @cube-planner/frontend lint`
Expected: everything green, including the CI-mirroring checks lefthook runs on pre-push.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/features/events
git commit -m "test(events): axe smoke tests for events screens"
```

---

## Manual acceptance (after all tasks)

Mateusz tests in the browser (as in sub-projects 2–4): `make up` + `make stripe-listen` with test-mode keys in `.env`, flip his user to admin (`update users set role = 'admin' where email = …` via psql), then: create + publish a paid event → register + pay with Stripe test card `4242 4242 4242 4242` → webhook flips to paid → cancel within window → refund lands in Stripe dashboard → waitlist flow with a second account → refund queue approve/deny → cancel event mass refund. Free-event flow without Stripe keys.

## Deviations

Document any deviations from this plan at the end of this file during execution (same convention as sub-project 4).
