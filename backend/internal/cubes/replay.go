// Package cubes implements cube CRUD and append-only changelog
// versioning: every save applies a validated diff to the materialized
// current list and appends one change; past states replay the log
// backwards from the current state.
package cubes

import (
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

// Entry is one oracle card in a cube state: chosen printing + count.
type Entry struct {
	OracleID   uuid.UUID
	ScryfallID uuid.UUID
	Quantity   int32
}

// Delta is one line of a change diff (always positive quantity; adds and
// removes are separate lists).
type Delta struct {
	OracleID   uuid.UUID
	ScryfallID uuid.UUID
	Quantity   int32
}

// Change is one committed changelog entry, as needed for replay.
type Change struct {
	Version int32
	Adds    []Delta
	Removes []Delta
}

// ErrInvalidChange is wrapped by every diff validation failure; the HTTP
// layer maps it to 422 with the wrapped reason as detail.
var ErrInvalidChange = errors.New("invalid cube change")

// ValidateDiff checks a proposed diff against the current state: the diff
// must be non-empty, an oracle card may appear at most once per side and
// never on both sides, and removes must not exceed what is present.
func ValidateDiff(current []Entry, adds, removes []Delta) error {
	if len(adds) == 0 && len(removes) == 0 {
		return fmt.Errorf("%w: empty diff", ErrInvalidChange)
	}
	have := make(map[uuid.UUID]int32, len(current))
	for _, e := range current {
		have[e.OracleID] = e.Quantity
	}
	added := make(map[uuid.UUID]bool, len(adds))
	for _, a := range adds {
		if added[a.OracleID] {
			return fmt.Errorf("%w: duplicate add for oracle %s", ErrInvalidChange, a.OracleID)
		}
		added[a.OracleID] = true
	}
	removed := make(map[uuid.UUID]bool, len(removes))
	for _, r := range removes {
		if added[r.OracleID] {
			return fmt.Errorf("%w: oracle %s in both adds and removes", ErrInvalidChange, r.OracleID)
		}
		if removed[r.OracleID] {
			return fmt.Errorf("%w: duplicate remove for oracle %s", ErrInvalidChange, r.OracleID)
		}
		removed[r.OracleID] = true
		if have[r.OracleID] < r.Quantity {
			return fmt.Errorf("%w: removing %d of oracle %s but cube has %d",
				ErrInvalidChange, r.Quantity, r.OracleID, have[r.OracleID])
		}
	}
	return nil
}

// ApplyDiff returns the state after applying a (validated) diff. Adding an
// oracle card already present increments its quantity and keeps its
// existing printing.
func ApplyDiff(current []Entry, adds, removes []Delta) []Entry {
	state := stateMap(current)
	for _, a := range adds {
		if e, ok := state[a.OracleID]; ok {
			e.Quantity += a.Quantity
			state[a.OracleID] = e
		} else {
			state[a.OracleID] = Entry(a)
		}
	}
	for _, r := range removes {
		e := state[r.OracleID]
		e.Quantity -= r.Quantity
		if e.Quantity <= 0 {
			delete(state, r.OracleID)
		} else {
			state[r.OracleID] = e
		}
	}
	return sorted(state)
}

// ReplayBackwards reconstructs the state at targetVersion by inverting
// changes from the current state. changes must contain every change with
// version > targetVersion, sorted newest first. Fully removed cards are
// restored with the printing recorded in the change item.
func ReplayBackwards(current []Entry, changes []Change, targetVersion int32) []Entry {
	state := stateMap(current)
	for _, ch := range changes {
		if ch.Version <= targetVersion {
			break
		}
		for _, a := range ch.Adds { // invert add: subtract
			e := state[a.OracleID]
			e.Quantity -= a.Quantity
			if e.Quantity <= 0 {
				delete(state, a.OracleID)
			} else {
				state[a.OracleID] = e
			}
		}
		for _, r := range ch.Removes { // invert remove: restore
			if e, ok := state[r.OracleID]; ok {
				e.Quantity += r.Quantity
				state[r.OracleID] = e
			} else {
				state[r.OracleID] = Entry(r)
			}
		}
	}
	return sorted(state)
}

func stateMap(entries []Entry) map[uuid.UUID]Entry {
	m := make(map[uuid.UUID]Entry, len(entries))
	for _, e := range entries {
		m[e.OracleID] = e
	}
	return m
}

func sorted(state map[uuid.UUID]Entry) []Entry {
	out := make([]Entry, 0, len(state))
	for _, e := range state {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OracleID.String() < out[j].OracleID.String()
	})
	return out
}
