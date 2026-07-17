package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/cards"
	"github.com/mjabloniec/cube-planner/backend/internal/collections"
	"github.com/mjabloniec/cube-planner/backend/internal/cubes"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func newCollectionsServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:        auth.NewService(q, noopMailer{}, "http://test"),
		Sessions:    auth.NewSessions(q, false),
		Queries:     q,
		Cards:       cards.NewService(q),
		Cubes:       cubes.NewService(q, pool),
		Collections: collections.NewService(q, pool),
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q
}

type collectionItemBody struct {
	ScryfallID string `json:"scryfallId"`
	OracleID   string `json:"oracleId"`
	Name       string `json:"name"`
	SetName    string `json:"setName"`
	Quantity   int32  `json:"quantity"`
}

type collectionListBody struct {
	Items       []collectionItemBody `json:"items"`
	Total       int64                `json:"total"`
	TotalCopies int64                `json:"totalCopies"`
}

type collectionItemResp struct {
	Item *collectionItemBody `json:"item"`
}

func putQuantity(t *testing.T, c *cookieClient, scryfallID uuid.UUID, quantity int32) *http.Response {
	t.Helper()
	return c.do(t, "PUT", "/api/collection/cards/"+scryfallID.String(),
		fmt.Sprintf(`{"quantity":%d}`, quantity))
}

func TestCollectionRequiresAuth(t *testing.T) {
	srv, _, _ := newCollectionsServer(t)
	if code := getJSON(t, srv, "/api/collection", nil); code != http.StatusUnauthorized {
		t.Fatalf("GET /api/collection anonymous = %d, want 401", code)
	}
}

func TestSetQuantityLifecycle(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "col1@test.dev")
	boltS, boltO := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: boltO, name: "Lightning Bolt"})

	// Insert.
	resp := putQuantity(t, c, boltS, 3)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("insert = %d, want 200", resp.StatusCode)
	}
	body := decode[collectionItemResp](t, resp)
	if body.Item == nil || body.Item.Quantity != 3 || body.Item.Name != "Lightning Bolt" {
		t.Fatalf("insert body = %+v", body.Item)
	}

	// Idempotent set.
	resp = putQuantity(t, c, boltS, 7)
	if body := decode[collectionItemResp](t, resp); body.Item == nil || body.Item.Quantity != 7 {
		t.Fatalf("update body = %+v", body.Item)
	}

	// Delete at 0 → null item; row gone.
	resp = putQuantity(t, c, boltS, 0)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete = %d, want 200", resp.StatusCode)
	}
	if body := decode[collectionItemResp](t, resp); body.Item != nil {
		t.Fatalf("delete body = %+v, want null item", body.Item)
	}
	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Total != 0 {
		t.Fatalf("total after delete = %d, want 0", list.Total)
	}

	// Deleting again is a no-op, not an error.
	if resp = putQuantity(t, c, boltS, 0); resp.StatusCode != http.StatusOK {
		t.Fatalf("re-delete = %d, want 200", resp.StatusCode)
	}

	// Unknown printing → 422; quantity 1000 → huma 422.
	if resp = putQuantity(t, c, uuid.New(), 1); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unknown printing = %d, want 422", resp.StatusCode)
	}
	if resp = putQuantity(t, c, boltS, 1000); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("quantity 1000 = %d, want 422", resp.StatusCode)
	}
}

func TestCollectionListSearchAndPagination(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "col2@test.dev")
	other := loggedInClient(t, srv, q, "col2-other@test.dev")

	boltS, ringS := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: uuid.New(), name: "Lightning Bolt"})
	seedCard(t, pool, testCard{scryfallID: ringS, oracleID: uuid.New(), name: "Sol Ring", typeLine: "Artifact", colorIdentity: []string{}})

	putQuantity(t, c, boltS, 4)
	putQuantity(t, c, ringS, 1)
	putQuantity(t, other, boltS, 9) // must not leak into c's list

	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Total != 2 || list.TotalCopies != 5 {
		t.Fatalf("total=%d copies=%d, want 2/5", list.Total, list.TotalCopies)
	}
	// Name-sorted: Lightning Bolt before Sol Ring.
	if list.Items[0].Name != "Lightning Bolt" || list.Items[1].Name != "Sol Ring" {
		t.Fatalf("order = %q, %q", list.Items[0].Name, list.Items[1].Name)
	}

	filtered := decode[collectionListBody](t, c.do(t, "GET", "/api/collection?search=bolt", ""))
	if filtered.Total != 1 || filtered.Items[0].Name != "Lightning Bolt" {
		t.Fatalf("filtered = %+v", filtered)
	}

	page2 := decode[collectionListBody](t, c.do(t, "GET", "/api/collection?limit=1&offset=1", ""))
	if len(page2.Items) != 1 || page2.Items[0].Name != "Sol Ring" || page2.Total != 2 {
		t.Fatalf("page2 = %+v", page2)
	}
}

func changePrinting(t *testing.T, c *cookieClient, from, to uuid.UUID) *http.Response {
	t.Helper()
	return c.do(t, "POST", "/api/collection/cards/"+from.String()+"/change-printing",
		fmt.Sprintf(`{"newScryfallId":%q}`, to))
}

func TestChangePrinting(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "col3@test.dev")

	boltO := uuid.New()
	alphaS, m10S := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: alphaS, oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	seedCard(t, pool, testCard{scryfallID: m10S, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	strikeS := uuid.New()
	seedCard(t, pool, testCard{scryfallID: strikeS, oracleID: uuid.New(), name: "Lightning Strike"})

	// Simple re-key: 3× alpha → 3× m10, alpha row gone.
	putQuantity(t, c, alphaS, 3)
	resp := changePrinting(t, c, alphaS, m10S)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change = %d, want 200", resp.StatusCode)
	}
	if body := decode[collectionItemResp](t, resp); body.Item == nil ||
		body.Item.ScryfallID != m10S.String() || body.Item.Quantity != 3 {
		t.Fatalf("changed item = %+v", body.Item)
	}
	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Total != 1 {
		t.Fatalf("total = %d, want 1 (source row re-keyed)", list.Total)
	}

	// Merge onto an existing target, clamped at 999.
	putQuantity(t, c, alphaS, 998)
	resp = changePrinting(t, c, alphaS, m10S) // 998 + 3, clamps to 999
	if body := decode[collectionItemResp](t, resp); body.Item == nil || body.Item.Quantity != 999 {
		t.Fatalf("merged item = %+v, want quantity 999", body.Item)
	}

	// 422 family: same printing, oracle mismatch, missing source, unknown target.
	putQuantity(t, c, alphaS, 1)
	for name, resp := range map[string]*http.Response{
		"same printing":   changePrinting(t, c, alphaS, alphaS),
		"oracle mismatch": changePrinting(t, c, alphaS, strikeS),
		"missing source":  changePrinting(t, c, uuid.New(), m10S),
		"unknown target":  changePrinting(t, c, alphaS, uuid.New()),
	} {
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s = %d, want 422", name, resp.StatusCode)
		}
	}
}

type importLineBody struct {
	LineNumber int32  `json:"lineNumber"`
	Raw        string `json:"raw"`
	Quantity   int32  `json:"quantity"`
	Status     string `json:"status"`
	Match      *struct {
		ScryfallID string `json:"scryfallId"`
		Name       string `json:"name"`
	} `json:"match"`
	Suggestions []struct {
		ScryfallID string `json:"scryfallId"`
		Name       string `json:"name"`
	} `json:"suggestions"`
}

type resolveBody struct {
	Lines []importLineBody `json:"lines"`
}

type importResultBody struct {
	AddedRows   int32 `json:"addedRows"`
	UpdatedRows int32 `json:"updatedRows"`
}

func TestResolveImport(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "imp1@test.dev")

	boltO := uuid.New()
	// Two printings of Bolt: representative = newer non-promo.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	newBolt := uuid.New()
	seedCard(t, pool, testCard{scryfallID: newBolt, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	// Duplicate name across two oracle ids → ambiguous even on exact match.
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Twin Name"})
	seedCard(t, pool, testCard{scryfallID: uuid.New(), oracleID: uuid.New(), name: "Twin Name"})

	resp := c.do(t, "POST", "/api/collection/import/resolve",
		`{"text":"4 Lightning Bolt\nLihgtning Blot\nTwin Name\n17 Utter Gibberish Nonexistent\n0 Lightning Bolt"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve = %d, want 200", resp.StatusCode)
	}
	body := decode[resolveBody](t, resp)
	if len(body.Lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(body.Lines))
	}

	exact := body.Lines[0]
	if exact.Status != "matched" || exact.Quantity != 4 ||
		exact.Match == nil || exact.Match.ScryfallID != newBolt.String() {
		t.Fatalf("exact line = %+v (want matched, representative printing)", exact)
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

func TestApplyImport(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "imp2@test.dev")

	boltS, ringS := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: uuid.New(), name: "Lightning Bolt"})
	seedCard(t, pool, testCard{scryfallID: ringS, oracleID: uuid.New(), name: "Sol Ring", typeLine: "Artifact", colorIdentity: []string{}})
	putQuantity(t, c, ringS, 2) // pre-existing row → "updated"

	// In-request duplicates sum (2+2 bolt), adds stack on existing rows.
	resp := c.do(t, "POST", "/api/collection/import", fmt.Sprintf(
		`{"items":[{"scryfallId":%q,"quantity":2},{"scryfallId":%q,"quantity":2},{"scryfallId":%q,"quantity":1}]}`,
		boltS, boltS, ringS,
	))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import = %d, want 200", resp.StatusCode)
	}
	res := decode[importResultBody](t, resp)
	if res.AddedRows != 1 || res.UpdatedRows != 1 {
		t.Fatalf("added=%d updated=%d, want 1/1", res.AddedRows, res.UpdatedRows)
	}
	list := decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.TotalCopies != 7 { // 4 bolts + 3 rings
		t.Fatalf("copies = %d, want 7", list.TotalCopies)
	}

	// Clamp at 999.
	resp = c.do(t, "POST", "/api/collection/import", fmt.Sprintf(
		`{"items":[{"scryfallId":%q,"quantity":999}]}`, boltS,
	))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clamp import = %d", resp.StatusCode)
	}
	list = decode[collectionListBody](t, c.do(t, "GET", "/api/collection", ""))
	if list.Items[0].Quantity != 999 {
		t.Fatalf("bolt quantity = %d, want clamped 999", list.Items[0].Quantity)
	}

	// Unknown id fails the whole batch.
	resp = c.do(t, "POST", "/api/collection/import", fmt.Sprintf(
		`{"items":[{"scryfallId":%q,"quantity":1},{"scryfallId":%q,"quantity":1}]}`,
		boltS, uuid.New(),
	))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unknown id = %d, want 422", resp.StatusCode)
	}
}

type wantlistEntryBody struct {
	OracleID        string `json:"oracleId"`
	Name            string `json:"name"`
	MissingQuantity int32  `json:"missingQuantity"`
	CubeQuantity    int32  `json:"cubeQuantity"`
	OwnedQuantity   int32  `json:"ownedQuantity"`
}

type wantlistBody struct {
	CubeName     string              `json:"cubeName"`
	Items        []wantlistEntryBody `json:"items"`
	TotalMissing int64               `json:"totalMissing"`
}

func TestCubeWantlist(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	owner := loggedInClient(t, srv, q, "want-owner@test.dev")
	viewer := loggedInClient(t, srv, q, "want-viewer@test.dev")

	boltO := uuid.New()
	boltAlpha, boltM10 := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltAlpha, oracleID: boltO, name: "Lightning Bolt", released: "1993-08-05"})
	seedCard(t, pool, testCard{scryfallID: boltM10, oracleID: boltO, name: "Lightning Bolt", released: "2010-07-16"})
	ringS := uuid.New()
	seedCard(t, pool, testCard{scryfallID: ringS, oracleID: uuid.New(), name: "Sol Ring", typeLine: "Artifact", colorIdentity: []string{}})

	cube := createCube(t, owner, "Wantlist Cube", "public")
	resp := owner.do(t, "POST", "/api/cubes/"+cube.ID+"/changes", fmt.Sprintf(
		`{"expectedVersion":0,"adds":[{"scryfallId":%q,"quantity":4},{"scryfallId":%q,"quantity":1}]}`,
		boltAlpha, ringS,
	))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed cube = %d", resp.StatusCode)
	}

	// Viewer owns 1 alpha + 2 m10 bolts: 3 of 4 → missing 1; Sol Ring fully missing.
	putQuantity(t, viewer, boltAlpha, 1)
	putQuantity(t, viewer, boltM10, 2)

	resp = viewer.do(t, "GET", "/api/cubes/"+cube.ID+"/wantlist", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("wantlist = %d, want 200", resp.StatusCode)
	}
	body := decode[wantlistBody](t, resp)
	if body.CubeName != "Wantlist Cube" || body.TotalMissing != 2 || len(body.Items) != 2 {
		t.Fatalf("wantlist = %+v", body)
	}
	// Name-sorted: Bolt first.
	bolt, ring := body.Items[0], body.Items[1]
	if bolt.Name != "Lightning Bolt" || bolt.MissingQuantity != 1 ||
		bolt.CubeQuantity != 4 || bolt.OwnedQuantity != 3 {
		t.Fatalf("bolt entry = %+v (owned printings must aggregate)", bolt)
	}
	if ring.Name != "Sol Ring" || ring.MissingQuantity != 1 || ring.OwnedQuantity != 0 {
		t.Fatalf("ring entry = %+v", ring)
	}

	// Owning everything → empty list.
	putQuantity(t, viewer, boltM10, 3)
	putQuantity(t, viewer, ringS, 1)
	body = decode[wantlistBody](t, viewer.do(t, "GET", "/api/cubes/"+cube.ID+"/wantlist", ""))
	if len(body.Items) != 0 || body.TotalMissing != 0 {
		t.Fatalf("fully-owned wantlist = %+v, want empty", body)
	}
}

func TestCubeWantlistVisibilityAndAuth(t *testing.T) {
	srv, _, q := newCollectionsServer(t)
	owner := loggedInClient(t, srv, q, "want-priv@test.dev")
	stranger := loggedInClient(t, srv, q, "want-stranger@test.dev")

	priv := createCube(t, owner, "Secret Cube", "private")

	// Anonymous → 401 (session required even for public cubes).
	if code := getJSON(t, srv, "/api/cubes/"+priv.ID+"/wantlist", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", code)
	}
	// Private cube, non-owner → 404, no existence leak.
	if resp := stranger.do(t, "GET", "/api/cubes/"+priv.ID+"/wantlist", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stranger = %d, want 404", resp.StatusCode)
	}
	// Owner sees it (empty cube → empty wantlist).
	resp := owner.do(t, "GET", "/api/cubes/"+priv.ID+"/wantlist", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner = %d, want 200", resp.StatusCode)
	}
	if body := decode[wantlistBody](t, resp); body.TotalMissing != 0 {
		t.Fatalf("empty cube wantlist = %+v", body)
	}
}

func TestCollectionSearchTreatsWildcardsLiterally(t *testing.T) {
	srv, pool, q := newCollectionsServer(t)
	c := loggedInClient(t, srv, q, "col-wild@test.dev")

	boltS := uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: uuid.New(), name: "Lightning Bolt"})
	putQuantity(t, c, boltS, 1)

	// ILIKE metacharacters in the query must match literally, not act as
	// wildcards: "_" would otherwise match every non-empty name.
	for _, query := range []string{"%25", "_"} { // %25 = url-encoded %
		got := decode[collectionListBody](t, c.do(t, "GET", "/api/collection?search="+query, ""))
		if got.Total != 0 {
			t.Fatalf("search %q must match nothing, got total=%d", query, got.Total)
		}
	}
}
