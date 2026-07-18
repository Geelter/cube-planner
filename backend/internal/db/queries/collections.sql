-- The session user's collection, name-sorted, with display data and
-- window totals (distinct rows + summed copies) over the filtered set.
-- Plain ILIKE is enough at collection scale — no trigram machinery.
-- name: ListCollectionItems :many
select ci.scryfall_id, ci.oracle_id, ci.quantity,
    ca.name, ca.mana_cost, ca.type_line, ca.set_code, ca.set_name,
    ca.collector_number, ca.image_small, ca.image_normal,
    count(*) over () as total,
    sum(ci.quantity) over ()::bigint as total_copies
from collection_items ci
join cards ca on ca.scryfall_id = ci.scryfall_id
where ci.user_id = sqlc.arg(user_id)
  and (sqlc.narg(search)::text is null or ca.name ilike '%' || sqlc.narg(search) || '%')
order by ca.name, ca.set_code, ca.collector_number
limit sqlc.arg(page_limit)::int offset sqlc.arg(page_offset)::int;

-- Set-quantity upsert. Selecting from cards resolves oracle_id at write
-- time AND makes an unknown printing insert zero rows (pgx.ErrNoRows on
-- :one), which the service maps to the invalid-item error.
-- name: UpsertCollectionItem :one
insert into collection_items (user_id, scryfall_id, oracle_id, quantity)
select sqlc.arg(user_id), c.scryfall_id, c.oracle_id, sqlc.arg(quantity)
from cards c
where c.scryfall_id = sqlc.arg(scryfall_id)
on conflict (user_id, scryfall_id)
do update set quantity = excluded.quantity, updated_at = now()
returning *;

-- Add-quantity upsert (import + change-printing merge). 999 is the hard
-- per-printing maximum everywhere, so results clamp instead of erroring.
-- name: AddCollectionItem :execrows
insert into collection_items (user_id, scryfall_id, oracle_id, quantity)
select sqlc.arg(user_id), c.scryfall_id, c.oracle_id, least(sqlc.arg(quantity)::int, 999)
from cards c
where c.scryfall_id = sqlc.arg(scryfall_id)
on conflict (user_id, scryfall_id)
do update set
    quantity = least(collection_items.quantity + excluded.quantity, 999),
    updated_at = now();

-- name: DeleteCollectionItem :execrows
delete from collection_items
where user_id = sqlc.arg(user_id) and scryfall_id = sqlc.arg(scryfall_id);

-- One item with display data (PUT / change-printing responses).
-- name: GetCollectionItemEntry :one
select ci.scryfall_id, ci.oracle_id, ci.quantity,
    ca.name, ca.mana_cost, ca.type_line, ca.set_code, ca.set_name,
    ca.collector_number, ca.image_small, ca.image_normal
from collection_items ci
join cards ca on ca.scryfall_id = ci.scryfall_id
where ci.user_id = sqlc.arg(user_id) and ci.scryfall_id = sqlc.arg(scryfall_id);

-- Locks the source row for the change-printing transaction.
-- name: GetCollectionItemForUpdate :one
select * from collection_items
where user_id = sqlc.arg(user_id) and scryfall_id = sqlc.arg(scryfall_id)
for update;

-- Which of these printings does the user already own? (import summary)
-- name: GetOwnedScryfallIDs :many
select scryfall_id from collection_items
where user_id = sqlc.arg(user_id) and scryfall_id = any(sqlc.arg(ids)::uuid[]);

-- Exact-name resolution for import: representative printing per oracle
-- card (same non-promo/newest/has-image rule as autocomplete). Several
-- oracle cards sharing one name all come back — the service treats that
-- name as ambiguous.
-- name: GetCardsByNormalizedNames :many
with matches as (
    select distinct on (oracle_id) *
    from cards
    where normalized_name = any(sqlc.arg(names)::text[])
    order by oracle_id, promo, released_at desc, (image_small is null)
)
select scryfall_id, oracle_id, name, normalized_name, mana_cost, type_line,
    set_code, set_name, collector_number, image_small, image_normal
from matches;

-- Fuzzy suggestions for one unresolved import line. Same <% + GUC
-- threshold setup as autocomplete (see cards.sql for why the operator
-- form matters); oracle-level with a representative printing.
-- name: SuggestCardsByName :many
with matches as (
    select distinct on (oracle_id) *
    from cards
    where sqlc.arg(query)::text <% normalized_name
    order by oracle_id, promo, released_at desc, (image_small is null)
)
select scryfall_id, oracle_id, name, mana_cost, type_line,
    set_code, set_name, collector_number, image_small, image_normal
from matches
order by
    word_similarity(sqlc.arg(query), normalized_name) desc,
    similarity(sqlc.arg(query), normalized_name) desc,
    name asc
limit 5;

-- Wantlist: per cube row (already oracle-level), missing = cube quantity
-- minus the user's copies of that oracle across all printings, rows with
-- missing > 0 only. Computed on demand, never stored.
-- name: GetCubeWantlist :many
select cc.oracle_id, cc.scryfall_id,
    cc.quantity as cube_quantity,
    coalesce(own.owned, 0)::int as owned_quantity,
    (cc.quantity - coalesce(own.owned, 0))::int as missing_quantity,
    ca.name, ca.mana_cost, ca.image_small, ca.image_normal
from cube_cards cc
join cards ca on ca.scryfall_id = cc.scryfall_id
left join (
    select oracle_id, sum(quantity)::int as owned
    from collection_items
    where user_id = sqlc.arg(user_id)
    group by oracle_id
) own on own.oracle_id = cc.oracle_id
where cc.cube_id = sqlc.arg(cube_id)
  and cc.quantity > coalesce(own.owned, 0)
order by ca.name;
