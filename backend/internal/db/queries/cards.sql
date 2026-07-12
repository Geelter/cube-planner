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
