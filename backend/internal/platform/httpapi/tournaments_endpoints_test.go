package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
	"github.com/mjabloniec/cube-planner/backend/internal/tournaments"
)

func newTournamentServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:        auth.NewService(q, noopMailer{}, "http://test"),
		Sessions:    auth.NewSessions(q, false),
		Queries:     q,
		Tournaments: tournaments.NewService(q, pool),
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q
}

// meID fetches the caller's user id via /api/me, so tests can match
// tournament players without depending on how loggedInClient derives
// display names.
func meID(t *testing.T, c *cookieClient) uuid.UUID {
	t.Helper()
	resp := c.do(t, http.MethodGet, "/api/me", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/me = %d", resp.StatusCode)
	}
	me := decode[struct {
		ID uuid.UUID `json:"id"`
	}](t, resp)
	return me.ID
}

// seedStartedEvent creates a started event with n paid registered users
// (emails p0@test.. / password-free session via loggedInClient). It
// returns the organizer's and each player's logged-in client directly
// rather than letting the caller re-derive one from the same email:
// loggedInClient inserts a fresh user row on every call, so logging in
// twice for the same address would violate the users.email unique
// constraint.
func seedStartedEvent(t *testing.T, pool *pgxpool.Pool, q *db.Queries, srv *httptest.Server, n int) (eventID uuid.UUID, emails []string, org *cookieClient, players []*cookieClient) {
	t.Helper()
	ctx := context.Background()
	org = loggedInClient(t, srv, q, "org@test")
	makeAdmin(t, pool, "org@test")
	var organizerID uuid.UUID
	if err := pool.QueryRow(ctx,
		`select id from users where email = 'org@test'`).Scan(&organizerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`insert into events (organizer_id, name, starts_at, fee_cents,
		    max_participants, status)
		 values ($1, 'tourney', now(), 0, 64, 'started') returning id`,
		organizerID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	emails = make([]string, n)
	players = make([]*cookieClient, n)
	for i := range n {
		emails[i] = fmt.Sprintf("p%d@test", i)
		players[i] = loggedInClient(t, srv, q, emails[i])
		if _, err := pool.Exec(ctx,
			`insert into registrations (event_id, user_id, status, paid_at)
			 select $1, id, 'paid', now() from users where email = $2`,
			eventID, emails[i]); err != nil {
			t.Fatal(err)
		}
	}
	return eventID, emails, org, players
}

// jsonBody marshals v to a JSON string for cookieClient.do.
func jsonBody(t *testing.T, v any) string {
	t.Helper()
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTournamentEndpointsHappyPath(t *testing.T) {
	srv, pool, q := newTournamentServer(t)
	eventID, _, org, players := seedStartedEvent(t, pool, q, srv, 4)
	player := players[0]
	myPlayerUserID := meID(t, player)
	base := fmt.Sprintf("/api/events/%s/tournament", eventID)

	// 404 before creation.
	resp := player.do(t, http.MethodGet, base, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("pre-creation GET = %d, want 404", resp.StatusCode)
	}
	// Non-admin cannot pair.
	resp = player.do(t, http.MethodPost, base+"/rounds", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("player pair = %d, want 403", resp.StatusCode)
	}
	// Organizer pairs round 1 (auto-creates), sees the draft round.
	resp = org.do(t, http.MethodPost, base+"/rounds", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pair = %d", resp.StatusCode)
	}
	tourOrg := decode[httpapi.TournamentInfo](t, resp)
	if len(tourOrg.Rounds) != 1 || tourOrg.Rounds[0].Status != "draft" {
		t.Fatalf("organizer draft round missing: %+v", tourOrg.Rounds)
	}
	// Player does NOT see the draft round.
	resp = player.do(t, http.MethodGet, base, "")
	tourPl := decode[httpapi.TournamentInfo](t, resp)
	if len(tourPl.Rounds) != 0 {
		t.Fatalf("player sees draft round")
	}
	// Publish; player now sees pairings and reports own match.
	resp = org.do(t, http.MethodPost, base+"/rounds/1/publish", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish = %d", resp.StatusCode)
	}
	resp = player.do(t, http.MethodGet, base, "")
	tourPl = decode[httpapi.TournamentInfo](t, resp)
	var myPlayerID uuid.UUID
	for _, p := range tourPl.Players {
		if p.UserID == myPlayerUserID {
			myPlayerID = p.ID
		}
	}
	if myPlayerID == uuid.Nil {
		t.Fatal("caller's tournament player not found")
	}
	var myMatch *httpapi.TournamentMatchInfo
	for i, m := range tourPl.Rounds[0].Matches {
		if m.Player1ID == myPlayerID || (m.Player2ID != nil && *m.Player2ID == myPlayerID) {
			myMatch = &tourPl.Rounds[0].Matches[i]
		}
	}
	if myMatch == nil {
		t.Fatal("caller's match not found")
	}
	resp = player.do(t, http.MethodPut,
		fmt.Sprintf("%s/matches/%s/result", base, myMatch.ID),
		jsonBody(t, map[string]any{"p1Games": 2, "p2Games": 1, "draws": 0}))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("player report = %d", resp.StatusCode)
	}
	// Standings react.
	resp = player.do(t, http.MethodGet, base, "")
	tourPl = decode[httpapi.TournamentInfo](t, resp)
	if tourPl.Standings[0].MatchPoints != 3 {
		t.Fatalf("leader MP = %d, want 3", tourPl.Standings[0].MatchPoints)
	}
	// Drop self.
	resp = player.do(t, http.MethodPost,
		fmt.Sprintf("%s/players/%s/drop", base, myPlayerID), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("self-drop = %d", resp.StatusCode)
	}
	// Complete blocked while a result is missing.
	resp = org.do(t, http.MethodPost, base+"/rounds/1/complete", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("complete incomplete = %d, want 409", resp.StatusCode)
	}
}

func TestTournamentAdminGates(t *testing.T) {
	srv, pool, q := newTournamentServer(t)
	eventID, _, _, players := seedStartedEvent(t, pool, q, srv, 4)
	player := players[1]
	base := fmt.Sprintf("/api/events/%s/tournament", eventID)
	// swap needs a schema-valid body (matchId/slot are required fields);
	// otherwise huma's request validation rejects it with 422 before the
	// handler's requireAdmin check ever runs, which would mask the gate
	// this test is asserting.
	swapBody := jsonBody(t, map[string]any{
		"a": map[string]any{"matchId": uuid.New(), "slot": 1},
		"b": map[string]any{"matchId": uuid.New(), "slot": 2},
	})
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, base, jsonBody(t, map[string]any{})},
		{http.MethodPost, base + "/rounds", jsonBody(t, map[string]any{})},
		{http.MethodPost, base + "/rounds/1/reroll", jsonBody(t, map[string]any{})},
		{http.MethodPost, base + "/rounds/1/publish", jsonBody(t, map[string]any{})},
		{http.MethodPost, base + "/rounds/1/complete", jsonBody(t, map[string]any{})},
		{http.MethodPost, base + "/rounds/1/swap", swapBody},
	} {
		resp := player.do(t, tc.method, tc.path, tc.body)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
	}
}
