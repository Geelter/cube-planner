package swiss

import (
	"sort"

	"github.com/google/uuid"
)

// Standing is one computed standings row. Percentages are ×100
// (33.3, not 0.333), matching the wire format.
type Standing struct {
	Rank        int
	PlayerID    uuid.UUID
	DisplayName string
	Dropped     bool
	MatchPoints int
	OMWPercent  float64
	GWPercent   float64
	OGWPercent  float64
}

const tiebreakFloor = 1.0 / 3.0

// tally accumulates one player's reported results.
type tally struct {
	matchPoints int
	matches     int // reported matches, byes included
	gamePoints  int // 3 per game win, 1 per drawn game; byes excluded
	games       int // games played, byes excluded
	opponents   []uuid.UUID
}

// ComputeStandings ranks players per MTR: match points, then OMW%, GW%,
// OGW%. A bye is an awarded 2-0 win (3 MP, counts in own MW%) but is
// excluded from tiebreaks otherwise: not an opponent, and its games do
// not enter GW%. Opponents' MW%/GW% are floored at 1/3. Dropped players
// stay ranked and keep feeding opponents' tiebreaks. Ties on all keys
// share a rank (competition ranking) ordered by display name.
func ComputeStandings(players []Player, matches []Match) []Standing {
	tallies := make(map[uuid.UUID]*tally, len(players))
	for _, p := range players {
		tallies[p.ID] = &tally{}
	}
	for _, m := range matches {
		if m.Result == nil {
			continue
		}
		r := *m.Result
		t1 := tallies[m.Player1]
		if m.Player2 == nil {
			if t1 != nil { // bye: match points only
				t1.matchPoints += 3
				t1.matches++
			}
			continue
		}
		t2 := tallies[*m.Player2]
		if t1 == nil || t2 == nil {
			continue
		}
		t1.matches++
		t2.matches++
		t1.opponents = append(t1.opponents, *m.Player2)
		t2.opponents = append(t2.opponents, m.Player1)
		switch {
		case r.P1Games > r.P2Games:
			t1.matchPoints += 3
		case r.P2Games > r.P1Games:
			t2.matchPoints += 3
		default:
			t1.matchPoints++
			t2.matchPoints++
		}
		games := r.P1Games + r.P2Games + r.Draws
		t1.games += games
		t2.games += games
		t1.gamePoints += 3*r.P1Games + r.Draws
		t2.gamePoints += 3*r.P2Games + r.Draws
	}

	// Opponent metrics (floored per MTR).
	mwFloored := func(t *tally) float64 {
		if t.matches == 0 {
			return tiebreakFloor
		}
		return max(float64(t.matchPoints)/float64(3*t.matches), tiebreakFloor)
	}
	gwRaw := func(t *tally) float64 {
		if t.games == 0 {
			return 0
		}
		return float64(t.gamePoints) / float64(3*t.games)
	}
	gwFloored := func(t *tally) float64 { return max(gwRaw(t), tiebreakFloor) }

	rows := make([]Standing, len(players))
	for i, p := range players {
		t := tallies[p.ID]
		var omw, ogw float64
		for _, opp := range t.opponents {
			omw += mwFloored(tallies[opp])
			ogw += gwFloored(tallies[opp])
		}
		if n := len(t.opponents); n > 0 {
			omw /= float64(n)
			ogw /= float64(n)
		}
		rows[i] = Standing{
			PlayerID: p.ID, DisplayName: p.DisplayName, Dropped: p.Dropped,
			MatchPoints: t.matchPoints,
			OMWPercent:  omw * 100, GWPercent: gwRaw(t) * 100, OGWPercent: ogw * 100,
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.MatchPoints != b.MatchPoints {
			return a.MatchPoints > b.MatchPoints
		}
		if a.OMWPercent != b.OMWPercent {
			return a.OMWPercent > b.OMWPercent
		}
		if a.GWPercent != b.GWPercent {
			return a.GWPercent > b.GWPercent
		}
		if a.OGWPercent != b.OGWPercent {
			return a.OGWPercent > b.OGWPercent
		}
		return a.DisplayName < b.DisplayName
	})
	const eps = 1e-9
	for i := range rows {
		if i > 0 && rows[i].MatchPoints == rows[i-1].MatchPoints &&
			absDiff(rows[i].OMWPercent, rows[i-1].OMWPercent) < eps &&
			absDiff(rows[i].GWPercent, rows[i-1].GWPercent) < eps &&
			absDiff(rows[i].OGWPercent, rows[i-1].OGWPercent) < eps {
			rows[i].Rank = rows[i-1].Rank
		} else {
			rows[i].Rank = i + 1
		}
	}
	return rows
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
