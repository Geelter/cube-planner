package swiss

import (
	"testing"

	"github.com/google/uuid"
)

func matchPointsOf(history []Match) map[uuid.UUID]int {
	pts := map[uuid.UUID]int{}
	for _, m := range history {
		if m.Result == nil {
			continue
		}
		if m.Player2 == nil {
			pts[m.Player1] += 3
			continue
		}
		switch {
		case m.Result.P1Games > m.Result.P2Games:
			pts[m.Player1] += 3
		case m.Result.P2Games > m.Result.P1Games:
			pts[*m.Player2] += 3
		default:
			pts[m.Player1]++
			pts[*m.Player2]++
		}
	}
	return pts
}

// every active player exactly once; dropped players absent; tables 1..n.
func assertValidPairings(t *testing.T, players []Player, got []Pairing) {
	t.Helper()
	seen := map[uuid.UUID]bool{}
	for i, p := range got {
		if p.TableNumber != i+1 {
			t.Errorf("table %d at index %d", p.TableNumber, i)
		}
		for _, id := range pairingIDs(p) {
			if seen[id] {
				t.Errorf("player %v paired twice", id)
			}
			seen[id] = true
		}
	}
	for _, pl := range players {
		if pl.Dropped && seen[pl.ID] {
			t.Errorf("dropped player %v paired", pl.ID)
		}
		if !pl.Dropped && !seen[pl.ID] {
			t.Errorf("active player %v unpaired", pl.ID)
		}
	}
}

func pairingIDs(p Pairing) []uuid.UUID {
	ids := []uuid.UUID{p.Player1}
	if p.Player2 != nil {
		ids = append(ids, *p.Player2)
	}
	return ids
}

func TestPairRoundOne(t *testing.T) {
	players, _ := testPlayers(8)
	got := Pair(players, nil, 42)
	if len(got) != 4 {
		t.Fatalf("pairings = %d, want 4", len(got))
	}
	assertValidPairings(t, players, got)
}

func TestPairDeterministicPerSeed(t *testing.T) {
	players, _ := testPlayers(8)
	a := Pair(players, nil, 7)
	b := Pair(players, nil, 7)
	for i := range a {
		if a[i].Player1 != b[i].Player1 {
			t.Fatalf("same seed produced different pairings")
		}
	}
}

func TestPairOddCountAssignsOneBye(t *testing.T) {
	players, _ := testPlayers(7)
	got := Pair(players, nil, 1)
	assertValidPairings(t, players, got)
	last := got[len(got)-1]
	if last.Player2 != nil {
		t.Fatalf("last pairing should be the bye")
	}
	for _, p := range got[:len(got)-1] {
		if p.Player2 == nil {
			t.Errorf("extra bye at table %d", p.TableNumber)
		}
	}
}

func TestPairByeGoesToLowestGroupWithoutPriorBye(t *testing.T) {
	players, ids := testPlayers(5)
	// r1: 0 beats 1, 2 beats 3, bye 4. Losers (1,3) are the low group;
	// 4 has 3 MP and a prior bye.
	history := []Match{
		vs(ids[0], ids[1], res(2, 0, 0)),
		vs(ids[2], ids[3], res(2, 0, 0)),
		bye(ids[4]),
	}
	for seed := int64(0); seed < 20; seed++ {
		got := Pair(players, history, seed)
		byeP := got[len(got)-1]
		if byeP.Player2 != nil {
			t.Fatalf("seed %d: no bye", seed)
		}
		if byeP.Player1 != ids[1] && byeP.Player1 != ids[3] {
			t.Errorf("seed %d: bye to %v, want a 0-point player", seed, byeP.Player1)
		}
	}
}

func TestPairGroupsByPoints(t *testing.T) {
	players, ids := testPlayers(8)
	// r1 winners: 0,2,4,6.
	history := []Match{
		vs(ids[0], ids[1], res(2, 0, 0)), vs(ids[2], ids[3], res(2, 0, 0)),
		vs(ids[4], ids[5], res(2, 0, 0)), vs(ids[6], ids[7], res(2, 0, 0)),
	}
	pts := matchPointsOf(history)
	got := Pair(players, history, 3)
	assertValidPairings(t, players, got)
	for _, p := range got {
		if pts[p.Player1] != pts[*p.Player2] {
			t.Errorf("cross-group pairing %v(%d) vs %v(%d) with even groups",
				p.Player1, pts[p.Player1], *p.Player2, pts[*p.Player2])
		}
	}
}

func TestPairAvoidsRematches(t *testing.T) {
	players, ids := testPlayers(4)
	history := []Match{
		vs(ids[0], ids[1], res(2, 0, 0)), vs(ids[2], ids[3], res(2, 0, 0)),
	}
	played := map[[2]uuid.UUID]bool{
		{ids[0], ids[1]}: true, {ids[1], ids[0]}: true,
		{ids[2], ids[3]}: true, {ids[3], ids[2]}: true,
	}
	for seed := int64(0); seed < 20; seed++ {
		for _, p := range Pair(players, history, seed) {
			if played[[2]uuid.UUID{p.Player1, *p.Player2}] {
				t.Errorf("seed %d: rematch %v vs %v", seed, p.Player1, *p.Player2)
			}
		}
	}
}

// 2 players, already played: a rematch is unavoidable — Pair must still
// return a full pairing rather than fail.
func TestPairMinimalRematchFallback(t *testing.T) {
	players, ids := testPlayers(2)
	history := []Match{vs(ids[0], ids[1], res(2, 0, 0))}
	got := Pair(players, history, 5)
	if len(got) != 1 || got[0].Player2 == nil {
		t.Fatalf("want the single forced rematch, got %+v", got)
	}
}

func TestPairExcludesDropped(t *testing.T) {
	players, _ := testPlayers(6)
	players[2].Dropped = true
	got := Pair(players, nil, 9)
	assertValidPairings(t, players, got)
	// 5 actives → 2 tables + bye.
	if len(got) != 3 {
		t.Fatalf("pairings = %d, want 3", len(got))
	}
}

func TestPairNoRepeatByeWidensUpward(t *testing.T) {
	players, ids := testPlayers(3)
	// Everyone at 0 except: 0 had the bye already (3 MP).
	history := []Match{bye(ids[0]), vs(ids[1], ids[2], res(1, 1, 1))}
	for seed := int64(0); seed < 20; seed++ {
		got := Pair(players, history, seed)
		byeP := got[len(got)-1]
		if byeP.Player1 == ids[0] {
			t.Errorf("seed %d: repeat bye", seed)
		}
	}
}
