package cubes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// Service owns cube CRUD, visibility rules, and the diff-commit
// transaction. Viewer identity is a plain uuid.UUID; uuid.Nil means
// anonymous.
type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
}

func NewService(queries *db.Queries, pool *pgxpool.Pool) *Service {
	return &Service{queries: queries, pool: pool}
}

var (
	// ErrNotFound also covers "exists but private and you are not the
	// owner" — private cubes must not leak their existence.
	ErrNotFound        = errors.New("cube not found")
	ErrForbidden       = errors.New("not the cube owner")
	ErrVersionConflict = errors.New("cube version conflict")
)

type UpdateParams struct {
	Name        *string
	Description *string
	Visibility  *string
}

type AddInput struct {
	ScryfallID uuid.UUID
	Quantity   int32
}

type RemoveInput struct {
	OracleID uuid.UUID
	Quantity int32
}

// CardEntry is one cube list line with card display data, used for both
// the current list and reconstructed past states.
type CardEntry struct {
	ScryfallID    uuid.UUID
	OracleID      uuid.UUID
	Name          string
	ManaCost      string
	TypeLine      string
	Rarity        string
	Cmc           float64
	Colors        []string
	ColorIdentity []string
	ImageSmall    *string
	ImageNormal   *string
	Quantity      int32
}

type ChangeItem struct {
	OracleID uuid.UUID
	Name     string
	Quantity int32
}

type ChangeEntry struct {
	ID         uuid.UUID
	Version    int32
	AuthorName string
	Note       string
	CreatedAt  time.Time
	Adds       []ChangeItem
	Removes    []ChangeItem
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name, description, visibility string) (db.GetCubeRow, error) {
	cube, err := s.queries.CreateCube(ctx, db.CreateCubeParams{
		OwnerID: ownerID, Name: name, Description: description, Visibility: visibility,
	})
	if err != nil {
		return db.GetCubeRow{}, err
	}
	return s.queries.GetCube(ctx, cube.ID)
}

func (s *Service) Get(ctx context.Context, cubeID, viewerID uuid.UUID) (db.GetCubeRow, error) {
	row, err := s.queries.GetCube(ctx, cubeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetCubeRow{}, ErrNotFound
	}
	if err != nil {
		return db.GetCubeRow{}, err
	}
	if row.Visibility == "private" && row.OwnerID != viewerID {
		return db.GetCubeRow{}, ErrNotFound
	}
	return row, nil
}

// getOwned loads a cube for a mutation: private+non-owner → ErrNotFound
// (no existence leak), public+non-owner → ErrForbidden.
func (s *Service) getOwned(ctx context.Context, cubeID, viewerID uuid.UUID) (db.GetCubeRow, error) {
	row, err := s.Get(ctx, cubeID, viewerID)
	if err != nil {
		return db.GetCubeRow{}, err
	}
	if row.OwnerID != viewerID {
		return db.GetCubeRow{}, ErrForbidden
	}
	return row, nil
}

func (s *Service) Update(ctx context.Context, cubeID, viewerID uuid.UUID, p UpdateParams) (db.GetCubeRow, error) {
	if _, err := s.getOwned(ctx, cubeID, viewerID); err != nil {
		return db.GetCubeRow{}, err
	}
	if err := s.queries.UpdateCubeMeta(ctx, db.UpdateCubeMetaParams{
		ID: cubeID, Name: p.Name, Description: p.Description, Visibility: p.Visibility,
	}); err != nil {
		return db.GetCubeRow{}, err
	}
	return s.queries.GetCube(ctx, cubeID)
}

func (s *Service) Delete(ctx context.Context, cubeID, viewerID uuid.UUID) error {
	if _, err := s.getOwned(ctx, cubeID, viewerID); err != nil {
		return err
	}
	return s.queries.DeleteCube(ctx, cubeID)
}

func (s *Service) ListPublic(ctx context.Context, query string, limit, offset int32) ([]db.ListPublicCubesRow, int64, error) {
	var q *string
	if query != "" {
		q = &query
	}
	rows, err := s.queries.ListPublicCubes(ctx, db.ListPublicCubesParams{
		Query: q, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if len(rows) > 0 {
		total = rows[0].Total
	}
	return rows, total, nil
}

func (s *Service) ListMine(ctx context.Context, ownerID uuid.UUID) ([]db.ListMyCubesRow, error) {
	return s.queries.ListMyCubes(ctx, ownerID)
}

// GetCards returns the cube list at a version. atVersion -1 (or the
// current version) serves the materialized list; older versions replay
// the changelog backwards. Returns the version actually served.
//
// All reads below must share one snapshot: ApplyChange can commit
// concurrently between two separate pool statements, which would either
// replay a change never applied to the quantities we read (older-version
// branch) or pair a freshly-committed card list with a stale cube.Version
// (current-version branch). A REPEATABLE READ transaction pins one
// snapshot for the whole read; READ COMMITTED (the pool's default) takes
// a fresh snapshot per statement and would not fix this.
func (s *Service) GetCards(ctx context.Context, cubeID, viewerID uuid.UUID, atVersion int32) ([]CardEntry, int32, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only tx; rollback after commit/read is a no-op
	qtx := s.queries.WithTx(tx)

	cube, err := s.getVisible(ctx, qtx, cubeID, viewerID)
	if err != nil {
		return nil, 0, err
	}
	if atVersion > cube.Version {
		return nil, 0, ErrNotFound
	}
	if atVersion == -1 || atVersion == cube.Version {
		rows, err := qtx.GetCubeCards(ctx, cubeID)
		if err != nil {
			return nil, 0, err
		}
		entries := make([]CardEntry, len(rows))
		for i, r := range rows {
			entries[i] = CardEntry{
				ScryfallID: r.ScryfallID, OracleID: r.OracleID, Name: r.Name,
				ManaCost: r.ManaCost, TypeLine: r.TypeLine, Rarity: r.Rarity,
				Cmc: r.Cmc, Colors: r.Colors, ColorIdentity: r.ColorIdentity,
				ImageSmall: r.ImageSmall, ImageNormal: r.ImageNormal,
				Quantity: r.Quantity,
			}
		}
		return entries, cube.Version, nil
	}

	quantities, err := qtx.GetCubeCardQuantities(ctx, cubeID)
	if err != nil {
		return nil, 0, err
	}
	current := make([]Entry, len(quantities))
	for i, q := range quantities {
		current[i] = Entry{OracleID: q.OracleID, ScryfallID: q.ScryfallID, Quantity: q.Quantity}
	}
	items, err := qtx.ListChangeItemsAfterVersion(ctx, db.ListChangeItemsAfterVersionParams{
		CubeID: cubeID, AfterVersion: atVersion,
	})
	if err != nil {
		return nil, 0, err
	}
	past := ReplayBackwards(current, groupChanges(items), atVersion)

	ids := make([]uuid.UUID, len(past))
	for i, e := range past {
		ids[i] = e.ScryfallID
	}
	cards, err := qtx.GetCardsDisplayByScryfallIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	display := make(map[uuid.UUID]db.GetCardsDisplayByScryfallIDsRow, len(cards))
	for _, c := range cards {
		display[c.ScryfallID] = c
	}
	entries := make([]CardEntry, 0, len(past))
	for _, e := range past {
		c, ok := display[e.ScryfallID]
		if !ok {
			// Sync guard (Task 2) keeps referenced cards, so this is a
			// genuine invariant violation worth surfacing.
			return nil, 0, fmt.Errorf("card %s referenced by history is missing", e.ScryfallID)
		}
		entries = append(entries, CardEntry{
			ScryfallID: c.ScryfallID, OracleID: c.OracleID, Name: c.Name,
			ManaCost: c.ManaCost, TypeLine: c.TypeLine, Rarity: c.Rarity,
			Cmc: c.Cmc, Colors: c.Colors, ColorIdentity: c.ColorIdentity,
			ImageSmall: c.ImageSmall, ImageNormal: c.ImageNormal,
			Quantity: e.Quantity,
		})
	}
	return entries, atVersion, nil
}

// getVisible replicates Get's visibility rule (private + non-owner →
// ErrNotFound) against a tx-bound queries handle, so GetCards can enforce
// it inside the same snapshot as the rest of its reads.
func (s *Service) getVisible(ctx context.Context, qtx *db.Queries, cubeID, viewerID uuid.UUID) (db.GetCubeRow, error) {
	row, err := qtx.GetCube(ctx, cubeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.GetCubeRow{}, ErrNotFound
	}
	if err != nil {
		return db.GetCubeRow{}, err
	}
	if row.Visibility == "private" && row.OwnerID != viewerID {
		return db.GetCubeRow{}, ErrNotFound
	}
	return row, nil
}

// groupChanges folds the flat newest-first item rows into per-version
// Changes, preserving newest-first order for ReplayBackwards.
func groupChanges(items []db.ListChangeItemsAfterVersionRow) []Change {
	var changes []Change
	for _, it := range items {
		if len(changes) == 0 || changes[len(changes)-1].Version != it.Version {
			changes = append(changes, Change{Version: it.Version})
		}
		ch := &changes[len(changes)-1]
		d := Delta{OracleID: it.OracleID, ScryfallID: it.ScryfallID, Quantity: it.Quantity}
		if it.Kind == "add" {
			ch.Adds = append(ch.Adds, d)
		} else {
			ch.Removes = append(ch.Removes, d)
		}
	}
	return changes
}

// ApplyChange runs the diff-commit transaction: lock the cube row, check
// the expected version, validate and apply the diff, append the change,
// bump the version.
func (s *Service) ApplyChange(ctx context.Context, cubeID, authorID uuid.UUID, expectedVersion int32, note string, adds []AddInput, removes []RemoveInput) (ChangeEntry, int32, error) {
	// Resolve add printings to oracle identity before opening the tx.
	addIDs := make([]uuid.UUID, len(adds))
	for i, a := range adds {
		addIDs[i] = a.ScryfallID
	}
	cardRows, err := s.queries.GetCardsByScryfallIDs(ctx, addIDs)
	if err != nil {
		return ChangeEntry{}, 0, err
	}
	oracleOf := make(map[uuid.UUID]db.GetCardsByScryfallIDsRow, len(cardRows))
	for _, c := range cardRows {
		oracleOf[c.ScryfallID] = c
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ChangeEntry{}, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.queries.WithTx(tx)

	cube, err := qtx.GetCubeForUpdate(ctx, cubeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ChangeEntry{}, 0, ErrNotFound
	}
	if err != nil {
		return ChangeEntry{}, 0, err
	}
	if cube.OwnerID != authorID {
		if cube.Visibility == "private" {
			return ChangeEntry{}, 0, ErrNotFound
		}
		return ChangeEntry{}, 0, ErrForbidden
	}
	if cube.Version != expectedVersion {
		return ChangeEntry{}, 0, ErrVersionConflict
	}

	quantities, err := qtx.GetCubeCardQuantities(ctx, cubeID)
	if err != nil {
		return ChangeEntry{}, 0, err
	}
	current := make([]Entry, len(quantities))
	printingOf := make(map[uuid.UUID]uuid.UUID, len(quantities)) // oracle → current printing
	for i, q := range quantities {
		current[i] = Entry{OracleID: q.OracleID, ScryfallID: q.ScryfallID, Quantity: q.Quantity}
		printingOf[q.OracleID] = q.ScryfallID
	}

	addDeltas := make([]Delta, len(adds))
	addNames := make(map[uuid.UUID]string, len(adds))
	for i, a := range adds {
		card, ok := oracleOf[a.ScryfallID]
		if !ok {
			return ChangeEntry{}, 0, fmt.Errorf("%w: unknown card %s", ErrInvalidChange, a.ScryfallID)
		}
		addDeltas[i] = Delta{OracleID: card.OracleID, ScryfallID: a.ScryfallID, Quantity: a.Quantity}
		addNames[card.OracleID] = card.Name
	}
	removeDeltas := make([]Delta, len(removes))
	for i, r := range removes {
		printing, ok := printingOf[r.OracleID]
		if !ok {
			return ChangeEntry{}, 0, fmt.Errorf("%w: oracle %s is not in the cube", ErrInvalidChange, r.OracleID)
		}
		removeDeltas[i] = Delta{OracleID: r.OracleID, ScryfallID: printing, Quantity: r.Quantity}
	}
	if err := ValidateDiff(current, addDeltas, removeDeltas); err != nil {
		return ChangeEntry{}, 0, err
	}

	for _, d := range addDeltas {
		if err := qtx.AddCubeCard(ctx, db.AddCubeCardParams{
			CubeID: cubeID, OracleID: d.OracleID, ScryfallID: d.ScryfallID, Quantity: d.Quantity,
		}); err != nil {
			return ChangeEntry{}, 0, err
		}
	}
	for _, d := range removeDeltas {
		if err := qtx.RemoveCubeCardQuantity(ctx, db.RemoveCubeCardQuantityParams{
			CubeID: cubeID, OracleID: d.OracleID, Quantity: d.Quantity,
		}); err != nil {
			return ChangeEntry{}, 0, err
		}
	}
	if err := qtx.DeleteDepletedCubeCards(ctx, cubeID); err != nil {
		return ChangeEntry{}, 0, err
	}

	newVersion := cube.Version + 1
	change, err := qtx.InsertCubeChange(ctx, db.InsertCubeChangeParams{
		CubeID: cubeID, Version: newVersion, AuthorID: authorID, Note: note,
	})
	if err != nil {
		return ChangeEntry{}, 0, err
	}
	for _, d := range addDeltas {
		if err := qtx.InsertCubeChangeItem(ctx, db.InsertCubeChangeItemParams{
			ChangeID: change.ID, Kind: "add", OracleID: d.OracleID,
			ScryfallID: d.ScryfallID, Quantity: d.Quantity,
		}); err != nil {
			return ChangeEntry{}, 0, err
		}
	}
	for _, d := range removeDeltas {
		if err := qtx.InsertCubeChangeItem(ctx, db.InsertCubeChangeItemParams{
			ChangeID: change.ID, Kind: "remove", OracleID: d.OracleID,
			ScryfallID: d.ScryfallID, Quantity: d.Quantity,
		}); err != nil {
			return ChangeEntry{}, 0, err
		}
	}
	if err := qtx.BumpCubeVersion(ctx, cubeID); err != nil {
		return ChangeEntry{}, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ChangeEntry{}, 0, err
	}

	entry := ChangeEntry{
		ID: change.ID, Version: newVersion, Note: note, CreatedAt: change.CreatedAt,
	}
	// The commit already succeeded at this point: everything below is
	// presentation enrichment (author/card display names), not part of
	// the transactional outcome. A lookup failure here must never turn
	// into a failed request — the client would believe the commit
	// failed and retry into a spurious 409. Failures fall back to an
	// explicit, honest placeholder instead of silently substituting
	// unrelated data.
	user, err := s.queries.GetUserByID(ctx, authorID)
	if err != nil {
		// Deliberate non-fatal fallback: leave AuthorName blank rather
		// than serving the cube's name (wrong data) or failing the
		// request for a commit that already succeeded.
		entry.AuthorName = ""
	} else {
		entry.AuthorName = user.DisplayName
	}
	for _, d := range addDeltas {
		entry.Adds = append(entry.Adds, ChangeItem{OracleID: d.OracleID, Name: addNames[d.OracleID], Quantity: d.Quantity})
	}
	// Removed cards aren't in addNames, so their display names need a
	// dedicated lookup (single query for all remove printings).
	removeNames := make(map[uuid.UUID]string, len(removeDeltas))
	if len(removeDeltas) > 0 {
		ids := make([]uuid.UUID, len(removeDeltas))
		for i, d := range removeDeltas {
			ids[i] = d.ScryfallID
		}
		rows, err := s.queries.GetCardsByScryfallIDs(ctx, ids)
		if err != nil {
			// Deliberate non-fatal fallback: fall back to the raw
			// oracle ID per item below rather than failing a request
			// for a commit that already succeeded.
			rows = nil
		}
		for _, r := range rows {
			removeNames[r.OracleID] = r.Name
		}
	}
	for _, d := range removeDeltas {
		name, ok := removeNames[d.OracleID]
		if !ok {
			// Explicit fallback when the lookup failed or omitted this
			// card: better than an empty string for identifying which
			// card changed.
			name = d.OracleID.String()
		}
		entry.Removes = append(entry.Removes, ChangeItem{OracleID: d.OracleID, Name: name, Quantity: d.Quantity})
	}
	return entry, newVersion, nil
}

func (s *Service) ListChanges(ctx context.Context, cubeID, viewerID uuid.UUID, limit, offset int32) ([]ChangeEntry, int64, error) {
	if _, err := s.Get(ctx, cubeID, viewerID); err != nil {
		return nil, 0, err
	}
	rows, err := s.queries.ListCubeChanges(ctx, db.ListCubeChangesParams{
		CubeID: cubeID, PageLimit: limit, PageOffset: offset,
	})
	if err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return []ChangeEntry{}, 0, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	items, err := s.queries.ListChangeItemsForChanges(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	byChange := make(map[uuid.UUID][]db.ListChangeItemsForChangesRow)
	for _, it := range items {
		byChange[it.ChangeID] = append(byChange[it.ChangeID], it)
	}
	entries := make([]ChangeEntry, len(rows))
	for i, r := range rows {
		e := ChangeEntry{
			ID: r.ID, Version: r.Version, AuthorName: r.AuthorName,
			Note: r.Note, CreatedAt: r.CreatedAt,
		}
		for _, it := range byChange[r.ID] {
			ci := ChangeItem{OracleID: it.OracleID, Name: it.Name, Quantity: it.Quantity}
			if it.Kind == "add" {
				e.Adds = append(e.Adds, ci)
			} else {
				e.Removes = append(e.Removes, ci)
			}
		}
		entries[i] = e
	}
	return entries, rows[0].Total, nil
}
