package events

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

// pendingEmail is composed inside a transaction and sent only after
// commit — mail must never fail or delay a state transition.
type pendingEmail struct {
	to      string
	subject string
	body    string
}

func (s *Service) sendEmails(ctx context.Context, emails []pendingEmail) {
	for _, e := range emails {
		if err := s.mailer.Send(ctx, e.to, e.subject, e.body); err != nil {
			s.log.Error("events: send mail", "to", e.to, "subject", e.subject, "error", err)
		}
	}
}

// closeRegistrationLocked and cancelEventLocked are implemented in
// Tasks 4 and 6; these compile-time stubs keep Transition total.
func (s *Service) closeRegistrationLocked(ctx context.Context, qtx *db.Queries, ev db.Event) error {
	_ = pgx.ErrNoRows
	return nil
}

func (s *Service) cancelEventLocked(ctx context.Context, qtx *db.Queries, ev db.Event) ([]pendingEmail, error) {
	return nil, nil
}
