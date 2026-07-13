package collections

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// Service owns collection CRUD, import resolution, and the wantlist.
// Collections are strictly private: every method operates on the
// session user's rows only.
type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewService(queries *db.Queries, pool *pgxpool.Pool) *Service {
	return &Service{queries: queries, pool: pool}
}

var (
	// ErrInvalidItem covers unknown printings, oracle mismatches, and
	// change-printing misuse — the whole 422 invalid-collection-item family.
	ErrInvalidItem = errors.New("invalid collection item")
	// ErrInvalidImport covers bad import batches (unknown ids, too many lines).
	ErrInvalidImport = errors.New("invalid import")
	// ErrCubeNotFound also covers "exists but private and you are not the
	// owner" — private cubes must not leak their existence (same rule as
	// the cubes package).
	ErrCubeNotFound = errors.New("cube not found")
)

// ItemEntry is one collection line with card display data.
type ItemEntry struct {
	ScryfallID      uuid.UUID
	OracleID        uuid.UUID
	Name            string
	ManaCost        string
	TypeLine        string
	SetCode         string
	SetName         string
	CollectorNumber string
	ImageSmall      *string
	ImageNormal     *string
	Quantity        int32
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, search string, limit, offset int32) ([]ItemEntry, int64, int64, error) {
	var q *string
	if search != "" {
		q = &search
	}
	rows, err := s.queries.ListCollectionItems(ctx, db.ListCollectionItemsParams{
		UserID: userID, Search: q, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, 0, 0, err
	}
	var total, copies int64
	if len(rows) > 0 {
		total, copies = rows[0].Total, rows[0].TotalCopies
	}
	entries := make([]ItemEntry, len(rows))
	for i, r := range rows {
		entries[i] = ItemEntry{
			ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
			ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
			SetName: r.SetName, CollectorNumber: r.CollectorNumber,
			ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			Quantity: r.Quantity,
		}
	}
	return entries, total, copies, nil
}

// SetQuantity is the idempotent set-quantity upsert; 0 deletes the row
// (nil entry, no error — deleting an absent row is a no-op by design).
func (s *Service) SetQuantity(ctx context.Context, userID, scryfallID uuid.UUID, quantity int32) (*ItemEntry, error) {
	if quantity == 0 {
		_, err := s.queries.DeleteCollectionItem(ctx, db.DeleteCollectionItemParams{
			UserID: userID, ScryfallID: scryfallID,
		})
		return nil, err
	}
	_, err := s.queries.UpsertCollectionItem(ctx, db.UpsertCollectionItemParams{
		UserID: userID, ScryfallID: scryfallID, Quantity: quantity,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidItem
	}
	if err != nil {
		return nil, err
	}
	return s.getEntry(ctx, userID, scryfallID)
}

func (s *Service) getEntry(ctx context.Context, userID, scryfallID uuid.UUID) (*ItemEntry, error) {
	r, err := s.queries.GetCollectionItemEntry(ctx, db.GetCollectionItemEntryParams{
		UserID: userID, ScryfallID: scryfallID,
	})
	if err != nil {
		return nil, err
	}
	return &ItemEntry{
		ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
		ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
		SetName: r.SetName, CollectorNumber: r.CollectorNumber,
		ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
		Quantity: r.Quantity,
	}, nil
}

// ChangePrinting re-keys an owned printing to another printing of the
// same oracle card in one transaction: add the source quantity onto the
// target row (clamped at 999), delete the source. Atomic so a torn pair
// can never leave a duplicated quantity.
func (s *Service) ChangePrinting(ctx context.Context, userID, fromID, toID uuid.UUID) (*ItemEntry, error) {
	if fromID == toID {
		// A naive add-then-delete on the same row would lose the row.
		return nil, fmt.Errorf("%w: target is the same printing", ErrInvalidItem)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.queries.WithTx(tx)

	src, err := qtx.GetCollectionItemForUpdate(ctx, db.GetCollectionItemForUpdateParams{
		UserID: userID, ScryfallID: fromID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: printing not in collection", ErrInvalidItem)
	}
	if err != nil {
		return nil, err
	}
	targets, err := qtx.GetCardsByScryfallIDs(ctx, []uuid.UUID{toID})
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("%w: unknown target printing", ErrInvalidItem)
	}
	if targets[0].OracleID != src.OracleID {
		return nil, fmt.Errorf("%w: target is a different card", ErrInvalidItem)
	}
	if err := qtx.AddCollectionItem(ctx, db.AddCollectionItemParams{
		UserID: userID, ScryfallID: toID, Quantity: src.Quantity,
	}); err != nil {
		return nil, err
	}
	if _, err := qtx.DeleteCollectionItem(ctx, db.DeleteCollectionItemParams{
		UserID: userID, ScryfallID: fromID,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.getEntry(ctx, userID, toID)
}
