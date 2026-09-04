-- +goose Up
-- Append-only log of every reported result. The authoritative result stays
-- on matches (last write wins); this exists so the organizer can see that
-- a result was overwritten and by whom, which is the score-dispute signal.
create table match_result_reports (
    id uuid primary key default gen_random_uuid(),
    match_id uuid not null references matches (id) on delete cascade,
    reported_by uuid not null references users (id),
    -- Denormalized at write time: whether the reporter acted as organizer.
    -- An organizer report locks the match against further player reports,
    -- and that must not change retroactively if roles ever change.
    is_organizer boolean not null,
    p1_games int not null check (p1_games between 0 and 2),
    p2_games int not null check (p2_games between 0 and 2),
    reported_at timestamptz not null default now()
);

create index match_result_reports_match_idx
    on match_result_reports (match_id, reported_at);

-- +goose Down
drop table match_result_reports;
