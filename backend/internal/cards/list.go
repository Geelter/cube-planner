package cards

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
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

// CardRef is a resolved card reference for import review (a match or a
// suggestion) — display fields, no quantity.
type CardRef struct {
	ScryfallID      uuid.UUID
	OracleID        uuid.UUID
	Name            string
	ManaCost        string
	TypeLine        string
	SetCode         string
	SetName         string
	CollectorNumber string
	ImageSmall      *string
	ImageNormal     *string
}

// ResolvedLine statuses.
const (
	StatusMatched   = "matched"
	StatusAmbiguous = "ambiguous"
	StatusUnmatched = "unmatched"
)

type ResolvedLine struct {
	LineNumber  int32
	Raw         string
	Quantity    int32
	Status      string
	Match       *CardRef
	Suggestions []CardRef
}

// ResolveList parses pasted text and resolves each line. Pure read:
// nothing is written. Exact (case-insensitive, normalized) name matches
// resolve to the oracle card's representative printing; a name shared by
// several oracle cards falls through to ambiguous; misses get fuzzy
// suggestions or unmatched.
func (s *Service) ResolveList(ctx context.Context, text string) ([]ResolvedLine, error) {
	lines, err := ParseImportText(text)
	if err != nil {
		return nil, err
	}

	nameSet := make(map[string]struct{})
	var names []string
	for _, l := range lines {
		if !l.OK {
			continue
		}
		n := NormalizeName(l.Name)
		if _, seen := nameSet[n]; !seen {
			nameSet[n] = struct{}{}
			names = append(names, n)
		}
	}
	exact := make(map[string][]CardRef)
	if len(names) > 0 {
		rows, err := s.queries.GetCardsByNormalizedNames(ctx, names)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			exact[r.NormalizedName] = append(exact[r.NormalizedName], CardRef{
				ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
				ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
				SetName: r.SetName, CollectorNumber: r.CollectorNumber,
				ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			})
		}
	}

	out := make([]ResolvedLine, len(lines))
	for i, l := range lines {
		rl := ResolvedLine{LineNumber: l.LineNumber, Raw: l.Raw, Quantity: l.Quantity}
		switch {
		case !l.OK:
			rl.Status = StatusUnmatched
		default:
			matches := exact[NormalizeName(l.Name)]
			switch len(matches) {
			case 1:
				rl.Status = StatusMatched
				m := matches[0]
				rl.Match = &m
			case 0:
				// Only misses pay for a fuzzy query.
				suggestions, err := s.suggest(ctx, l.Name)
				if err != nil {
					return nil, err
				}
				if len(suggestions) > 0 {
					rl.Status = StatusAmbiguous
					rl.Suggestions = suggestions
				} else {
					rl.Status = StatusUnmatched
				}
			default:
				// One name, several oracle cards — the user must choose.
				rl.Status = StatusAmbiguous
				rl.Suggestions = matches
			}
		}
		out[i] = rl
	}
	return out, nil
}

func (s *Service) suggest(ctx context.Context, name string) ([]CardRef, error) {
	rows, err := s.queries.SuggestCardsByName(ctx, NormalizeName(name))
	if err != nil {
		return nil, err
	}
	refs := make([]CardRef, len(rows))
	for i, r := range rows {
		refs[i] = CardRef{
			ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
			ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
			SetName: r.SetName, CollectorNumber: r.CollectorNumber,
			ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
		}
	}
	return refs, nil
}
