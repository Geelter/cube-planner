package swiss

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

// ids[i] gives stable test identities; names sort a < b < c < ...
func testPlayers(n int) ([]Player, []uuid.UUID) {
	ids := make([]uuid.UUID, n)
	players := make([]Player, n)
	for i := range n {
		ids[i] = uuid.New()
		players[i] = Player{ID: ids[i], DisplayName: string(rune('a' + i))}
	}
	return players, ids
}

func res(p1, p2, d int) *Result { return &Result{P1Games: p1, P2Games: p2, Draws: d} }

func vs(p1, p2 uuid.UUID, r *Result) Match { return Match{Player1: p1, Player2: &p2, Result: r} }

func bye(p uuid.UUID) Match { return Match{Player1: p, Result: res(2, 0, 0)} }

func within(t *testing.T, got, want float64, label string) {
	t.Helper()
	if math.Abs(got-want) > 0.05 {
		t.Errorf("%s = %.3f, want %.3f", label, got, want)
	}
}

func byID(s []Standing, id uuid.UUID) Standing {
	for _, row := range s {
		if row.PlayerID == id {
			return row
		}
	}
	return Standing{}
}

func TestStandingsMatchPoints(t *testing.T) {
	players, ids := testPlayers(4)
	// r1: 0 beats 1 (2-0), 2 draws 3 (1-1-1)
	s := ComputeStandings(players, []Match{
		vs(ids[0], ids[1], res(2, 0, 0)),
		vs(ids[2], ids[3], res(1, 1, 1)),
	})
	if got := byID(s, ids[0]).MatchPoints; got != 3 {
		t.Errorf("winner MP = %d, want 3", got)
	}
	if got := byID(s, ids[1]).MatchPoints; got != 0 {
		t.Errorf("loser MP = %d, want 0", got)
	}
	for _, id := range []uuid.UUID{ids[2], ids[3]} {
		if got := byID(s, id).MatchPoints; got != 1 {
			t.Errorf("draw MP = %d, want 1", got)
		}
	}
	if s[0].PlayerID != ids[0] || s[0].Rank != 1 {
		t.Errorf("rank 1 = %v (rank %d), want player 0", s[0].PlayerID, s[0].Rank)
	}
}

func TestStandingsUnreportedIgnored(t *testing.T) {
	players, ids := testPlayers(2)
	s := ComputeStandings(players, []Match{vs(ids[0], ids[1], nil)})
	for _, row := range s {
		if row.MatchPoints != 0 {
			t.Errorf("unreported match granted points: %+v", row)
		}
	}
}

// MTR fixture, hand-computed. 4 players, 2 rounds:
//
//	r1: A beats B 2-1, C beats D 2-0
//	r2: A beats C 2-1, B beats D 2-1
//
// A: 6 MP. B: 3, C: 3, D: 0.
// MW%: A=1.0, B=.5, C=.5, D=0→floor 1/3.
// A OMW% = avg(B .5, C .5) = 50.0
// B OMW% = avg(A 1.0, D 1/3) = 66.7 → B ranks above C
// C OMW% = avg(D 1/3, A 1.0) = 66.7 — equal; game tiebreaks decide:
// B GW%: games 3+3=6, points 3*(1+2)=9 → wait: B won 1 game r1, 2 games r2
//
//	→ gamePoints 9, games 6 → 50.0
//
// C GW%: r1 2 wins of 2, r2 1 win of 3 → points 9, games 5 → 60.0 → C above B
// GW% raw: A=12/18=.6667, B=9/18=.5, C=9/15=.6, D=3/15=.2 (floors to 1/3 as opponent).
// A OGW% = avg(B .5, C .6) = 55.0
// B OGW% = avg(A .6667, D floored 1/3) = 50.0
// D OGW% = avg(C .6, B .5) = 55.0
func TestStandingsTiebreakerChain(t *testing.T) {
	players, ids := testPlayers(4)
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]
	s := ComputeStandings(players, []Match{
		vs(a, b, res(2, 1, 0)), vs(c, d, res(2, 0, 0)),
		vs(a, c, res(2, 1, 0)), vs(b, d, res(2, 1, 0)),
	})
	within(t, byID(s, a).OMWPercent, 50.0, "A OMW%")
	within(t, byID(s, b).OMWPercent, 66.7, "B OMW%")
	within(t, byID(s, c).OMWPercent, 66.7, "C OMW%")
	within(t, byID(s, b).GWPercent, 50.0, "B GW%")
	within(t, byID(s, c).GWPercent, 60.0, "C GW%")
	within(t, byID(s, a).OGWPercent, 55.0, "A OGW%")
	within(t, byID(s, b).OGWPercent, 50.0, "B OGW%")
	within(t, byID(s, d).OGWPercent, 55.0, "D OGW%")
	if s[0].PlayerID != a || s[1].PlayerID != c || s[2].PlayerID != b || s[3].PlayerID != d {
		t.Errorf("order = %v, want A C B D", []uuid.UUID{s[0].PlayerID, s[1].PlayerID, s[2].PlayerID, s[3].PlayerID})
	}
	if s[0].Rank != 1 || s[1].Rank != 2 || s[2].Rank != 3 || s[3].Rank != 4 {
		t.Errorf("ranks = %d %d %d %d, want 1 2 3 4", s[0].Rank, s[1].Rank, s[2].Rank, s[3].Rank)
	}
}

// Byes: 3 MP; included in own MW% denominator (MTR: a bye is an awarded
// win) but the bye is not an opponent and its games don't count in GW%.
func TestStandingsByeExclusion(t *testing.T) {
	players, ids := testPlayers(3)
	a, b, c := ids[0], ids[1], ids[2]
	s := ComputeStandings(players, []Match{
		vs(a, b, res(2, 0, 0)), bye(c),
	})
	rc := byID(s, c)
	if rc.MatchPoints != 3 {
		t.Errorf("bye MP = %d, want 3", rc.MatchPoints)
	}
	// c played nobody: OMW% 0, GW% 0 (no real games).
	within(t, rc.OMWPercent, 0, "C OMW% (bye only)")
	within(t, rc.GWPercent, 0, "C GW% (bye only)")
	// a's OMW% = b's floored MW% = 33.3; b's OMW% = a's 100.
	within(t, byID(s, a).OMWPercent, 33.3, "A OMW%")
	within(t, byID(s, b).OMWPercent, 100, "B OMW%")
}

// The 1/3 floor: an 0-2 opponent contributes 33.3, not 0.
func TestStandingsFloor(t *testing.T) {
	players, ids := testPlayers(4)
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]
	s := ComputeStandings(players, []Match{
		vs(a, d, res(2, 0, 0)), vs(b, c, res(2, 0, 0)),
		vs(a, b, res(2, 0, 0)), vs(c, d, res(2, 0, 0)),
	})
	// d lost both: raw MW% 0 → floored 1/3 in a's OMW%.
	within(t, byID(s, a).OMWPercent, (100.0/3+50)/2, "A OMW% with floored D")
}

// Ties on every key share a rank; order within the tie is by name.
func TestStandingsSharedRank(t *testing.T) {
	players, ids := testPlayers(4)
	a, b, c, d := ids[0], ids[1], ids[2], ids[3]
	// Two independent identical results: a beats b, c beats d — a/c and
	// b/d are symmetric on all keys.
	s := ComputeStandings(players, []Match{
		vs(a, b, res(2, 0, 0)), vs(c, d, res(2, 0, 0)),
	})
	if s[0].Rank != 1 || s[1].Rank != 1 || s[2].Rank != 3 || s[3].Rank != 3 {
		t.Errorf("ranks = %d %d %d %d, want 1 1 3 3", s[0].Rank, s[1].Rank, s[2].Rank, s[3].Rank)
	}
	if s[0].DisplayName != "a" || s[1].DisplayName != "c" {
		t.Errorf("tie order = %s, %s; want a, c", s[0].DisplayName, s[1].DisplayName)
	}
}

// Full-tournament characterization fixture: 5 players (a–e), 3 rounds,
// exercising a bye, a draw, a mid-tournament drop, and both floors at
// once. Every expectation is hand-computed from the spec (§3.4) below.
//
//	r1: A beats B 2-1, C beats D 2-0, E bye
//	r2: A draws C 1-1-1, E beats B 2-0, D bye
//	    (D drops after r2 → r3 pairs 4 players, no bye)
//	r3: A beats E 2-1, C beats B 2-1
//
// Match points (win 3 / draw 1 / bye 3):
//
//	A: W+D+W          = 7    C: W+D+W = 7    E: bye+W+L = 6
//	D: L+bye          = 3    B: L+L+L = 0    (sum 23 = 17 played + 6 bye ✓)
//
// Own MW% (MP / 3·matches, byes INCLUDED as wins, floored 1/3 as opp):
//
//	A: 7/9    B: 0 → 1/3    C: 7/9    D: 3/6 = 1/2    E: 6/9 = 2/3
//
// Own GW% (gamePoints / 3·games, bye games EXCLUDED; floored as opp):
//
//	A: (6+4+6)/(9+9+9)   = 16/27 = 59.26%
//	B: (3+0+3)/(9+6+9)   =  6/24 = 25.00% → 1/3 as opponent
//	C: (6+4+6)/(6+9+9)   = 16/24 = 66.67%
//	D: 0/6               =  0.00%         → 1/3 as opponent
//	E: (6+3)/(6+9)       =  9/15 = 60.00%
//
// OMW% (avg of opponents' floored MW%; byes are not opponents):
//
//	A vs B,C,E: (1/3 + 7/9 + 2/3)/3   = 16/27 = 59.26%
//	B vs A,E,C: (7/9 + 2/3 + 7/9)/3   = 20/27 = 74.07%
//	C vs D,A,B: (1/2 + 7/9 + 1/3)/3   = 29/54 = 53.70%
//	D vs C:      7/9                  = 77.78%
//	E vs B,A:   (1/3 + 7/9)/2         =  5/9  = 55.56%
//
// OGW% (avg of opponents' floored GW%):
//
//	A vs B,C,E: (1/3 + 2/3 + 3/5)/3      =  8/15  = 53.33%
//	B vs A,E,C: (16/27 + 3/5 + 2/3)/3    = 251/405 = 61.98%
//	C vs D,A,B: (1/3 + 16/27 + 1/3)/3    = 34/81  = 41.98%
//	D vs C:      2/3                     = 66.67%
//	E vs B,A:   (1/3 + 16/27)/2          = 25/54  = 46.30%
//
// Sort MP → OMW%: A (7, 59.26) over C (7, 53.70), then E 6, D 3, B 0.
func TestStandingsFullTournament(t *testing.T) {
	players, ids := testPlayers(5)
	a, b, c, d, e := ids[0], ids[1], ids[2], ids[3], ids[4]
	players[3].Dropped = true
	s := ComputeStandings(players, []Match{
		vs(a, b, res(2, 1, 0)), vs(c, d, res(2, 0, 0)), bye(e),
		vs(a, c, res(1, 1, 1)), vs(e, b, res(2, 0, 0)), bye(d),
		vs(a, e, res(2, 1, 0)), vs(c, b, res(2, 1, 0)),
	})

	want := []struct {
		id           uuid.UUID
		rank, mp     int
		omw, gw, ogw float64
		dropped      bool
	}{
		{a, 1, 7, 59.26, 59.26, 53.33, false},
		{c, 2, 7, 53.70, 66.67, 41.98, false},
		{e, 3, 6, 55.56, 60.00, 46.30, false},
		{d, 4, 3, 77.78, 0.00, 66.67, true},
		{b, 5, 0, 74.07, 25.00, 61.98, false},
	}
	if len(s) != len(want) {
		t.Fatalf("got %d rows, want %d", len(s), len(want))
	}
	for i, w := range want {
		row := s[i]
		if row.PlayerID != w.id {
			t.Errorf("row %d = %s, want %s", i, row.DisplayName, byID(s, w.id).DisplayName)
			continue
		}
		if row.Rank != w.rank {
			t.Errorf("%s rank = %d, want %d", row.DisplayName, row.Rank, w.rank)
		}
		if row.MatchPoints != w.mp {
			t.Errorf("%s MP = %d, want %d", row.DisplayName, row.MatchPoints, w.mp)
		}
		if row.Dropped != w.dropped {
			t.Errorf("%s dropped = %v, want %v", row.DisplayName, row.Dropped, w.dropped)
		}
		within(t, row.OMWPercent, w.omw, row.DisplayName+" OMW%")
		within(t, row.GWPercent, w.gw, row.DisplayName+" GW%")
		within(t, row.OGWPercent, w.ogw, row.DisplayName+" OGW%")
	}
}

// Dropped players stay ranked (flagged) and feed opponents' tiebreaks.
func TestStandingsDropped(t *testing.T) {
	players, ids := testPlayers(2)
	players[1].Dropped = true
	s := ComputeStandings(players, []Match{vs(ids[0], ids[1], res(0, 2, 0))})
	top := s[0]
	if top.PlayerID != ids[1] || !top.Dropped {
		t.Errorf("dropped winner should lead standings flagged, got %+v", top)
	}
}
