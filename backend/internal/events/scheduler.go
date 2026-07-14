package events

import (
	"context"
	"errors"
	"time"
)

// DefaultSweepInterval: the payment window is 24h, so minute-level
// resolution is far more than enough.
const DefaultSweepInterval = time.Minute

// RunSweeper expires overdue pending_payment registrations and promotes
// waitlists on a ticker. Blocks until ctx is cancelled — run in a
// goroutine (same lifecycle as cards.RunScheduler).
func (s *Service) RunSweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.log.Error("events sweep failed", "error", err)
			}
		}
	}
}

// Sweep runs one pass over every event with overdue pending rows. Each
// event is its own transaction; one failure doesn't block the others.
func (s *Service) Sweep(ctx context.Context) error {
	now := s.now()
	ids, err := s.queries.ListEventIDsWithOverduePending(ctx, &now)
	if err != nil {
		return err
	}
	var firstErr error
	for _, id := range ids {
		if err := s.expireAndPromote(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
