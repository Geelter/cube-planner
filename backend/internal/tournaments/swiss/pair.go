package swiss

import (
	"math/rand"
	"sort"

	"github.com/google/uuid"
)

// Pair generates the next round: seeded shuffle within match-point
// groups, pair-down for odd groups, backtracking to avoid rematches
// (falling back to the minimum number of rematches when unavoidable),
// and — for an odd player count — a bye from the lowest score group
// among players without a prior bye. Deterministic per seed.
func Pair(players []Player, history []Match, seed int64) []Pairing {
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // reproducible pairing, not crypto

	points := map[uuid.UUID]int{}
	hadBye := map[uuid.UUID]bool{}
	played := map[uuid.UUID]map[uuid.UUID]bool{}
	for _, m := range history {
		if m.Player2 == nil {
			hadBye[m.Player1] = true
		} else {
			if played[m.Player1] == nil {
				played[m.Player1] = map[uuid.UUID]bool{}
			}
			if played[*m.Player2] == nil {
				played[*m.Player2] = map[uuid.UUID]bool{}
			}
			played[m.Player1][*m.Player2] = true
			played[*m.Player2][m.Player1] = true
		}
		if m.Result == nil {
			continue
		}
		switch {
		case m.Player2 == nil, m.Result.P1Games > m.Result.P2Games:
			points[m.Player1] += 3
		case m.Result.P2Games > m.Result.P1Games:
			points[*m.Player2] += 3
		default:
			points[m.Player1]++
			points[*m.Player2]++
		}
	}

	var active []Player
	for _, p := range players {
		if !p.Dropped {
			active = append(active, p)
		}
	}

	var byeID *uuid.UUID
	if len(active)%2 == 1 {
		id := pickBye(active, points, hadBye, rng)
		byeID = &id
	}

	// Order: points desc, seeded-random within a group. Sorting by a
	// per-player random key implements the within-group shuffle.
	rnd := make(map[uuid.UUID]float64, len(active))
	var order []uuid.UUID
	for _, p := range active {
		if byeID != nil && p.ID == *byeID {
			continue
		}
		rnd[p.ID] = rng.Float64()
		order = append(order, p.ID)
	}
	sort.Slice(order, func(i, j int) bool {
		if points[order[i]] != points[order[j]] {
			return points[order[i]] > points[order[j]]
		}
		return rnd[order[i]] < rnd[order[j]]
	})

	// Backtracking with a growing rematch budget: budget 0 finds a
	// rematch-free pairing when one exists; otherwise the smallest
	// budget that works = the minimum number of rematches.
	var pairs [][2]uuid.UUID
	for budget := 0; ; budget++ {
		if pairs = tryPair(order, played, budget); pairs != nil {
			break
		}
	}

	out := make([]Pairing, 0, len(pairs)+1)
	for i, pr := range pairs {
		p2 := pr[1]
		out = append(out, Pairing{TableNumber: i + 1, Player1: pr[0], Player2: &p2})
	}
	if byeID != nil {
		out = append(out, Pairing{TableNumber: len(pairs) + 1, Player1: *byeID})
	}
	return out
}

// pickBye draws from the lowest match-point group among players without
// a prior bye, widening upward group by group; if every active player
// has had a bye the constraint is waived (lowest group).
func pickBye(active []Player, points map[uuid.UUID]int, hadBye map[uuid.UUID]bool, rng *rand.Rand) uuid.UUID {
	groups := map[int][]uuid.UUID{}
	var keys []int
	for _, p := range active {
		pts := points[p.ID]
		if len(groups[pts]) == 0 {
			keys = append(keys, pts)
		}
		groups[pts] = append(groups[pts], p.ID)
	}
	sort.Ints(keys)
	for _, k := range keys {
		var fresh []uuid.UUID
		for _, id := range groups[k] {
			if !hadBye[id] {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) > 0 {
			return fresh[rng.Intn(len(fresh))]
		}
	}
	lowest := groups[keys[0]]
	return lowest[rng.Intn(len(lowest))]
}

// tryPair pairs order[0] with the nearest available opponent (same
// score group first, then pairing down), recursing over the rest;
// each rematch consumes budget. Returns nil when no pairing fits.
func tryPair(order []uuid.UUID, played map[uuid.UUID]map[uuid.UUID]bool, budget int) [][2]uuid.UUID {
	if len(order) == 0 {
		return [][2]uuid.UUID{}
	}
	p1 := order[0]
	for i := 1; i < len(order); i++ {
		p2 := order[i]
		cost := 0
		if played[p1][p2] {
			cost = 1
		}
		if cost > budget {
			continue
		}
		rest := make([]uuid.UUID, 0, len(order)-2)
		rest = append(rest, order[1:i]...)
		rest = append(rest, order[i+1:]...)
		if sub := tryPair(rest, played, budget-cost); sub != nil {
			return append([][2]uuid.UUID{{p1, p2}}, sub...)
		}
	}
	return nil
}
