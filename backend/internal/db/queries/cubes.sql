-- name: CreateCube :one
insert into cubes (owner_id, name, description, visibility)
values (sqlc.arg(owner_id), sqlc.arg(name), sqlc.arg(description), sqlc.arg(visibility))
returning *;

-- name: GetCube :one
select c.*, u.display_name as owner_name,
    coalesce((select sum(cc.quantity) from cube_cards cc where cc.cube_id = c.id), 0)::bigint as card_count
from cubes c
join users u on u.id = c.owner_id
where c.id = sqlc.arg(id);

-- name: GetCubeForUpdate :one
select * from cubes where id = sqlc.arg(id) for update;

-- name: UpdateCubeMeta :exec
update cubes set
    name = coalesce(sqlc.narg(name), name),
    description = coalesce(sqlc.narg(description), description),
    visibility = coalesce(sqlc.narg(visibility), visibility),
    updated_at = now()
where id = sqlc.arg(id);

-- name: DeleteCube :exec
delete from cubes where id = sqlc.arg(id);

-- Browser: public cubes, newest-updated first, optional trgm name search.
-- pg_trgm lowercases trigrams, so matching is case-insensitive as-is.
-- name: ListPublicCubes :many
select c.*, u.display_name as owner_name,
    coalesce((select sum(cc.quantity) from cube_cards cc where cc.cube_id = c.id), 0)::bigint as card_count,
    count(*) over () as total
from cubes c
join users u on u.id = c.owner_id
where c.visibility = 'public'
  and (sqlc.narg(query)::text is null or sqlc.narg(query)::text <% c.name)
order by c.updated_at desc
limit sqlc.arg(page_limit)::int offset sqlc.arg(page_offset)::int;

-- name: ListMyCubes :many
select c.*, u.display_name as owner_name,
    coalesce((select sum(cc.quantity) from cube_cards cc where cc.cube_id = c.id), 0)::bigint as card_count
from cubes c
join users u on u.id = c.owner_id
where c.owner_id = sqlc.arg(owner_id)
order by c.updated_at desc;

-- Current list with display data for the grouped view.
-- name: GetCubeCards :many
select cc.oracle_id, cc.scryfall_id, cc.quantity,
    ca.name, ca.mana_cost, ca.type_line, ca.cmc, ca.colors,
    ca.color_identity, ca.rarity, ca.image_small, ca.image_normal
from cube_cards cc
join cards ca on ca.scryfall_id = cc.scryfall_id
where cc.cube_id = sqlc.arg(cube_id)
order by ca.name;

-- Bare quantities for diff validation and replay.
-- name: GetCubeCardQuantities :many
select oracle_id, scryfall_id, quantity
from cube_cards
where cube_id = sqlc.arg(cube_id);

-- Add: insert or increment. On increment the existing printing wins —
-- excluded.scryfall_id is deliberately ignored.
-- name: AddCubeCard :exec
insert into cube_cards (cube_id, oracle_id, scryfall_id, quantity)
values (sqlc.arg(cube_id), sqlc.arg(oracle_id), sqlc.arg(scryfall_id), sqlc.arg(quantity))
on conflict (cube_id, oracle_id)
do update set quantity = cube_cards.quantity + excluded.quantity;

-- name: RemoveCubeCardQuantity :exec
update cube_cards
set quantity = quantity - sqlc.arg(quantity)
where cube_id = sqlc.arg(cube_id) and oracle_id = sqlc.arg(oracle_id);

-- name: DeleteDepletedCubeCards :exec
delete from cube_cards where cube_id = sqlc.arg(cube_id) and quantity <= 0;

-- name: InsertCubeChange :one
insert into cube_changes (cube_id, version, author_id, note)
values (sqlc.arg(cube_id), sqlc.arg(version), sqlc.arg(author_id), sqlc.arg(note))
returning *;

-- name: InsertCubeChangeItem :exec
insert into cube_change_items (change_id, kind, oracle_id, scryfall_id, quantity)
values (sqlc.arg(change_id), sqlc.arg(kind), sqlc.arg(oracle_id), sqlc.arg(scryfall_id), sqlc.arg(quantity));

-- name: BumpCubeVersion :exec
update cubes set version = version + 1, updated_at = now() where id = sqlc.arg(id);

-- name: ListCubeChanges :many
select ch.*, u.display_name as author_name, count(*) over () as total
from cube_changes ch
join users u on u.id = ch.author_id
where ch.cube_id = sqlc.arg(cube_id)
order by ch.version desc
limit sqlc.arg(page_limit)::int offset sqlc.arg(page_offset)::int;

-- name: ListChangeItemsForChanges :many
select ci.change_id, ci.kind, ci.oracle_id, ci.quantity, ca.name
from cube_change_items ci
join cards ca on ca.scryfall_id = ci.scryfall_id
where ci.change_id = any(sqlc.arg(change_ids)::uuid[])
order by ca.name;

-- Replay input: items of every change newer than after_version, newest
-- change first.
-- name: ListChangeItemsAfterVersion :many
select ch.version, ci.kind, ci.oracle_id, ci.scryfall_id, ci.quantity
from cube_changes ch
join cube_change_items ci on ci.change_id = ch.id
where ch.cube_id = sqlc.arg(cube_id) and ch.version > sqlc.arg(after_version)
order by ch.version desc;

-- Resolve add-inputs (scryfall id) to oracle identity.
-- name: GetCardsByScryfallIDs :many
select scryfall_id, oracle_id, name
from cards
where scryfall_id = any(sqlc.arg(ids)::uuid[]);

-- Display data for reconstructed past states.
-- name: GetCardsDisplayByScryfallIDs :many
select scryfall_id, oracle_id, name, mana_cost, type_line, cmc, colors,
    color_identity, rarity, image_small, image_normal
from cards
where scryfall_id = any(sqlc.arg(ids)::uuid[]);
