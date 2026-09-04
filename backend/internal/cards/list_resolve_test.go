package cards

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// cardsEnv is a lightweight integration harness for resolver tests: a
// migrated database plus a Service. seedCard inserts printings directly
// (no Scryfall sync round trip needed here), sharing one oracle_id per
// distinct card name the way the real mirror does.
type cardsEnv struct {
	pool      *pgxpool.Pool
	svc       *Service
	oracleIDs map[string]uuid.UUID
}

func newCardsEnv(t *testing.T) *cardsEnv {
	t.Helper()
	pool := testdb.New(t)
	return &cardsEnv{pool: pool, svc: NewService(db.New(pool)), oracleIDs: map[string]uuid.UUID{}}
}

// seedCard inserts one printing, returning its scryfall_id. normalized_name
// is set with NormalizeName, matching what the real sync pipeline stores.
func (e *cardsEnv) seedCard(t *testing.T, name, setCode, collectorNumber string) uuid.UUID {
	t.Helper()
	oracleID, ok := e.oracleIDs[name]
	if !ok {
		oracleID = uuid.New()
		e.oracleIDs[name] = oracleID
	}
	scryfallID := uuid.New()
	_, err := e.pool.Exec(context.Background(), `
		insert into cards (
			scryfall_id, oracle_id, name, normalized_name, released_at,
			set_code, set_name, collector_number, rarity, layout, mana_cost,
			cmc, type_line, oracle_text, colors, color_identity, promo
		) values ($1, $2, $3, $4, '2020-01-01', $5, $5, $6, 'common', 'normal', '', 0, '', '', $7, $7, false)
	`, scryfallID, oracleID, name, NormalizeName(name), setCode, collectorNumber, []string{})
	if err != nil {
		t.Fatal(err)
	}
	return scryfallID
}

func TestResolveListExactPrinting(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Urza's Mine", "atq", "83a")
	e.seedCard(t, "Urza's Mine", "atq", "83b")

	lines, err := e.svc.ResolveList(context.Background(), "1 Urza's Mine (ATQ) 83a")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched {
		t.Fatalf("want matched, got %s", lines[0].Status)
	}
	if lines[0].Match.CollectorNumber != "83a" {
		t.Fatalf("want the 83a printing, got %s", lines[0].Match.CollectorNumber)
	}
}

func TestResolveListSetWithVariantsIsAmbiguous(t *testing.T) {
	e := newCardsEnv(t)
	for _, cn := range []string{"83a", "83b", "83c", "83d"} {
		e.seedCard(t, "Urza's Mine", "atq", cn)
	}
	lines, err := e.svc.ResolveList(context.Background(), "1 Urza's Mine (ATQ)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusAmbiguous {
		t.Fatalf("want ambiguous, got %s", lines[0].Status)
	}
	if len(lines[0].Suggestions) != 4 {
		t.Fatalf("want all 4 variants offered, got %d", len(lines[0].Suggestions))
	}
}

func TestResolveListSetWithOnePrintingMatches(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Lightning Bolt", "leb", "162")
	e.seedCard(t, "Lightning Bolt", "mm2", "138")
	lines, err := e.svc.ResolveList(context.Background(), "1 Lightning Bolt (MM2)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched || lines[0].Match.SetCode != "mm2" {
		t.Fatalf("want the mm2 printing matched, got %s / %+v", lines[0].Status, lines[0].Match)
	}
}

func TestResolveListUnknownPrintingSuggestsOthers(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Lightning Bolt", "leb", "162")
	e.seedCard(t, "Lightning Bolt", "mm2", "138")
	lines, err := e.svc.ResolveList(context.Background(), "1 Lightning Bolt (XYZ) 999")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusPrintingNotFound {
		t.Fatalf("want printing-not-found, got %s", lines[0].Status)
	}
	if len(lines[0].Suggestions) != 2 {
		t.Fatalf("want both real printings suggested, got %d", len(lines[0].Suggestions))
	}
}

func TestResolveListParenthesisedNameStillResolves(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "B.F.M. (Big Furry Monster)", "ugl", "28")
	lines, err := e.svc.ResolveList(context.Background(), "1 B.F.M. (Big Furry Monster)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched {
		t.Fatalf("a parenthesised card name must still resolve, got %s", lines[0].Status)
	}
}

func TestResolveListPrintingNameMismatchDoesNotAdoptWrongCard(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Lightning Bolt", "leb", "162")
	// A different card occupies the collector number the user actually typed.
	e.seedCard(t, "Brainstorm", "leb", "163")

	lines, err := e.svc.ResolveList(context.Background(), "1 Lightning Bolt (LEB) 163")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusPrintingNotFound {
		t.Fatalf("want printing-not-found (name mismatch on that slot), got %s", lines[0].Status)
	}
	if lines[0].Match != nil {
		t.Fatalf("must not silently resolve to the card actually occupying that slot, got %+v", lines[0].Match)
	}
}

func TestResolveListTypoedNameWithSetFallsBackToFuzzySuggestions(t *testing.T) {
	e := newCardsEnv(t)
	e.seedCard(t, "Lightning Bolt", "leb", "162")

	lines, err := e.svc.ResolveList(context.Background(), "1 Lihgtning Blot (LEB)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusPrintingNotFound {
		t.Fatalf("want printing-not-found, got %s", lines[0].Status)
	}
	if len(lines[0].Suggestions) == 0 {
		t.Fatalf("a typo'd name with a real set code must still get fuzzy suggestions, got none")
	}
}

func TestResolveListSetAwareLineFallsBackToWholeLineAsName(t *testing.T) {
	e := newCardsEnv(t)
	// A card whose name ends in a set-code-shaped parenthesis.
	e.seedCard(t, "Sift Through Sands (FOO)", "xyz", "1")
	lines, err := e.svc.ResolveList(context.Background(), "1 Sift Through Sands (FOO)")
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Status != StatusMatched {
		t.Fatalf("the whole-line retry must rescue this, got %s", lines[0].Status)
	}
}
