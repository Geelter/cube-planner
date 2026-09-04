package cards

import (
	"errors"
	"strings"
	"testing"
)

func TestParseImportText(t *testing.T) {
	tests := []struct {
		name string
		line string
		want ParsedLine
	}{
		{"bare name", "Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "Lightning Bolt", Quantity: 1, Name: "Lightning Bolt", OK: true}},
		{"qty space name", "4 Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "4 Lightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"qty x suffix", "4x Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "4x Lightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"qty X suffix", "4X Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "4X Lightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"tab separator", "4\tLightning Bolt", ParsedLine{LineNumber: 1, Raw: "4\tLightning Bolt", Quantity: 4, Name: "Lightning Bolt", OK: true}},
		{"surrounding whitespace", "  2 Sol Ring  ", ParsedLine{LineNumber: 1, Raw: "2 Sol Ring", Quantity: 2, Name: "Sol Ring", OK: true}},
		{"name starting with digits stays a name", "Borrowing 100,000 Arrows", ParsedLine{LineNumber: 1, Raw: "Borrowing 100,000 Arrows", Quantity: 1, Name: "Borrowing 100,000 Arrows", OK: true}},
		{"quantity zero unparsable", "0 Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "0 Lightning Bolt", OK: false}},
		{"quantity 1000 unparsable", "1000 Lightning Bolt", ParsedLine{LineNumber: 1, Raw: "1000 Lightning Bolt", OK: false}},
		{"quantity without name unparsable", "4x", ParsedLine{LineNumber: 1, Raw: "4x", OK: false}},
		{"lone x is a name", "x Bolt", ParsedLine{LineNumber: 1, Raw: "x Bolt", Quantity: 1, Name: "x Bolt", OK: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseImportText(tt.line)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("lines = %d, want 1", len(got))
			}
			if got[0] != tt.want {
				t.Fatalf("got %+v, want %+v", got[0], tt.want)
			}
		})
	}
}

func TestParseImportTextSkipsBlankLinesButCountsThem(t *testing.T) {
	got, err := ParseImportText("Lightning Bolt\n\n   \n2 Sol Ring\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("lines = %d, want 2", len(got))
	}
	if got[0].LineNumber != 1 || got[1].LineNumber != 4 {
		t.Fatalf("line numbers = %d, %d; want 1, 4", got[0].LineNumber, got[1].LineNumber)
	}
	if got[1].Name != "Sol Ring" {
		t.Fatalf("CRLF line parsed as %q", got[1].Name)
	}
}

func TestParseSetAndCollectorNumber(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantQty int32
		wantNm  string
		wantSet string
		wantCN  string
		wantOK  bool
	}{
		{"bare name", "Lightning Bolt", 1, "Lightning Bolt", "", "", true},
		{"qty and name", "4 Lightning Bolt", 4, "Lightning Bolt", "", "", true},
		{"qty x and name", "4x Lightning Bolt", 4, "Lightning Bolt", "", "", true},
		{"name and set", "Lightning Bolt (LEB)", 1, "Lightning Bolt", "leb", "", true},
		{"qty name set", "4 Lightning Bolt (leb)", 4, "Lightning Bolt", "leb", "", true},
		{"full", "1 Urza's Mine (ATQ) 83a", 1, "Urza's Mine", "atq", "83a", true},
		{"numeric set", "1 Lightning Bolt (2X2) 117", 1, "Lightning Bolt", "2x2", "117", true},
		{"star collector number", "1 Arcane Signet (SLD) ★12", 1, "Arcane Signet", "sld", "★12", true},
		// A parenthesized token that is not set-shaped stays part of the name.
		{"paren name unglued", "B.F.M. (Big Furry Monster)", 1, "B.F.M. (Big Furry Monster)", "", "", true},
		{"paren name unhinged", "Erase (Not the Urza's Legacy One)", 1, "Erase (Not the Urza's Legacy One)", "", "", true},
		// A bare trailing number is part of the name, not a collector number.
		{"no set means no collector number", "Fire // Ice 128", 1, "Fire // Ice 128", "", "", true},
		{"quantity over cap", "1000 Lightning Bolt", 0, "", "", "", false},
		// Regression: a name that begins with a big number is not a quantity.
		{"numeric name", "1996 World Champion", 1, "1996 World Champion", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLine(1, tt.line)
			if got.OK != tt.wantOK {
				t.Fatalf("OK: want %v, got %v", tt.wantOK, got.OK)
			}
			if !tt.wantOK {
				return
			}
			if got.Quantity != tt.wantQty || got.Name != tt.wantNm ||
				got.SetCode != tt.wantSet || got.CollectorNumber != tt.wantCN {
				t.Fatalf("want qty=%d name=%q set=%q cn=%q, got qty=%d name=%q set=%q cn=%q",
					tt.wantQty, tt.wantNm, tt.wantSet, tt.wantCN,
					got.Quantity, got.Name, got.SetCode, got.CollectorNumber)
			}
		})
	}
}

func TestParseImportTextLineCap(t *testing.T) {
	text := strings.Repeat("Lightning Bolt\n", MaxImportLines)
	if _, err := ParseImportText(text); err != nil {
		t.Fatalf("exactly %d lines must be fine: %v", MaxImportLines, err)
	}
	text += "One More\n"
	if _, err := ParseImportText(text); !errors.Is(err, ErrTooManyLines) {
		t.Fatalf("err = %v, want ErrTooManyLines", err)
	}
}
