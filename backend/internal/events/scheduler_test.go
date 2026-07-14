package events

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSweepExpiresAcrossEvents(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev1 := env.createEvent(t, org, 5000, 1)
	ev2 := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev1.ID)
	env.publish(t, ev2.ID)
	a := env.seedUser(t, "a@test")
	b := env.seedUser(t, "b@test")
	r1 := env.register(t, ev1.ID, a)
	r2 := env.register(t, ev2.ID, b)

	env.clock.Advance(PaymentWindow + time.Minute)
	if err := env.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{r1.ID, r2.ID} {
		got, err := env.q.GetRegistration(ctx, id)
		if err != nil || got.Status != "expired" {
			t.Fatalf("sweep must expire overdue rows in every event: %v %+v", err, got)
		}
	}
}

func TestSweepSkipsLiveCheckoutSessions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	org := env.seedUser(t, "org@test")
	ev := env.createEvent(t, org, 5000, 1)
	env.publish(t, ev.ID)
	a := env.seedUser(t, "a@test")
	ra := env.register(t, ev.ID, a)

	// Pay with 10 minutes left: the session gets the 30-minute Stripe
	// minimum, outliving the registration window.
	env.clock.Advance(PaymentWindow - 10*time.Minute)
	if _, err := env.svc.Pay(ctx, ev.ID, a); err != nil {
		t.Fatal(err)
	}

	// 15 minutes later the registration window is past but the session
	// is live → the sweeper must NOT expire (the webhook owns this row).
	env.clock.Advance(15 * time.Minute)
	if err := env.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := env.q.GetRegistration(ctx, ra.ID)
	if got.Status != "pending_payment" {
		t.Fatalf("sweep must skip live checkout sessions, got %s", got.Status)
	}

	// Once the session is dead too, the sweeper expires the row.
	env.clock.Advance(20 * time.Minute)
	if err := env.svc.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = env.q.GetRegistration(ctx, ra.ID)
	if got.Status != "expired" {
		t.Fatalf("sweep must expire once the session died, got %s", got.Status)
	}
}

func TestRunSweeperStopsOnCancel(t *testing.T) {
	env := newTestEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		env.svc.RunSweeper(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunSweeper did not stop on context cancel")
	}
}
