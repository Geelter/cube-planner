package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/cards"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

type testCard struct {
	scryfallID    uuid.UUID
	oracleID      uuid.UUID
	name          string
	released      string
	setCode       string
	rarity        string
	cmc           float64
	typeLine      string
	colorIdentity []string
	promo         bool
}

func seedCard(t *testing.T, pool *pgxpool.Pool, c testCard) {
	t.Helper()
	if c.released == "" {
		c.released = "2020-01-01"
	}
	if c.setCode == "" {
		c.setCode = "tst"
	}
	if c.rarity == "" {
		c.rarity = "common"
	}
	if c.typeLine == "" {
		c.typeLine = "Instant"
	}
	if c.colorIdentity == nil {
		c.colorIdentity = []string{"R"}
	}
	img := "https://img.test/" + c.scryfallID.String() + ".jpg"
	_, err := pool.Exec(context.Background(), `insert into cards (
		scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
		set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
		oracle_text, colors, color_identity, promo, image_small, image_normal
	) values ($1, $2, $3, $4, $5, $6, 'Test Set', '1', $7, 'normal', '{R}',
		$8, $9, 'Test text.', $10, $10, $11, $12, $12)`,
		c.scryfallID, c.oracleID, c.name, cards.NormalizeName(c.name), c.released,
		c.setCode, c.rarity, c.cmc, c.typeLine, c.colorIdentity, c.promo, img)
	if err != nil {
		t.Fatal(err)
	}
}

func newCardsServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{Queries: q, Cards: cards.NewService(q)}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool
}

func getJSON(t *testing.T, srv *httptest.Server, path string, out any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode
}

func TestAutocompleteEndpoint(t *testing.T) {
	srv, pool := newCardsServer(t)
	oracleBolt := uuid.New()
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: oracleBolt, name: "Lightning Bolt", released: "1993-08-05"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: oracleBolt, name: "Lightning Bolt", released: "2010-07-16"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Lightning Strike"})

	var body struct {
		Cards []struct {
			Name     string `json:"name"`
			OracleID string `json:"oracleId"`
		} `json:"cards"`
	}
	if code := getJSON(t, srv, "/api/cards/autocomplete?q=lightning+bo", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(body.Cards) != 2 {
		t.Fatalf("results = %d, want 2 oracle-level entries", len(body.Cards))
	}
	if body.Cards[0].Name != "Lightning Bolt" {
		t.Fatalf("first = %q", body.Cards[0].Name)
	}

	// Validation: q shorter than 2 chars → 422.
	if code := getJSON(t, srv, "/api/cards/autocomplete?q=a", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("short q status = %d, want 422", code)
	}
}

func TestSearchEndpoint(t *testing.T) {
	srv, pool := newCardsServer(t)
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Lightning Bolt"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Sol Ring", typeLine: "Artifact", cmc: 1, colorIdentity: []string{}})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Izzet Charm", cmc: 2, colorIdentity: []string{"U", "R"}})

	var body struct {
		Cards []struct {
			Name string `json:"name"`
		} `json:"cards"`
		Total int64 `json:"total"`
	}

	// colors=R (repeated-param style, as the generated client sends it).
	if code := getJSON(t, srv, "/api/cards/search?colors=R", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Total != 2 {
		t.Fatalf("total = %d, want 2 (bolt + colorless sol ring)", body.Total)
	}

	// colors=C alone → colorless only.
	if code := getJSON(t, srv, "/api/cards/search?colors=C", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Total != 1 || body.Cards[0].Name != "Sol Ring" {
		t.Fatalf("colorless = %+v", body)
	}

	// No filters → everything, paginated.
	if code := getJSON(t, srv, "/api/cards/search?limit=2", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(body.Cards) != 2 || body.Total != 3 {
		t.Fatalf("page = %d/total %d, want 2/3", len(body.Cards), body.Total)
	}

	// Invalid rarity → 422.
	if code := getJSON(t, srv, "/api/cards/search?rarity=legendary", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("bad rarity status = %d, want 422", code)
	}
}

func TestPrintingsEndpoint(t *testing.T) {
	srv, pool := newCardsServer(t)
	oracleID := uuid.New()
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: oracleID, name: "Sol Ring", released: "1993-08-05"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: oracleID, name: "Sol Ring", released: "2020-01-01"})

	var body struct {
		Printings []struct {
			SetCode  string `json:"setCode"`
			Released string `json:"releasedAt"`
		} `json:"printings"`
	}
	if code := getJSON(t, srv, "/api/cards/"+oracleID.String()+"/printings", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(body.Printings) != 2 {
		t.Fatalf("printings = %d, want 2", len(body.Printings))
	}

	if code := getJSON(t, srv, "/api/cards/"+uuid.New().String()+"/printings", nil); code != http.StatusNotFound {
		t.Fatalf("unknown oracle status = %d, want 404", code)
	}
	if code := getJSON(t, srv, "/api/cards/not-a-uuid/printings", nil); code != http.StatusNotFound {
		t.Fatalf("malformed oracle status = %d, want 404", code)
	}
}
