-- name: GetLastSucceededSyncRun :one
select * from card_sync_runs where status = 'succeeded' order by id desc limit 1;

-- name: CreateSyncRun :one
insert into card_sync_runs (status) values ('running') returning id;

-- name: FinishSyncRunSuccess :exec
update card_sync_runs
set status = 'succeeded', finished_at = now(),
    bulk_updated_at = sqlc.arg(bulk_updated_at), cards_count = sqlc.arg(cards_count)
where id = sqlc.arg(id);

-- name: FinishSyncRunFailure :exec
update card_sync_runs
set status = 'failed', finished_at = now(), error = sqlc.arg(error)
where id = sqlc.arg(id);

-- name: FailStaleRunningSyncRuns :exec
update card_sync_runs
set status = 'failed', finished_at = now(), error = 'interrupted (process restart)'
where status = 'running';

-- name: CountCards :one
select count(*) from cards;

-- name: TruncateCardsStaging :exec
truncate cards_staging;

-- name: UpsertCardsFromStaging :execrows
insert into cards (
    scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
    set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
    oracle_text, colors, color_identity, promo, image_small, image_normal,
    back_image_small, back_image_normal, updated_at
)
select scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
    set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
    oracle_text, colors, color_identity, promo, image_small, image_normal,
    back_image_small, back_image_normal, now()
from cards_staging
on conflict (scryfall_id) do update set
    oracle_id = excluded.oracle_id,
    name = excluded.name,
    normalized_name = excluded.normalized_name,
    released_at = excluded.released_at,
    set_code = excluded.set_code,
    set_name = excluded.set_name,
    collector_number = excluded.collector_number,
    rarity = excluded.rarity,
    layout = excluded.layout,
    mana_cost = excluded.mana_cost,
    cmc = excluded.cmc,
    type_line = excluded.type_line,
    oracle_text = excluded.oracle_text,
    colors = excluded.colors,
    color_identity = excluded.color_identity,
    promo = excluded.promo,
    image_small = excluded.image_small,
    image_normal = excluded.image_normal,
    back_image_small = excluded.back_image_small,
    back_image_normal = excluded.back_image_normal,
    updated_at = now();

-- name: DeleteCardsMissingFromStaging :execrows
delete from cards
where scryfall_id not in (select scryfall_id from cards_staging);

-- Autocomplete: oracle-level with a representative printing (non-promo
-- first, then newest, then has-image). Prefix matches rank above fuzzy
-- word-similarity matches. The CTE selects * because DISTINCT ON needs to
-- order by promo/released_at, which are not in the output list.
-- The <% operator (word-similarity commutator) is index-backed by the trgm
-- GIN index on normalized_name; the threshold is set at the database level
-- via pg_trgm.word_similarity_threshold (see migration 00003), not inlined
-- here, because the function-call form defeats the index. ORDER BY still
-- uses word_similarity()/similarity() for ranking — that's fine post-filter.
-- name: AutocompleteCards :many
with matches as (
    select distinct on (oracle_id) *
    from cards
    where normalized_name like sqlc.arg(prefix) || '%'
       or sqlc.arg(query)::text <% normalized_name
    order by oracle_id, promo, released_at desc, (image_small is null)
)
select scryfall_id, oracle_id, name, mana_cost, type_line, image_small
from matches
order by
    (normalized_name like sqlc.arg(prefix) || '%') desc,
    word_similarity(sqlc.arg(query), normalized_name) desc,
    similarity(sqlc.arg(query), normalized_name) desc,
    name asc
limit 15;

-- Filtered search, AND-combined, all filters optional. colors implements
-- cube semantics: color_identity ⊆ selected (empty array = colorless only).
-- total is a window count over the filtered oracle-level set.
-- name: SearchCards :many
with matches as (
    select distinct on (oracle_id) *
    from cards
    where (sqlc.narg(name)::text is null
           or normalized_name like sqlc.narg(name_prefix) || '%'
           or sqlc.narg(name)::text <% normalized_name)
      and (sqlc.narg(colors)::text[] is null or color_identity <@ sqlc.narg(colors)::text[])
      and (sqlc.narg(card_type)::text is null or type_line ilike '%' || sqlc.narg(card_type) || '%')
      and (sqlc.narg(cmc_min)::float8 is null or cmc >= sqlc.narg(cmc_min))
      and (sqlc.narg(cmc_max)::float8 is null or cmc <= sqlc.narg(cmc_max))
      and (sqlc.narg(rarity)::text is null or rarity = sqlc.narg(rarity))
      and (sqlc.narg(set_code)::text is null or set_code = sqlc.narg(set_code))
    order by oracle_id, promo, released_at desc, (image_small is null)
)
select *, count(*) over () as total
from matches
order by
    case when sqlc.narg(name)::text is not null
         then word_similarity(sqlc.narg(name), normalized_name) end desc nulls last,
    case when sqlc.narg(name)::text is not null
         then similarity(sqlc.narg(name), normalized_name) end desc nulls last,
    name asc
limit sqlc.arg(page_limit)::int offset sqlc.arg(page_offset)::int;

-- name: GetPrintingsByOracleID :many
select * from cards
where oracle_id = sqlc.arg(oracle_id)
order by released_at desc, set_code asc, collector_number asc;
