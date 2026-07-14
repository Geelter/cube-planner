package httpapi_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/events"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/httpapi"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// endpointFakeStripe implements events.StripeClient for endpoint tests.
type endpointFakeStripe struct {
	mu       sync.Mutex
	sessions []events.CheckoutParams
	refunds  []string
}

func (f *endpointFakeStripe) Configured() bool { return true }

func (f *endpointFakeStripe) CreateCheckoutSession(_ context.Context, p events.CheckoutParams) (*events.CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, p)
	return &events.CheckoutSession{
		ID:        "cs_test_" + p.RegistrationID.String(),
		URL:       "https://checkout.stripe.test/" + p.RegistrationID.String(),
		ExpiresAt: p.ExpiresAt,
	}, nil
}

func (f *endpointFakeStripe) RefundPaymentIntent(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunds = append(f.refunds, id)
	return nil
}

func newEventsServer(t *testing.T) (*httptest.Server, *pgxpool.Pool, *db.Queries, *events.Service, *endpointFakeStripe) {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	fake := &endpointFakeStripe{}
	svc := events.NewService(q, pool, fake, noopMailer{}, "http://test", slog.Default())
	deps := httpapi.Deps{
		Auth:     auth.NewService(q, noopMailer{}, "http://test"),
		Sessions: auth.NewSessions(q, false),
		Queries:  q,
		Events:   svc,
	}
	_, handler := httpapi.Build(deps)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv, pool, q, svc, fake
}

func makeAdmin(t *testing.T, pool *pgxpool.Pool, email string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`update users set role = 'admin' where email = $1`, email); err != nil {
		t.Fatal(err)
	}
}

// seedPublishedEvent creates + publishes an event directly via the service.
func seedPublishedEvent(t *testing.T, pool *pgxpool.Pool, svc *events.Service, feeCents, maxParticipants int32) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var orgID uuid.UUID
	err := pool.QueryRow(ctx,
		`insert into users (email, display_name, email_verified_at, role)
		 values ('organizer+' || gen_random_uuid()::text || '@test', 'Organizer', now(), 'admin')
		 returning id`).Scan(&orgID)
	if err != nil {
		t.Fatal(err)
	}
	ev, err := svc.Create(ctx, orgID, events.CreateEventParams{
		Name: "Cube Night", StartsAt: time.Now().Add(7 * 24 * time.Hour),
		FeeCents: feeCents, MaxParticipants: maxParticipants,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, ev.ID, "publish"); err != nil {
		t.Fatal(err)
	}
	return ev.ID
}

type registrationInfoBody struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	WaitlistPos *int64 `json:"waitlistPos"`
}

func TestEventsRequireAuth(t *testing.T) {
	srv, _, _, _, _ := newEventsServer(t)
	c := newCookieClient(t, srv)
	for _, probe := range []struct{ method, path string }{
		{"GET", "/api/events"},
		{"GET", "/api/events/" + uuid.NewString()},
		{"POST", "/api/events/" + uuid.NewString() + "/register"},
		{"POST", "/api/events/" + uuid.NewString() + "/registration/pay"},
		{"DELETE", "/api/events/" + uuid.NewString() + "/registration"},
	} {
		resp := c.do(t, probe.method, probe.path, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s: want 401, got %d", probe.method, probe.path, resp.StatusCode)
		}
	}
}

func TestRegisterPayCancelFlow(t *testing.T) {
	srv, pool, q, svc, fake := newEventsServer(t)
	evID := seedPublishedEvent(t, pool, svc, 5000, 2)
	alice := loggedInClient(t, srv, q, "alice@test")

	// Register → pending_payment.
	resp := alice.do(t, "POST", fmt.Sprintf("/api/events/%s/register", evID), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	reg := decode[registrationInfoBody](t, resp)
	if reg.Status != "pending_payment" {
		t.Fatalf("want pending_payment, got %s", reg.Status)
	}

	// Pay → checkout url from the (fake) Stripe session.
	resp = alice.do(t, "POST", fmt.Sprintf("/api/events/%s/registration/pay", evID), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pay: %d", resp.StatusCode)
	}
	pay := decode[struct {
		CheckoutUrl string `json:"checkoutUrl"`
	}](t, resp)
	if pay.CheckoutUrl == "" || len(fake.sessions) != 1 {
		t.Fatalf("want a checkout session, got %+v / %d sessions", pay, len(fake.sessions))
	}

	// Detail shows my registration.
	resp = alice.do(t, "GET", fmt.Sprintf("/api/events/%s", evID), "")
	detail := decode[struct {
		MyRegistration *registrationInfoBody `json:"myRegistration"`
		PendingCount   int32                 `json:"pendingCount"`
	}](t, resp)
	if detail.MyRegistration == nil || detail.MyRegistration.Status != "pending_payment" || detail.PendingCount != 1 {
		t.Fatalf("detail: %+v", detail)
	}

	// Cancel → cancelled (pending, no money moved).
	resp = alice.do(t, "DELETE", fmt.Sprintf("/api/events/%s/registration", evID), "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel: %d", resp.StatusCode)
	}
	reg = decode[registrationInfoBody](t, resp)
	if reg.Status != "cancelled" || len(fake.refunds) != 0 {
		t.Fatalf("pending cancel must not refund: %+v %v", reg, fake.refunds)
	}
}

func TestDraftEventHiddenOverHTTP(t *testing.T) {
	srv, pool, q, svc, _ := newEventsServer(t)
	ctx := context.Background()
	var orgID uuid.UUID
	if err := pool.QueryRow(ctx,
		`insert into users (email, display_name, email_verified_at, role)
		 values ('org@test', 'Org', now(), 'admin') returning id`).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	ev, err := svc.Create(ctx, orgID, events.CreateEventParams{
		Name: "Secret Draft", StartsAt: time.Now().Add(24 * time.Hour),
		FeeCents: 0, MaxParticipants: 8,
	})
	if err != nil {
		t.Fatal(err)
	}

	user := loggedInClient(t, srv, q, "user@test")
	if resp := user.do(t, "GET", fmt.Sprintf("/api/events/%s", ev.ID), ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("draft must 404 for non-admin, got %d", resp.StatusCode)
	}

	admin := loggedInClient(t, srv, q, "admin@test")
	makeAdmin(t, pool, "admin@test")
	if resp := admin.do(t, "GET", fmt.Sprintf("/api/events/%s", ev.ID), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin must see the draft, got %d", resp.StatusCode)
	}
}

func TestConcurrentRegistrationOnLastSpot(t *testing.T) {
	srv, pool, q, svc, _ := newEventsServer(t)
	evID := seedPublishedEvent(t, pool, svc, 5000, 1)
	a := loggedInClient(t, srv, q, "a@test")
	b := loggedInClient(t, srv, q, "b@test")

	var wg sync.WaitGroup
	statuses := make([]string, 2)
	for i, c := range []*cookieClient{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp := c.do(t, "POST", fmt.Sprintf("/api/events/%s/register", evID), "")
			if resp.StatusCode != http.StatusOK {
				t.Errorf("register %d: %d", i, resp.StatusCode)
				return
			}
			statuses[i] = decode[registrationInfoBody](t, resp).Status
		}()
	}
	wg.Wait()
	got := map[string]int{}
	for _, s := range statuses {
		got[s]++
	}
	if got["pending_payment"] != 1 || got["waitlisted"] != 1 {
		t.Fatalf("exactly one spot + one waitlist expected, got %v", got)
	}
}

func TestAdminGating(t *testing.T) {
	srv, _, q, _, _ := newEventsServer(t)
	user := loggedInClient(t, srv, q, "pleb@test")
	resp := user.do(t, "POST", "/api/events",
		`{"name":"Nope","startsAt":"2026-08-01T18:00:00Z","feeCents":0,"maxParticipants":8}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin create: want 403, got %d", resp.StatusCode)
	}
}

func TestOrganizerLifecycleOverHTTP(t *testing.T) {
	srv, pool, q, _, fake := newEventsServer(t)
	admin := loggedInClient(t, srv, q, "boss@test")
	makeAdmin(t, pool, "boss@test")

	// Create draft.
	resp := admin.do(t, "POST", "/api/events",
		`{"name":"Vintage Cube Night","startsAt":"2026-08-01T18:00:00Z","feeCents":5000,"maxParticipants":1,"refundDeadline":"2026-07-30T18:00:00Z"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	ev := decode[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}](t, resp)
	if ev.Status != "draft" {
		t.Fatalf("want draft, got %s", ev.Status)
	}

	// Publish, then a locked-field PATCH must 409.
	if resp := admin.do(t, "POST", "/api/events/"+ev.ID+"/publish", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("publish: %d", resp.StatusCode)
	}
	if resp := admin.do(t, "PATCH", "/api/events/"+ev.ID, `{"feeCents":9900}`); resp.StatusCode != http.StatusConflict {
		t.Fatalf("locked-field patch: want 409, got %d", resp.StatusCode)
	}
	if resp := admin.do(t, "PATCH", "/api/events/"+ev.ID, `{"description":"bring snacks"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("description patch: %d", resp.StatusCode)
	}

	// A user registers + pays (via service-level webhook simulation is
	// Task 6-tested; over HTTP we just check the organizer panel list).
	user := loggedInClient(t, srv, q, "player@test")
	if resp := user.do(t, "POST", "/api/events/"+ev.ID+"/register", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("register: %d", resp.StatusCode)
	}
	resp = admin.do(t, "GET", "/api/events/"+ev.ID+"/registrations", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("registrations list: %d", resp.StatusCode)
	}
	regs := decode[struct {
		Registrations []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Email  string `json:"email"`
		} `json:"registrations"`
	}](t, resp)
	if len(regs.Registrations) != 1 || regs.Registrations[0].Email != "player@test" {
		t.Fatalf("organizer list: %+v", regs)
	}
	if resp := user.do(t, "GET", "/api/events/"+ev.ID+"/registrations", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin registrations list: want 403, got %d", resp.StatusCode)
	}
	_ = fake
}
