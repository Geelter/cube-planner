// Package swiss implements pure swiss-pairing and standings math.
// No I/O: plain structs in, plain structs out; Pair is deterministic
// for a given seed.
package swiss

import "github.com/google/uuid"

// Player is a roster entry as the engine sees it.
type Player struct {
	ID          uuid.UUID
	DisplayName string
	Dropped     bool
}

// Result is a reported Bo3 score (games won per player, plus drawn games).
type Result struct {
	P1Games int
	P2Games int
	Draws   int
}

// Match is one pairing from any round. Player2 == nil is a bye;
// Result == nil is an unreported match (ignored by standings).
type Match struct {
	Player1 uuid.UUID
	Player2 *uuid.UUID
	Result  *Result
}

// Pairing is one table of a newly paired round. Player2 == nil is the bye.
type Pairing struct {
	TableNumber int
	Player1     uuid.UUID
	Player2     *uuid.UUID
}
