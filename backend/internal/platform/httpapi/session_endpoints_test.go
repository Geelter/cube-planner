package httpapi_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

type noopMailer struct{}

func (noopMailer) Send(context.Context, string, string, string) error { return nil }

func newTestServer(t *testing.T) (*httptest.Server, *db.Queries) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	deps := httpapi.Deps{
		Auth:     auth.NewService(q, noopMailer{}, "http://test"),
		Sessions: auth.NewSessions(q, false),
		Queries:  q,
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, q
}

func TestLoginMeLogout(t *testing.T) {
	srv, q := newTestServer(t)
	ctx := context.Background()

	// Seed a verified user directly.
	hash, _ := auth.HashPassword("password123")
	u, err := q.CreateUser(ctx, db.CreateUserParams{Email: "eve@x.y", DisplayName: "Eve", PasswordHash: &hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.MarkEmailVerified(ctx, u.ID); err != nil {
		t.Fatal(err)
	}

	jar := newCookieClient(t, srv)

	// Anonymous /api/me → 401.
	resp := jar.do(t, "GET", "/api/me", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me anonymous: status %d, want 401", resp.StatusCode)
	}

	// Login sets the cookie.
	resp = jar.do(t, "POST", "/api/auth/login", `{"email":"eve@x.y","password":"password123"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d, want 200", resp.StatusCode)
	}

	// /api/me now works.
	resp = jar.do(t, "GET", "/api/me", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: status %d, want 200", resp.StatusCode)
	}
	me := decode[struct {
		Role string `json:"role"`
	}](t, resp)
	if me.Role != "user" {
		t.Fatalf("me role: got %q, want %q", me.Role, "user")
	}

	// Logout clears the session.
	resp = jar.do(t, "POST", "/api/auth/logout", "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: status %d, want 204", resp.StatusCode)
	}
	resp = jar.do(t, "GET", "/api/me", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout: status %d, want 401", resp.StatusCode)
	}
}

// cookieClient is a tiny helper wrapping http.Client with a cookie jar.
type cookieClient struct {
	base   string
	client *http.Client
}

func newCookieClient(t *testing.T, srv *httptest.Server) *cookieClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &cookieClient{base: srv.URL, client: &http.Client{Jar: jar}}
}

func (c *cookieClient) do(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}
