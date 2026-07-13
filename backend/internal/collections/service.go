package collections

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/cards"
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

// CardRef is a resolved card reference for import review (a match or a
// suggestion) — display fields, no quantity.
type CardRef struct {
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
}

// ResolvedLine statuses.
const (
	StatusMatched   = "matched"
	StatusAmbiguous = "ambiguous"
	StatusUnmatched = "unmatched"
)

type ResolvedLine struct {
	LineNumber  int32
	Raw         string
	Quantity    int32
	Status      string
	Match       *CardRef
	Suggestions []CardRef
}

// ResolveImport parses pasted text and resolves each line. Pure read:
// nothing is written. Exact (case-insensitive, normalized) name matches
// resolve to the oracle card's representative printing; a name shared by
// several oracle cards falls through to ambiguous; misses get fuzzy
// suggestions or unmatched.
func (s *Service) ResolveImport(ctx context.Context, text string) ([]ResolvedLine, error) {
	lines, err := ParseImportText(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidImport, err)
	}

	nameSet := make(map[string]struct{})
	var names []string
	for _, l := range lines {
		if !l.OK {
			continue
		}
		n := cards.NormalizeName(l.Name)
		if _, seen := nameSet[n]; !seen {
			nameSet[n] = struct{}{}
			names = append(names, n)
		}
	}
	exact := make(map[string][]CardRef)
	if len(names) > 0 {
		rows, err := s.queries.GetCardsByNormalizedNames(ctx, names)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			exact[r.NormalizedName] = append(exact[r.NormalizedName], CardRef{
				ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
				ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
				SetName: r.SetName, CollectorNumber: r.CollectorNumber,
				ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			})
		}
	}

	out := make([]ResolvedLine, len(lines))
	for i, l := range lines {
		rl := ResolvedLine{LineNumber: l.LineNumber, Raw: l.Raw, Quantity: l.Quantity}
		switch {
		case !l.OK:
			rl.Status = StatusUnmatched
		default:
			matches := exact[cards.NormalizeName(l.Name)]
			switch len(matches) {
			case 1:
				rl.Status = StatusMatched
				m := matches[0]
				rl.Match = &m
			case 0:
				// Only misses pay for a fuzzy query.
				suggestions, err := s.suggest(ctx, l.Name)
				if err != nil {
					return nil, err
				}
				if len(suggestions) > 0 {
					rl.Status = StatusAmbiguous
					rl.Suggestions = suggestions
				} else {
					rl.Status = StatusUnmatched
				}
			default:
				// One name, several oracle cards — the user must choose.
				rl.Status = StatusAmbiguous
				rl.Suggestions = matches
			}
		}
		out[i] = rl
	}
	return out, nil
}

func (s *Service) suggest(ctx context.Context, name string) ([]CardRef, error) {
	rows, err := s.queries.SuggestCardsByName(ctx, cards.NormalizeName(name))
	if err != nil {
		return nil, err
	}
	refs := make([]CardRef, len(rows))
	for i, r := range rows {
		refs[i] = CardRef{
			ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
			ManaCost: r.ManaCost, TypeLine: r.TypeLine, SetCode: r.SetCode,
			SetName: r.SetName, CollectorNumber: r.CollectorNumber,
			ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
		}
	}
	return refs, nil
}

type WantlistItem struct {
	OracleID        uuid.UUID
	ScryfallID      uuid.UUID // the cube's chosen printing, for imagery
	Name            string
	ManaCost        string
	ImageSmall      *string
	ImageNormal     *string
	MissingQuantity int32
	CubeQuantity    int32
	OwnedQuantity   int32
}

// Wantlist computes cube-minus-collection at oracle level, on demand,
// never stored. Cube visibility follows the cubes rule: private cubes
// 404 for non-owners (no existence leak). No dependency on the cubes
// package — the same GetCube query enforces it here.
func (s *Service) Wantlist(ctx context.Context, cubeID, userID uuid.UUID) (string, []WantlistItem, int64, error) {
	cube, err := s.queries.GetCube(ctx, cubeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, 0, ErrCubeNotFound
	}
	if err != nil {
		return "", nil, 0, err
	}
	if cube.Visibility == "private" && cube.OwnerID != userID {
		return "", nil, 0, ErrCubeNotFound
	}
	rows, err := s.queries.GetCubeWantlist(ctx, db.GetCubeWantlistParams{
		CubeID: cubeID, UserID: userID,
	})
	if err != nil {
		return "", nil, 0, err
	}
	items := make([]WantlistItem, len(rows))
	var totalMissing int64
	for i, r := range rows {
		items[i] = WantlistItem{
			OracleID: r.OracleID, ScryfallID: r.ScryfallID, Name: r.Name,
			ManaCost: r.ManaCost, ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
			MissingQuantity: r.MissingQuantity, CubeQuantity: r.CubeQuantity,
			OwnedQuantity: r.OwnedQuantity,
		}
		totalMissing += int64(r.MissingQuantity)
	}
	return cube.Name, items, totalMissing, nil
}

type ImportItem struct {
	ScryfallID uuid.UUID
	Quantity   int32
}

// ApplyImport ADDS quantities onto existing rows. In-request duplicates
// are summed first; results clamp at 999; any unknown printing fails the
// whole batch (the review step should never produce one).
func (s *Service) ApplyImport(ctx context.Context, userID uuid.UUID, items []ImportItem) (added, updated int32, err error) {
	merged := make(map[uuid.UUID]int32, len(items))
	order := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		if _, seen := merged[it.ScryfallID]; !seen {
			order = append(order, it.ScryfallID)
		}
		q := merged[it.ScryfallID] + it.Quantity
		if q > MaxItemQuantity {
			q = MaxItemQuantity
		}
		merged[it.ScryfallID] = q
	}

	known, err := s.queries.GetCardsByScryfallIDs(ctx, order)
	if err != nil {
		return 0, 0, err
	}
	if len(known) != len(order) {
		return 0, 0, fmt.Errorf("%w: unknown printing in batch", ErrInvalidImport)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.queries.WithTx(tx)

	owned, err := qtx.GetOwnedScryfallIDs(ctx, db.GetOwnedScryfallIDsParams{
		UserID: userID, Ids: order,
	})
	if err != nil {
		return 0, 0, err
	}
	ownedSet := make(map[uuid.UUID]struct{}, len(owned))
	for _, id := range owned {
		ownedSet[id] = struct{}{}
	}
	for _, id := range order {
		if err := qtx.AddCollectionItem(ctx, db.AddCollectionItemParams{
			UserID: userID, ScryfallID: id, Quantity: merged[id],
		}); err != nil {
			return 0, 0, err
		}
		if _, ok := ownedSet[id]; ok {
			updated++
		} else {
			added++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return added, updated, nil
}
