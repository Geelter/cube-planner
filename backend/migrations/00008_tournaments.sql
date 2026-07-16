-- +goose Up
create table tournaments (
    id uuid primary key default gen_random_uuid(),
    event_id uuid not null unique references events (id) on delete cascade,
    planned_rounds int not null check (planned_rounds >= 1),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table tournament_players (
    id uuid primary key default gen_random_uuid(),
    tournament_id uuid not null references tournaments (id) on delete cascade,
    user_id uuid not null references users (id),
    dropped_at timestamptz,
    created_at timestamptz not null default now(),
    unique (tournament_id, user_id)
);

create index tournament_players_tournament_idx
    on tournament_players (tournament_id);

create table rounds (
    id uuid primary key default gen_random_uuid(),
    tournament_id uuid not null references tournaments (id) on delete cascade,
    number int not null check (number >= 1),
    status text not null default 'draft'
        check (status in ('draft', 'published', 'completed')),
    -- Pairing seed: re-roll stores a new seed; pairing is deterministic
    -- given (roster, history, seed).
    seed bigint not null,
    published_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (tournament_id, number)
);

create index rounds_tournament_idx on rounds (tournament_id);

create table matches (
    id uuid primary key default gen_random_uuid(),
    round_id uuid not null references rounds (id) on delete cascade,
    table_number int not null check (table_number >= 1),
    player1_id uuid not null references tournament_players (id),
    -- Null player2 = bye. Byes are inserted with an auto 2-0 result so
    -- standings math has a single input shape.
    player2_id uuid references tournament_players (id),
    p1_games int check (p1_games between 0 and 2),
    p2_games int check (p2_games between 0 and 2),
    draws int check (draws between 0 and 3),
    -- Result columns are all null (unreported) or all non-null; the
    -- service enforces that plus p1+p2+draws <= 3 and not both at 2.
    reported_by uuid references users (id),
    reported_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (round_id, table_number),
    check (player2_id is null or player1_id <> player2_id)
);

create index matches_round_idx on matches (round_id);

-- +goose Down
drop table matches;
drop table rounds;
drop table tournament_players;
drop table tournaments;
