package cards

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// fakeScryfall serves a mutable bulk file: metadata at
// /bulk-data/default-cards, the array itself at /download.
type fakeScryfall struct {
	srv          *httptest.Server
	updatedAt    time.Time
	cards        []scryfallCard
	failDownload bool
}

func newFakeScryfall(t *testing.T) *fakeScryfall {
	t.Helper()
	f := &fakeScryfall{updatedAt: time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)}
	mux := http.NewServeMux()
	mux.HandleFunc("/bulk-data/default-cards", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"updated_at":   f.updatedAt,
			"download_uri": f.srv.URL + "/download",
		})
	})
	mux.HandleFunc("/download", func(w http.ResponseWriter, _ *http.Request) {
		if f.failDownload {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(f.cards)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func fixtureCard(id, oracleID, name string) scryfallCard {
	return scryfallCard{
		ID:              id,
		OracleID:        oracleID,
		Name:            name,
		ReleasedAt:      "2020-01-01",
		Set:             "tst",
		SetName:         "Test Set",
		SetType:         "expansion",
		CollectorNumber: "1",
		Rarity:          "common",
		Layout:          "normal",
		ManaCost:        "{R}",
		CMC:             1,
		TypeLine:        "Instant",
		OracleText:      "Test.",
		Colors:          []string{"R"},
		ColorIdentity:   []string{"R"},
		Games:           []string{"paper"},
	}
}

const (
	idA     = "11111111-1111-1111-1111-111111111111"
	idB     = "22222222-2222-2222-2222-222222222222"
	idC     = "33333333-3333-3333-3333-333333333333"
	oracleA = "aaaaaaaa-1111-1111-1111-111111111111"
	oracleB = "aaaaaaaa-2222-2222-2222-222222222222"
	oracleC = "aaaaaaaa-3333-3333-3333-333333333333"
)

func newSyncTest(t *testing.T) (*fakeScryfall, *Syncer, *db.Queries, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	f := newFakeScryfall(t)
	syncer := NewSyncer(pool, NewScryfallClient(f.srv.URL, "cube-planner/test"), slog.Default())
	return f, syncer, db.New(pool), pool
}

func TestSyncImportsFiltersAndSkips(t *testing.T) {
	f, syncer, q, _ := newSyncTest(t)
	ctx := context.Background()

	token := fixtureCard(idC, oracleC, "Goblin Token")
	token.Layout = "token"
	f.cards = []scryfallCard{
		fixtureCard(idA, oracleA, "Alpha Strike"),
		fixtureCard(idB, oracleB, "Beta Blast"),
		token,
	}

	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := q.CountCards(ctx); n != 2 {
		t.Fatalf("cards = %d, want 2 (token filtered)", n)
	}
	run, err := q.GetLastSucceededSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run.CardsCount == nil || *run.CardsCount != 2 {
		t.Fatalf("cards_count = %v, want 2", run.CardsCount)
	}

	// Same updated_at → skip: no new sync run appears.
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	run2, err := q.GetLastSucceededSyncRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if run2.ID != run.ID {
		t.Fatalf("second sync created run %d, want skip (still %d)", run2.ID, run.ID)
	}
}

func TestSyncUpsertsAndDeletes(t *testing.T) {
	f, syncer, q, pool := newSyncTest(t)
	ctx := context.Background()

	f.cards = []scryfallCard{
		fixtureCard(idA, oracleA, "Alpha Strike"),
		fixtureCard(idB, oracleB, "Beta Blast"),
	}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// Next day: A renamed, B gone, C new.
	f.updatedAt = f.updatedAt.Add(24 * time.Hour)
	f.cards = []scryfallCard{
		fixtureCard(idA, oracleA, "Alpha Strike, Renamed"),
		fixtureCard(idC, oracleC, "Gamma Ray"),
	}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	if n, _ := q.CountCards(ctx); n != 2 {
		t.Fatalf("cards = %d, want 2", n)
	}
	var name string
	if err := pool.QueryRow(ctx, "select name from cards where scryfall_id = $1", idA).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Alpha Strike, Renamed" {
		t.Fatalf("name = %q, want renamed", name)
	}
	var exists bool
	_ = pool.QueryRow(ctx, "select exists(select 1 from cards where scryfall_id = $1)", idB).Scan(&exists)
	if exists {
		t.Fatal("card B must have been deleted")
	}
}

func TestSyncFailureLeavesCardsIntact(t *testing.T) {
	f, syncer, q, _ := newSyncTest(t)
	ctx := context.Background()

	f.cards = []scryfallCard{fixtureCard(idA, oracleA, "Alpha Strike")}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	f.updatedAt = f.updatedAt.Add(24 * time.Hour)
	f.failDownload = true
	if err := syncer.Sync(ctx); err == nil {
		t.Fatal("want error when download fails")
	}
	if n, _ := q.CountCards(ctx); n != 1 {
		t.Fatalf("cards = %d, want 1 (previous data intact)", n)
	}
}

func TestSyncRefusesEmptyBulkFile(t *testing.T) {
	f, syncer, q, _ := newSyncTest(t)
	ctx := context.Background()

	f.cards = []scryfallCard{fixtureCard(idA, oracleA, "Alpha Strike")}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	f.updatedAt = f.updatedAt.Add(24 * time.Hour)
	f.cards = nil
	err := syncer.Sync(ctx)
	if !errors.Is(err, ErrEmptyBulkFile) {
		t.Fatalf("err = %v, want ErrEmptyBulkFile", err)
	}
	if n, _ := q.CountCards(ctx); n != 1 {
		t.Fatalf("cards = %d, want 1 (never wiped by empty file)", n)
	}
}

func TestSyncKeepsCollectionReferencedCards(t *testing.T) {
	f, syncer, _, pool := newSyncTest(t)
	ctx := context.Background()

	f.cards = []scryfallCard{
		fixtureCard(idA, oracleA, "Alpha Strike"),
		fixtureCard(idB, oracleB, "Beta Blast"),
	}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	// A user owns copies of B; B then vanishes from the bulk file.
	var userID uuid.UUID
	if err := pool.QueryRow(
		ctx,
		`insert into users (email, display_name) values ('owner@test', 'Owner') returning id`,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`insert into collection_items (user_id, scryfall_id, oracle_id, quantity)
		 values ($1, $2, $3, 2)`, userID, idB, oracleB); err != nil {
		t.Fatal(err)
	}

	f.updatedAt = f.updatedAt.Add(24 * time.Hour)
	f.cards = []scryfallCard{fixtureCard(idA, oracleA, "Alpha Strike")}
	if err := syncer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	var exists bool
	if err := pool.QueryRow(ctx,
		`select exists(select 1 from cards where scryfall_id = $1)`, idB).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("card B is in a collection and must survive the sync delete")
	}
}
