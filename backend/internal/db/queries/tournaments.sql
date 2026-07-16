-- name: CreateTournament :one
insert into tournaments (event_id, planned_rounds)
values (sqlc.arg(event_id), sqlc.arg(planned_rounds))
returning *;

-- name: GetTournamentByEvent :one
select * from tournaments where event_id = sqlc.arg(event_id);

-- Mutations lock the tournament row: one round in flight, no racing
-- pair/publish/complete/report.
-- name: GetTournamentByEventForUpdate :one
select * from tournaments where event_id = sqlc.arg(event_id) for update;

-- name: UpdatePlannedRounds :one
update tournaments set planned_rounds = sqlc.arg(planned_rounds),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- Roster snapshot source (spec §3.1): paid registrations only.
-- name: ListPaidRegistrationUsers :many
select r.user_id, u.display_name
from registrations r
join users u on u.id = r.user_id
where r.event_id = sqlc.arg(event_id) and r.status = 'paid'
order by u.display_name;

-- name: InsertTournamentPlayer :one
insert into tournament_players (tournament_id, user_id)
values (sqlc.arg(tournament_id), sqlc.arg(user_id))
returning *;

-- name: ListTournamentPlayers :many
select tp.id, tp.user_id, tp.dropped_at, u.display_name
from tournament_players tp
join users u on u.id = tp.user_id
where tp.tournament_id = sqlc.arg(tournament_id)
order by u.display_name;

-- name: GetTournamentPlayer :one
select tp.id, tp.tournament_id, tp.user_id, tp.dropped_at, u.display_name
from tournament_players tp
join users u on u.id = tp.user_id
where tp.id = sqlc.arg(id);

-- name: SetPlayerDropped :one
update tournament_players set dropped_at = sqlc.narg(dropped_at)
where id = sqlc.arg(id)
returning *;

-- name: CreateRound :one
insert into rounds (tournament_id, number, seed)
values (sqlc.arg(tournament_id), sqlc.arg(number), sqlc.arg(seed))
returning *;

-- name: ListRounds :many
select * from rounds
where tournament_id = sqlc.arg(tournament_id)
order by number;

-- name: GetRoundByNumber :one
select * from rounds
where tournament_id = sqlc.arg(tournament_id)
    and number = sqlc.arg(number);

-- name: SetRoundSeed :one
update rounds set seed = sqlc.arg(seed), updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: SetRoundStatus :one
update rounds set status = sqlc.arg(status),
    published_at = coalesce(rounds.published_at, sqlc.narg(published_at)),
    completed_at = coalesce(rounds.completed_at, sqlc.narg(completed_at)),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: DeleteMatchesForRound :exec
delete from matches where round_id = sqlc.arg(round_id);

-- name: InsertMatch :one
insert into matches (round_id, table_number, player1_id, player2_id,
    p1_games, p2_games, draws, reported_at)
values (sqlc.arg(round_id), sqlc.arg(table_number), sqlc.arg(player1_id),
    sqlc.narg(player2_id), sqlc.narg(p1_games), sqlc.narg(p2_games),
    sqlc.narg(draws), sqlc.narg(reported_at))
returning *;

-- name: ListMatchesForRound :many
select * from matches
where round_id = sqlc.arg(round_id)
order by table_number;

-- name: ListMatchesForTournament :many
select m.*, r.number as round_number, r.status as round_status
from matches m
join rounds r on r.id = m.round_id
where r.tournament_id = sqlc.arg(tournament_id)
order by r.number, m.table_number;

-- name: GetMatch :one
select m.*, r.number as round_number, r.status as round_status,
    r.tournament_id
from matches m
join rounds r on r.id = m.round_id
where m.id = sqlc.arg(id);

-- name: UpdateMatchResult :one
update matches set p1_games = sqlc.arg(p1_games),
    p2_games = sqlc.arg(p2_games), draws = sqlc.arg(draws),
    reported_by = sqlc.narg(reported_by), reported_at = now(),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- name: SetMatchPlayers :one
update matches set player1_id = sqlc.arg(player1_id),
    player2_id = sqlc.narg(player2_id), updated_at = now()
where id = sqlc.arg(id)
returning *;

-- Finish guard (spec §3.2): events.Transition("finish") refuses while
-- any round is not completed.
-- name: CountOpenRoundsForEvent :one
select count(*) from rounds r
join tournaments t on t.id = r.tournament_id
where t.event_id = sqlc.arg(event_id) and r.status <> 'completed';
