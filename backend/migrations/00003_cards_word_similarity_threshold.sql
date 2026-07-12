-- +goose Up
-- The <% operator reads pg_trgm.word_similarity_threshold (default 0.6).
-- 0.35 is calibrated so the spec's typo example matches:
-- word_similarity('lighntin bol', 'lightning bolt') = 0.368.
-- +goose StatementBegin
do $$
begin
    execute format('alter database %I set pg_trgm.word_similarity_threshold = 0.35', current_database());
end
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
do $$
begin
    execute format('alter database %I reset pg_trgm.word_similarity_threshold', current_database());
end
$$;
-- +goose StatementEnd
