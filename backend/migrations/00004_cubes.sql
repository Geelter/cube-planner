-- +goose Up
create table cubes (
    id uuid primary key default gen_random_uuid(),
    owner_id uuid not null references users (id) on delete cascade,
    name text not null check (char_length(name) between 1 and 100),
    description text not null default '',
    visibility text not null check (visibility in ('public', 'private')),
    version int not null default 0,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index cubes_visibility_updated_idx on cubes (visibility, updated_at desc);
create index cubes_owner_idx on cubes (owner_id);
create index cubes_name_trgm_idx on cubes using gin (name gin_trgm_ops);

-- Materialized current list. One row per oracle card: quantities live in
-- the column; two printings of the same card cannot coexist.
-- quantity >= 0 (not >= 1): RemoveCubeCardQuantity and DeleteDepletedCubeCards
-- run as two separate statements, so a full-depletion removal must be able to
-- park a row at 0 before the cleanup delete runs. Postgres CHECK constraints
-- cannot be DEFERRABLE, so >= 1 would reject the intermediate UPDATE outright.
create table cube_cards (
    cube_id uuid not null references cubes (id) on delete cascade,
    oracle_id uuid not null,
    scryfall_id uuid not null references cards (scryfall_id),
    quantity int not null check (quantity >= 0),
    added_at timestamptz not null default now(),
    primary key (cube_id, oracle_id)
);

create index cube_cards_scryfall_idx on cube_cards (scryfall_id);

-- Append-only changelog. version = the cube version this change produced.
create table cube_changes (
    id uuid primary key default gen_random_uuid(),
    cube_id uuid not null references cubes (id) on delete cascade,
    version int not null,
    author_id uuid not null references users (id),
    note text not null default '' check (char_length(note) <= 500),
    created_at timestamptz not null default now(),
    unique (cube_id, version)
);

create table cube_change_items (
    change_id uuid not null references cube_changes (id) on delete cascade,
    kind text not null check (kind in ('add', 'remove')),
    oracle_id uuid not null,
    scryfall_id uuid not null references cards (scryfall_id),
    quantity int not null check (quantity >= 1),
    primary key (change_id, kind, oracle_id)
);

create index cube_change_items_scryfall_idx on cube_change_items (scryfall_id);

-- +goose Down
drop table cube_change_items;
drop table cube_changes;
drop table cube_cards;
drop table cubes;
