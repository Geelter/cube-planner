package cards

import (
	"encoding/json"
	"testing"
)

func parse(t *testing.T, raw string) scryfallCard {
	t.Helper()
	var sc scryfallCard
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return sc
}

const boltJSON = `{
  "id": "ce711943-c1a1-43a0-8b89-8d169cfb8e06",
  "oracle_id": "4457ed35-7c10-48c8-9776-456485fdf070",
  "name": "Lightning Bolt",
  "released_at": "2010-07-16",
  "set": "m11", "set_name": "Magic 2011", "set_type": "core",
  "collector_number": "149", "rarity": "common", "layout": "normal",
  "mana_cost": "{R}", "cmc": 1.0, "type_line": "Instant",
  "oracle_text": "Lightning Bolt deals 3 damage to any target.",
  "colors": ["R"], "color_identity": ["R"],
  "promo": false, "oversized": false,
  "games": ["paper", "mtgo"],
  "image_uris": {"small": "https://img.test/bolt-s.jpg", "normal": "https://img.test/bolt-n.jpg"}
}`

const delverJSON = `{
  "id": "11bf83bb-c95b-4b4f-9a56-ce7a1816307a",
  "oracle_id": "28ea1d43-4b1b-4e40-91f4-e26ce7d5f24f",
  "name": "Delver of Secrets // Insectile Aberration",
  "released_at": "2011-09-30",
  "set": "isd", "set_name": "Innistrad", "set_type": "expansion",
  "collector_number": "51", "rarity": "common", "layout": "transform",
  "cmc": 1.0,
  "type_line": "Creature — Human Wizard // Creature — Human Insect",
  "color_identity": ["U"],
  "promo": false, "oversized": false,
  "games": ["paper", "mtgo"],
  "card_faces": [
    {"mana_cost": "{U}", "oracle_text": "At the beginning of your upkeep, look at the top card of your library.", "colors": ["U"],
     "image_uris": {"small": "https://img.test/delver-s.jpg", "normal": "https://img.test/delver-n.jpg"}},
    {"mana_cost": "", "oracle_text": "Flying", "colors": ["U"],
     "image_uris": {"small": "https://img.test/aberration-s.jpg", "normal": "https://img.test/aberration-n.jpg"}}
  ]
}`

const fireIceJSON = `{
  "id": "8b475e5f-b6cf-42a6-8ba1-33e10fd824bb",
  "oracle_id": "23a5c395-cd4a-4d34-a5e1-c26979b58d99",
  "name": "Fire // Ice",
  "released_at": "2002-06-24",
  "set": "apc", "set_name": "Apocalypse", "set_type": "expansion",
  "collector_number": "128", "rarity": "uncommon", "layout": "split",
  "mana_cost": "{1}{R} // {1}{U}", "cmc": 4.0,
  "type_line": "Instant // Instant",
  "colors": ["R", "U"], "color_identity": ["R", "U"],
  "promo": false, "oversized": false,
  "games": ["paper", "mtgo"],
  "image_uris": {"small": "https://img.test/fireice-s.jpg", "normal": "https://img.test/fireice-n.jpg"},
  "card_faces": [
    {"mana_cost": "{1}{R}", "oracle_text": "Fire deals 2 damage divided as you choose."},
    {"mana_cost": "{1}{U}", "oracle_text": "Tap or untap target permanent.\nDraw a card."}
  ]
}`

func TestTransformNormalCard(t *testing.T) {
	c, ok := transformCard(parse(t, boltJSON))
	if !ok {
		t.Fatal("bolt must be kept")
	}
	if c.Name != "Lightning Bolt" || c.NormalizedName != "lightning bolt" {
		t.Fatalf("name = %q / %q", c.Name, c.NormalizedName)
	}
	if c.SetCode != "m11" || c.Rarity != "common" || c.CMC != 1.0 || c.Promo {
		t.Fatalf("unexpected fields: %+v", c)
	}
	if c.ReleasedAt.Format("2006-01-02") != "2010-07-16" {
		t.Fatalf("released_at = %v", c.ReleasedAt)
	}
	if c.ImageSmall == nil || *c.ImageSmall != "https://img.test/bolt-s.jpg" {
		t.Fatalf("image_small = %v", c.ImageSmall)
	}
	if c.BackImageSmall != nil {
		t.Fatal("single-faced card must have no back image")
	}
}

func TestTransformTransformCard(t *testing.T) {
	c, ok := transformCard(parse(t, delverJSON))
	if !ok {
		t.Fatal("delver must be kept")
	}
	if c.ManaCost != "{U}" {
		t.Fatalf("mana_cost = %q, want front-face fallback", c.ManaCost)
	}
	if c.ImageNormal == nil || *c.ImageNormal != "https://img.test/delver-n.jpg" {
		t.Fatalf("front image = %v", c.ImageNormal)
	}
	if c.BackImageNormal == nil || *c.BackImageNormal != "https://img.test/aberration-n.jpg" {
		t.Fatalf("back image = %v", c.BackImageNormal)
	}
	if c.OracleText != "At the beginning of your upkeep, look at the top card of your library.\n//\nFlying" {
		t.Fatalf("oracle_text = %q", c.OracleText)
	}
	if len(c.Colors) != 1 || c.Colors[0] != "U" {
		t.Fatalf("colors union = %v", c.Colors)
	}
}

func TestTransformSplitCard(t *testing.T) {
	c, ok := transformCard(parse(t, fireIceJSON))
	if !ok {
		t.Fatal("split card must be kept")
	}
	if c.ManaCost != "{1}{R} // {1}{U}" {
		t.Fatalf("mana_cost = %q, want top-level value kept", c.ManaCost)
	}
	if c.OracleText != "Fire deals 2 damage divided as you choose.\n//\nTap or untap target permanent.\nDraw a card." {
		t.Fatalf("oracle_text = %q", c.OracleText)
	}
	if c.BackImageSmall != nil {
		t.Fatal("split card has one shared image, no back image")
	}
}

func TestTransformFilters(t *testing.T) {
	base := parse(t, boltJSON)
	tests := []struct {
		name   string
		mutate func(*scryfallCard)
	}{
		{"digital only", func(sc *scryfallCard) { sc.Games = []string{"arena"} }},
		{"no games", func(sc *scryfallCard) { sc.Games = nil }},
		{"token layout", func(sc *scryfallCard) { sc.Layout = "token" }},
		{"double_faced_token", func(sc *scryfallCard) { sc.Layout = "double_faced_token" }},
		{"emblem", func(sc *scryfallCard) { sc.Layout = "emblem" }},
		{"art_series", func(sc *scryfallCard) { sc.Layout = "art_series" }},
		{"oversized", func(sc *scryfallCard) { sc.Oversized = true }},
		{"memorabilia", func(sc *scryfallCard) { sc.SetType = "memorabilia" }},
		{"bad id", func(sc *scryfallCard) { sc.ID = "not-a-uuid" }},
		{"missing oracle id", func(sc *scryfallCard) { sc.OracleID = "" }},
		{"bad date", func(sc *scryfallCard) { sc.ReleasedAt = "sometime" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := base
			tt.mutate(&sc)
			if _, ok := transformCard(sc); ok {
				t.Fatalf("%s must be filtered out", tt.name)
			}
		})
	}
	// Sanity: unmutated base is kept.
	if _, ok := transformCard(base); !ok {
		t.Fatal("base card must be kept")
	}
}

func TestTransformReversibleCardTakesFaceOracleID(t *testing.T) {
	sc := parse(t, delverJSON)
	sc.OracleID = ""
	sc.CardFaces[0].OracleID = "28ea1d43-4b1b-4e40-91f4-e26ce7d5f24f"
	c, ok := transformCard(sc)
	if !ok {
		t.Fatal("reversible-style card must be kept")
	}
	if c.OracleID.String() != "28ea1d43-4b1b-4e40-91f4-e26ce7d5f24f" {
		t.Fatalf("oracle_id = %v, want face fallback", c.OracleID)
	}
}
