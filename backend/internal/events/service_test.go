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

	"github.com/mjabloniec/cube-planner/backend/internal/cubes"
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

// TestFinishGuardBlocksOpenRound is the events-side unit test for the
// finish guard (5b spec §3.2): a started event with a tournament round
// that is not completed must refuse finish. The tournament package owns
// the end-to-end lifecycle test; this seeds the minimal round row via
// raw SQL since the events package does not depend on tournaments.
func TestFinishGuardBlocksOpenRound(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 0, 8)
	env.publish(t, ev.ID)
	if _, err := env.svc.Transition(ctx, ev.ID, "start"); err != nil {
		t.Fatal(err)
	}

	var tournamentID uuid.UUID
	if err := env.pool.QueryRow(ctx,
		`insert into tournaments (event_id, planned_rounds) values ($1, 1) returning id`,
		ev.ID).Scan(&tournamentID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.pool.Exec(ctx,
		`insert into rounds (tournament_id, number, status, seed) values ($1, 1, 'draft', 1)`,
		tournamentID); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.Transition(ctx, ev.ID, "finish"); !errors.Is(err, ErrRoundOpen) {
		t.Fatalf("finish with open round = %v, want ErrRoundOpen", err)
	}

	if _, err := env.pool.Exec(ctx,
		`update rounds set status = 'completed' where tournament_id = $1`, tournamentID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.Transition(ctx, ev.ID, "finish"); err != nil {
		t.Fatalf("finish after round completed: %v", err)
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

// ---- Task 5 tests ----

func TestPayCreatesCheckoutSession(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	reg := env.register(t, ev.ID, alice)

	url, err := env.svc.Pay(ctx, ev.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if url == "" || len(env.stripe.sessions) != 1 {
		t.Fatalf("want one checkout session, got url=%q sessions=%+v", url, env.stripe.sessions)
	}
	p := env.stripe.sessions[0]
	if p.AmountCents != 5000 || p.Currency != "pln" || p.RegistrationID != reg.ID {
		t.Fatalf("bad checkout params: %+v", p)
	}
	if !p.ExpiresAt.Equal(env.clock.Now().Add(PaymentWindow)) {
		t.Fatalf("session expiry should match the registration window, got %v", p.ExpiresAt)
	}

	// Second Pay reuses the live session — no new Stripe call.
	url2, err := env.svc.Pay(ctx, ev.ID, alice)
	if err != nil || url2 != url {
		t.Fatalf("want reused session url, got %q err=%v", url2, err)
	}
	if len(env.stripe.sessions) != 1 {
		t.Fatalf("no second session should be created, got %d", len(env.stripe.sessions))
	}
}

func TestPaySessionExpiryClampedToStripeMinimum(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	env.register(t, ev.ID, alice)

	// 10 minutes left on the registration — Stripe minimum is 30.
	env.clock.Advance(PaymentWindow - 10*time.Minute)
	if _, err := env.svc.Pay(ctx, ev.ID, alice); err != nil {
		t.Fatal(err)
	}
	want := env.clock.Now().Add(30 * time.Minute)
	if got := env.stripe.sessions[0].ExpiresAt; !got.Equal(want) {
		t.Fatalf("want clamped expiry %v, got %v", want, got)
	}
}

func TestPayValidation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.register(t, ev.ID, a)
	env.register(t, ev.ID, b) // waitlisted

	// Waitlisted can't pay.
	if _, err := env.svc.Pay(ctx, ev.ID, b); !errors.Is(err, ErrRegistrationNotPayable) {
		t.Fatalf("want ErrRegistrationNotPayable, got %v", err)
	}
	// No registration at all.
	nobody := env.seedUser(t, "nobody@test")
	if _, err := env.svc.Pay(ctx, ev.ID, nobody); !errors.Is(err, ErrRegistrationNotFound) {
		t.Fatalf("want ErrRegistrationNotFound, got %v", err)
	}
	// Expired window can't pay.
	env.clock.Advance(PaymentWindow + time.Minute)
	if _, err := env.svc.Pay(ctx, ev.ID, a); !errors.Is(err, ErrRegistrationNotPayable) {
		t.Fatalf("want ErrRegistrationNotPayable after expiry, got %v", err)
	}
}

// ---- Task 6 tests ----

// paidRegistration drives a registration through register → Pay →
// checkout.session.completed, returning the paid row. If the user already
// holds an active pending_payment registration (e.g. promoted from the
// waitlist by an earlier expiry sweep), that row is reused instead of
// registering fresh — Register rejects a second active registration.
func (e *testEnv) paidRegistration(t *testing.T, eventID, userID uuid.UUID) *db.Registration {
	t.Helper()
	ctx := context.Background()
	var reg *db.Registration
	if existing, err := e.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
		EventID: eventID, UserID: userID,
	}); err == nil {
		reg = &existing
	} else {
		reg = e.register(t, eventID, userID)
	}
	if reg.Status != "pending_payment" {
		t.Fatalf("expected pending_payment before pay, got %s", reg.Status)
	}
	if _, err := e.svc.Pay(ctx, eventID, userID); err != nil {
		t.Fatal(err)
	}
	err := e.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID:                "evt_completed_" + reg.ID.String(),
		Type:              "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + reg.ID.String(),
		PaymentIntentID:   "pi_" + reg.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.q.GetRegistration(ctx, reg.ID)
	if err != nil || got.Status != "paid" {
		t.Fatalf("webhook should mark paid: %v %+v", err, got)
	}
	return &got
}

func TestWebhookCompletedIsIdempotent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	reg := env.paidRegistration(t, ev.ID, alice)

	mails := len(env.mailer.sent)
	// Redelivery of the same Stripe event id must no-op.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_completed_" + reg.ID.String(), Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + reg.ID.String(), PaymentIntentID: "pi_" + reg.ID.String(),
	})
	if err != nil || len(env.mailer.sent) != mails {
		t.Fatalf("duplicate delivery must no-op: %v (mails %d→%d)", err, mails, len(env.mailer.sent))
	}
}

func TestSelfCancelWaitlistedAndPending(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.register(t, ev.ID, a) // pending, holds the spot
	env.register(t, ev.ID, b) // waitlisted

	// Waitlisted cancel: no promotion side effects, no refund.
	got, err := env.svc.CancelRegistration(ctx, ev.ID, b)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("waitlist cancel: %v %+v", err, got)
	}
	// Re-waitlist b, then cancel the pending holder → b promotes.
	env.register(t, ev.ID, b)
	got, err = env.svc.CancelRegistration(ctx, ev.ID, a)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("pending cancel: %v %+v", err, got)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted after pending cancel: %v %+v", err, gotB)
	}
	if len(env.stripe.refunds) != 0 {
		t.Fatalf("no refunds expected, got %v", env.stripe.refunds)
	}
}

func TestSelfCancelPaidBeforeDeadlineRefunds(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b) // waitlisted

	got, err := env.svc.CancelRegistration(ctx, ev.ID, a)
	if err != nil || got.Status != "refunded" {
		t.Fatalf("in-window paid cancel: %v %+v", err, got)
	}
	if len(env.stripe.refunds) != 1 || env.stripe.refunds[0] != "pi_"+ra.ID.String() {
		t.Fatalf("want refund of a's payment intent, got %v", env.stripe.refunds)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
}

func TestSelfCancelPaidAfterDeadlineQueuesRefund(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	deadline := env.clock.Now().Add(time.Hour)
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{RefundDeadline: &deadline}); err != nil {
		t.Fatal(err)
	}
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b)

	env.clock.Advance(2 * time.Hour) // past the refund deadline, event not started

	got, err := env.svc.CancelRegistration(ctx, ev.ID, a)
	if err != nil || got.Status != "refund_requested" {
		t.Fatalf("post-deadline cancel must queue: %v %+v", err, got)
	}
	if len(env.stripe.refunds) != 0 {
		t.Fatalf("no automatic refund past deadline, got %v", env.stripe.refunds)
	}
	// Spot freed immediately: b promoted.
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
	// refund_requested blocks re-registering.
	if _, err := env.svc.Register(ctx, ev.ID, a); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("refund_requested must block re-register, got %v", err)
	}
}

func TestOrganizerRefundAndDeny(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 2)
	deadline := env.clock.Now().Add(time.Hour)
	if _, err := env.svc.Update(ctx, ev.ID, UpdateEventParams{RefundDeadline: &deadline}); err != nil {
		t.Fatal(err)
	}
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	rb := env.paidRegistration(t, ev.ID, b)
	env.clock.Advance(2 * time.Hour)

	// Both self-cancel post-deadline → queue.
	if _, err := env.svc.CancelRegistration(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.CancelRegistration(ctx, ev.ID, b); err != nil {
		t.Fatal(err)
	}

	// Organizer approves a → refunded + refund email.
	got, err := env.svc.OrganizerRefund(ctx, ev.ID, ra.ID)
	if err != nil || got.Status != "refunded" {
		t.Fatalf("organizer refund: %v %+v", err, got)
	}
	if len(env.stripe.refunds) != 1 {
		t.Fatalf("want 1 refund, got %v", env.stripe.refunds)
	}
	// Organizer denies b → cancelled + denied email.
	got, err = env.svc.DenyRefund(ctx, ev.ID, rb.ID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("deny refund: %v %+v", err, got)
	}
	deniedMail := env.mailer.sent[len(env.mailer.sent)-1]
	if deniedMail.to != "b@test" {
		t.Fatalf("denied email should go to b, got %+v", deniedMail)
	}
	// Deny on a non-queued row is invalid.
	if _, err := env.svc.DenyRefund(ctx, ev.ID, ra.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("deny on refunded row must fail, got %v", err)
	}
}

func TestOrganizerKickRefundsPaid(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b) // waitlisted

	got, err := env.svc.OrganizerRefund(ctx, ev.ID, ra.ID)
	if err != nil || got.Status != "refunded" {
		t.Fatalf("kick+refund: %v %+v", err, got)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted after kick: %v %+v", err, gotB)
	}
}

func TestEventCancelMassRefundsAndRetries(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	c := env.seedUser(t, "c@test")
	env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b) // pending_payment
	env.register(t, ev.ID, c) // waitlisted

	// First cancel: the refund call fails → a's row stays paid, retryable.
	env.stripe.refundErr = errors.New("stripe is down")
	got, err := env.svc.Transition(ctx, ev.ID, "cancel")
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("cancel: %v %+v", err, got)
	}
	rows, err := env.q.ListEventRegistrations(ctx, ev.ID)
	if err != nil {
		t.Fatal(err)
	}
	statusByUser := map[uuid.UUID]string{}
	for _, r := range rows {
		statusByUser[r.UserID] = r.Status
	}
	if statusByUser[b] != "cancelled" || statusByUser[c] != "cancelled" {
		t.Fatalf("pending+waitlisted must be cancelled: %+v", statusByUser)
	}
	if statusByUser[a] != "paid" {
		t.Fatalf("failed refund must leave the row untouched: %+v", statusByUser)
	}

	// Retry: cancel again with Stripe healthy → a refunded.
	env.stripe.refundErr = nil
	if _, err := env.svc.Transition(ctx, ev.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	rows, _ = env.q.ListEventRegistrations(ctx, ev.ID)
	for _, r := range rows {
		if r.UserID == a && r.Status != "refunded" {
			t.Fatalf("retry must refund a, got %s", r.Status)
		}
	}
	if len(env.stripe.refunds) != 1 {
		t.Fatalf("want exactly one successful refund, got %v", env.stripe.refunds)
	}
}

func TestWebhookExpiredPromotesEarly(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	env.register(t, ev.ID, b)

	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_expired_1", Type: "checkout.session.expired",
		CheckoutSessionID: "cs_test_" + ra.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "expired" {
		t.Fatalf("session expiry must expire the registration, got %s", gotA.Status)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
}

func TestWebhookLatePaymentRefundsWhenFull(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.register(t, ev.ID, a)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	env.register(t, ev.ID, b)

	// a's registration expires; b promotes and pays — the event is full again.
	env.clock.Advance(PaymentWindow + 25*time.Hour) // past a's session expiry too
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	env.paidRegistration(t, ev.ID, b)

	// a's payment lands anyway → immediate refund + apology email.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_late_1", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + ra.ID.String(), PaymentIntentID: "pi_late",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "refunded" {
		t.Fatalf("late payment with no spot must refund, got %s", gotA.Status)
	}
	if len(env.stripe.refunds) != 1 || env.stripe.refunds[0] != "pi_late" {
		t.Fatalf("want pi_late refunded, got %v", env.stripe.refunds)
	}
}

func TestWebhookLatePaymentReclaimsFreeSpot(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	ra := env.register(t, ev.ID, a)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}
	env.clock.Advance(PaymentWindow + 25*time.Hour)
	if err := env.svc.expireAndPromote(ctx, ev.ID); err != nil {
		t.Fatal(err)
	}
	// No waitlist — the spot is still free when the late payment lands.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_late_2", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + ra.ID.String(), PaymentIntentID: "pi_late2",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "paid" {
		t.Fatalf("late payment with a free spot must reclaim it, got %s", gotA.Status)
	}
}

func TestWebhookChargeRefundedSyncsDashboardRefunds(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	ra := env.paidRegistration(t, ev.ID, a)
	env.register(t, ev.ID, b)

	// Organizer refunds from the Stripe dashboard: only the webhook arrives.
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_refund_1", Type: "charge.refunded",
		PaymentIntentID: "pi_" + ra.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	gotA, _ := env.q.GetRegistration(ctx, ra.ID)
	if gotA.Status != "refunded" {
		t.Fatalf("dashboard refund must sync, got %s", gotA.Status)
	}
	gotB, err := env.q.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{EventID: ev.ID, UserID: b})
	if err != nil || gotB.Status != "pending_payment" {
		t.Fatalf("b must be promoted: %v %+v", err, gotB)
	}
}

// ---- whole-branch review fixes (task 19) ----

// Finding 1a: two concurrent Pay calls each create a session but the row
// stores only the last write. A payment through the orphaned sibling must
// still complete via the client_reference_id fallback, and the paying
// session id must be recorded on the row.
func TestWebhookCompletedOrphanSessionFallsBackToClientReference(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	reg := env.register(t, ev.ID, alice)
	if _, err := env.svc.Pay(ctx, ev.ID, alice); err != nil {
		t.Fatal(err)
	}

	// The stored session is cs_test_<reg>; the user paid through a sibling
	// session the row never stored.
	orphanSession := "cs_orphan_" + reg.ID.String()
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_orphan_completed", Type: "checkout.session.completed",
		CheckoutSessionID: orphanSession,
		ClientReferenceID: reg.ID.String(),
		PaymentIntentID:   "pi_orphan",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.q.GetRegistration(ctx, reg.ID)
	if err != nil || got.Status != "paid" {
		t.Fatalf("orphan-session payment must complete: %v %+v", err, got)
	}
	if got.StripePaymentIntentID == nil || *got.StripePaymentIntentID != "pi_orphan" {
		t.Fatalf("payment intent must be recorded, got %v", got.StripePaymentIntentID)
	}
	if got.StripeCheckoutSessionID == nil || *got.StripeCheckoutSessionID != orphanSession {
		t.Fatalf("winning session id must be recorded, got %v", got.StripeCheckoutSessionID)
	}
	// A garbage/unknown reference still just acknowledges.
	err = env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_orphan_unknown", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_nobody", ClientReferenceID: "not-a-uuid",
	})
	if err != nil {
		t.Fatalf("unknown session + bad reference must be acknowledged: %v", err)
	}
}

// Finding 1b: an expired orphan session says nothing about the
// registration — its stored session may still be live and payable, so the
// row must not be expired.
func TestWebhookExpiredOrphanSessionLeavesRegistrationAlone(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	reg := env.register(t, ev.ID, alice)
	if _, err := env.svc.Pay(ctx, ev.ID, alice); err != nil {
		t.Fatal(err)
	}

	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_orphan_expired", Type: "checkout.session.expired",
		CheckoutSessionID: "cs_orphan_" + reg.ID.String(),
		ClientReferenceID: reg.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := env.q.GetRegistration(ctx, reg.ID)
	if err != nil || got.Status != "pending_payment" {
		t.Fatalf("orphan expiry must not touch the registration: %v %+v", err, got)
	}
	if got.StripeCheckoutSessionID == nil || *got.StripeCheckoutSessionID != "cs_test_"+reg.ID.String() {
		t.Fatalf("stored live session must survive, got %v", got.StripeCheckoutSessionID)
	}
	// The stored session expiring still works (control).
	err = env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_stored_expired", Type: "checkout.session.expired",
		CheckoutSessionID: "cs_test_" + reg.ID.String(),
		ClientReferenceID: reg.ID.String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = env.q.GetRegistration(ctx, reg.ID)
	if got.Status != "expired" {
		t.Fatalf("stored-session expiry must expire the row, got %s", got.Status)
	}
}

// Finding 4: Pay on a draft must 404 like Register/GetDetail/Cancel — a
// registration-not-found here would leak the draft's existence.
func TestPayDraftEventHidden(t *testing.T) {
	env := newTestEnv(t)
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2) // stays draft
	if _, err := env.svc.Pay(context.Background(), ev.ID, alice); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("draft pay must 404 (no leak), got %v", err)
	}
}

// Finding 2: deleting a cube linked to an event must succeed and simply
// drop the link (FK is on delete cascade as of migration 00007).
func TestCubeDeletionCascadesEventLink(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 0, 8)
	cubeID := env.seedCube(t, org, "Doomed Cube", "public")
	if err := env.svc.SetCubes(ctx, ev.ID, []CubeLinkInput{{CubeID: cubeID}}); err != nil {
		t.Fatal(err)
	}

	if err := cubes.NewService(env.q, env.pool).Delete(ctx, cubeID, org); err != nil {
		t.Fatalf("cube delete must succeed despite the event link: %v", err)
	}
	var n int
	if err := env.pool.QueryRow(ctx,
		`select count(*) from event_cubes where cube_id = $1`, cubeID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("event_cubes row must cascade away, %d left", n)
	}
	detail, err := env.svc.GetDetail(ctx, ev.ID, org, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Cubes) != 0 {
		t.Fatalf("event detail must no longer list the cube, got %+v", detail.Cubes)
	}
}

func TestWebhookDuplicateChargeRefundedWithoutTouchingRegistration(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	reg := env.register(t, ev.ID, alice)
	if _, err := env.svc.Pay(ctx, ev.ID, alice); err != nil {
		t.Fatal(err)
	}

	// The stored session completes: registration becomes paid on pi_first.
	if err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_dup_a", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + reg.ID.String(), PaymentIntentID: "pi_first",
	}); err != nil {
		t.Fatal(err)
	}

	// An orphaned sibling session from a Pay race also completes: the user
	// was charged twice. The duplicate charge must be refunded and the
	// paid row must keep pointing at the winning session/intent, or a
	// dashboard refund of pi_first would no longer match the row.
	if err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_dup_b", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_orphan_sibling", ClientReferenceID: reg.ID.String(),
		PaymentIntentID: "pi_second",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := env.q.GetRegistration(ctx, reg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paid" {
		t.Fatalf("duplicate charge must not change status, got %s", got.Status)
	}
	if got.StripePaymentIntentID == nil || *got.StripePaymentIntentID != "pi_first" {
		t.Fatalf("intent pointer must keep the winning charge, got %v", got.StripePaymentIntentID)
	}
	if got.StripeCheckoutSessionID == nil || *got.StripeCheckoutSessionID != "cs_test_"+reg.ID.String() {
		t.Fatalf("session pointer must keep the winning session, got %v", got.StripeCheckoutSessionID)
	}
	if len(env.stripe.refunds) != 1 || env.stripe.refunds[0] != "pi_second" {
		t.Fatalf("duplicate intent must be auto-refunded, got %v", env.stripe.refunds)
	}
}

func TestWebhookLatePaymentAfterReRegisterRefundsInsteadOf500(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	alice := env.seedUser(t, "alice@test")
	ev := env.createEvent(t, org, 5000, 2)
	env.publish(t, ev.ID)
	oldReg := env.register(t, ev.ID, alice)
	if _, err := env.svc.Pay(ctx, ev.ID, alice); err != nil {
		t.Fatal(err)
	}
	// Alice cancels, then registers again: a new active row exists.
	if _, err := env.svc.CancelRegistration(ctx, ev.ID, alice); err != nil {
		t.Fatal(err)
	}
	newReg := env.register(t, ev.ID, alice)

	// The old session was still live at Stripe and gets paid. Reclaiming
	// the cancelled row would collide with registrations_one_active_idx —
	// the webhook must refund-with-apology instead of erroring (a
	// returned error would make Stripe retry the delivery forever).
	err := env.svc.HandleWebhookEvent(ctx, WebhookEvent{
		ID: "evt_rereg_late", Type: "checkout.session.completed",
		CheckoutSessionID: "cs_test_" + oldReg.ID.String(), ClientReferenceID: oldReg.ID.String(),
		PaymentIntentID: "pi_old_session",
	})
	if err != nil {
		t.Fatalf("webhook must not error on re-register collision: %v", err)
	}
	if len(env.stripe.refunds) != 1 || env.stripe.refunds[0] != "pi_old_session" {
		t.Fatalf("old session's charge must be refunded, got %v", env.stripe.refunds)
	}
	gotOld, _ := env.q.GetRegistration(ctx, oldReg.ID)
	if gotOld.Status != "refunded" {
		t.Fatalf("old registration must end refunded, got %s", gotOld.Status)
	}
	gotNew, _ := env.q.GetRegistration(ctx, newReg.ID)
	if gotNew.Status != "pending_payment" {
		t.Fatalf("new registration must be untouched, got %s", gotNew.Status)
	}
}
