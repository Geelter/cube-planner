package cards

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

const copyBatchSize = 1000

// ErrEmptyBulkFile guards against a truncated or malformed download ever
// wiping the cards table via the delete-missing step.
var ErrEmptyBulkFile = errors.New("bulk file contained no playable cards")

// Syncer imports Scryfall's default-cards bulk file: stream-decode →
// filter/transform → COPY into cards_staging → one transaction that
// upserts into cards and deletes rows missing from staging. Only the
// scheduler goroutine calls Sync, so staging is never contended.
type Syncer struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	client  *ScryfallClient
	log     *slog.Logger
}

func NewSyncer(pool *pgxpool.Pool, client *ScryfallClient, log *slog.Logger) *Syncer {
	return &Syncer{pool: pool, queries: db.New(pool), client: client, log: log}
}

// Sync checks the bulk-data descriptor and imports when it is newer than
// the last succeeded run. A nil return with no work means "up to date".
func (s *Syncer) Sync(ctx context.Context) error {
	meta, err := s.client.DefaultCardsMetadata(ctx)
	if err != nil {
		return fmt.Errorf("bulk metadata: %w", err)
	}
	last, err := s.queries.GetLastSucceededSyncRun(ctx)
	switch {
	case err == nil && last.BulkUpdatedAt != nil && !meta.UpdatedAt.After(*last.BulkUpdatedAt):
		s.log.Info("cards sync: bulk data unchanged, skipping", "bulkUpdatedAt", meta.UpdatedAt)
		return nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("last sync run: %w", err)
	}

	runID, err := s.queries.CreateSyncRun(ctx)
	if err != nil {
		return fmt.Errorf("create sync run: %w", err)
	}
	count, err := s.importBulk(ctx, meta)
	if err != nil {
		// WithoutCancel: record the failure even when ctx itself died.
		msg := err.Error()
		if ferr := s.queries.FinishSyncRunFailure(context.WithoutCancel(ctx),
			db.FinishSyncRunFailureParams{ID: runID, Error: &msg}); ferr != nil {
			s.log.Error("cards sync: record failure", "error", ferr)
		}
		return err
	}
	c := int32(count) //nolint:gosec // bulk file is ~100k cards, far below int32 max
	if err := s.queries.FinishSyncRunSuccess(ctx, db.FinishSyncRunSuccessParams{
		ID: runID, BulkUpdatedAt: &meta.UpdatedAt, CardsCount: &c,
	}); err != nil {
		return fmt.Errorf("record success: %w", err)
	}
	s.log.Info("cards sync: imported", "cards", count, "bulkUpdatedAt", meta.UpdatedAt)
	return nil
}

func (s *Syncer) importBulk(ctx context.Context, meta BulkMetadata) (int, error) {
	if err := s.queries.TruncateCardsStaging(ctx); err != nil {
		return 0, fmt.Errorf("truncate staging: %w", err)
	}
	total := 0
	batch := make([]Card, 0, copyBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.copyBatch(ctx, batch); err != nil {
			return err
		}
		total += len(batch)
		batch = batch[:0]
		return nil
	}
	err := s.client.StreamCards(ctx, meta.DownloadURI, func(sc scryfallCard) error {
		card, ok := transformCard(sc)
		if !ok {
			return nil
		}
		batch = append(batch, card)
		if len(batch) == copyBatchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("stream bulk cards: %w", err)
	}
	if err := flush(); err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, ErrEmptyBulkFile
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.queries.WithTx(tx)
	if _, err := qtx.UpsertCardsFromStaging(ctx); err != nil {
		return 0, fmt.Errorf("upsert cards: %w", err)
	}
	deleted, err := qtx.DeleteCardsMissingFromStaging(ctx)
	if err != nil {
		return 0, fmt.Errorf("delete missing cards: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if deleted > 0 {
		s.log.Info("cards sync: removed cards absent from bulk file", "count", deleted)
	}
	return total, nil
}

var stagingColumns = []string{
	"scryfall_id", "oracle_id", "name", "normalized_name", "released_at",
	"set_code", "set_name", "collector_number", "rarity", "layout",
	"mana_cost", "cmc", "type_line", "oracle_text", "colors",
	"color_identity", "promo", "image_small", "image_normal",
	"back_image_small", "back_image_normal",
}

func (s *Syncer) copyBatch(ctx context.Context, cards []Card) error {
	rows := make([][]any, len(cards))
	for i, c := range cards {
		rows[i] = []any{
			c.ScryfallID, c.OracleID, c.Name, c.NormalizedName, c.ReleasedAt,
			c.SetCode, c.SetName, c.CollectorNumber, c.Rarity, c.Layout,
			c.ManaCost, c.CMC, c.TypeLine, c.OracleText, c.Colors,
			c.ColorIdentity, c.Promo, c.ImageSmall, c.ImageNormal,
			c.BackImageSmall, c.BackImageNormal,
		}
	}
	if _, err := s.pool.CopyFrom(ctx, pgx.Identifier{"cards_staging"}, stagingColumns, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy into staging: %w", err)
	}
	return nil
}
