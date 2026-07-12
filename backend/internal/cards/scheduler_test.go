package cards

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestRunSchedulerSyncsOnStartAndStopsOnCancel(t *testing.T) {
	pool := testdb.New(t)
	f := newFakeScryfall(t)
	f.cards = []scryfallCard{fixtureCard(idA, oracleA, "Alpha Strike")}
	syncer := NewSyncer(pool, NewScryfallClient(f.srv.URL, "cube-planner/test"), slog.Default())
	q := db.New(pool)
	ctx := context.Background()

	// Simulate a crash mid-sync: a stale 'running' row.
	if _, err := q.CreateSyncRun(ctx); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		syncer.RunScheduler(runCtx, time.Hour)
		close(done)
	}()

	// The startup sync lands promptly.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if n, _ := q.CountCards(ctx); n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup sync did not import within 30s")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The stale running row was failed before syncing.
	var staleFailed int
	if err := pool.QueryRow(
		ctx,
		"select count(*) from card_sync_runs where status = 'failed' and error = 'interrupted (process restart)'",
	).Scan(&staleFailed); err != nil {
		t.Fatal(err)
	}
	if staleFailed != 1 {
		t.Fatalf("stale failed runs = %d, want 1", staleFailed)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not stop on cancel")
	}
}
