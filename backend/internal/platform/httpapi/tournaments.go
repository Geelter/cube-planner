package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/tournaments"
)

type TournamentPlayerInfo struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"userId"`
	DisplayName string    `json:"displayName"`
	Dropped     bool      `json:"dropped"`
}

type TournamentMatchInfo struct {
	ID          uuid.UUID  `json:"id"`
	TableNumber int32      `json:"tableNumber"`
	Player1ID   uuid.UUID  `json:"player1Id"`
	Player2ID   *uuid.UUID `json:"player2Id,omitempty"`
	P1Games     *int32     `json:"p1Games,omitempty"`
	P2Games     *int32     `json:"p2Games,omitempty"`
	Draws       *int32     `json:"draws,omitempty"`
	ReportedAt  *time.Time `json:"reportedAt,omitempty"`
}

type TournamentRoundInfo struct {
	Number  int32                 `json:"number"`
	Status  string                `json:"status" enum:"draft,published,completed"`
	Matches []TournamentMatchInfo `json:"matches"`
}

type TournamentStandingInfo struct {
	Rank        int       `json:"rank"`
	PlayerID    uuid.UUID `json:"playerId"`
	DisplayName string    `json:"displayName"`
	Dropped     bool      `json:"dropped"`
	MatchPoints int       `json:"matchPoints"`
	OmwPercent  float64   `json:"omwPercent"`
	GwPercent   float64   `json:"gwPercent"`
	OgwPercent  float64   `json:"ogwPercent"`
}

type TournamentInfo struct {
	EventID       uuid.UUID                `json:"eventId"`
	PlannedRounds int32                    `json:"plannedRounds"`
	CurrentRound  *int32                   `json:"currentRound,omitempty"`
	Players       []TournamentPlayerInfo   `json:"players"`
	Rounds        []TournamentRoundInfo    `json:"rounds"`
	Standings     []TournamentStandingInfo `json:"standings"`
}

func tournamentInfoFrom(d *tournaments.Detail) TournamentInfo {
	out := TournamentInfo{
		EventID: d.EventID, PlannedRounds: d.PlannedRounds,
		Players:   make([]TournamentPlayerInfo, len(d.Players)),
		Rounds:    make([]TournamentRoundInfo, len(d.Rounds)),
		Standings: make([]TournamentStandingInfo, len(d.Standings)),
	}
	for i, p := range d.Players {
		out.Players[i] = TournamentPlayerInfo{
			ID: p.ID, UserID: p.UserID, DisplayName: p.DisplayName, Dropped: p.Dropped,
		}
	}
	for i, r := range d.Rounds {
		matches := make([]TournamentMatchInfo, len(r.Matches))
		for j, m := range r.Matches {
			matches[j] = TournamentMatchInfo{
				ID: m.ID, TableNumber: m.TableNumber, Player1ID: m.Player1ID,
				Player2ID: m.Player2ID, P1Games: m.P1Games, P2Games: m.P2Games,
				Draws: m.Draws, ReportedAt: m.ReportedAt,
			}
		}
		out.Rounds[i] = TournamentRoundInfo{Number: r.Number, Status: r.Status, Matches: matches}
	}
	if n := len(d.Rounds); n > 0 {
		num := d.Rounds[n-1].Number
		out.CurrentRound = &num
	}
	for i, s := range d.Standings {
		out.Standings[i] = TournamentStandingInfo{
			Rank: s.Rank, PlayerID: s.PlayerID, DisplayName: s.DisplayName,
			Dropped: s.Dropped, MatchPoints: s.MatchPoints,
			OmwPercent: s.OMWPercent, GwPercent: s.GWPercent, OgwPercent: s.OGWPercent,
		}
	}
	return out
}

func mapTournamentErr(err error) error {
	switch {
	case errors.Is(err, tournaments.ErrEventNotFound):
		return huma.Error404NotFound("event not found")
	case errors.Is(err, tournaments.ErrTournamentNotFound):
		return eventProblem(http.StatusNotFound, "tournament-not-found", "no tournament yet")
	case errors.Is(err, tournaments.ErrEventNotStarted):
		return eventProblem(http.StatusConflict, "event-not-started", err.Error())
	case errors.Is(err, tournaments.ErrNoPlayers):
		return eventProblem(http.StatusConflict, "tournament-no-players", err.Error())
	case errors.Is(err, tournaments.ErrPlannedRoundsTooLow):
		return eventProblem(http.StatusConflict, "planned-rounds-too-low", err.Error())
	case errors.Is(err, tournaments.ErrAllRoundsPaired):
		return eventProblem(http.StatusConflict, "planned-rounds-reached", err.Error())
	case errors.Is(err, tournaments.ErrTooFewPlayers):
		return eventProblem(http.StatusConflict, "too-few-players", err.Error())
	case errors.Is(err, tournaments.ErrRoundNotFound):
		return eventProblem(http.StatusNotFound, "round-not-found", err.Error())
	case errors.Is(err, tournaments.ErrRoundExists):
		return eventProblem(http.StatusConflict, "round-exists", err.Error())
	case errors.Is(err, tournaments.ErrRoundNotDraft):
		return eventProblem(http.StatusConflict, "round-not-draft", err.Error())
	case errors.Is(err, tournaments.ErrRoundNotPublished):
		return eventProblem(http.StatusConflict, "round-not-published", err.Error())
	case errors.Is(err, tournaments.ErrRoundIncomplete):
		return eventProblem(http.StatusConflict, "round-incomplete", err.Error())
	case errors.Is(err, tournaments.ErrMatchNotFound):
		return eventProblem(http.StatusNotFound, "match-not-found", "no such match")
	case errors.Is(err, tournaments.ErrNotInMatch):
		return eventProblem(http.StatusForbidden, "not-in-match", err.Error())
	case errors.Is(err, tournaments.ErrResultLocked):
		return eventProblem(http.StatusConflict, "result-locked", err.Error())
	case errors.Is(err, tournaments.ErrResultInvalid):
		return eventProblem(http.StatusUnprocessableEntity, "result-invalid", err.Error())
	case errors.Is(err, tournaments.ErrByeImmutable):
		return eventProblem(http.StatusConflict, "bye-immutable", err.Error())
	case errors.Is(err, tournaments.ErrPlayerNotFound):
		return eventProblem(http.StatusNotFound, "player-not-found", "no such player")
	case errors.Is(err, tournaments.ErrNotYourPlayer):
		return eventProblem(http.StatusForbidden, "not-your-player", err.Error())
	case errors.Is(err, tournaments.ErrAlreadyDropped):
		return eventProblem(http.StatusConflict, "already-dropped", err.Error())
	case errors.Is(err, tournaments.ErrNotDropped):
		return eventProblem(http.StatusConflict, "not-dropped", err.Error())
	case errors.Is(err, tournaments.ErrUndropTooLate):
		return eventProblem(http.StatusConflict, "undrop-too-late", err.Error())
	case errors.Is(err, tournaments.ErrSwapInvalid):
		return eventProblem(http.StatusUnprocessableEntity, "swap-invalid", err.Error())
	default:
		return err
	}
}

type tournamentOutput struct {
	Body TournamentInfo
}

// tournamentBody re-reads the aggregate after any mutation so every
// endpoint returns the same fresh TournamentInfo.
func (deps Deps) tournamentBody(ctx context.Context, eventID uuid.UUID, admin bool) (*tournamentOutput, error) {
	d, err := deps.Tournaments.Get(ctx, eventID, admin)
	if err != nil {
		return nil, mapTournamentErr(err)
	}
	return &tournamentOutput{Body: tournamentInfoFrom(d)}, nil
}

type upsertTournamentInput struct {
	EventID string `path:"eventId"`
	Body    struct {
		PlannedRounds *int32 `json:"plannedRounds,omitempty" minimum:"1" maximum:"30"`
	}
}

type roundNumberInput struct {
	EventID string `path:"eventId"`
	Number  int32  `path:"number" minimum:"1"`
}

// SwapSlot is exported for a distinct huma schema name (see EventCubeLink).
type SwapSlot struct {
	MatchID uuid.UUID `json:"matchId"`
	Slot    int32     `json:"slot" minimum:"1" maximum:"2"`
}

type swapInput struct {
	EventID string `path:"eventId"`
	Number  int32  `path:"number" minimum:"1"`
	Body    struct {
		A SwapSlot `json:"a"`
		B SwapSlot `json:"b"`
	}
}

type reportResultInput struct {
	EventID string `path:"eventId"`
	MatchID string `path:"matchId"`
	Body    struct {
		P1Games int32 `json:"p1Games" minimum:"0" maximum:"2"`
		P2Games int32 `json:"p2Games" minimum:"0" maximum:"2"`
		Draws   int32 `json:"draws" minimum:"0" maximum:"3"`
	}
}

type playerActionInput struct {
	EventID  string `path:"eventId"`
	PlayerID string `path:"playerId"`
}

func registerTournaments(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "getTournament",
		Method:      http.MethodGet,
		Path:        "/api/events/{eventId}/tournament",
		Summary:     "Tournament aggregate: players, rounds, matches, standings",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *eventIDInput) (*tournamentOutput, error) {
		if _, ok := CurrentUserID(ctx); !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		return deps.tournamentBody(ctx, id, isAdmin(ctx, deps))
	})

	huma.Register(api, huma.Operation{
		OperationID: "upsertTournament",
		Method:      http.MethodPut,
		Path:        "/api/events/{eventId}/tournament",
		Summary:     "Create the tournament (snapshot paid roster) or set planned rounds (organizer)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *upsertTournamentInput) (*tournamentOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		if err := deps.Tournaments.Upsert(ctx, id, in.Body.PlannedRounds); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, true)
	})

	huma.Register(api, huma.Operation{
		OperationID: "pairNextRound",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/tournament/rounds",
		Summary:     "Pair the next round as a draft (organizer; creates the tournament if absent)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *eventIDInput) (*tournamentOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		if err := deps.Tournaments.PairNextRound(ctx, id); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, true)
	})

	type roundAction struct {
		id  string
		sum string
		fn  func(ctx context.Context, eventID uuid.UUID, number int32) error
	}
	for _, a := range []roundAction{
		{"rerollRound", "Regenerate the draft round's pairings", deps.Tournaments.Reroll},
		{"publishRound", "Publish the draft round", deps.Tournaments.Publish},
		{"completeRound", "Complete the round (all results in)", deps.Tournaments.Complete},
	} {
		path := "/api/events/{eventId}/tournament/rounds/{number}/" +
			map[string]string{"rerollRound": "reroll", "publishRound": "publish", "completeRound": "complete"}[a.id]
		huma.Register(api, huma.Operation{
			OperationID: a.id,
			Method:      http.MethodPost,
			Path:        path,
			Summary:     a.sum + " (organizer)",
			Tags:        []string{"tournaments"},
		}, func(ctx context.Context, in *roundNumberInput) (*tournamentOutput, error) {
			if _, err := requireAdmin(ctx, deps); err != nil {
				return nil, err
			}
			id, err := parseEventID(in.EventID)
			if err != nil {
				return nil, err
			}
			if err := a.fn(ctx, id, in.Number); err != nil {
				return nil, mapTournamentErr(err)
			}
			return deps.tournamentBody(ctx, id, true)
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "swapRoundSlots",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/tournament/rounds/{number}/swap",
		Summary:     "Swap two player slots in the draft round (organizer)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *swapInput) (*tournamentOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		a := tournaments.SlotRef{MatchID: in.Body.A.MatchID, Slot: in.Body.A.Slot}
		b := tournaments.SlotRef{MatchID: in.Body.B.MatchID, Slot: in.Body.B.Slot}
		if err := deps.Tournaments.Swap(ctx, id, in.Number, a, b); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, true)
	})

	huma.Register(api, huma.Operation{
		OperationID: "reportMatchResult",
		Method:      http.MethodPut,
		Path:        "/api/events/{eventId}/tournament/matches/{matchId}/result",
		Summary:     "Report or override a Bo3 result (player in the match, or organizer)",
		Tags:        []string{"tournaments"},
	}, func(ctx context.Context, in *reportResultInput) (*tournamentOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		matchID, err := uuid.Parse(in.MatchID)
		if err != nil {
			return nil, eventProblem(http.StatusNotFound, "match-not-found", "no such match")
		}
		admin := isAdmin(ctx, deps)
		if err := deps.Tournaments.ReportResult(ctx, id, matchID, uid, admin, tournaments.Result{
			P1Games: in.Body.P1Games, P2Games: in.Body.P2Games, Draws: in.Body.Draws,
		}); err != nil {
			return nil, mapTournamentErr(err)
		}
		return deps.tournamentBody(ctx, id, admin)
	})

	for _, action := range []string{"drop", "undrop"} {
		huma.Register(api, huma.Operation{
			OperationID: action + "TournamentPlayer",
			Method:      http.MethodPost,
			Path:        "/api/events/{eventId}/tournament/players/{playerId}/" + action,
			Summary:     "Player " + action + " (self or organizer)",
			Tags:        []string{"tournaments"},
		}, func(ctx context.Context, in *playerActionInput) (*tournamentOutput, error) {
			uid, ok := CurrentUserID(ctx)
			if !ok {
				return nil, huma.Error401Unauthorized("authentication required")
			}
			id, err := parseEventID(in.EventID)
			if err != nil {
				return nil, err
			}
			playerID, err := uuid.Parse(in.PlayerID)
			if err != nil {
				return nil, eventProblem(http.StatusNotFound, "player-not-found", "no such player")
			}
			admin := isAdmin(ctx, deps)
			fn := deps.Tournaments.Drop
			if action == "undrop" {
				fn = deps.Tournaments.Undrop
			}
			if err := fn(ctx, id, playerID, uid, admin); err != nil {
				return nil, mapTournamentErr(err)
			}
			return deps.tournamentBody(ctx, id, admin)
		})
	}
}
