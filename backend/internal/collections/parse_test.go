package collections

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
