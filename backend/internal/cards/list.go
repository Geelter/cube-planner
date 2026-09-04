package cards

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
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
// card name, then optionally a parenthesized set code and (only if a set
// was given) a collector number — the Moxfield/Archidekt convention:
// "4 Lightning Bolt (LEB) 123". A line whose first token is not numeric
// is a bare name with quantity 1. OK=false = unparsable (bad quantity or
// no name).
type ParsedLine struct {
	LineNumber      int32 // 1-based position in the original text; blank lines count
	Raw             string
	Quantity        int32
	Name            string
	SetCode         string // lowercased, "" if not specified
	CollectorNumber string // lowercased, "" if not specified
	OK              bool
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

var (
	setCodeRe   = regexp.MustCompile(`^\(([A-Za-z0-9]{2,5})\)$`)
	collectorRe = regexp.MustCompile(`^[A-Za-z0-9★†-]+$`)
)

// splitSelectors peels an optional trailing "(SET)" and, only when a set
// was found, an optional trailing collector number. Card names really do
// contain parentheses ("B.F.M. (Big Furry Monster)"), so the token must
// look like a set code — 2–5 alphanumerics — to be treated as one.
func splitSelectors(name string) (rest, setCode, collectorNumber string) {
	fields := strings.Fields(name)
	if len(fields) < 2 {
		return name, "", ""
	}
	last := len(fields) - 1
	// Case: "... (SET) 83a"
	if last >= 1 && collectorRe.MatchString(fields[last]) {
		if mt := setCodeRe.FindStringSubmatch(fields[last-1]); mt != nil {
			return strings.Join(fields[:last-1], " "), strings.ToLower(mt[1]), strings.ToLower(fields[last])
		}
	}
	// Case: "... (SET)"
	if mt := setCodeRe.FindStringSubmatch(fields[last]); mt != nil {
		return strings.Join(fields[:last], " "), strings.ToLower(mt[1]), ""
	}
	return name, "", ""
}

// NameWithoutQuantity strips a leading quantity token ("4", "4x") if one is
// present. Exported because list resolution retries a line as a bare name
// when the set-aware reading finds nothing, and it must strip the quantity
// the same way parseLine does.
func NameWithoutQuantity(line string) string {
	first, rest := line, ""
	if i := strings.IndexAny(line, " \t"); i >= 0 {
		first, rest = line[:i], strings.TrimSpace(line[i+1:])
	}
	tok := strings.TrimSuffix(strings.TrimSuffix(first, "x"), "X")
	qty, err := strconv.Atoi(tok)
	if err != nil || rest == "" || qty < 1 || qty > MaxItemQuantity {
		return line
	}
	return rest
}

// looksLikeCardYear reports whether qty is shaped like a year a card name
// might embed (e.g. the actual card "1996 World Champion") rather than a
// mistyped quantity. Magic's first set (Alpha) released in 1993.
func looksLikeCardYear(qty int) bool {
	return qty >= 1993 && qty <= 2099
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
	var nameText string
	qty, err := strconv.Atoi(qtyToken)
	switch {
	case err != nil:
		// No leading quantity — the whole line is the name.
		qty = 1
		nameText = line
	case qty > MaxItemQuantity && rest != "" && looksLikeCardYear(qty):
		// "1996 World Champion" is a card, not 1996 copies of something —
		// but an arbitrary over-cap number like "1000 Lightning Bolt" is
		// almost certainly a mistyped quantity, so only numbers shaped
		// like a year (Magic's first set released in 1993) get the pass.
		qty = 1
		nameText = line
	case qty < 1 || qty > MaxItemQuantity || rest == "":
		return p
	default:
		nameText = rest
	}

	rest, setCode, collectorNumber := splitSelectors(nameText)
	if rest == "" {
		// The whole line was a selector; treat it as unparsable rather than
		// producing an empty name.
		return p
	}
	p.Quantity, p.Name, p.SetCode, p.CollectorNumber, p.OK = int32(qty), rest, setCode, collectorNumber, true
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
	Colors          []string
	ImageSmall      *string
	ImageNormal     *string
}

// ResolvedLine statuses.
const (
	StatusMatched   = "matched"
	StatusAmbiguous = "ambiguous"
	StatusUnmatched = "unmatched"
	// StatusPrintingNotFound: the name is real but the requested printing
	// is not. Distinct from unmatched because the fix is choosing another
	// printing, not correcting a typo.
	StatusPrintingNotFound = "printing-not-found"
)

type ResolvedLine struct {
	LineNumber      int32
	Raw             string
	Quantity        int32
	Status          string
	SetCode         string // echo of what was parsed, "" if absent
	CollectorNumber string
	Match           *CardRef
	Suggestions     []CardRef
}

func cardRefFromSetAndCollectorRow(r db.GetCardsBySetAndCollectorNumbersRow) CardRef {
	return CardRef{
		ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
		ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
		SetName: r.SetName, CollectorNumber: r.CollectorNumber,
		Colors:     r.Colors,
		ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
	}
}

func cardRefFromNameAndSetRow(r db.GetCardsByNameAndSetRow) CardRef {
	return CardRef{
		ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
		ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
		SetName: r.SetName, CollectorNumber: r.CollectorNumber,
		Colors:     r.Colors,
		ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
	}
}

func cardRefFromCard(c db.Card) CardRef {
	return CardRef{
		ScryfallID: c.ScryfallID, OracleID: c.OracleID, Name: c.Name,
		ManaCost: c.ManaCost, TypeLine: c.TypeLine, SetCode: c.SetCode,
		SetName: c.SetName, CollectorNumber: c.CollectorNumber,
		Colors:     c.Colors,
		ImageSmall: c.ImageSmall, ImageNormal: c.ImageNormal,
	}
}

// ResolveList parses pasted text and resolves each line. Pure read:
// nothing is written.
//
// Resolution ladder, first match wins:
//   - set + collector number given: the exact printing, or
//     printing-not-found (suggesting every printing of that name).
//   - set given alone: printings of that name within that set — one is
//     matched, several (e.g. the four Antiquities Urza's Mine variants)
//     are ambiguous; none is printing-not-found.
//   - neither given: exact (case-insensitive, normalized) name match
//     resolves to the oracle card's representative printing; a name
//     shared by several oracle cards is ambiguous; misses get fuzzy
//     suggestions or unmatched.
//
// A card name can itself look like it carries a selector ("Sift Through
// Sands (FOO)"); when the set-aware reading finds nothing, the line is
// retried as a bare name before giving up (see the loop below).
//
// Resolution stays batched: three constant-count queries regardless of
// line count, plus the existing per-line fuzzy fallback for lines that
// reach it.
func (s *Service) ResolveList(ctx context.Context, text string) ([]ResolvedLine, error) {
	lines, err := ParseImportText(text)
	if err != nil {
		// Preserves the pre-move wording ("invalid import: ...") — the
		// RFC 7807 detail string is user-facing copy, not an internal
		// detail, so it must not drift just because the code moved.
		return nil, fmt.Errorf("invalid import: %w", err)
	}

	nameSet := make(map[string]struct{})
	var names []string
	addName := func(n string) {
		if n == "" {
			return
		}
		if _, seen := nameSet[n]; !seen {
			nameSet[n] = struct{}{}
			names = append(names, n)
		}
	}

	printingPairSeen := make(map[string]struct{})
	var printingSetCodes, printingCollectorNumbers []string
	setPairSeen := make(map[string]struct{})
	var setNames, setNameSetCodes []string

	for _, l := range lines {
		if !l.OK {
			continue
		}
		addName(NormalizeName(l.Name))
		if l.SetCode != "" {
			// For the whole-line retry below to hit without issuing its own
			// query, the raw line (sans quantity) must already be in the
			// batched name set.
			addName(NormalizeName(NameWithoutQuantity(l.Raw)))
		}
		switch {
		case l.SetCode != "" && l.CollectorNumber != "":
			key := l.SetCode + "|" + l.CollectorNumber
			if _, seen := printingPairSeen[key]; !seen {
				printingPairSeen[key] = struct{}{}
				printingSetCodes = append(printingSetCodes, l.SetCode)
				printingCollectorNumbers = append(printingCollectorNumbers, l.CollectorNumber)
			}
		case l.SetCode != "":
			key := NormalizeName(l.Name) + "|" + l.SetCode
			if _, seen := setPairSeen[key]; !seen {
				setPairSeen[key] = struct{}{}
				setNames = append(setNames, NormalizeName(l.Name))
				setNameSetCodes = append(setNameSetCodes, l.SetCode)
			}
		}
	}

	// Three constant-count batches, not one query per line.
	exact := make(map[string][]CardRef) // normalized name
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
				Colors:     r.Colors,
				ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			})
		}
	}

	exactByPrinting := make(map[string]CardRef) // "set|collectorNumber"
	if len(printingSetCodes) > 0 {
		rows, err := s.queries.GetCardsBySetAndCollectorNumbers(ctx, db.GetCardsBySetAndCollectorNumbersParams{
			SetCodes:         printingSetCodes,
			CollectorNumbers: printingCollectorNumbers,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			key := strings.ToLower(r.SetCode) + "|" + strings.ToLower(r.CollectorNumber)
			exactByPrinting[key] = cardRefFromSetAndCollectorRow(r)
		}
	}

	bySetAndName := make(map[string][]CardRef) // "normalizedName|set"
	if len(setNames) > 0 {
		rows, err := s.queries.GetCardsByNameAndSet(ctx, db.GetCardsByNameAndSetParams{
			Names:    setNames,
			SetCodes: setNameSetCodes,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			key := r.NormalizedName + "|" + strings.ToLower(r.SetCode)
			bySetAndName[key] = append(bySetAndName[key], cardRefFromNameAndSetRow(r))
		}
	}

	out := make([]ResolvedLine, len(lines))
	for i, l := range lines {
		rl := ResolvedLine{
			LineNumber: l.LineNumber, Raw: l.Raw, Quantity: l.Quantity,
			SetCode: l.SetCode, CollectorNumber: l.CollectorNumber,
		}
		switch {
		case !l.OK:
			rl.Status = StatusUnmatched
		case l.SetCode != "" && l.CollectorNumber != "":
			if ref, ok := exactByPrinting[l.SetCode+"|"+l.CollectorNumber]; ok {
				rl.Status, rl.Match = StatusMatched, &ref
				break
			}
			rl.Status = StatusPrintingNotFound
			rl.Suggestions, err = s.printingsForName(ctx, exact, l.Name)
			if err != nil {
				return nil, err
			}
		case l.SetCode != "":
			refs := bySetAndName[NormalizeName(l.Name)+"|"+l.SetCode]
			switch len(refs) {
			case 1:
				rl.Status, rl.Match = StatusMatched, &refs[0]
			case 0:
				rl.Status = StatusPrintingNotFound
				rl.Suggestions, err = s.printingsForName(ctx, exact, l.Name)
				if err != nil {
					return nil, err
				}
			default:
				// The Antiquities-Urza's-Mine case: several printings share
				// the set — the user must choose.
				rl.Status, rl.Suggestions = StatusAmbiguous, refs
			}
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

		// Belt-and-braces guard: a card name may itself end in something
		// set-code-shaped ("Sift Through Sands (FOO)"). If the set-aware
		// reading found nothing, retry the whole line as a name before
		// giving up, so no pathological card name becomes unimportable.
		if (rl.Status == StatusPrintingNotFound || rl.Status == StatusUnmatched) && l.SetCode != "" {
			if retry := exact[NormalizeName(NameWithoutQuantity(l.Raw))]; len(retry) == 1 {
				rl.Status, rl.Match = StatusMatched, &retry[0]
				rl.SetCode, rl.CollectorNumber = "", ""
				rl.Suggestions = nil
			}
		}

		out[i] = rl
	}
	return out, nil
}

// printingsForName returns every printing of name, for use as
// printing-not-found suggestions. The oracle id comes from the already
// batched exact-name lookup, so only misses pay for the extra query —
// the same trade-off suggest() makes for the name-only branch.
func (s *Service) printingsForName(ctx context.Context, exact map[string][]CardRef, name string) ([]CardRef, error) {
	matches := exact[NormalizeName(name)]
	if len(matches) == 0 {
		return nil, nil
	}
	rows, err := s.queries.GetPrintingsByOracleID(ctx, matches[0].OracleID)
	if err != nil {
		return nil, err
	}
	refs := make([]CardRef, len(rows))
	for i, r := range rows {
		refs[i] = cardRefFromCard(r)
	}
	return refs, nil
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
			Colors:     r.Colors,
			ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
		}
	}
	return refs, nil
}
