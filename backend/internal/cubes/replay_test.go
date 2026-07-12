package cubes

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

var (
	oA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	oB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	oC = uuid.MustParse("00000000-0000-0000-0000-00000000000c")
	sA = uuid.MustParse("10000000-0000-0000-0000-00000000000a")
	sB = uuid.MustParse("10000000-0000-0000-0000-00000000000b")
	sC = uuid.MustParse("10000000-0000-0000-0000-00000000000c")
)

func TestValidateDiff(t *testing.T) {
	current := []Entry{{OracleID: oA, ScryfallID: sA, Quantity: 2}}
	tests := []struct {
		name    string
		adds    []Delta
		removes []Delta
		wantErr bool
	}{
		{"empty diff", nil, nil, true},
		{"plain add", []Delta{{OracleID: oB, ScryfallID: sB, Quantity: 1}}, nil, false},
		{"add existing increments", []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 1}}, nil, false},
		{"remove within quantity", nil, []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 2}}, false},
		{"remove exceeds quantity", nil, []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 3}}, true},
		{"remove absent card", nil, []Delta{{OracleID: oB, ScryfallID: sB, Quantity: 1}}, true},
		{"same oracle added and removed", []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 1}}, []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 1}}, true},
		{"duplicate add lines", []Delta{{OracleID: oB, ScryfallID: sB, Quantity: 1}, {OracleID: oB, ScryfallID: sB, Quantity: 1}}, nil, true},
		{"duplicate remove lines", nil, []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 1}, {OracleID: oA, ScryfallID: sA, Quantity: 1}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDiff(current, tt.adds, tt.removes)
			if tt.wantErr && !errors.Is(err, ErrInvalidChange) {
				t.Fatalf("err = %v, want ErrInvalidChange", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestApplyDiff(t *testing.T) {
	current := []Entry{{OracleID: oA, ScryfallID: sA, Quantity: 2}}
	next := ApplyDiff(current,
		[]Delta{{OracleID: oB, ScryfallID: sB, Quantity: 1}, {OracleID: oA, ScryfallID: sA, Quantity: 1}},
		nil)
	if len(next) != 2 {
		t.Fatalf("entries = %d, want 2", len(next))
	}
	byOracle := map[uuid.UUID]Entry{}
	for _, e := range next {
		byOracle[e.OracleID] = e
	}
	if byOracle[oA].Quantity != 3 || byOracle[oB].Quantity != 1 {
		t.Fatalf("quantities = %+v", byOracle)
	}

	gone := ApplyDiff(next, nil, []Delta{{OracleID: oB, ScryfallID: sB, Quantity: 1}})
	if len(gone) != 1 || gone[0].OracleID != oA {
		t.Fatalf("after remove-to-zero: %+v", gone)
	}
}

func TestReplayBackwards(t *testing.T) {
	// History: v0 empty → v1 adds A(2), B(1) → v2 removes A(1), adds C(1)
	// → v3 removes B(1) entirely.
	v1 := Change{Version: 1, Adds: []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 2}, {OracleID: oB, ScryfallID: sB, Quantity: 1}}}
	v2 := Change{Version: 2, Adds: []Delta{{OracleID: oC, ScryfallID: sC, Quantity: 1}}, Removes: []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 1}}}
	v3 := Change{Version: 3, Removes: []Delta{{OracleID: oB, ScryfallID: sB, Quantity: 1}}}

	// Current state = v3: A(1), C(1).
	current := []Entry{
		{OracleID: oA, ScryfallID: sA, Quantity: 1},
		{OracleID: oC, ScryfallID: sC, Quantity: 1},
	}

	tests := []struct {
		name    string
		target  int32
		changes []Change // newest first
		want    map[uuid.UUID]int32
	}{
		{"to v3 (no-op)", 3, nil, map[uuid.UUID]int32{oA: 1, oC: 1}},
		{"to v2", 2, []Change{v3}, map[uuid.UUID]int32{oA: 1, oB: 1, oC: 1}},
		{"to v1", 1, []Change{v3, v2}, map[uuid.UUID]int32{oA: 2, oB: 1}},
		{"to v0 (empty)", 0, []Change{v3, v2, v1}, map[uuid.UUID]int32{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReplayBackwards(current, tt.changes, tt.target)
			if len(got) != len(tt.want) {
				t.Fatalf("entries = %+v, want %v", got, tt.want)
			}
			for _, e := range got {
				if tt.want[e.OracleID] != e.Quantity {
					t.Fatalf("oracle %s qty = %d, want %d", e.OracleID, e.Quantity, tt.want[e.OracleID])
				}
			}
		})
	}
}

func TestReplayRestoresRemovedPrinting(t *testing.T) {
	// A card fully removed at v2 must reappear (with the printing recorded
	// in the change item) when replaying back to v1.
	current := []Entry{}
	v2 := Change{Version: 2, Removes: []Delta{{OracleID: oA, ScryfallID: sA, Quantity: 2}}}
	got := ReplayBackwards(current, []Change{v2}, 1)
	if len(got) != 1 || got[0].ScryfallID != sA || got[0].Quantity != 2 {
		t.Fatalf("restored = %+v, want A(2) printing sA", got)
	}
}
