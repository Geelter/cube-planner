-- +goose Up
create table users (
    id uuid primary key default gen_random_uuid(),
    email text not null unique,
    display_name text not null,
    password_hash text,
    email_verified_at timestamptz,
    created_at timestamptz not null default now()
);

create table oauth_identities (
    provider text not null,
    provider_user_id text not null,
    user_id uuid not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    primary key (provider, provider_user_id)
);

create table sessions (
    token_hash bytea primary key,
    user_id uuid not null references users (id) on delete cascade,
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);

create table auth_tokens (
    token_hash bytea primary key,
    user_id uuid not null references users (id) on delete cascade,
    purpose text not null check (purpose in ('email_verification', 'password_reset')),
    created_at timestamptz not null default now(),
    expires_at timestamptz not null
);

-- +goose Down
drop table auth_tokens;
drop table sessions;
drop table oauth_identities;
drop table users;
