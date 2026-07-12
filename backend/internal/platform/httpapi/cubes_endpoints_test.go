package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/cards"
	"github.com/mjabloniec/cube-planner/backend/internal/cubes"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func newCubesServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:     auth.NewService(q, noopMailer{}, "http://test"),
		Sessions: auth.NewSessions(q, false),
		Queries:  q,
		Cards:    cards.NewService(q),
		Cubes:    cubes.NewService(q, pool),
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q
}

// loggedInClient seeds a verified user and returns a cookie-jar client
// with an active session.
func loggedInClient(t *testing.T, srv *httptest.Server, q *db.Queries, email string) *cookieClient {
	t.Helper()
	ctx := context.Background()
	hash, _ := auth.HashPassword("password123")
	u, err := q.CreateUser(ctx, db.CreateUserParams{Email: email, DisplayName: "User " + email, PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.MarkEmailVerified(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	c := newCookieClient(t, srv)
	resp := c.do(t, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"password123"}`, email))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	return c
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

type cubeDetailBody struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Version    int32  `json:"version"`
	CardCount  int64  `json:"cardCount"`
	OwnerName  string `json:"ownerName"`
}

func createCube(t *testing.T, c *cookieClient, name, visibility string) cubeDetailBody {
	t.Helper()
	resp := c.do(t, "POST", "/api/cubes",
		fmt.Sprintf(`{"name":%q,"description":"d","visibility":%q}`, name, visibility))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create cube: %d", resp.StatusCode)
	}
	return decode[cubeDetailBody](t, resp)
}

func TestCubeCRUDAndVisibility(t *testing.T) {
	srv, _, q := newCubesServer(t)
	owner := loggedInClient(t, srv, q, "owner@x.y")
	other := loggedInClient(t, srv, q, "other@x.y")
	anon := newCookieClient(t, srv)

	pub := createCube(t, owner, "Public Cube", "public")
	priv := createCube(t, owner, "Private Cube", "private")

	// Anonymous create → 401.
	if resp := anon.do(t, "POST", "/api/cubes", `{"name":"x","visibility":"public"}`); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon create: %d, want 401", resp.StatusCode)
	}

	// Read matrix.
	for _, tc := range []struct {
		client *cookieClient
		id     string
		want   int
	}{
		{owner, pub.ID, 200},
		{owner, priv.ID, 200},
		{other, pub.ID, 200},
		{other, priv.ID, 404},
		{anon, pub.ID, 200},
		{anon, priv.ID, 404},
	} {
		if resp := tc.client.do(t, "GET", "/api/cubes/"+tc.id, ""); resp.StatusCode != tc.want {
			t.Fatalf("get %s: %d, want %d", tc.id, resp.StatusCode, tc.want)
		}
	}

	// Mutations by non-owner: public → 403, private → 404.
	if resp := other.do(t, "PATCH", "/api/cubes/"+pub.ID, `{"name":"hax"}`); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("other patch public: %d, want 403", resp.StatusCode)
	}
	if resp := other.do(t, "PATCH", "/api/cubes/"+priv.ID, `{"name":"hax"}`); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other patch private: %d, want 404", resp.StatusCode)
	}
	if resp := other.do(t, "DELETE", "/api/cubes/"+pub.ID, ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("other delete public: %d, want 403", resp.StatusCode)
	}

	// Owner update + delete.
	resp := owner.do(t, "PATCH", "/api/cubes/"+pub.ID, `{"description":"updated"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner patch: %d", resp.StatusCode)
	}
	if resp := owner.do(t, "DELETE", "/api/cubes/"+priv.ID, ""); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("owner delete: %d, want 204", resp.StatusCode)
	}
	if resp := owner.do(t, "GET", "/api/cubes/"+priv.ID, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted cube: %d, want 404", resp.StatusCode)
	}

	// Malformed id → 404, not 500.
	if resp := owner.do(t, "GET", "/api/cubes/not-a-uuid", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("malformed id: %d, want 404", resp.StatusCode)
	}
}

func TestCubeBrowserAndMine(t *testing.T) {
	srv, _, q := newCubesServer(t)
	owner := loggedInClient(t, srv, q, "owner@x.y")
	anon := newCookieClient(t, srv)

	createCube(t, owner, "Vintage Cube", "public")
	createCube(t, owner, "Peasant Cube", "public")
	createCube(t, owner, "Secret Cube", "private")

	var list struct {
		Cubes []struct {
			Name string `json:"name"`
		} `json:"cubes"`
		Total int64 `json:"total"`
	}
	resp := anon.do(t, "GET", "/api/cubes", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	list = decode[struct {
		Cubes []struct {
			Name string `json:"name"`
		} `json:"cubes"`
		Total int64 `json:"total"`
	}](t, resp)
	if list.Total != 2 {
		t.Fatalf("public total = %d, want 2 (private hidden)", list.Total)
	}

	// trgm name search.
	resp = anon.do(t, "GET", "/api/cubes?q=vintage", "")
	list = decode[struct {
		Cubes []struct {
			Name string `json:"name"`
		} `json:"cubes"`
		Total int64 `json:"total"`
	}](t, resp)
	if list.Total != 1 || list.Cubes[0].Name != "Vintage Cube" {
		t.Fatalf("search = %+v", list)
	}

	// /api/me/cubes: all three for the owner; 401 anonymous.
	var mine struct {
		Cubes []struct {
			Name string `json:"name"`
		} `json:"cubes"`
	}
	resp = owner.do(t, "GET", "/api/me/cubes", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mine: %d", resp.StatusCode)
	}
	mine = decode[struct {
		Cubes []struct {
			Name string `json:"name"`
		} `json:"cubes"`
	}](t, resp)
	if len(mine.Cubes) != 3 {
		t.Fatalf("mine = %d, want 3", len(mine.Cubes))
	}
	if resp := anon.do(t, "GET", "/api/me/cubes", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon mine: %d, want 401", resp.StatusCode)
	}
}

func TestCommitChangeAndHistory(t *testing.T) {
	srv, pool, q := newCubesServer(t)
	owner := loggedInClient(t, srv, q, "owner@x.y")
	cube := createCube(t, owner, "Evolving Cube", "public")

	boltS, boltO := uuid.New(), uuid.New()
	ringS, ringO := uuid.New(), uuid.New()
	seedCard(t, pool, testCard{scryfallID: boltS, oracleID: boltO, name: "Lightning Bolt"})
	seedCard(t, pool, testCard{scryfallID: ringS, oracleID: ringO, name: "Sol Ring", typeLine: "Artifact", colorIdentity: []string{}})

	// v1: add bolt ×2 + ring ×1.
	resp := owner.do(t, "POST", "/api/cubes/"+cube.ID+"/changes", fmt.Sprintf(
		`{"expectedVersion":0,"note":"initial","adds":[{"scryfallId":%q,"quantity":2},{"scryfallId":%q,"quantity":1}]}`,
		boltS, ringS,
	))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit v1: %d", resp.StatusCode)
	}
	v1 := decode[struct {
		Version int32 `json:"version"`
	}](t, resp)
	if v1.Version != 1 {
		t.Fatalf("version = %d, want 1", v1.Version)
	}

	// Stale expectedVersion → 409.
	resp = owner.do(t, "POST", "/api/cubes/"+cube.ID+"/changes", fmt.Sprintf(
		`{"expectedVersion":0,"adds":[{"scryfallId":%q,"quantity":1}]}`, boltS,
	))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale commit: %d, want 409", resp.StatusCode)
	}

	// 422 family: remove more than present / unknown card / empty diff.
	for name, body := range map[string]string{
		"remove exceeds": fmt.Sprintf(`{"expectedVersion":1,"removes":[{"oracleId":%q,"quantity":5}]}`, boltO),
		"unknown card":   fmt.Sprintf(`{"expectedVersion":1,"adds":[{"scryfallId":%q,"quantity":1}]}`, uuid.New()),
		"empty diff":     `{"expectedVersion":1}`,
		"add and remove same oracle": fmt.Sprintf(
			`{"expectedVersion":1,"adds":[{"scryfallId":%q,"quantity":1}],"removes":[{"oracleId":%q,"quantity":1}]}`,
			boltS, boltO,
		),
	} {
		resp = owner.do(t, "POST", "/api/cubes/"+cube.ID+"/changes", body)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %d, want 422", name, resp.StatusCode)
		}
	}

	// v2: remove one bolt.
	resp = owner.do(t, "POST", "/api/cubes/"+cube.ID+"/changes", fmt.Sprintf(
		`{"expectedVersion":1,"note":"trim","removes":[{"oracleId":%q,"quantity":1}]}`, boltO,
	))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("commit v2: %d", resp.StatusCode)
	}

	// Current cards: bolt(1) + ring(1).
	var cardsBody struct {
		Cards []struct {
			Name     string `json:"name"`
			Quantity int32  `json:"quantity"`
		} `json:"cards"`
		Version int32 `json:"version"`
	}
	resp = owner.do(t, "GET", "/api/cubes/"+cube.ID+"/cards", "")
	cardsBody = decode[struct {
		Cards []struct {
			Name     string `json:"name"`
			Quantity int32  `json:"quantity"`
		} `json:"cards"`
		Version int32 `json:"version"`
	}](t, resp)
	if cardsBody.Version != 2 || len(cardsBody.Cards) != 2 {
		t.Fatalf("current = %+v", cardsBody)
	}

	// Past state at v1: bolt(2) + ring(1).
	resp = owner.do(t, "GET", "/api/cubes/"+cube.ID+"/cards?atVersion=1", "")
	cardsBody = decode[struct {
		Cards []struct {
			Name     string `json:"name"`
			Quantity int32  `json:"quantity"`
		} `json:"cards"`
		Version int32 `json:"version"`
	}](t, resp)
	if cardsBody.Version != 1 {
		t.Fatalf("atVersion served %d, want 1", cardsBody.Version)
	}
	for _, c := range cardsBody.Cards {
		if c.Name == "Lightning Bolt" && c.Quantity != 2 {
			t.Fatalf("bolt at v1 = %d, want 2", c.Quantity)
		}
	}

	// At v0: empty. Beyond current: 404.
	resp = owner.do(t, "GET", "/api/cubes/"+cube.ID+"/cards?atVersion=0", "")
	cardsBody = decode[struct {
		Cards []struct {
			Name     string `json:"name"`
			Quantity int32  `json:"quantity"`
		} `json:"cards"`
		Version int32 `json:"version"`
	}](t, resp)
	if len(cardsBody.Cards) != 0 {
		t.Fatalf("v0 cards = %d, want 0", len(cardsBody.Cards))
	}
	if resp := owner.do(t, "GET", "/api/cubes/"+cube.ID+"/cards?atVersion=99", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("future version: %d, want 404", resp.StatusCode)
	}

	// Changelog: two entries, newest first, with items.
	var hist struct {
		Changes []struct {
			Version int32  `json:"version"`
			Note    string `json:"note"`
			Adds    []struct {
				Name     string `json:"name"`
				Quantity int32  `json:"quantity"`
			} `json:"adds"`
			Removes []struct {
				Name     string `json:"name"`
				Quantity int32  `json:"quantity"`
			} `json:"removes"`
		} `json:"changes"`
		Total int64 `json:"total"`
	}
	resp = owner.do(t, "GET", "/api/cubes/"+cube.ID+"/changes", "")
	hist = decode[struct {
		Changes []struct {
			Version int32  `json:"version"`
			Note    string `json:"note"`
			Adds    []struct {
				Name     string `json:"name"`
				Quantity int32  `json:"quantity"`
			} `json:"adds"`
			Removes []struct {
				Name     string `json:"name"`
				Quantity int32  `json:"quantity"`
			} `json:"removes"`
		} `json:"changes"`
		Total int64 `json:"total"`
	}](t, resp)
	if hist.Total != 2 || hist.Changes[0].Version != 2 || hist.Changes[0].Note != "trim" {
		t.Fatalf("history = %+v", hist)
	}
	if len(hist.Changes[0].Removes) != 1 || hist.Changes[0].Removes[0].Name != "Lightning Bolt" {
		t.Fatalf("v2 removes = %+v", hist.Changes[0].Removes)
	}
	if len(hist.Changes[1].Adds) != 2 {
		t.Fatalf("v1 adds = %+v", hist.Changes[1].Adds)
	}
}
