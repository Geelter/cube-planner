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

func refundIssuedEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Zwrot środków / Refund issued: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nZwróciliśmy Twoją opłatę za %s. Środki wrócą na Twoją kartę w ciągu kilku dni.\n\n---\n\nHi %s,\n\nYour fee for %s has been refunded. The money should reach your card within a few days.",
			u.DisplayName, ev.Name, u.DisplayName, ev.Name,
		),
	}
}

func refundDeniedEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Odmowa zwrotu / Refund denied: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nOrganizator odrzucił zwrot opłaty za %s (rezygnacja po terminie).\n\n---\n\nHi %s,\n\nThe organizer declined the refund for %s (cancellation after the deadline).",
			u.DisplayName, ev.Name, u.DisplayName, ev.Name,
		),
	}
}

func eventCancelledEmail(u db.User, ev db.Event, refunded bool, baseURL string) pendingEmail {
	refundNotePL, refundNoteEN := "", ""
	if refunded {
		refundNotePL = " Twoja opłata została zwrócona."
		refundNoteEN = " Your fee has been refunded."
	}
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Wydarzenie odwołane / Event cancelled: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nWydarzenie %s zostało odwołane.%s\n\n---\n\nHi %s,\n\nThe event %s has been cancelled.%s",
			u.DisplayName, ev.Name, refundNotePL, u.DisplayName, ev.Name, refundNoteEN,
		),
	}
}

func paymentAfterExpiryEmail(u db.User, ev db.Event, baseURL string) pendingEmail {
	return pendingEmail{
		to:      u.Email,
		subject: fmt.Sprintf("Płatność po terminie — zwrot / Late payment refunded: %s", ev.Name),
		body: fmt.Sprintf(
			"Cześć %s,\n\nTwoja płatność za %s dotarła po wygaśnięciu rezerwacji, a miejsce zostało już zajęte. Zwróciliśmy pełną kwotę.\n\n---\n\nHi %s,\n\nYour payment for %s arrived after your reservation expired and the spot was already taken. We refunded the full amount.",
			u.DisplayName, ev.Name, u.DisplayName, ev.Name,
		),
	}
}
