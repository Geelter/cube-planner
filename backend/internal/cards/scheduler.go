package cards

import (
	"context"
	"errors"
	"time"
)

// DefaultSyncCheckInterval is how often the scheduler re-reads the bulk
// descriptor. The check is one cheap GET; a download happens only when
// Scryfall's updated_at moved (roughly daily).
const DefaultSyncCheckInterval = 6 * time.Hour

// RunScheduler owns the sync cadence: mark any 'running' rows abandoned by
// a previous process as failed, attempt one sync immediately (first boot
// imports; later boots no-op via the updated_at check), then re-check on a
// ticker. Blocks until ctx is cancelled — run in a goroutine.
func (s *Syncer) RunScheduler(ctx context.Context, interval time.Duration) {
	if err := s.queries.FailStaleRunningSyncRuns(ctx); err != nil {
		s.log.Error("cards sync: fail stale runs", "error", err)
	}
	s.syncAndLog(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncAndLog(ctx)
		}
	}
}

func (s *Syncer) syncAndLog(ctx context.Context) {
	if err := s.Sync(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Error("cards sync failed", "error", err)
	}
}
