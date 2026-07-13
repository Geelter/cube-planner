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
