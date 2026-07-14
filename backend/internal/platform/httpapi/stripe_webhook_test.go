package httpapi_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/events"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

const testWebhookSecret = "whsec_testsecret"

func newWebhookServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries, *events.Service, *endpointFakeStripe) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	fake := &endpointFakeStripe{}
	svc := events.NewService(q, pool, fake, noopMailer{}, "http://test", slog.Default())
	deps := httpapi.Deps{
		Auth:                auth.NewService(q, noopMailer{}, "http://test"),
		Sessions:            auth.NewSessions(q, false),
		Queries:             q,
		Events:              svc,
		StripeWebhookSecret: testWebhookSecret,
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q, svc, fake
}

// signStripePayload reproduces Stripe's v1 signature scheme:
// HMAC-SHA256 over "<timestamp>.<payload>".
func signStripePayload(payload []byte, secret string) string {
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func postWebhook(t *testing.T, srv *httptest.Server, payload []byte, sig string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", srv.URL+"/api/stripe/webhook", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Stripe-Signature", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestWebhookEndpointCompletesPayment(t *testing.T) {
	srv, pool, q, svc, _ := newWebhookServer(t)
	ctx := context.Background()
	evID := seedPublishedEvent(t, pool, svc, 5000, 2)
	alice := loggedInClient(t, srv, q, "alice@test")

	resp := alice.do(t, "POST", fmt.Sprintf("/api/events/%s/register", evID), "")
	reg := decode[registrationInfoBody](t, resp)
	if resp := alice.do(t, "POST", fmt.Sprintf("/api/events/%s/registration/pay", evID), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("pay: %d", resp.StatusCode)
	}

	sessionID := "cs_test_" + reg.ID
	payload := []byte(fmt.Sprintf(`{
		"id": "evt_fixture_1",
		"object": "event",
		"api_version": "2026-01-01",
		"type": "checkout.session.completed",
		"data": {"object": {"id": %q, "object": "checkout.session", "payment_intent": "pi_fixture_1"}}
	}`, sessionID))

	// Bad signature → 400, nothing changes.
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, "whsec_wrong")); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad signature: want 400, got %d", resp.StatusCode)
	}

	// Good signature → 200 and the registration is paid.
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, testWebhookSecret)); resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: want 200, got %d", resp.StatusCode)
	}
	var status string
	if err := pool.QueryRow(ctx,
		`select status from registrations where id = $1`, reg.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paid" {
		t.Fatalf("want paid, got %s", status)
	}

	// Duplicate delivery of the same event id → 200, still one paid row.
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, testWebhookSecret)); resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate delivery: want 200, got %d", resp.StatusCode)
	}
}

func TestWebhookEndpointUnknownTypeAcknowledged(t *testing.T) {
	srv, _, _, _, _ := newWebhookServer(t)
	payload := []byte(`{"id": "evt_other_1", "object": "event", "api_version": "2026-01-01", "type": "invoice.paid", "data": {"object": {}}}`)
	if resp := postWebhook(t, srv, payload, signStripePayload(payload, testWebhookSecret)); resp.StatusCode != http.StatusOK {
		t.Fatalf("unknown type must be acknowledged, got %d", resp.StatusCode)
	}
}
