package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
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
	edhrec        *int32
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
		oracle_text, colors, color_identity, promo, image_small, image_normal,
		edhrec_rank
	) values ($1, $2, $3, $4, $5, $6, 'Test Set', '1', $7, 'normal', '{R}',
		$8, $9, 'Test text.', $10, $10, $11, $12, $12, $13)`,
		c.scryfallID, c.oracleID, c.name, cards.NormalizeName(c.name), c.released,
		c.setCode, c.rarity, c.cmc, c.typeLine, c.colorIdentity, c.promo, img, c.edhrec)
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

// newCardsServerWithSession additionally wires Auth/Sessions for endpoints
// that require a logged-in caller (resolve-list).
func newCardsServerWithSession(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:     auth.NewService(q, noopMailer{}, "http://test"),
		Sessions: auth.NewSessions(q, false),
		Queries:  q,
		Cards:    cards.NewService(q),
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q
}

func getJSON(t *testing.T, srv *httptest.Server, path string, out any) int {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
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
			Name     string   `json:"name"`
			OracleID string   `json:"oracleId"`
			Colors   []string `json:"colors"`
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
	if len(body.Cards[0].Colors) != 1 || body.Cards[0].Colors[0] != "R" {
		t.Fatalf("colors = %v, want [R]", body.Cards[0].Colors)
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

func rank(n int32) *int32 { return &n }

func TestAutocompletePopularityOrdering(t *testing.T) {
	srv, pool := newCardsServer(t)
	// All three tie at word_similarity 1.0 for q=bolt; popularity must break
	// the tie (low rank = popular), unranked last. None is a prefix match.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Frost Bolt", edhrec: rank(5000)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Lightning Bolt", edhrec: rank(100)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Shadow Bolt"}) // unranked
	// Prefix matches still outrank everything, popular or not.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Boltwing Marauder"})

	var body struct {
		Cards []struct {
			Name string `json:"name"`
		} `json:"cards"`
	}
	if code := getJSON(t, srv, "/api/cards/autocomplete?q=bolt", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := make([]string, len(body.Cards))
	for i, c := range body.Cards {
		got[i] = c.Name
	}
	want := []string{"Boltwing Marauder", "Lightning Bolt", "Frost Bolt", "Shadow Bolt"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestSearchPopularityOrdering(t *testing.T) {
	srv, pool := newCardsServer(t)
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Frost Bolt", edhrec: rank(5000)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Lightning Bolt", edhrec: rank(100)})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Shadow Bolt"})

	var body struct {
		Cards []struct {
			Name string `json:"name"`
		} `json:"cards"`
	}
	if code := getJSON(t, srv, "/api/cards/search?name=bolt", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := make([]string, len(body.Cards))
	for i, c := range body.Cards {
		got[i] = c.Name
	}
	want := []string{"Lightning Bolt", "Frost Bolt", "Shadow Bolt"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}

	// Without a name filter, browsing stays alphabetical — popularity must
	// not reorder it.
	if code := getJSON(t, srv, "/api/cards/search", &body); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Cards[0].Name != "Frost Bolt" {
		t.Fatalf("no-name first = %q, want alphabetical Frost Bolt", body.Cards[0].Name)
	}
}

type resolveListLineBody struct {
	LineNumber      int32  `json:"lineNumber"`
	Raw             string `json:"raw"`
	Quantity        int32  `json:"quantity"`
	Status          string `json:"status"`
	SetCode         string `json:"setCode"`
	CollectorNumber string `json:"collectorNumber"`
	Match           *struct {
		ScryfallID      string   `json:"scryfallId"`
		Name            string   `json:"name"`
		SetCode         string   `json:"setCode"`
		CollectorNumber string   `json:"collectorNumber"`
		Colors          []string `json:"colors"`
	} `json:"match"`
	Suggestions []struct {
		ScryfallID      string `json:"scryfallId"`
		Name            string `json:"name"`
		CollectorNumber string `json:"collectorNumber"`
	} `json:"suggestions"`
}

type resolveListBody struct {
	Lines []resolveListLineBody `json:"lines"`
}

func TestResolveCardListEndpoint(t *testing.T) {
	srv, pool, q := newCardsServerWithSession(t)
	c := loggedInClient(t, srv, q, "imp1@test.dev")

	boltO := uuid.New()
	// Two printings of Bolt: representative = newer non-promo.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	newBolt := uuid.New()
	seedCard(t, pool, testCard{scryfallID: newBolt, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	// Duplicate name across two oracle ids → ambiguous even on exact match.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Twin Name"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Twin Name"})

	resp := c.do(t, "POST", "/api/cards/resolve-list",
		`{"text":"4 Lightning Bolt\nLihgtning Blot\nTwin Name\n17 Utter Gibberish Nonexistent\n0 Lightning Bolt"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve = %d, want 200", resp.StatusCode)
	}
	body := decode[resolveListBody](t, resp)
	if len(body.Lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(body.Lines))
	}

	exact := body.Lines[0]
	if exact.Status != "matched" || exact.Quantity != 4 ||
		exact.Match == nil || exact.Match.ScryfallID != newBolt.String() {
		t.Fatalf("exact line = %+v (want matched, representative printing)", exact)
	}
	// Colors must ride along with the match so a card staged into a cube's
	// pending diff groups by its real color, not "colorless", before commit.
	if len(exact.Match.Colors) != 1 || exact.Match.Colors[0] != "R" {
		t.Fatalf("exact match colors = %v, want [R]", exact.Match.Colors)
	}
	fuzzy := body.Lines[1]
	if fuzzy.Status != "ambiguous" || len(fuzzy.Suggestions) == 0 ||
		fuzzy.Suggestions[0].Name != "Lightning Bolt" {
		t.Fatalf("fuzzy line = %+v (want ambiguous with Bolt suggestion)", fuzzy)
	}
	twin := body.Lines[2]
	if twin.Status != "ambiguous" || len(twin.Suggestions) != 2 {
		t.Fatalf("duplicate-name line = %+v (want ambiguous, 2 suggestions)", twin)
	}
	if body.Lines[3].Status != "unmatched" {
		t.Fatalf("gibberish line = %+v, want unmatched", body.Lines[3])
	}
	if body.Lines[4].Status != "unmatched" {
		t.Fatalf("bad-quantity line = %+v, want unmatched", body.Lines[4])
	}
}

// TestResolveCardListEndpointPrintingSelectors covers the wire-level
// contract for Task 25: setCode/collectorNumber echoed on every line, and
// the printing-not-found status when the name is real but the requested
// printing is not.
func TestResolveCardListEndpointPrintingSelectors(t *testing.T) {
	srv, pool, q := newCardsServerWithSession(t)
	c := loggedInClient(t, srv, q, "imp-printings@test.dev")

	boltO := uuid.New()
	leb := uuid.New()
	seedCard(t, pool, testCard{scryfallID: leb, oracleID: boltO, name: "Lightning Bolt", setCode: "leb"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: boltO, name: "Lightning Bolt", setCode: "mm2"})

	resp := c.do(t, "POST", "/api/cards/resolve-list",
		`{"text":"1 Lightning Bolt (LEB)\n1 Lightning Bolt (XYZ) 999"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve = %d, want 200", resp.StatusCode)
	}
	body := decode[resolveListBody](t, resp)
	if len(body.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(body.Lines))
	}

	bySet := body.Lines[0]
	if bySet.Status != "matched" || bySet.SetCode != "leb" ||
		bySet.Match == nil || bySet.Match.ScryfallID != leb.String() {
		t.Fatalf("set-only line = %+v, want matched to the leb printing", bySet)
	}

	notFound := body.Lines[1]
	if notFound.Status != "printing-not-found" || notFound.SetCode != "xyz" ||
		notFound.CollectorNumber != "999" {
		t.Fatalf("bad-printing line = %+v, want printing-not-found echoing xyz/999", notFound)
	}
	if len(notFound.Suggestions) != 2 {
		t.Fatalf("suggestions = %d, want both real printings", len(notFound.Suggestions))
	}
}

// TestResolveCardListTooManyLines guards the RFC 7807 detail string the
// endpoint returns for an over-limit paste. That copy is user-facing and
// must not drift silently — it moved from collections.ResolveImport's
// wrapping (`"invalid import: " + parse error`) to cards.ResolveList's own
// wrap, and nothing at the HTTP layer asserted the exact wording before.
func TestResolveCardListTooManyLines(t *testing.T) {
	srv, _, q := newCardsServerWithSession(t)
	c := loggedInClient(t, srv, q, "imp-toomany@test.dev")

	text := strings.Repeat("Lightning Bolt\n", cards.MaxImportLines+1)
	resp := c.do(t, "POST", "/api/cards/resolve-list", fmt.Sprintf(`{"text":%q}`, text))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	problem := decode[struct {
		Type   string `json:"type"`
		Detail string `json:"detail"`
	}](t, resp)
	if problem.Type != "invalid-import" {
		t.Fatalf("type = %q, want invalid-import", problem.Type)
	}
	const wantDetail = "invalid import: import exceeds 500 lines"
	if problem.Detail != wantDetail {
		t.Fatalf("detail = %q, want %q", problem.Detail, wantDetail)
	}
}
