// Package collections owns per-user card collections and the
// cube-vs-collection wantlist.
package collections

import (
	"errors"
	"strconv"
	"strings"
)

const (
	// MaxImportLines caps one pasted import.
	MaxImportLines = 500
	// MaxItemQuantity is the hard per-printing maximum everywhere:
	// the PUT bound, and the clamp for import / change-printing adds.
	MaxItemQuantity = 999
)

var ErrTooManyLines = errors.New("import exceeds 500 lines")

// ParsedLine is one non-blank line of a pasted import list.
// Grammar: optional quantity prefix ("4" or "4x"/"4X", 1–999), then a
// card name. A line whose first token is not numeric is a bare name
// with quantity 1. OK=false = unparsable (bad quantity or no name).
type ParsedLine struct {
	LineNumber int32 // 1-based position in the original text; blank lines count
	Raw        string
	Quantity   int32
	Name       string
	OK         bool
}

func ParseImportText(text string) ([]ParsedLine, error) {
	var out []ParsedLine
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if len(out) == MaxImportLines {
			return nil, ErrTooManyLines
		}
		out = append(out, parseLine(int32(i+1), line))
	}
	return out, nil
}

func parseLine(n int32, line string) ParsedLine {
	p := ParsedLine{LineNumber: n, Raw: line}
	first, rest := line, ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		first, rest = line[:i], strings.TrimSpace(line[i+1:])
	}
	qtyToken := first
	if len(qtyToken) > 1 && (strings.HasSuffix(qtyToken, "x") || strings.HasSuffix(qtyToken, "X")) {
		qtyToken = qtyToken[:len(qtyToken)-1]
	}
	qty, err := strconv.Atoi(qtyToken)
	if err != nil {
		// No leading quantity — the whole line is the name.
		p.Quantity, p.Name, p.OK = 1, line, true
		return p
	}
	if qty < 1 || qty > MaxItemQuantity || rest == "" {
		return p
	}
	p.Quantity, p.Name, p.OK = int32(qty), rest, true
	return p
}
