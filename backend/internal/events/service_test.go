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
