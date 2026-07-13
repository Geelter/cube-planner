package events

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

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

// cancelEventLocked is implemented in Task 6; this compile-time stub keeps
// Transition total.
func (s *Service) cancelEventLocked(ctx context.Context, qtx *db.Queries, ev db.Event) ([]pendingEmail, error) {
	return nil, nil
}

// Emails are bilingual in one body — Polish first, then English — because
// users have no stored locale (decided in the spec §6).

func eventLink(baseURL string, eventID uuid.UUID) string {
	return fmt.Sprintf("%s/events/%s", baseURL, eventID)
}

func payByLabel(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

func confirmationEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Potwierdzenie zapisu / Registration confirmed: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nTwoje miejsce na %s jest potwierdzone.\n%s\n\n---\n\nHi %s,\n\nYour spot at %s is confirmed.\n%s",
			u.DisplayName, ev.Name, eventLink(baseURL, ev.ID),
			u.DisplayName, ev.Name, eventLink(baseURL, ev.ID),
		),
	}
}

func promotionEmail(u db.User, ev db.Event, expiresAt time.Time, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Zwolniło się miejsce / A spot opened: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nZwolniło się miejsce na %s. Zapłać do %s, żeby je zająć:\n%s\n\n---\n\nHi %s,\n\nA spot opened at %s. Pay by %s to claim it:\n%s",
			u.DisplayName, ev.Name, payByLabel(expiresAt), eventLink(baseURL, ev.ID),
			u.DisplayName, ev.Name, payByLabel(expiresAt), eventLink(baseURL, ev.ID),
		),
	}
}
