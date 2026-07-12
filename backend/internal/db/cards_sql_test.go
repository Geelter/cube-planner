package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestCardSyncRunLifecycle(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	if _, err := q.GetLastSucceededSyncRun(ctx); err == nil {
		t.Fatal("expected no succeeded run on fresh db")
	}

	id, err := q.CreateSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	count := int32(2)
	if err := q.FinishSyncRunSuccess(ctx, db.FinishSyncRunSuccessParams{
		ID: id, BulkUpdatedAt: &when, CardsCount: &count,
	}); err != nil {
		t.Fatal(err)
	}

	run, err := q.GetLastSucceededSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run.BulkUpdatedAt == nil || !run.BulkUpdatedAt.Equal(when) {
		t.Fatalf("bulk_updated_at = %v, want %v", run.BulkUpdatedAt, when)
	}

	// A 'running' row left behind by a crash gets failed on the next boot.
	id2, err := q.CreateSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.FailStaleRunningSyncRuns(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := pool.QueryRow(ctx, "select status from card_sync_runs where id = $1", id2).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("stale run status = %q, want failed", status)
	}
}

func TestCardsStagingSwap(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	// pg_trgm is installed by the migration.
	var sim float64
	if err := pool.QueryRow(ctx, "select word_similarity('bolt', 'lightning bolt')").Scan(&sim); err != nil {
		t.Fatalf("pg_trgm missing: %v", err)
	}
	if sim < 0.9 {
		t.Fatalf("word_similarity('bolt','lightning bolt') = %v, want ~1", sim)
	}

	insertStaging := func(id uuid.UUID, name string) {
		t.Helper()
		_, err := pool.Exec(ctx, `insert into cards_staging (
			scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
			set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
			oracle_text, colors, color_identity, promo
		) values ($1, $2, $3, lower($3), '2020-01-01', 'tst', 'Test Set', '1',
			'common', 'normal', '{R}', 1, 'Instant', '', '{R}', '{R}', false)`, id, uuid.New(), name)
		if err != nil {
			t.Fatal(err)
		}
	}

	a, b := uuid.New(), uuid.New()
	insertStaging(a, "Alpha Bolt")
	insertStaging(b, "Beta Bolt")
	if n, err := q.UpsertCardsFromStaging(ctx); err != nil || n != 2 {
		t.Fatalf("upsert = (%d, %v), want (2, nil)", n, err)
	}

	// Second run: staging now only holds card A with a new name.
	if err := q.TruncateCardsStaging(ctx); err != nil {
		t.Fatal(err)
	}
	insertStaging(a, "Alpha Bolt Renamed")
	if _, err := q.UpsertCardsFromStaging(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := q.DeleteCardsMissingFromStaging(ctx); err != nil || n != 1 {
		t.Fatalf("delete missing = (%d, %v), want (1, nil)", n, err)
	}
	total, err := q.CountCards(ctx)
	if err != nil || total != 1 {
		t.Fatalf("count = (%d, %v), want (1, nil)", total, err)
	}
	var name string
	if err := pool.QueryRow(ctx, "select name from cards where scryfall_id = $1", a).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Alpha Bolt Renamed" {
		t.Fatalf("name = %q, want updated name", name)
	}
}
