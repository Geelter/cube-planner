package cards

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

const (
	idBoltOld    = "44444444-4444-4444-4444-444444444401"
	idBoltNew    = "44444444-4444-4444-4444-444444444402"
	idBoltPromo  = "44444444-4444-4444-4444-444444444403"
	idStrike     = "44444444-4444-4444-4444-444444444404"
	idSeance     = "44444444-4444-4444-4444-444444444405"
	idSolRing    = "44444444-4444-4444-4444-444444444406"
	idIzzet      = "44444444-4444-4444-4444-444444444407"
	oracleBolt   = "bbbbbbbb-1111-1111-1111-111111111111"
	oracleStrike = "bbbbbbbb-2222-2222-2222-222222222222"
	oracleSeance = "bbbbbbbb-3333-3333-3333-333333333333"
	oracleSol    = "bbbbbbbb-4444-4444-4444-444444444444"
	oracleIzzet  = "bbbbbbbb-5555-5555-5555-555555555555"
)

func seededService(t *testing.T) *Service {
	t.Helper()
	pool := testdb.New(t)
	f := newFakeScryfall(t)

	boltOld := fixtureCard(idBoltOld, oracleBolt, "Lightning Bolt")
	boltOld.ReleasedAt = "1993-08-05"
	boltNew := fixtureCard(idBoltNew, oracleBolt, "Lightning Bolt")
	boltNew.ReleasedAt = "2010-07-16"
	boltPromo := fixtureCard(idBoltPromo, oracleBolt, "Lightning Bolt")
	boltPromo.ReleasedAt = "2024-01-01"
	boltPromo.Promo = true
	strike := fixtureCard(idStrike, oracleStrike, "Lightning Strike")
	seance := fixtureCard(idSeance, oracleSeance, "Séance")
	seance.TypeLine = "Enchantment"
	seance.Rarity = "rare"
	seance.CMC = 3
	seance.ManaCost = "{2}{W}"
	seance.Colors = []string{"W"}
	seance.ColorIdentity = []string{"W"}
	solRing := fixtureCard(idSolRing, oracleSol, "Sol Ring")
	solRing.TypeLine = "Artifact"
	solRing.CMC = 1
	solRing.ManaCost = "{1}"
	solRing.Rarity = "uncommon"
	solRing.Colors = []string{}
	solRing.ColorIdentity = []string{}
	solRing.Set = "cmd"
	solRing.SetName = "Commander"
	izzet := fixtureCard(idIzzet, oracleIzzet, "Izzet Charm")
	izzet.CMC = 2
	izzet.ManaCost = "{U}{R}"
	izzet.Colors = []string{"U", "R"}
	izzet.ColorIdentity = []string{"U", "R"}

	f.cards = []scryfallCard{boltOld, boltNew, boltPromo, strike, seance, solRing, izzet}
	syncer := NewSyncer(pool, NewScryfallClient(f.srv.URL, "cube-planner/test"), slog.Default())
	if err := syncer.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewService(db.New(pool))
}

func TestAutocomplete(t *testing.T) {
	svc := seededService(t)
	ctx := context.Background()

	rows, err := svc.Autocomplete(ctx, "lightning bo")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Name != "Lightning Bolt" {
		t.Fatalf("first result = %+v, want Lightning Bolt", rows)
	}
	// Representative printing: non-promo preferred over the newer promo,
	// then latest release → the 2010 printing.
	if rows[0].ScryfallID != uuid.MustParse(idBoltNew) {
		t.Fatalf("representative printing = %s, want %s", rows[0].ScryfallID, idBoltNew)
	}
	// One entry per oracle card despite three Bolt printings.
	boltCount := 0
	for _, r := range rows {
		if r.OracleID == uuid.MustParse(oracleBolt) {
			boltCount++
		}
	}
	if boltCount != 1 {
		t.Fatalf("bolt appears %d times, want 1", boltCount)
	}

	// Typo tolerance.
	rows, err = svc.Autocomplete(ctx, "lighntin bolt")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Name == "Lightning Bolt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("typo query missed Lightning Bolt: %+v", rows)
	}

	// Spec's own typo example: word_similarity('lighntin bol', 'lightning
	// bolt') = 0.368, below the default 0.6 GUC and even the old 0.4
	// function-call threshold. Only matches with the 0.35 database-level
	// GUC set by migration 00003 (proves the migration is effective here).
	rows, err = svc.Autocomplete(ctx, "lighntin bol")
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, r := range rows {
		if r.Name == "Lightning Bolt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("spec typo example missed Lightning Bolt: %+v", rows)
	}

	// Tiebreak regression guard: every name containing "bolt" scores
	// word_similarity 1.0, so without a similarity() tiebreak the
	// alphabetical fallback can bury the exact name outside the top 15.
	rows, err = svc.Autocomplete(ctx, "bolt")
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, r := range rows {
		if r.Name == "Lightning Bolt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("bolt query missed Lightning Bolt: %+v", rows)
	}

	// Diacritics fold both ways.
	rows, err = svc.Autocomplete(ctx, "seance")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Name != "Séance" {
		t.Fatalf("seance → %+v, want Séance", rows)
	}

	// LIKE metacharacters in input match literally, not as wildcards.
	rows, err = svc.Autocomplete(ctx, "100% bolt_")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Name == "Sol Ring" {
			t.Fatal("wildcard leak: % matched everything")
		}
	}
}

func TestSearchFilters(t *testing.T) {
	svc := seededService(t)
	ctx := context.Background()
	base := SearchParams{Limit: 20, Offset: 0}

	// colors=R → red cards + colorless, not multicolor izzet.
	p := base
	p.Colors = []string{"R"}
	rows, total, err := svc.Search(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range rows {
		names[r.Name] = true
	}
	if !names["Lightning Bolt"] || !names["Lightning Strike"] || !names["Sol Ring"] {
		t.Fatalf("colors=R missing expected cards: %v", names)
	}
	if names["Izzet Charm"] || names["Séance"] {
		t.Fatalf("colors=R leaked wider identities: %v", names)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}

	// colors=[] (colorless only).
	p = base
	p.Colors = []string{}
	rows, _, err = svc.Search(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Sol Ring" {
		t.Fatalf("colorless-only = %+v, want just Sol Ring", rows)
	}

	// type substring, case-insensitive.
	p = base
	p.Type = "enchant"
	rows, _, err = svc.Search(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Séance" {
		t.Fatalf("type=enchant = %+v", rows)
	}

	// cmc range.
	two := 2.0
	p = base
	p.CMCMin = &two
	rows, _, err = svc.Search(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Cmc < 2 {
			t.Fatalf("cmc filter leaked %s (cmc %v)", r.Name, r.Cmc)
		}
	}

	// rarity + set.
	p = base
	p.Rarity = "uncommon"
	p.Set = "CMD" // case-insensitive via lowering in the service
	rows, _, err = svc.Search(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Sol Ring" {
		t.Fatalf("rarity+set = %+v", rows)
	}

	// pagination: 5 oracle cards total, page size 2.
	p = base
	p.Limit = 2
	rows, total, err = svc.Search(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || total != 5 {
		t.Fatalf("page = %d rows / total %d, want 2 / 5", len(rows), total)
	}

	// name relevance ordering.
	p = base
	p.Name = "lightning bolt"
	rows, _, err = svc.Search(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].Name != "Lightning Bolt" {
		t.Fatalf("name search first = %+v", rows)
	}
}

func TestPrintings(t *testing.T) {
	svc := seededService(t)
	ctx := context.Background()

	rows, err := svc.Printings(ctx, uuid.MustParse(oracleBolt))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("printings = %d, want 3", len(rows))
	}
	if !rows[0].ReleasedAt.After(rows[2].ReleasedAt) {
		t.Fatalf("printings not sorted newest first: %v then %v", rows[0].ReleasedAt, rows[2].ReleasedAt)
	}

	rows, err = svc.Printings(ctx, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("unknown oracle id returned %d rows", len(rows))
	}
}

func TestNormalizeColorFilter(t *testing.T) {
	if NormalizeColorFilter(nil) != nil {
		t.Fatal("nil in → nil out")
	}
	if got := NormalizeColorFilter([]string{"C"}); got == nil || len(got) != 0 {
		t.Fatalf("[C] → %v, want empty non-nil (colorless only)", got)
	}
	got := NormalizeColorFilter([]string{"W", "C", "U"})
	if len(got) != 2 || got[0] != "W" || got[1] != "U" {
		t.Fatalf("[W C U] → %v, want [W U]", got)
	}
}
