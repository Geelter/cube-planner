-- +goose Up
-- Scryfall's EDHREC popularity rank (1 = most popular); NULL for cards
-- Scryfall doesn't rank (tokens, brand-new sets). Used as a search-ranking
-- tiebreak (issue #9). Backfilled by the next bulk sync.
alter table cards add column edhrec_rank integer;
alter table cards_staging add column edhrec_rank integer;

-- +goose Down
alter table cards drop column edhrec_rank;
alter table cards_staging drop column edhrec_rank;
