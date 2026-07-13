package events

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// ---- shared fakes (extended by Tasks 4–7) ----

type fakeStripe struct {
	configured bool
	mu         sync.Mutex
	sessions   []CheckoutParams
	refunds    []string
	refundErr  error
}

func (f *fakeStripe) Configured() bool { return f.configured }

func (f *fakeStripe) CreateCheckoutSession(_ context.Context, p CheckoutParams) (*CheckoutSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = append(f.sessions, p)
	return &CheckoutSession{
		ID:        "cs_test_" + p.RegistrationID.String(),
		URL:       "https://checkout.stripe.test/" + p.RegistrationID.String(),
		ExpiresAt: p.ExpiresAt,
	}, nil
}

func (f *fakeStripe) RefundPaymentIntent(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refundErr != nil {
		return f.refundErr
	}
	f.refunds = append(f.refunds, id)
	return nil
}

type sentMail struct{ to, subject, body string }

type recordMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

func (m *recordMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{to, subject, body})
	return nil
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type testEnv struct {
	svc    *Service
	pool   *pgxpool.Pool
	q      *db.Queries
	stripe *fakeStripe
	mailer *recordMailer
	clock  *fakeClock
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	st := &fakeStripe{configured: true}
	m := &recordMailer{}
	clock := &fakeClock{t: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	svc := NewService(q, pool, st, m, "http://localhost:5173", slog.Default())
	svc.now = clock.Now
	return &testEnv{svc: svc, pool: pool, q: q, stripe: st, mailer: m, clock: clock}
}

func (e *testEnv) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.pool.QueryRow(context.Background(),
		`insert into users (email, display_name, email_verified_at)
		 values ($1, split_part($1, '@', 1), now()) returning id`, email).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *testEnv) seedCube(t *testing.T, ownerID uuid.UUID, name, visibility string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := e.pool.QueryRow(context.Background(),
		`insert into cubes (owner_id, name, visibility) values ($1, $2, $3) returning id`,
		ownerID, name, visibility).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *testEnv) createEvent(t *testing.T, organizerID uuid.UUID, feeCents int32, maxParticipants int32) *db.Event {
	t.Helper()
	ev, err := e.svc.Create(context.Background(), organizerID, CreateEventParams{
		Name: "Cube Night", StartsAt: e.clock.Now().Add(7 * 24 * time.Hour),
		FeeCents: feeCents, MaxParticipants: maxParticipants,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func (e *testEnv) publish(t *testing.T, eventID uuid.UUID) {
	t.Helper()
	if _, err := e.svc.Transition(context.Background(), eventID, "publish"); err != nil {
		t.Fatal(err)
	}
}

// ---- Task 3 tests ----

func TestCreatePaidEventRequiresStripe(t *testing.T) {
	env := newTestEnv(t)
	env.stripe.configured = false
	org := env.seedUser(t, "org@test")
	_, err := env.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Paid Night", StartsAt: env.clock.Now(), FeeCents: 5000, MaxParticipants: 8,
	})
	if !errors.Is(err, ErrPaymentsUnconfigured) {
		t.Fatalf("want ErrPaymentsUnconfigured, got %v", err)
	}
	// Free events work without Stripe.
	if _, err := env.svc.Create(context.Background(), org, CreateEventParams{
		Name: "Free Night", StartsAt: env.clock.Now(), FeeCents: 0, MaxParticipants: 8,
	}); err != nil {
		t.Fatalf("free event should not need stripe: %v", err)
	}
}

func TestUpdateFieldWhitelist(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 8)

	// Draft: everything editable.
	newFee := int32(6000)
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{FeeCents: &newFee}); err != nil {
		t.Fatalf("draft fee edit: %v", err)
	}

	env.publish(t, ev.ID)

	// Published: fee locked, description fine.
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{FeeCents: &newFee}); !errors.Is(err, ErrEventLocked) {
		t.Fatalf("want ErrEventLocked, got %v", err)
	}
	desc := "bring sleeves"
	got, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{Description: &desc})
	if err != nil || got.Description != desc {
		t.Fatalf("published description edit: %v (%+v)", err, got)
	}
}

func TestTransitions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 0, 8)

	if _, err := env.svc.Transition(ctx, ev.ID, "start"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("draft→started must fail, got %v", err)
	}
	if _, err := env.svc.Transition(ctx, ev.ID, "publish"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Transition(ctx, ev.ID, "publish"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("double publish must fail, got %v", err)
	}
	if _, err := env.svc.Transition(ctx, ev.ID, "start"); err != nil {
		t.Fatal(err)
	}
	got, err := env.svc.Transition(ctx, ev.ID, "finish")
	if err != nil || got.Status != "finished" {
		t.Fatalf("finish: %v (%+v)", err, got)
	}
}

func TestPublishPaidEventRequiresStripe(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 8)
	env.stripe.configured = false
	if _, err := env.svc.Transition(context.Background(), ev.ID, "publish"); !errors.Is(err, ErrPaymentsUnconfigured) {
		t.Fatalf("want ErrPaymentsUnconfigured, got %v", err)
	}
}

func TestSetCubesValidation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	other := env.seedUser(t, "other@test")
	ev := env.createEvent(t, org, 0, 8)

	pubCube := env.seedCube(t, other, "Public Cube", "public")
	privOther := env.seedCube(t, other, "Private Other", "private")
	privOwn := env.seedCube(t, org, "Private Own", "private")

	// Public + organizer-owned private are fine; foreign private is not.
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: pubCube}, {CubeID: privOwn}}); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: privOther}}); !errors.Is(err, ErrInvalidEventCube) {
		t.Fatalf("want ErrInvalidEventCube, got %v", err)
	}

	// A pin must belong to its cube.
	var changeID uuid.UUID
	if err := env.pool.QueryRow(ctx,
		`insert into cube_changes (cube_id, version, author_id) values ($1, 1, $2) returning id`,
		pubCube, other).Scan(&changeID); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: privOwn, CubeChangeID: &changeID}}); !errors.Is(err, ErrInvalidEventCube) {
		t.Fatalf("cross-cube pin must fail, got %v", err)
	}
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: pubCube, CubeChangeID: &changeID}}); err != nil {
		t.Fatal(err)
	}

	// Replace-set semantics: the previous two links are gone.
	detail, err := env.svc.GetDetail(ctx, ev.ID, org, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Cubes) != 1 || detail.Cubes[0].CubeID != pubCube {
		t.Fatalf("expected exactly the pinned pubCube link, got %+v", detail.Cubes)
	}

	// Locked after publish.
	env.publish(t, ev.ID)
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: pubCube}}); !errors.Is(err, ErrCubesLocked) {
		t.Fatalf("want ErrCubesLocked, got %v", err)
	}
}

func TestDraftHiddenFromNonAdmins(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	user := env.seedUser(t, "user@test")
	ev := env.createEvent(t, org, 0, 8)

	if _, err := env.svc.GetDetail(ctx, ev.ID, user, false); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("draft must 404 for non-admin, got %v", err)
	}
	if _, err := env.svc.GetDetail(ctx, ev.ID, org, true); err != nil {
		t.Fatal(err)
	}
	list, err := env.svc.List(ctx, user, false)
	if err != nil || len(list) != 0 {
		t.Fatalf("draft must be absent from non-admin list: %v %+v", err, list)
	}
	list, err = env.svc.List(ctx, org, true)
	if err != nil || len(list) != 1 {
		t.Fatalf("admin list must include draft: %v %+v", err, list)
	}
}

// ---- Task 4 tests ----

func (e *testEnv) register(t *testing.T, eventID, userID uuid.UUID) *db.Registration {
	t.Helper()
	reg, err := e.svc.Register(context.Background(), eventID, userID)
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestRegisterPaidEvent(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)

	reg := env.register(t, ev.ID, alice)
	if reg.Status != "pending_payment" {
		t.Fatalf("want pending_payment, got %s", reg.Status)
	}
	wantExpiry := env.clock.Now().Add(PaymentWindow)
	if reg.ExpiresAt == nil || !reg.ExpiresAt.Equal(wantExpiry) {
		t.Fatalf("want expires_at %v, got %v", wantExpiry, reg.ExpiresAt)
	}
	// Duplicate active registration is rejected.
	if _, err := env.svc.Register(context.Background(), ev.ID, alice); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("want ErrAlreadyRegistered, got %v", err)
	}
}

func TestRegisterFreeEventConfirmsInstantly(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 0, 2)
	env.publish(t, ev.ID)

	reg := env.register(t, ev.ID, alice)
	if reg.Status != "paid" || reg.PaidAt == nil {
		t.Fatalf("free event must confirm instantly, got %+v", reg)
	}
	if len(env.mailer.sent) != 1 || env.mailer.sent[0].to != "alice@test" {
		t.Fatalf("want one confirmation email to alice, got %+v", env.mailer.sent)
	}
}

func TestRegisterFullEventWaitlists(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	c := env.seedUser(t, "c@test")
	env.register(t, ev.ID, a) // takes the spot (pending_payment)
	rb := env.register(t, ev.ID, b)
	rc := env.register(t, ev.ID, c)
	if rb.Status != "waitlisted" || rb.WaitlistPos == nil || *rb.WaitlistPos != 1 {
		t.Fatalf("b should be waitlist #1, got %+v", rb)
	}
	if rc.Status != "waitlisted" || rc.WaitlistPos == nil || *rc.WaitlistPos != 2 {
		t.Fatalf("c should be waitlist #2, got %+v", rc)
	}
}

func TestExpiryPromotesWaitlist(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a)
	env.register(t, ev.ID, b)

	env.clock.Advance(PaymentWindow + time.Minute)
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}

	gotA, err := env.q.GetRegistration(ctx, ra.ID)
	if err != nil || gotA.Status != "expired" {
		t.Fatalf("a should be expired: %v %+v", err, gotA)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" || gotB.ExpiresAt == nil {
		t.Fatalf("b should be promoted to pending_payment: %v %+v", err, gotB)
	}
	// Promotion email with a pay-by deadline went to b.
	found := false
	for _, m := range env.mailer.sent {
		found = found || m.to == "b@test"
	}
	if !found {
		t.Fatalf("want promotion email to b, got %+v", env.mailer.sent)
	}

	// a can re-register after expiry (partial unique index only covers active rows).
	if _, err := env.svc.Register(ctx, ev.ID, a); err != nil {
		t.Fatalf("re-register after expiry: %v", err)
	}
}

func TestFreeEventPromotionConfirmsInstantly(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 0, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.register(t, ev.ID, a) // paid instantly (free event)
	rb := env.register(t, ev.ID, b)
	if rb.Status != "waitlisted" {
		t.Fatalf("want waitlisted, got %+v", rb)
	}

	// Free the spot via start-agnostic path: directly cancel a's row in Task 6;
	// here simulate by expiring via SQL is impossible (paid rows don't expire),
	// so use the sweeper path after flipping a to pending manually. The
	// overdue threshold is anchored to the fake clock (not the DB's real
	// wall clock) so this doesn't depend on time-of-day the suite runs at.
	pastExpiry := env.clock.Now().Add(-time.Hour)
	if _, err := env.pool.Exec(ctx,
		`update registrations set status = 'pending_payment', expires_at = $3, paid_at = null
		 where event_id = $1 and user_id = $2`, ev.ID, a, pastExpiry); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "paid" || gotB.PaidAt == nil {
		t.Fatalf("free-event promotion must confirm instantly: %v %+v", err, gotB)
	}
}

func TestMultipleFreedSpotsPromoteMultiple(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)

	users := make([]uuid.UUID, 4)
	for i, email := range []string{"a@test", "b@test", "c@test", "d@test"} {
		users[i] = env.seedUser(t, email)
		env.register(t, ev.ID, users[i])
	}
	// a, b hold spots; c, d waitlisted. Both spots expire → both promote in order.
	env.clock.Advance(PaymentWindow + time.Minute)
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	for _, u := range users[2:] {
		got, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: u})
		if err != nil || got.Status != "pending_payment" {
			t.Fatalf("waitlisted user should be promoted: %v %+v", err, got)
		}
	}
}

func TestRegisterRequiresPublished(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")

	draft := env.createEvent(t, org, 0, 8)
	if _, err := env.svc.Register(ctx, draft.ID, alice); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("draft register must 404 (no leak), got %v", err)
	}

	started := env.createEvent(t, org, 0, 8)
	env.publish(t, started.ID)
	if _, err := env.svc.Transition(ctx, started.ID, "start"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Register(ctx, started.ID, alice); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("started register must be closed, got %v", err)
	}
}

func TestStartClosesRegistration(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)

	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a) // pending_payment
	rb := env.register(t, ev.ID, b) // waitlisted

	if _, err := env.svc.Transition(ctx, ev.ID, "start"); err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	gotB, _ := env.q.GetRegistration(ctx, rb.ID)
	if gotA.Status != "expired" || gotB.Status != "cancelled" {
		t.Fatalf("start must expire pending and cancel waitlist, got %s / %s", gotA.Status, gotB.Status)
	}
}
