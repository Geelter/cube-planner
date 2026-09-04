package collections

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

type collectionsEnv struct {
	svc  *Service
	pool *pgxpool.Pool
	q    *db.Queries
}

func newCollectionsEnv(t *testing.T) *collectionsEnv {
	t.Helper()
	pool := testdb.New(t)
	q := db.New(pool)
	return &collectionsEnv{svc: NewService(q, pool), pool: pool, q: q}
}

func (e *collectionsEnv) seedUser(t *testing.T, email string) uuid.UUID {
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

func (e *collectionsEnv) seedCube(t *testing.T, ownerID uuid.UUID, name, visibility string) uuid.UUID {
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

// oracleNamespace derives a deterministic oracle_id from a card name, so
// two seedCard calls for the same name (two printings of one card) share
// an oracle id, the way the real scryfall sync would.
var oracleNamespace = uuid.MustParse("11111111-2222-3333-4444-555555555555")

// seedCard inserts one printing (name/set/collector number) and returns
// its scryfall id. Two calls with the same name share an oracle id, as
// two printings of the same card would.
func (e *collectionsEnv) seedCard(t *testing.T, name, setCode, collectorNumber string) uuid.UUID {
	t.Helper()
	scryfallID := uuid.New()
	oracleID := uuid.NewSHA1(oracleNamespace, []byte(name))
	_, err := e.pool.Exec(context.Background(), `insert into cards (
		scryfall_id, oracle_id, name, normalized_name, released_at, set_code,
		set_name, collector_number, rarity, layout, mana_cost, cmc, type_line,
		oracle_text, colors, color_identity, promo, image_small, image_normal
	) values ($1, $2, $3, $3, '2020-01-01', $4, $4, $5, 'common', 'normal',
		'{R}', 1, 'Instant', '', '{R}', '{R}', false, null, null)`,
		scryfallID, oracleID, name, setCode, collectorNumber)
	if err != nil {
		t.Fatal(err)
	}
	return scryfallID
}

func (e *collectionsEnv) seedCubeCard(t *testing.T, cubeID, scryfallID uuid.UUID, quantity int32) {
	t.Helper()
	var oracleID uuid.UUID
	if err := e.pool.QueryRow(context.Background(),
		`select oracle_id from cards where scryfall_id = $1`, scryfallID).Scan(&oracleID); err != nil {
		t.Fatal(err)
	}
	_, err := e.pool.Exec(context.Background(),
		`insert into cube_cards (cube_id, oracle_id, scryfall_id, quantity) values ($1, $2, $3, $4)`,
		cubeID, oracleID, scryfallID, quantity)
	if err != nil {
		t.Fatal(err)
	}
}

func (e *collectionsEnv) seedCollectionItem(t *testing.T, userID, scryfallID uuid.UUID, quantity int32) {
	t.Helper()
	var oracleID uuid.UUID
	if err := e.pool.QueryRow(context.Background(),
		`select oracle_id from cards where scryfall_id = $1`, scryfallID).Scan(&oracleID); err != nil {
		t.Fatal(err)
	}
	_, err := e.pool.Exec(context.Background(),
		`insert into collection_items (user_id, scryfall_id, oracle_id, quantity) values ($1, $2, $3, $4)`,
		userID, scryfallID, oracleID, quantity)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWantlistPrintingModeIgnoresOtherPrintings(t *testing.T) {
	e := newCollectionsEnv(t)
	owner := e.seedUser(t, "owner@example.com")
	// Two printings of one oracle card.
	leb := e.seedCard(t, "Lightning Bolt", "leb", "162")
	mm2 := e.seedCard(t, "Lightning Bolt", "mm2", "138")

	cube := e.seedCube(t, owner, "Vintage", "public")
	e.seedCubeCard(t, cube, mm2, 1)        // the cube calls for the MM2 printing
	e.seedCollectionItem(t, owner, leb, 1) // the user owns only the LEB one

	// Oracle mode: any printing satisfies the slot.
	_, items, total, err := e.svc.Wantlist(context.Background(), cube, owner, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("oracle mode should be satisfied by any printing, got %d missing", total)
	}

	// Printing mode: the exact printing is missing.
	_, items, total, err = e.svc.Wantlist(context.Background(), cube, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("printing mode should want the MM2 copy, got %d missing", total)
	}
	if items[0].SetCode != "mm2" || items[0].CollectorNumber != "138" {
		t.Fatalf("want mm2/138 on the entry, got %s/%s", items[0].SetCode, items[0].CollectorNumber)
	}
}
