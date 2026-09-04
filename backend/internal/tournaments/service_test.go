package tournaments

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

type fixture struct {
	svc     *Service
	q       *db.Queries
	pool    *pgxpool.Pool
	eventID uuid.UUID
	users   []uuid.UUID // paid players, index-stable
}

// newFixture seeds an organizer, a started event, and n paid players.
func newFixture(t *testing.T, n int) *fixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.New(t)
	q := db.New(pool)

	var organizerID uuid.UUID
	err := pool.QueryRow(ctx,
		`insert into users (email, display_name, role, email_verified_at)
		 values ('org@test', 'org', 'admin', now()) returning id`).Scan(&organizerID)
	if err != nil {
		t.Fatal(err)
	}
	var eventID uuid.UUID
	err = pool.QueryRow(ctx,
		`insert into events (organizer_id, name, starts_at, fee_cents,
		    max_participants, status)
		 values ($1, 'tourney', now(), 0, 64, 'started') returning id`,
		organizerID).Scan(&eventID)
	if err != nil {
		t.Fatal(err)
	}
	users := make([]uuid.UUID, n)
	for i := range n {
		err = pool.QueryRow(ctx,
			`insert into users (email, display_name, email_verified_at)
			 values ($1, $2, now()) returning id`,
			fmt.Sprintf("p%d@test", i), fmt.Sprintf("player%02d", i)).Scan(&users[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx,
			`insert into registrations (event_id, user_id, status, paid_at)
			 values ($1, $2, 'paid', now())`, eventID, users[i]); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(q, pool)
	svc.newSeed = func() int64 { return 42 }
	return &fixture{svc: svc, q: q, pool: pool, eventID: eventID, users: users}
}

func (f *fixture) detail(t *testing.T) *Detail {
	t.Helper()
	d, err := f.svc.Get(context.Background(), f.eventID, true)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *fixture) playerByUser(t *testing.T, userID uuid.UUID) PlayerDetail {
	t.Helper()
	for _, p := range f.detail(t).Players {
		if p.UserID == userID {
			return p
		}
	}
	t.Fatalf("player for user %v not found", userID)
	return PlayerDetail{}
}

// findMatch locates a match by ID within a Detail, for assertions after a
// fresh Get.
func findMatch(d *Detail, matchID uuid.UUID) MatchDetail {
	for _, rd := range d.Rounds {
		for _, m := range rd.Matches {
			if m.ID == matchID {
				return m
			}
		}
	}
	return MatchDetail{}
}

// userFor returns the user ID behind a tournament player ID.
func userFor(d *Detail, playerID uuid.UUID) uuid.UUID {
	for _, p := range d.Players {
		if p.ID == playerID {
			return p.UserID
		}
	}
	return uuid.UUID{}
}

// reportAll enters 2-0 (admin) for every unreported match of round n.
func (f *fixture) reportAll(t *testing.T, roundNumber int32) {
	t.Helper()
	ctx := context.Background()
	for _, r := range f.detail(t).Rounds {
		if r.Number != roundNumber {
			continue
		}
		for _, m := range r.Matches {
			if m.ReportedAt != nil {
				continue
			}
			if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
				Result{P1Games: 2}); err != nil {
				t.Fatalf("report match %v: %v", m.ID, err)
			}
		}
	}
}

func TestLazyCreationSnapshotsPaidRoster(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if d, err := f.svc.Get(ctx, f.eventID, true); err != nil || d.Exists {
		t.Fatalf("pre-creation Get = (%+v, %v), want Exists=false, err=nil", d, err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	if len(d.Players) != 4 || d.PlannedRounds != 2 {
		t.Errorf("players=%d planned=%d, want 4 and 2", len(d.Players), d.PlannedRounds)
	}
	// Late registration flip must not change the snapshot.
	if _, err := f.pool.Exec(ctx,
		`update registrations set status='refunded' where user_id=$1`, f.users[0]); err != nil {
		t.Fatal(err)
	}
	if got := len(f.detail(t).Players); got != 4 {
		t.Errorf("roster changed after refund: %d players", got)
	}
}

func TestRoundLifecycleHappyPath(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	// Second pair while draft in flight → round-exists.
	if err := f.svc.PairNextRound(ctx, f.eventID); err != ErrRoundExists {
		t.Fatalf("pair with draft open = %v, want ErrRoundExists", err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.eventID, 1); err != ErrRoundIncomplete {
		t.Fatalf("complete unreported = %v, want ErrRoundIncomplete", err)
	}
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	f2 := f.detail(t)
	if len(f2.Rounds) != 2 || f2.Rounds[1].Status != "draft" {
		t.Fatalf("round 2 missing or not draft: %+v", f2.Rounds)
	}
	// Round 3 would exceed planned 2 (after completing 2).
	if err := f.svc.Publish(ctx, f.eventID, 2); err != nil {
		t.Fatal(err)
	}
	f.reportAll(t, 2)
	if err := f.svc.Complete(ctx, f.eventID, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != ErrAllRoundsPaired {
		t.Fatalf("pair past planned = %v, want ErrAllRoundsPaired", err)
	}
}

func TestOddRosterGetsByeAutoFilledOnPublish(t *testing.T) {
	f := newFixture(t, 5)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	var byeSeen bool
	for _, m := range d.Rounds[0].Matches {
		if m.Player2ID == nil {
			byeSeen = true
			if m.P1Games == nil || *m.P1Games != 2 || m.ReportedAt == nil {
				t.Errorf("bye not auto-filled 2-0: %+v", m)
			}
			if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
				Result{P1Games: 2}); err != ErrByeImmutable {
				t.Errorf("bye report = %v, want ErrByeImmutable", err)
			}
		}
	}
	if !byeSeen {
		t.Error("no bye in a 5-player round")
	}
}

func TestResultPermissions(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	var inMatch, stranger uuid.UUID
	for _, p := range d.Players {
		if p.ID == m.Player1ID {
			inMatch = p.UserID
		}
	}
	for _, u := range f.users {
		hit := false
		for _, p := range d.Players {
			if p.UserID == u && (p.ID == m.Player1ID || (m.Player2ID != nil && p.ID == *m.Player2ID)) {
				hit = true
			}
		}
		if !hit {
			stranger = u
		}
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, stranger, false,
		Result{P1Games: 2}); err != ErrNotInMatch {
		t.Fatalf("stranger report = %v, want ErrNotInMatch", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, inMatch, false,
		Result{P1Games: 2, P2Games: 1}); err != nil {
		t.Fatalf("player report = %v", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, inMatch, false,
		Result{P1Games: 2, P2Games: 2}); err != ErrResultInvalid {
		t.Fatalf("2-2 = %v, want ErrResultInvalid", err)
	}
	// After completing the round, players are locked out; the admin
	// can still override until the next round is paired.
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, inMatch, false,
		Result{P1Games: 2}); err != ErrResultLocked {
		t.Fatalf("player post-complete = %v, want ErrResultLocked", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 0, P2Games: 2}); err != nil {
		t.Fatalf("admin override post-complete = %v", err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 2}); err != ErrResultLocked {
		t.Fatalf("admin after next pair = %v, want ErrResultLocked", err)
	}
}

// The result form no longer collects a drawn-games count; ReportResult
// must always persist 0 for it regardless of what the caller passes for
// P1Games/P2Games, and standings must still read a 1-1 score as a draw.
func TestReportResultPersistsZeroDraws(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 1, P2Games: 1}); err != nil {
		t.Fatalf("report 1-1 = %v", err)
	}
	after := f.detail(t)
	var reported MatchDetail
	for _, rd := range after.Rounds {
		if rd.Number != 1 {
			continue
		}
		for _, rm := range rd.Matches {
			if rm.ID == m.ID {
				reported = rm
			}
		}
	}
	if reported.Draws == nil || *reported.Draws != 0 {
		t.Fatalf("stored draws = %v, want 0", reported.Draws)
	}
	for _, id := range []uuid.UUID{m.Player1ID, *m.Player2ID} {
		var mp int
		for _, s := range after.Standings {
			if s.PlayerID == id {
				mp = s.MatchPoints
			}
		}
		if mp != 1 {
			t.Errorf("player %v: MP = %d, want 1 (a 1-1 draw)", id, mp)
		}
	}
}

// The report log is append-only: every reported result is preserved even
// when a later report overwrites the authoritative result on the match
// itself (last write wins there, unchanged).
func TestReportResultAppendsToTheLog(t *testing.T) {
	f := newFixture(t, 2)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	var p1User, p2User uuid.UUID
	for _, p := range d.Players {
		if p.ID == m.Player1ID {
			p1User = p.UserID
		}
		if m.Player2ID != nil && p.ID == *m.Player2ID {
			p2User = p.UserID
		}
	}
	tour, err := f.q.GetTournamentByEvent(ctx, f.eventID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, p1User, false,
		Result{P1Games: 2, P2Games: 1}); err != nil {
		t.Fatalf("player A report = %v", err)
	}
	reports, err := f.q.ListMatchResultReportsForTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports after A = %d, want 1", len(reports))
	}
	if reports[0].IsOrganizer {
		t.Error("player report must not be flagged organizer")
	}
	if reports[0].P1Games != 2 || reports[0].P2Games != 1 {
		t.Errorf("report A score = %d-%d, want 2-1", reports[0].P1Games, reports[0].P2Games)
	}

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, p2User, false,
		Result{P1Games: 1, P2Games: 2}); err != nil {
		t.Fatalf("player B report = %v", err)
	}
	reports, err = f.q.ListMatchResultReportsForTournament(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports after B = %d, want 2 (both preserved)", len(reports))
	}

	after := f.detail(t)
	var stored MatchDetail
	for _, rd := range after.Rounds {
		for _, rm := range rd.Matches {
			if rm.ID == m.ID {
				stored = rm
			}
		}
	}
	if stored.P1Games == nil || *stored.P1Games != 1 || stored.P2Games == nil || *stored.P2Games != 2 {
		t.Fatalf("stored result = %v-%v, want 1-2 (last write wins)", stored.P1Games, stored.P2Games)
	}
}

// Once the organizer reports, ordinary players may no longer overwrite the
// result — "let players do the work until the owner steps in" — but the
// organizer may keep re-reporting.
func TestOrganizerReportLocksOutPlayers(t *testing.T) {
	f := newFixture(t, 2)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	p1User := userFor(d, m.Player1ID)

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 2, P2Games: 0}); err != nil {
		t.Fatalf("organizer report = %v", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, p1User, false,
		Result{P1Games: 0, P2Games: 2}); err != ErrResultLocked {
		t.Fatalf("player report after organizer = %v, want ErrResultLocked", err)
	}
	after := f.detail(t)
	stored := findMatch(after, m.ID)
	if stored.P1Games == nil || *stored.P1Games != 2 || stored.P2Games == nil || *stored.P2Games != 0 {
		t.Fatalf("stored result after refused player report = %v-%v, want 2-0",
			stored.P1Games, stored.P2Games)
	}

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 2, P2Games: 1}); err != nil {
		t.Fatalf("organizer re-report = %v", err)
	}
	after = f.detail(t)
	stored = findMatch(after, m.ID)
	if stored.P1Games == nil || *stored.P1Games != 2 || stored.P2Games == nil || *stored.P2Games != 1 {
		t.Fatalf("stored result after organizer re-report = %v-%v, want 2-1",
			stored.P1Games, stored.P2Games)
	}
}

// disputed is derived from the latest report per player, so it clears the
// moment the players' latest reports agree again — even though the
// disagreement did happen.
func TestDisputeClearsWhenPlayersAgree(t *testing.T) {
	f := newFixture(t, 2)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	ann := userFor(d, m.Player1ID)
	bob := userFor(d, *m.Player2ID)

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, ann, false,
		Result{P1Games: 2, P2Games: 1}); err != nil {
		t.Fatalf("Ann report = %v", err)
	}
	got := findMatch(f.detail(t), m.ID)
	if got.Disputed {
		t.Error("disputed = true with only one reporter, want false")
	}

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, bob, false,
		Result{P1Games: 1, P2Games: 2}); err != nil {
		t.Fatalf("Bob report = %v", err)
	}
	got = findMatch(f.detail(t), m.ID)
	if !got.Disputed {
		t.Error("disputed = false after players disagree, want true")
	}
	if !got.HadDispute {
		t.Error("hadDispute = false after players disagree, want true")
	}

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, bob, false,
		Result{P1Games: 2, P2Games: 1}); err != nil {
		t.Fatalf("Bob correction = %v", err)
	}
	got = findMatch(f.detail(t), m.ID)
	if got.Disputed {
		t.Error("disputed = true after players agree again, want false (clears)")
	}
	if !got.HadDispute {
		t.Error("hadDispute = false after clearing, want true (sticky)")
	}
	if got.P1Games == nil || *got.P1Games != 2 || got.P2Games == nil || *got.P2Games != 1 {
		t.Fatalf("stored result = %v-%v, want 2-1", got.P1Games, got.P2Games)
	}
}

// An organizer ruling resolves the dispute badge even though the players
// themselves never agreed; the fact that they once disagreed stays visible
// to organizers via hadDispute.
func TestOrganizerReportResolvesDispute(t *testing.T) {
	f := newFixture(t, 2)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	ann := userFor(d, m.Player1ID)
	bob := userFor(d, *m.Player2ID)

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, ann, false,
		Result{P1Games: 2, P2Games: 1}); err != nil {
		t.Fatalf("Ann report = %v", err)
	}
	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, bob, false,
		Result{P1Games: 1, P2Games: 2}); err != nil {
		t.Fatalf("Bob report = %v", err)
	}
	if got := findMatch(f.detail(t), m.ID); !got.Disputed {
		t.Fatal("want disputed=true before the organizer rules")
	}

	if err := f.svc.ReportResult(ctx, f.eventID, m.ID, f.users[0], true,
		Result{P1Games: 2, P2Games: 0}); err != nil {
		t.Fatalf("organizer report = %v", err)
	}
	got := findMatch(f.detail(t), m.ID)
	if got.Disputed {
		t.Error("disputed = true after organizer ruling, want false")
	}
	if !got.HadDispute {
		t.Error("hadDispute = false, want true (sticky)")
	}
}

// Reporting the same score twice is not a disagreement.
func TestRepeatedIdenticalReportsAreNotADispute(t *testing.T) {
	f := newFixture(t, 2)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	ann := userFor(d, m.Player1ID)

	for i := 0; i < 2; i++ {
		if err := f.svc.ReportResult(ctx, f.eventID, m.ID, ann, false,
			Result{P1Games: 2, P2Games: 1}); err != nil {
			t.Fatalf("Ann report #%d = %v", i+1, err)
		}
	}
	got := findMatch(f.detail(t), m.ID)
	if got.Disputed {
		t.Error("disputed = true after identical repeats, want false")
	}
	if got.HadDispute {
		t.Error("hadDispute = true after identical repeats, want false")
	}
}

func TestDropAndUndropTiming(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.Upsert(ctx, f.eventID, nil); err != nil {
		t.Fatal(err)
	}
	victim := f.playerByUser(t, f.users[3])
	other := f.playerByUser(t, f.users[2])
	// Self-drop by someone else's account is rejected.
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[2], false); err != ErrNotYourPlayer {
		t.Fatalf("foreign drop = %v, want ErrNotYourPlayer", err)
	}
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[3], false); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[3], false); err != ErrAlreadyDropped {
		t.Fatalf("double drop = %v, want ErrAlreadyDropped", err)
	}
	// Undrop before any pairing: fine.
	if err := f.svc.Undrop(ctx, f.eventID, victim.ID, f.users[3], false); err != nil {
		t.Fatal(err)
	}
	// Drop again, pair a round → undrop now rejected.
	if err := f.svc.Drop(ctx, f.eventID, victim.ID, f.users[3], false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond) // round.created_at must be after dropped_at
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Undrop(ctx, f.eventID, victim.ID, f.users[3], false); err != ErrUndropTooLate {
		t.Fatalf("late undrop = %v, want ErrUndropTooLate", err)
	}
	// Dropped player is excluded from the paired round.
	for _, m := range f.detail(t).Rounds[0].Matches {
		if m.Player1ID == victim.ID || (m.Player2ID != nil && *m.Player2ID == victim.ID) {
			t.Error("dropped player was paired")
		}
	}
	// Admin can drop anyone.
	if err := f.svc.Drop(ctx, f.eventID, other.ID, f.users[0], true); err != nil {
		t.Fatal(err)
	}
}

func TestPlannedRoundsGuardrails(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	one := int32(1)
	three := int32(3)
	if err := f.svc.Upsert(ctx, f.eventID, &three); err != nil {
		t.Fatalf("raise planned = %v", err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Upsert(ctx, f.eventID, &one); err != ErrPlannedRoundsTooLow {
		t.Fatalf("lower below paired = %v, want ErrPlannedRoundsTooLow", err)
	}
}

func TestSwapAndReroll(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	r := d.Rounds[0]
	m1, m2 := r.Matches[0], r.Matches[1]
	// Swap m1 slot1 with m2 slot1.
	if err := f.svc.Swap(ctx, f.eventID, 1,
		SlotRef{MatchID: m1.ID, Slot: 1}, SlotRef{MatchID: m2.ID, Slot: 1}); err != nil {
		t.Fatal(err)
	}
	after := f.detail(t).Rounds[0]
	if after.Matches[0].Player1ID != m2.Player1ID || after.Matches[1].Player1ID != m1.Player1ID {
		t.Error("swap did not exchange players")
	}
	// Self-pair rejected: swap m1.slot2 into m1.slot1's opponent seat.
	if err := f.svc.Swap(ctx, f.eventID, 1,
		SlotRef{MatchID: m1.ID, Slot: 1}, SlotRef{MatchID: m1.ID, Slot: 1}); err != ErrSwapInvalid {
		t.Fatalf("same-slot swap = %v, want ErrSwapInvalid", err)
	}
	if err := f.svc.Reroll(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if got := len(f.detail(t).Rounds[0].Matches); got != 2 {
		t.Fatalf("reroll left %d matches, want 2", got)
	}
	// Published rounds refuse draft edits.
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Reroll(ctx, f.eventID, 1); err != ErrRoundNotDraft {
		t.Fatalf("reroll published = %v, want ErrRoundNotDraft", err)
	}
}

func TestSwapWithinSameMatch(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	d := f.detail(t)
	m := d.Rounds[0].Matches[0]
	if m.Player2ID == nil {
		t.Fatal("match 0 has no player2 to swap with")
	}
	p1Before, p2Before := m.Player1ID, *m.Player2ID
	if err := f.svc.Swap(ctx, f.eventID, 1,
		SlotRef{MatchID: m.ID, Slot: 1}, SlotRef{MatchID: m.ID, Slot: 2}); err != nil {
		t.Fatal(err)
	}
	after := f.detail(t)
	var got MatchDetail
	for _, mm := range after.Rounds[0].Matches {
		if mm.ID == m.ID {
			got = mm
		}
	}
	if got.Player1ID != p2Before || got.Player2ID == nil || *got.Player2ID != p1Before {
		t.Fatalf("same-match swap did not exchange players: got p1=%v p2=%v, want p1=%v p2=%v",
			got.Player1ID, got.Player2ID, p2Before, p1Before)
	}
}

func TestCompleteBeforePublishIsNotPublished(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.eventID, 1); err != ErrRoundNotPublished {
		t.Fatalf("complete draft round = %v, want ErrRoundNotPublished", err)
	}
}

func TestMutationsRejectedOnNonStartedEvent(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx,
		`update events set status='finished' where id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != ErrEventNotStarted {
		t.Fatalf("publish on finished = %v, want ErrEventNotStarted", err)
	}
	// Reads still work.
	if _, err := f.svc.Get(ctx, f.eventID, true); err != nil {
		t.Fatalf("Get on finished = %v", err)
	}
}

func TestDraftRoundHiddenFromNonAdmin(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	d, err := f.svc.Get(ctx, f.eventID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rounds) != 0 {
		t.Errorf("non-admin sees %d draft rounds, want 0", len(d.Rounds))
	}
}

func TestFinishGuardBlocksOpenRound(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.PairNextRound(ctx, f.eventID); err != nil {
		t.Fatal(err)
	}
	open, err := f.q.CountOpenRoundsForEvent(ctx, f.eventID)
	if err != nil {
		t.Fatal(err)
	}
	if open != 1 {
		t.Fatalf("open rounds = %d, want 1", open)
	}
	if err := f.svc.Publish(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	f.reportAll(t, 1)
	if err := f.svc.Complete(ctx, f.eventID, 1); err != nil {
		t.Fatal(err)
	}
	open, err = f.q.CountOpenRoundsForEvent(ctx, f.eventID)
	if err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Fatalf("open rounds after complete = %d, want 0", open)
	}
}

// ---- PR 6: empty tournament aggregate ----

func TestGetBeforeTournamentExistsReportsRecommendation(t *testing.T) {
	f := newFixture(t, 12)
	d, err := f.svc.Get(context.Background(), f.eventID, true)
	if err != nil {
		t.Fatalf("Get must succeed before a tournament exists: %v", err)
	}
	if d.Exists {
		t.Fatal("Exists must be false when no tournament row exists")
	}
	// ceil(log2(12)) = 4
	if d.RecommendedRounds != 4 {
		t.Fatalf("want RecommendedRounds 4 for 12 players, got %d", d.RecommendedRounds)
	}
	if d.PaidPlayerCount != 12 {
		t.Fatalf("want PaidPlayerCount 12, got %d", d.PaidPlayerCount)
	}
	if len(d.Rounds) != 0 {
		t.Fatalf("want no rounds, got %d", len(d.Rounds))
	}
}

func TestGetAfterTournamentExistsSetsExists(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if err := f.svc.Upsert(ctx, f.eventID, nil); err != nil {
		t.Fatal(err)
	}
	d, err := f.svc.Get(ctx, f.eventID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Exists {
		t.Fatal("Exists must be true once the tournament row exists")
	}
	if d.RecommendedRounds != 2 {
		t.Fatalf("want RecommendedRounds 2 for 4 players, got %d", d.RecommendedRounds)
	}
}

func TestGetUnknownEventStillNotFound(t *testing.T) {
	f := newFixture(t, 0)
	if _, err := f.svc.Get(context.Background(), uuid.New(), true); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("an unknown event must still be not-found, got %v", err)
	}
}

func TestGetDraftEventStillNotFound(t *testing.T) {
	f := newFixture(t, 4)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`update events set status='draft' where id=$1`, f.eventID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Get(ctx, f.eventID, true); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("a draft event must still be not-found, got %v", err)
	}
}
