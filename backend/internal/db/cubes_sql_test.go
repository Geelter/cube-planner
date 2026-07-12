package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func seedCubeUser(t *testing.T, q *db.Queries) db.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), db.CreateUserParams{
		Email: uuid.NewString() + "@x.y", DisplayName: "Owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCubeCardUpsertAndDeplete(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()
	owner := seedCubeUser(t, q)

	cube, err := q.CreateCube(ctx, db.CreateCubeParams{
		OwnerID: owner.ID, Name: "Test Cube", Description: "", Visibility: "public",
	})
	if err != nil {
		t.Fatal(err)
	}

	scry, oracle := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `insert into cards (
		scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
		set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
		oracle_text, colors, color_identity, promo
	) values ($1, $2, 'Bolt', 'bolt', '2020-01-01', 'tst', 'Test', '1',
		'common', 'normal', '{R}', 1, 'Instant', '', '{R}', '{R}', false)`,
		scry, oracle)
	if err != nil {
		t.Fatal(err)
	}

	add := db.AddCubeCardParams{CubeID: cube.ID, OracleID: oracle, ScryfallID: scry, Quantity: 1}
	if err := q.AddCubeCard(ctx, add); err != nil {
		t.Fatal(err)
	}
	add.Quantity = 2
	if err := q.AddCubeCard(ctx, add); err != nil {
		t.Fatal(err) // upsert must increment, not conflict
	}
	rows, err := q.GetCubeCardQuantities(ctx, cube.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Quantity != 3 {
		t.Fatalf("quantities = %+v, want one row qty 3", rows)
	}

	if err := q.RemoveCubeCardQuantity(ctx, db.RemoveCubeCardQuantityParams{
		CubeID: cube.ID, OracleID: oracle, Quantity: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := q.DeleteDepletedCubeCards(ctx, cube.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ = q.GetCubeCardQuantities(ctx, cube.ID)
	if len(rows) != 0 {
		t.Fatalf("rows after depletion = %d, want 0", len(rows))
	}
}

func TestSyncDeleteKeepsCubedCards(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()
	owner := seedCubeUser(t, q)

	cube, err := q.CreateCube(ctx, db.CreateCubeParams{
		OwnerID: owner.ID, Name: "Keep", Description: "", Visibility: "private",
	})
	if err != nil {
		t.Fatal(err)
	}
	scry, oracle := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `insert into cards (
		scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
		set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
		oracle_text, colors, color_identity, promo
	) values ($1, $2, 'Kept Card', 'kept card', '2020-01-01', 'tst', 'Test',
		'1', 'common', 'normal', '', 0, 'Artifact', '', '{}', '{}', false)`,
		scry, oracle)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.AddCubeCard(ctx, db.AddCubeCardParams{
		CubeID: cube.ID, OracleID: oracle, ScryfallID: scry, Quantity: 1,
	}); err != nil {
		t.Fatal(err)
	}

	// Empty staging: without the guard, the delete would wipe every card.
	if err := q.TruncateCardsStaging(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := q.DeleteCardsMissingFromStaging(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from cards where scryfall_id = $1`, scry).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("cube-referenced card was deleted by sync")
	}
}
