-- +goose Up
create extension if not exists pg_trgm;

create table cards (
    scryfall_id uuid primary key,
    oracle_id uuid not null,
    name text not null,
    normalized_name text not null,
    released_at date not null,
    set_code text not null,
    set_name text not null,
    collector_number text not null,
    rarity text not null,
    layout text not null,
    mana_cost text not null default '',
    cmc double precision not null,
    type_line text not null,
    oracle_text text not null default '',
    colors text[] not null,
    color_identity text[] not null,
    promo boolean not null,
    image_small text,
    image_normal text,
    back_image_small text,
    back_image_normal text,
    updated_at timestamptz not null default now()
);

create index cards_oracle_id_idx on cards (oracle_id);
create index cards_normalized_name_trgm_idx on cards using gin (normalized_name gin_trgm_ops);

-- Import lands here via COPY; one transaction then upserts into cards and
-- deletes rows missing from staging. Spelled out (not "like cards") so sqlc
-- can parse it.
create table cards_staging (
    scryfall_id uuid primary key,
    oracle_id uuid not null,
    name text not null,
    normalized_name text not null,
    released_at date not null,
    set_code text not null,
    set_name text not null,
    collector_number text not null,
    rarity text not null,
    layout text not null,
    mana_cost text not null default '',
    cmc double precision not null,
    type_line text not null,
    oracle_text text not null default '',
    colors text[] not null,
    color_identity text[] not null,
    promo boolean not null,
    image_small text,
    image_normal text,
    back_image_small text,
    back_image_normal text
);

create table card_sync_runs (
    id bigserial primary key,
    started_at timestamptz not null default now(),
    finished_at timestamptz,
    status text not null check (status in ('running', 'succeeded', 'failed')),
    bulk_updated_at timestamptz,
    cards_count int,
    error text
);

-- +goose Down
drop table card_sync_runs;
drop table cards_staging;
drop table cards;
drop extension if exists pg_trgm;
