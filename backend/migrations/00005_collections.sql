-- +goose Up
-- Printing-level collection. quantity >= 1 (not >= 0 like cube_cards):
-- setting quantity to 0 is a plain DELETE in the same handler, so no
-- intermediate 0 state ever exists.
create table collection_items (
    user_id uuid not null references users (id) on delete cascade,
    scryfall_id uuid not null references cards (scryfall_id),
    oracle_id uuid not null,
    quantity int not null check (quantity >= 1),
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (user_id, scryfall_id)
);

-- Wantlist aggregation groups a user's items by oracle card.
create index collection_items_user_oracle_idx on collection_items (user_id, oracle_id);
create index collection_items_scryfall_idx on collection_items (scryfall_id);

-- +goose Down
drop table collection_items;
