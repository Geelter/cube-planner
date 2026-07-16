// Package tournaments owns the swiss tournament state machine on top of
// the pure swiss core: roster snapshot, round lifecycle (draft →
// published → completed, one in flight), results, drops. Standings are
// computed on read, never stored. It reads events only through
// db.Queries — no dependency on internal/events.
package tournaments

import (
	"context"
	"errors"
	"log/slog"
	"math/bits"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/tournaments/swiss"
)

var (
	// ErrEventNotFound mirrors events' convention: drafts read as absent.
	ErrEventNotFound       = errors.New("event not found")
	ErrEventNotStarted     = errors.New("event not started")
	ErrTournamentNotFound  = errors.New("tournament not found")
	ErrNoPlayers           = errors.New("no paid players to seed the tournament")
	ErrPlannedRoundsTooLow = errors.New("planned rounds below rounds already paired")
	ErrAllRoundsPaired     = errors.New("all planned rounds already paired")
	ErrTooFewPlayers       = errors.New("fewer than two active players")
	ErrRoundNotFound       = errors.New("round not found")
	ErrRoundExists         = errors.New("a round is already in progress")
	ErrRoundNotDraft       = errors.New("round is not a draft")
	ErrRoundIncomplete     = errors.New("round has unreported matches")
	ErrMatchNotFound       = errors.New("match not found")
	ErrNotInMatch          = errors.New("caller is not in this match")
	ErrResultLocked        = errors.New("result can no longer be changed")
	ErrResultInvalid       = errors.New("invalid result")
	ErrByeImmutable        = errors.New("bye results are fixed")
	ErrPlayerNotFound      = errors.New("player not found")
	ErrNotYourPlayer       = errors.New("cannot act for another player")
	ErrAlreadyDropped      = errors.New("player already dropped")
	ErrNotDropped          = errors.New("player is not dropped")
	ErrUndropTooLate       = errors.New("a round was paired after the drop")
	ErrSwapInvalid         = errors.New("invalid swap")
)

type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
	log     *slog.Logger
	now     func() time.Time
	newSeed func() int64
}

func NewService(queries *db.Queries, pool *pgxpool.Pool, log *slog.Logger) *Service {
	return &Service{
		queries: queries, pool: pool, log: log, now: time.Now,
		newSeed: rand.Int63, //nolint:gosec // pairing seed, not crypto
	}
}

func (s *Service) withTx(ctx context.Context, fn func(qtx *db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ---- pgtype.UUID <-> *uuid.UUID helpers ----
//
// sqlc generates nullable uuid columns as pgtype.UUID (no override in
// sqlc.yaml; matches repo convention, e.g. EventCube.CubeChangeID).
// The service's exported types use *uuid.UUID throughout, so these
// helpers convert at the db boundary only.

func uuidPtr(u pgtype.UUID) *uuid.UUID {
	if !u.Valid {
		return nil
	}
	id := uuid.UUID(u.Bytes)
	return &id
}

func pgUUID(u *uuid.UUID) pgtype.UUID {
	if u == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *u, Valid: true}
}

// ---- read side ----

type PlayerDetail struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	DisplayName string
	Dropped     bool
}

type MatchDetail struct {
	ID          uuid.UUID
	TableNumber int32
	Player1ID   uuid.UUID
	Player2ID   *uuid.UUID
	P1Games     *int32
	P2Games     *int32
	Draws       *int32
	ReportedAt  *time.Time
}

type RoundDetail struct {
	Number  int32
	Status  string
	Matches []MatchDetail
}

type Detail struct {
	EventID       uuid.UUID
	PlannedRounds int32
	Players       []PlayerDetail
	Rounds        []RoundDetail
	Standings     []swiss.Standing
}

// Get returns the whole tournament aggregate. Draft rounds are included
// only for admins; standings never include draft matches.
func (s *Service) Get(ctx context.Context, eventID uuid.UUID, admin bool) (*Detail, error) {
	tour, err := s.queries.GetTournamentByEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTournamentNotFound
	}
	if err != nil {
		return nil, err
	}
	players, err := s.queries.ListTournamentPlayers(ctx, tour.ID)
	if err != nil {
		return nil, err
	}
	rounds, err := s.queries.ListRounds(ctx, tour.ID)
	if err != nil {
		return nil, err
	}
	allMatches, err := s.queries.ListMatchesForTournament(ctx, tour.ID)
	if err != nil {
		return nil, err
	}

	d := &Detail{EventID: eventID, PlannedRounds: tour.PlannedRounds}
	swissPlayers := make([]swiss.Player, len(players))
	for i, p := range players {
		d.Players = append(d.Players, PlayerDetail{
			ID: p.ID, UserID: p.UserID, DisplayName: p.DisplayName,
			Dropped: p.DroppedAt != nil,
		})
		swissPlayers[i] = swiss.Player{
			ID: p.ID, DisplayName: p.DisplayName, Dropped: p.DroppedAt != nil,
		}
	}

	byRound := map[uuid.UUID][]MatchDetail{}
	var swissMatches []swiss.Match
	for _, m := range allMatches {
		byRound[m.RoundID] = append(byRound[m.RoundID], MatchDetail{
			ID: m.ID, TableNumber: m.TableNumber, Player1ID: m.Player1ID,
			Player2ID: uuidPtr(m.Player2ID), P1Games: m.P1Games, P2Games: m.P2Games,
			Draws: m.Draws, ReportedAt: m.ReportedAt,
		})
		if m.RoundStatus == "draft" {
			continue // draft matches never count toward standings
		}
		sm := swiss.Match{Player1: m.Player1ID, Player2: uuidPtr(m.Player2ID)}
		if m.P1Games != nil && m.P2Games != nil && m.Draws != nil {
			sm.Result = &swiss.Result{
				P1Games: int(*m.P1Games), P2Games: int(*m.P2Games), Draws: int(*m.Draws),
			}
		}
		swissMatches = append(swissMatches, sm)
	}
	for _, r := range rounds {
		if r.Status == "draft" && !admin {
			continue
		}
		d.Rounds = append(d.Rounds, RoundDetail{
			Number: r.Number, Status: r.Status, Matches: byRound[r.ID],
		})
	}
	d.Standings = swiss.ComputeStandings(swissPlayers, swissMatches)
	return d, nil
}

// ---- shared mutation guards ----

// startedEvent loads the event and requires status 'started'. Draft
// events read as not found (same convention as internal/events).
func startedEvent(ctx context.Context, qtx *db.Queries, eventID uuid.UUID) (db.Event, error) {
	ev, err := qtx.GetEventForUpdate(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Event{}, ErrEventNotFound
	}
	if err != nil {
		return db.Event{}, err
	}
	if ev.Status == "draft" {
		return db.Event{}, ErrEventNotFound
	}
	if ev.Status != "started" {
		return db.Event{}, ErrEventNotStarted
	}
	return ev, nil
}

// lockTournament fetches the tournament under FOR UPDATE, serializing
// all round/result/drop mutations per tournament.
func lockTournament(ctx context.Context, qtx *db.Queries, eventID uuid.UUID) (db.Tournament, error) {
	tour, err := qtx.GetTournamentByEventForUpdate(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Tournament{}, ErrTournamentNotFound
	}
	return tour, err
}

func defaultRounds(playerCount int) int32 {
	if playerCount <= 2 {
		return 1
	}
	return int32(bits.Len(uint(playerCount - 1))) // ceil(log2(n))
}

// createTournament snapshots the paid roster (spec §3.1).
func (s *Service) createTournament(ctx context.Context, qtx *db.Queries, eventID uuid.UUID, plannedRounds *int32) (db.Tournament, error) {
	roster, err := qtx.ListPaidRegistrationUsers(ctx, eventID)
	if err != nil {
		return db.Tournament{}, err
	}
	if len(roster) == 0 {
		return db.Tournament{}, ErrNoPlayers
	}
	rounds := defaultRounds(len(roster))
	if plannedRounds != nil {
		rounds = *plannedRounds
	}
	tour, err := qtx.CreateTournament(ctx, db.CreateTournamentParams{
		EventID: eventID, PlannedRounds: rounds,
	})
	if err != nil {
		return db.Tournament{}, err
	}
	for _, r := range roster {
		if _, err := qtx.InsertTournamentPlayer(ctx, db.InsertTournamentPlayerParams{
			TournamentID: tour.ID, UserID: r.UserID,
		}); err != nil {
			return db.Tournament{}, err
		}
	}
	return tour, nil
}

// Upsert creates the tournament (roster snapshot) or updates
// planned_rounds — never below the rounds already paired.
func (s *Service) Upsert(ctx context.Context, eventID uuid.UUID, plannedRounds *int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if errors.Is(err, ErrTournamentNotFound) {
			_, err = s.createTournament(ctx, qtx, eventID, plannedRounds)
			return err
		}
		if err != nil {
			return err
		}
		if plannedRounds == nil {
			return nil
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		if int(*plannedRounds) < len(rounds) {
			return ErrPlannedRoundsTooLow
		}
		_, err = qtx.UpdatePlannedRounds(ctx, db.UpdatePlannedRoundsParams{
			ID: tour.ID, PlannedRounds: *plannedRounds,
		})
		return err
	})
}

// pairingInputs loads swiss inputs from the DB rows.
func pairingInputs(ctx context.Context, qtx *db.Queries, tournamentID uuid.UUID) ([]swiss.Player, []swiss.Match, error) {
	players, err := qtx.ListTournamentPlayers(ctx, tournamentID)
	if err != nil {
		return nil, nil, err
	}
	sp := make([]swiss.Player, len(players))
	for i, p := range players {
		sp[i] = swiss.Player{ID: p.ID, DisplayName: p.DisplayName, Dropped: p.DroppedAt != nil}
	}
	rows, err := qtx.ListMatchesForTournament(ctx, tournamentID)
	if err != nil {
		return nil, nil, err
	}
	var sm []swiss.Match
	for _, m := range rows {
		if m.RoundStatus == "draft" {
			continue
		}
		mm := swiss.Match{Player1: m.Player1ID, Player2: uuidPtr(m.Player2ID)}
		if m.P1Games != nil && m.P2Games != nil && m.Draws != nil {
			mm.Result = &swiss.Result{
				P1Games: int(*m.P1Games), P2Games: int(*m.P2Games), Draws: int(*m.Draws),
			}
		}
		sm = append(sm, mm)
	}
	return sp, sm, nil
}

func insertPairings(ctx context.Context, qtx *db.Queries, roundID uuid.UUID, pairings []swiss.Pairing) error {
	for _, p := range pairings {
		if _, err := qtx.InsertMatch(ctx, db.InsertMatchParams{
			RoundID: roundID, TableNumber: int32(p.TableNumber),
			Player1ID: p.Player1, Player2ID: pgUUID(p.Player2),
		}); err != nil {
			return err
		}
	}
	return nil
}

// PairNextRound creates the tournament with defaults when absent (spec:
// "just pair round 1" works in one click), then pairs the next round as
// a draft. Requires every earlier round completed and planned capacity.
func (s *Service) PairNextRound(ctx context.Context, eventID uuid.UUID) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if errors.Is(err, ErrTournamentNotFound) {
			if tour, err = s.createTournament(ctx, qtx, eventID, nil); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		for _, r := range rounds {
			if r.Status != "completed" {
				return ErrRoundExists
			}
		}
		if len(rounds) >= int(tour.PlannedRounds) {
			return ErrAllRoundsPaired
		}
		players, history, err := pairingInputs(ctx, qtx, tour.ID)
		if err != nil {
			return err
		}
		activeCount := 0
		for _, p := range players {
			if !p.Dropped {
				activeCount++
			}
		}
		if activeCount < 2 {
			return ErrTooFewPlayers
		}
		seed := s.newSeed()
		round, err := qtx.CreateRound(ctx, db.CreateRoundParams{
			TournamentID: tour.ID, Number: int32(len(rounds) + 1), Seed: seed,
		})
		if err != nil {
			return err
		}
		return insertPairings(ctx, qtx, round.ID, swiss.Pair(players, history, seed))
	})
}

// draftRound loads round `number` and requires draft status.
func draftRound(ctx context.Context, qtx *db.Queries, tournamentID uuid.UUID, number int32) (db.Round, error) {
	round, err := qtx.GetRoundByNumber(ctx, db.GetRoundByNumberParams{
		TournamentID: tournamentID, Number: number,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Round{}, ErrRoundNotFound
	}
	if err != nil {
		return db.Round{}, err
	}
	if round.Status != "draft" {
		return db.Round{}, ErrRoundNotDraft
	}
	return round, nil
}

// Reroll regenerates the draft round's pairings with a fresh seed.
func (s *Service) Reroll(ctx context.Context, eventID uuid.UUID, number int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := draftRound(ctx, qtx, tour.ID, number)
		if err != nil {
			return err
		}
		if err := qtx.DeleteMatchesForRound(ctx, round.ID); err != nil {
			return err
		}
		players, history, err := pairingInputs(ctx, qtx, tour.ID)
		if err != nil {
			return err
		}
		seed := s.newSeed()
		if _, err := qtx.SetRoundSeed(ctx, db.SetRoundSeedParams{ID: round.ID, Seed: seed}); err != nil {
			return err
		}
		return insertPairings(ctx, qtx, round.ID, swiss.Pair(players, history, seed))
	})
}

type SlotRef struct {
	MatchID uuid.UUID
	Slot    int32 // 1 | 2
}

// slotPlayer reads the player occupying a slot; nil slot-2 (a bye's
// empty side) is not swappable.
func slotPlayer(m db.GetMatchRow, slot int32) (uuid.UUID, error) {
	switch slot {
	case 1:
		return m.Player1ID, nil
	case 2:
		p2 := uuidPtr(m.Player2ID)
		if p2 == nil {
			return uuid.Nil, ErrSwapInvalid
		}
		return *p2, nil
	default:
		return uuid.Nil, ErrSwapInvalid
	}
}

func setSlot(ctx context.Context, qtx *db.Queries, m db.GetMatchRow, slot int32, player uuid.UUID) error {
	p1, p2 := m.Player1ID, uuidPtr(m.Player2ID)
	if slot == 1 {
		p1 = player
	} else {
		p2 = &player
	}
	if p2 != nil && p1 == *p2 {
		return ErrSwapInvalid
	}
	_, err := qtx.SetMatchPlayers(ctx, db.SetMatchPlayersParams{
		ID: m.ID, Player1ID: p1, Player2ID: pgUUID(p2),
	})
	return err
}

// Swap exchanges the players in two slots of the draft round. The only
// hard rule is no self-pairing; the organizer may knowingly create a
// rematch (spec §3.2).
func (s *Service) Swap(ctx context.Context, eventID uuid.UUID, number int32, a, b SlotRef) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := draftRound(ctx, qtx, tour.ID, number)
		if err != nil {
			return err
		}
		if a.MatchID == b.MatchID && a.Slot == b.Slot {
			return ErrSwapInvalid
		}
		ma, err := qtx.GetMatch(ctx, a.MatchID)
		if err != nil || ma.RoundID != round.ID {
			return ErrSwapInvalid
		}
		mb, err := qtx.GetMatch(ctx, b.MatchID)
		if err != nil || mb.RoundID != round.ID {
			return ErrSwapInvalid
		}
		pa, err := slotPlayer(ma, a.Slot)
		if err != nil {
			return err
		}
		pb, err := slotPlayer(mb, b.Slot)
		if err != nil {
			return err
		}
		if err := setSlot(ctx, qtx, ma, a.Slot, pb); err != nil {
			return err
		}
		if a.MatchID == b.MatchID {
			// Same-match swap: re-read so the second write sees the first.
			ma2, err := qtx.GetMatch(ctx, b.MatchID)
			if err != nil {
				return err
			}
			return setSlot(ctx, qtx, ma2, b.Slot, pa)
		}
		return setSlot(ctx, qtx, mb, b.Slot, pa)
	})
}

// Publish flips draft → published and auto-fills bye results 2-0
// (spec §3.2: "(re)applied on publish").
func (s *Service) Publish(ctx context.Context, eventID uuid.UUID, number int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := draftRound(ctx, qtx, tour.ID, number)
		if err != nil {
			return err
		}
		matches, err := qtx.ListMatchesForRound(ctx, round.ID)
		if err != nil {
			return err
		}
		for _, m := range matches {
			if !m.Player2ID.Valid {
				two, zero := int32(2), int32(0)
				if _, err := qtx.UpdateMatchResult(ctx, db.UpdateMatchResultParams{
					ID: m.ID, P1Games: &two, P2Games: &zero, Draws: &zero, ReportedBy: pgtype.UUID{},
				}); err != nil {
					return err
				}
			}
		}
		now := s.now()
		_, err = qtx.SetRoundStatus(ctx, db.SetRoundStatusParams{
			ID: round.ID, Status: "published", PublishedAt: &now,
		})
		return err
	})
}

// Complete requires every match reported.
func (s *Service) Complete(ctx context.Context, eventID uuid.UUID, number int32) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		round, err := qtx.GetRoundByNumber(ctx, db.GetRoundByNumberParams{
			TournamentID: tour.ID, Number: number,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRoundNotFound
		}
		if err != nil {
			return err
		}
		if round.Status != "published" {
			return ErrRoundNotDraft
		}
		matches, err := qtx.ListMatchesForRound(ctx, round.ID)
		if err != nil {
			return err
		}
		for _, m := range matches {
			if m.ReportedAt == nil {
				return ErrRoundIncomplete
			}
		}
		now := s.now()
		_, err = qtx.SetRoundStatus(ctx, db.SetRoundStatusParams{
			ID: round.ID, Status: "completed", CompletedAt: &now,
		})
		return err
	})
}

type Result struct {
	P1Games int32
	P2Games int32
	Draws   int32
}

func (r Result) valid() bool {
	return r.P1Games >= 0 && r.P1Games <= 2 &&
		r.P2Games >= 0 && r.P2Games <= 2 &&
		r.Draws >= 0 && r.Draws <= 3 &&
		r.P1Games+r.P2Games+r.Draws <= 3 &&
		!(r.P1Games == 2 && r.P2Games == 2)
}

// ReportResult writes a Bo3 score. Players in the match may report
// while the round is published and latest; the organizer may write
// until the next round is paired (draft rounds never accept results).
func (s *Service) ReportResult(ctx context.Context, eventID, matchID, callerID uuid.UUID, admin bool, r Result) error {
	if !r.valid() {
		return ErrResultInvalid
	}
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		m, err := qtx.GetMatch(ctx, matchID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMatchNotFound
		}
		if err != nil {
			return err
		}
		if m.TournamentID != tour.ID {
			return ErrMatchNotFound
		}
		player2ID := uuidPtr(m.Player2ID)
		if player2ID == nil {
			return ErrByeImmutable
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		latest := rounds[len(rounds)-1]
		if m.RoundNumber != latest.Number || m.RoundStatus == "draft" {
			return ErrResultLocked
		}
		if !admin {
			p1, err1 := qtx.GetTournamentPlayer(ctx, m.Player1ID)
			p2, err2 := qtx.GetTournamentPlayer(ctx, *player2ID)
			if err1 != nil || err2 != nil {
				return errors.Join(err1, err2)
			}
			if p1.UserID != callerID && p2.UserID != callerID {
				return ErrNotInMatch
			}
			if m.RoundStatus != "published" {
				return ErrResultLocked
			}
		}
		_, err = qtx.UpdateMatchResult(ctx, db.UpdateMatchResultParams{
			ID: m.ID, P1Games: &r.P1Games, P2Games: &r.P2Games, Draws: &r.Draws,
			ReportedBy: pgUUID(&callerID),
		})
		return err
	})
}

// playerForAction loads the player and enforces self-or-admin.
func playerForAction(ctx context.Context, qtx *db.Queries, tournamentID, playerID, callerID uuid.UUID, admin bool) (db.GetTournamentPlayerRow, error) {
	p, err := qtx.GetTournamentPlayer(ctx, playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetTournamentPlayerRow{}, ErrPlayerNotFound
	}
	if err != nil {
		return db.GetTournamentPlayerRow{}, err
	}
	if p.TournamentID != tournamentID {
		return db.GetTournamentPlayerRow{}, ErrPlayerNotFound
	}
	if !admin && p.UserID != callerID {
		return db.GetTournamentPlayerRow{}, ErrNotYourPlayer
	}
	return p, nil
}

func (s *Service) Drop(ctx context.Context, eventID, playerID, callerID uuid.UUID, admin bool) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		p, err := playerForAction(ctx, qtx, tour.ID, playerID, callerID, admin)
		if err != nil {
			return err
		}
		if p.DroppedAt != nil {
			return ErrAlreadyDropped
		}
		now := s.now()
		_, err = qtx.SetPlayerDropped(ctx, db.SetPlayerDroppedParams{ID: playerID, DroppedAt: &now})
		return err
	})
}

// Undrop is rejected once any round was created after the drop
// (spec §3.5): the player already missed a pairing.
func (s *Service) Undrop(ctx context.Context, eventID, playerID, callerID uuid.UUID, admin bool) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		if _, err := startedEvent(ctx, qtx, eventID); err != nil {
			return err
		}
		tour, err := lockTournament(ctx, qtx, eventID)
		if err != nil {
			return err
		}
		p, err := playerForAction(ctx, qtx, tour.ID, playerID, callerID, admin)
		if err != nil {
			return err
		}
		if p.DroppedAt == nil {
			return ErrNotDropped
		}
		rounds, err := qtx.ListRounds(ctx, tour.ID)
		if err != nil {
			return err
		}
		for _, r := range rounds {
			if r.CreatedAt.After(*p.DroppedAt) {
				return ErrUndropTooLate
			}
		}
		_, err = qtx.SetPlayerDropped(ctx, db.SetPlayerDroppedParams{ID: playerID, DroppedAt: nil})
		return err
	})
}
