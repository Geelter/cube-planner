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
