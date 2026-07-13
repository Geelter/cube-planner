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
