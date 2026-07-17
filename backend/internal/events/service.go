// Package events owns the event lifecycle, paid registration state
// machine, waitlist promotion, and Stripe integration.
package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
)

// PaymentWindow is how long a pending_payment registration holds a spot
// (registration and waitlist promotion alike). Matches Stripe Checkout's
// default session expiry so one mechanism covers both.
const PaymentWindow = 24 * time.Hour

// stripeMinSessionTTL is Stripe's minimum Checkout session lifetime.
const stripeMinSessionTTL = 30 * time.Minute

var (
	// ErrEventNotFound also hides drafts from non-admins (no existence leak,
	// same convention as private cubes).
	ErrEventNotFound     = errors.New("event not found")
	ErrInvalidTransition = errors.New("invalid event transition")
	// ErrEventLocked: a PATCH touched a field that is frozen post-publish
	// (people paid under those terms).
	ErrEventLocked            = errors.New("event field locked after publish")
	ErrCubesLocked            = errors.New("event cubes locked after publish")
	ErrInvalidEventCube       = errors.New("invalid event cube")
	ErrAlreadyRegistered      = errors.New("already registered")
	ErrRegistrationNotFound   = errors.New("registration not found")
	ErrRegistrationNotPayable = errors.New("registration not payable")
	ErrRegistrationClosed     = errors.New("registration closed")
	// ErrRoundOpen: finish refused while a tournament round is not
	// completed (5b spec §3.2).
	ErrRoundOpen = errors.New("a tournament round is still open")
)

type Service struct {
	queries *db.Queries
	pool    *pgxpool.Pool
	stripe  StripeClient
	mailer  mail.Mailer
	baseURL string
	log     *slog.Logger
	now     func() time.Time
}

func NewService(queries *db.Queries, pool *pgxpool.Pool, stripe StripeClient, mailer mail.Mailer, baseURL string, log *slog.Logger) *Service {
	return &Service{
		queries: queries, pool: pool, stripe: stripe, mailer: mailer,
		baseURL: baseURL, log: log, now: time.Now,
	}
}

func (s *Service) withTx(ctx context.Context, fn func(qtx *db.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type CreateEventParams struct {
	Name            string
	Description     string
	Location        string
	StartsAt        time.Time
	FeeCents        int32
	Currency        string
	MaxParticipants int32
	RefundDeadline  *time.Time
}

func (s *Service) Create(ctx context.Context, organizerID uuid.UUID, p CreateEventParams) (*db.Event, error) {
	if p.FeeCents > 0 && !s.stripe.Configured() {
		return nil, ErrPaymentsUnconfigured
	}
	if p.Currency == "" {
		p.Currency = "pln"
	}
	ev, err := s.queries.CreateEvent(ctx, db.CreateEventParams{
		OrganizerID: organizerID, Name: p.Name, Description: p.Description,
		Location: p.Location, StartsAt: p.StartsAt, FeeCents: p.FeeCents,
		Currency: p.Currency, MaxParticipants: p.MaxParticipants,
		RefundDeadline: p.RefundDeadline,
	})
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

type UpdateEventParams struct {
	Name            *string
	Description     *string
	Location        *string
	StartsAt        *time.Time
	FeeCents        *int32
	Currency        *string
	MaxParticipants *int32
	RefundDeadline  *time.Time
}

// Update enforces the lifecycle field whitelist: drafts are fully
// editable; from publish on, only description, location, and
// refund_deadline may change.
func (s *Service) Update(ctx context.Context, eventID uuid.UUID, p UpdateEventParams) (*db.Event, error) {
	var out db.Event
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		if ev.Status != "draft" {
			if p.Name != nil || p.StartsAt != nil || p.FeeCents != nil ||
				p.Currency != nil || p.MaxParticipants != nil {
				return ErrEventLocked
			}
		}
		if p.FeeCents != nil && *p.FeeCents > 0 && !s.stripe.Configured() {
			return ErrPaymentsUnconfigured
		}
		out, err = qtx.UpdateEventMeta(ctx, db.UpdateEventMetaParams{
			ID: eventID, Name: p.Name, Description: p.Description,
			Location: p.Location, StartsAt: p.StartsAt, FeeCents: p.FeeCents,
			Currency: p.Currency, MaxParticipants: p.MaxParticipants,
			RefundDeadline: p.RefundDeadline,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// legalTransitions: action → required current status.
var legalTransitions = map[string][]string{
	"publish": {"draft"},
	"start":   {"published"},
	"finish":  {"started"},
	// cancelled → cancel is a refund-retry no-op re-run (spec §3).
	"cancel": {"published", "started", "cancelled"},
}

// Transition validates and applies a lifecycle action. start additionally
// closes registration (Task 4) and cancel mass-refunds (Task 6).
func (s *Service) Transition(ctx context.Context, eventID uuid.UUID, action string) (*db.Event, error) {
	allowed, ok := legalTransitions[action]
	if !ok {
		return nil, fmt.Errorf("%w: unknown action %q", ErrInvalidTransition, action)
	}
	target := map[string]string{
		"publish": "published", "start": "started",
		"finish": "finished", "cancel": "cancelled",
	}[action]
	var out db.Event
	var emails []pendingEmail
	var refundRows []db.ListActiveRegistrationsForEventRow
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		legal := false
		for _, st := range allowed {
			legal = legal || ev.Status == st
		}
		if !legal {
			return fmt.Errorf("%w: %s from %s", ErrInvalidTransition, action, ev.Status)
		}
		if action == "publish" && ev.FeeCents > 0 && !s.stripe.Configured() {
			return ErrPaymentsUnconfigured
		}
		if action == "start" {
			if err := s.closeRegistrationLocked(ctx, qtx, ev); err != nil {
				return err
			}
		}
		if action == "finish" {
			open, err := qtx.CountOpenRoundsForEvent(ctx, eventID)
			if err != nil {
				return err
			}
			if open > 0 {
				return ErrRoundOpen
			}
		}
		if action == "cancel" {
			emails, refundRows, err = s.cancelEventLocked(ctx, qtx, ev)
			if err != nil {
				return err
			}
		}
		out, err = qtx.SetEventStatus(ctx, db.SetEventStatusParams{ID: eventID, Status: target})
		return err
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	if action == "cancel" {
		s.refundCancelledEventRows(ctx, out, refundRows)
	}
	return &out, nil
}

type CubeLinkInput struct {
	CubeID       uuid.UUID
	CubeChangeID *uuid.UUID
}

// SetCubes replaces the event's cube links. Draft-only (locked after
// publish). Each cube must be public or owned by the event's organizer;
// a pin must belong to its cube.
func (s *Service) SetCubes(ctx context.Context, eventID uuid.UUID, links []CubeLinkInput) error {
	return s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		if ev.Status != "draft" {
			return ErrCubesLocked
		}
		for _, l := range links {
			cube, err := qtx.GetCube(ctx, l.CubeID)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: unknown cube", ErrInvalidEventCube)
			}
			if err != nil {
				return err
			}
			if cube.Visibility != "public" && cube.OwnerID != ev.OrganizerID {
				return fmt.Errorf("%w: cube not accessible", ErrInvalidEventCube)
			}
			if l.CubeChangeID != nil {
				cubeID, err := qtx.GetCubeChangeCubeID(ctx, *l.CubeChangeID)
				if errors.Is(err, pgx.ErrNoRows) || (err == nil && cubeID != l.CubeID) {
					return fmt.Errorf("%w: pin does not belong to cube", ErrInvalidEventCube)
				}
				if err != nil {
					return err
				}
			}
		}
		if err := qtx.DeleteEventCubes(ctx, eventID); err != nil {
			return err
		}
		for _, l := range links {
			if err := qtx.InsertEventCube(ctx, db.InsertEventCubeParams{
				EventID: eventID, CubeID: l.CubeID, CubeChangeID: uuidPtrToPgtype(l.CubeChangeID),
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// uuidPtrToPgtype adapts CubeLinkInput.CubeChangeID (*uuid.UUID, the
// Service-level nullable convention) to the pgtype.UUID sqlc generates for
// InsertEventCubeParams.
func uuidPtrToPgtype(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}

type EventInfo struct {
	Event         db.Event
	PaidCount     int32
	PendingCount  int32
	WaitlistCount int32
	MyStatus      *string
}

func (s *Service) List(ctx context.Context, callerID uuid.UUID, isAdmin bool) ([]EventInfo, error) {
	rows, err := s.queries.ListEvents(ctx, db.ListEventsParams{
		UserID: callerID, IncludeDrafts: isAdmin,
	})
	if err != nil {
		return nil, err
	}
	out := make([]EventInfo, len(rows))
	for i, r := range rows {
		out[i] = EventInfo{
			Event: db.Event{
				ID: r.ID, OrganizerID: r.OrganizerID, Name: r.Name,
				Description: r.Description, Location: r.Location,
				StartsAt: r.StartsAt, FeeCents: r.FeeCents, Currency: r.Currency,
				MaxParticipants: r.MaxParticipants, RefundDeadline: r.RefundDeadline,
				Status: r.Status, WaitlistCounter: r.WaitlistCounter,
				CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			},
			PaidCount: r.PaidCount, PendingCount: r.PendingCount,
			WaitlistCount: r.WaitlistCount, MyStatus: r.MyStatus,
		}
	}
	return out, nil
}

type CubeLink struct {
	CubeID        uuid.UUID
	CubeName      string
	CubeChangeID  *uuid.UUID
	PinnedVersion *int32
	PinnedAt      *time.Time
}

type EventDetail struct {
	EventInfo
	Cubes          []CubeLink
	Attendees      []string
	MyRegistration *db.Registration
}

func (s *Service) GetDetail(ctx context.Context, eventID, callerID uuid.UUID, isAdmin bool) (*EventDetail, error) {
	ev, err := s.queries.GetEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEventNotFound
	}
	if err != nil {
		return nil, err
	}
	if ev.Status == "draft" && !isAdmin {
		return nil, ErrEventNotFound
	}
	counts, err := s.queries.GetEventCounts(ctx, eventID)
	if err != nil {
		return nil, err
	}
	cubes, err := s.queries.ListEventCubes(ctx, eventID)
	if err != nil {
		return nil, err
	}
	attendees, err := s.queries.ListPaidAttendees(ctx, eventID)
	if err != nil {
		return nil, err
	}
	detail := &EventDetail{
		EventInfo: EventInfo{
			Event: ev, PaidCount: counts.PaidCount,
			PendingCount: counts.PendingCount, WaitlistCount: counts.WaitlistCount,
		},
		Cubes:     make([]CubeLink, len(cubes)),
		Attendees: attendees,
	}
	for i, c := range cubes {
		detail.Cubes[i] = CubeLink{
			CubeID: c.CubeID, CubeName: c.CubeName, CubeChangeID: pgtypeUUIDToPtr(c.CubeChangeID),
			PinnedVersion: c.PinnedVersion, PinnedAt: c.PinnedAt,
		}
	}
	reg, err := s.queries.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
		EventID: eventID, UserID: callerID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// no active registration — fine
	case err != nil:
		return nil, err
	default:
		st := reg.Status
		detail.MyRegistration = &reg
		detail.MyStatus = &st
	}
	return detail, nil
}

// pgtypeUUIDToPtr is the read-side inverse of uuidPtrToPgtype.
func pgtypeUUIDToPtr(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	u := uuid.UUID(id.Bytes)
	return &u
}

// Register creates the caller's registration for a published event:
// spot free + paid event → pending_payment (24h window); spot free +
// free event → paid instantly; event full → waitlisted.
func (s *Service) Register(ctx context.Context, eventID, userID uuid.UUID) (*db.Registration, error) {
	var reg db.Registration
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		switch ev.Status {
		case "published":
			// registrable
		case "draft":
			return ErrEventNotFound // no existence leak
		default:
			return ErrRegistrationClosed
		}
		if _, err := qtx.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
			EventID: eventID, UserID: userID,
		}); err == nil {
			return ErrAlreadyRegistered
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		occupied, err := qtx.CountOccupiedSpots(ctx, eventID)
		if err != nil {
			return err
		}
		now := s.now()
		switch {
		case occupied >= int64(ev.MaxParticipants):
			pos, err := qtx.IncrementWaitlistCounter(ctx, eventID)
			if err != nil {
				return err
			}
			reg, err = qtx.CreateRegistration(ctx, db.CreateRegistrationParams{
				EventID: eventID, UserID: userID, Status: "waitlisted", WaitlistPos: &pos,
			})
			return err
		case ev.FeeCents > 0:
			expires := now.Add(PaymentWindow)
			reg, err = qtx.CreateRegistration(ctx, db.CreateRegistrationParams{
				EventID: eventID, UserID: userID, Status: "pending_payment", ExpiresAt: &expires,
			})
			return err
		default:
			reg, err = qtx.CreateRegistration(ctx, db.CreateRegistrationParams{
				EventID: eventID, UserID: userID, Status: "paid", PaidAt: &now,
			})
			if err != nil {
				return err
			}
			u, err := qtx.GetUserByID(ctx, userID)
			if err != nil {
				return err
			}
			emails = append(emails, confirmationEmail(u, ev, s.baseURL))
			return nil
		}
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	return &reg, nil
}

// promoteLocked fills free capacity from the waitlist. MUST run inside a
// transaction holding the event row lock. Every spot-freeing path
// (expiry, cancel, refund, webhook) calls it.
func (s *Service) promoteLocked(ctx context.Context, qtx *db.Queries, ev db.Event) ([]pendingEmail, error) {
	var emails []pendingEmail
	for {
		occupied, err := qtx.CountOccupiedSpots(ctx, ev.ID)
		if err != nil {
			return nil, err
		}
		if occupied >= int64(ev.MaxParticipants) {
			return emails, nil
		}
		next, err := qtx.NextWaitlisted(ctx, ev.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return emails, nil
		}
		if err != nil {
			return nil, err
		}
		u, err := qtx.GetUserByID(ctx, next.UserID)
		if err != nil {
			return nil, err
		}
		now := s.now()
		if ev.FeeCents > 0 {
			expires := now.Add(PaymentWindow)
			if _, err := qtx.PromoteToPendingPayment(ctx, db.PromoteToPendingPaymentParams{
				ID: next.ID, ExpiresAt: &expires,
			}); err != nil {
				return nil, err
			}
			emails = append(emails, promotionEmail(u, ev, expires, s.baseURL))
		} else {
			if _, err := qtx.MarkRegistrationPaid(ctx, db.MarkRegistrationPaidParams{
				ID: next.ID, PaidAt: &now,
			}); err != nil {
				return nil, err
			}
			emails = append(emails, confirmationEmail(u, ev, s.baseURL))
		}
	}
}

// expireAndPromote is the per-event sweeper body: flip overdue
// pending_payment rows to expired, then promote.
func (s *Service) expireAndPromote(ctx context.Context, eventID uuid.UUID) error {
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if err != nil {
			return err
		}
		now := s.now()
		if _, err := qtx.ExpireOverduePendingForEvent(ctx, db.ExpireOverduePendingForEventParams{
			EventID: eventID, Now: &now,
		}); err != nil {
			return err
		}
		emails, err = s.promoteLocked(ctx, qtx, ev)
		return err
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	return nil
}

// Pay returns a Stripe Checkout URL for the caller's pending_payment
// registration, creating the session on demand and reusing a live one.
// The session expiry is clamped to the registration's remaining window,
// respecting Stripe's 30-minute minimum.
func (s *Service) Pay(ctx context.Context, eventID, userID uuid.UUID) (string, error) {
	ev, err := s.queries.GetEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrEventNotFound
	}
	if err != nil {
		return "", err
	}
	// Drafts are invisible (same convention as Register/GetDetail/
	// CancelRegistration): without this, probing Pay with a draft id
	// returns registration-not-found instead of event-not-found, leaking
	// the draft's existence.
	if ev.Status == "draft" {
		return "", ErrEventNotFound
	}
	reg, err := s.queries.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
		EventID: eventID, UserID: userID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrRegistrationNotFound
	}
	if err != nil {
		return "", err
	}
	now := s.now()
	if reg.Status != "pending_payment" || reg.ExpiresAt == nil || !reg.ExpiresAt.After(now) {
		return "", ErrRegistrationNotPayable
	}
	if reg.StripeCheckoutSessionUrl != nil && reg.StripeSessionExpiresAt != nil &&
		reg.StripeSessionExpiresAt.After(now) {
		return *reg.StripeCheckoutSessionUrl, nil
	}
	if !s.stripe.Configured() {
		return "", ErrPaymentsUnconfigured
	}
	sessionExpiry := *reg.ExpiresAt
	if min := now.Add(stripeMinSessionTTL); sessionExpiry.Before(min) {
		sessionExpiry = min
	}
	cs, err := s.stripe.CreateCheckoutSession(ctx, CheckoutParams{
		RegistrationID: reg.ID,
		EventName:      ev.Name,
		Currency:       ev.Currency,
		AmountCents:    int64(ev.FeeCents),
		ExpiresAt:      sessionExpiry,
		SuccessURL:     eventLink(s.baseURL, ev.ID) + "?checkout=success",
		CancelURL:      eventLink(s.baseURL, ev.ID) + "?checkout=cancelled",
	})
	if err != nil {
		return "", err
	}
	if err := s.queries.SetRegistrationCheckoutSession(ctx, db.SetRegistrationCheckoutSessionParams{
		ID: reg.ID, SessionID: &cs.ID, SessionUrl: &cs.URL, SessionExpiresAt: &cs.ExpiresAt,
	}); err != nil {
		return "", err
	}
	return cs.URL, nil
}

// closeRegistrationLocked (start): registration closes — remaining
// pending_payment rows expire, the waitlist is cancelled, and nothing
// promotes. The paid roster is the 5b tournament roster.
func (s *Service) closeRegistrationLocked(ctx context.Context, qtx *db.Queries, ev db.Event) error {
	rows, err := qtx.ListActiveRegistrationsForEvent(ctx, ev.ID)
	if err != nil {
		return err
	}
	for _, r := range rows {
		var target string
		switch r.Status {
		case "pending_payment":
			target = "expired"
		case "waitlisted":
			target = "cancelled"
		default:
			continue
		}
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: r.ID, Status: target,
		}); err != nil {
			return err
		}
	}
	return nil
}

// refundDeadlineFor: explicit deadline, else the event start.
func refundDeadlineFor(ev db.Event) time.Time {
	if ev.RefundDeadline != nil {
		return *ev.RefundDeadline
	}
	return ev.StartsAt
}

// CancelRegistration is self-cancel. Cancelling always frees the spot
// (waitlist promotion is never blocked); only the MONEY depends on the
// deadline: in-window paid → automatic refund; past it → refund_requested
// (organizer decides). Free-event rows never touch Stripe.
func (s *Service) CancelRegistration(ctx context.Context, eventID, userID uuid.UUID) (*db.Registration, error) {
	var out db.Registration
	var emails []pendingEmail
	refundIntent := ""
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		ev, err := qtx.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		if ev.Status == "draft" {
			return ErrEventNotFound
		}
		if ev.Status != "published" {
			return ErrRegistrationClosed
		}
		reg, err := qtx.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
			EventID: eventID, UserID: userID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRegistrationNotFound
		}
		if err != nil {
			return err
		}
		switch reg.Status {
		case "waitlisted":
			out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
				ID: reg.ID, Status: "cancelled",
			})
			return err
		case "pending_payment":
			out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
				ID: reg.ID, Status: "cancelled",
			})
			if err != nil {
				return err
			}
			emails, err = s.promoteLocked(ctx, qtx, ev)
			return err
		case "paid":
			switch {
			case reg.StripePaymentIntentID == nil:
				// Free event: nothing to refund.
				out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
					ID: reg.ID, Status: "cancelled",
				})
			case s.now().Before(refundDeadlineFor(ev)):
				// Refund first, finalize after the call succeeds (below).
				refundIntent = *reg.StripePaymentIntentID
				out = reg
				return nil
			default:
				out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
					ID: reg.ID, Status: "refund_requested",
				})
			}
			if err != nil {
				return err
			}
			more, err := s.promoteLocked(ctx, qtx, ev)
			emails = append(emails, more...)
			return err
		default:
			return ErrRegistrationNotFound
		}
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	if refundIntent != "" {
		return s.refundRegistration(ctx, out.ID, refundIntent, false)
	}
	return &out, nil
}

// refundRegistration calls Stripe, then finalizes the row to 'refunded'
// under the event lock, promoting if the row still held a spot. Races
// with the charge.refunded webhook are resolved by the status check.
// apology selects the late-payment email instead of the plain refund one.
func (s *Service) refundRegistration(ctx context.Context, regID uuid.UUID, paymentIntentID string, apology bool) (*db.Registration, error) {
	if err := s.stripe.RefundPaymentIntent(ctx, paymentIntentID); err != nil {
		return nil, err
	}
	var out db.Registration
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		reg, err := qtx.GetRegistration(ctx, regID)
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		reg, err = qtx.GetRegistration(ctx, regID) // re-read under the lock
		if err != nil {
			return err
		}
		if reg.Status == "refunded" {
			out = reg
			return nil
		}
		wasHolding := reg.Status == "paid" || reg.Status == "pending_payment"
		out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: regID, Status: "refunded",
		})
		if err != nil {
			return err
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		if apology {
			emails = append(emails, paymentAfterExpiryEmail(u, ev, s.baseURL))
		} else {
			emails = append(emails, refundIssuedEmail(u, ev, s.baseURL))
		}
		if wasHolding {
			more, err := s.promoteLocked(ctx, qtx, ev)
			if err != nil {
				return err
			}
			emails = append(emails, more...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	return &out, nil
}

// OrganizerRefund resolves a refund_requested queue entry, or
// kick-refunds a paid registration.
func (s *Service) OrganizerRefund(ctx context.Context, eventID, registrationID uuid.UUID) (*db.Registration, error) {
	reg, err := s.queries.GetRegistration(ctx, registrationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && reg.EventID != eventID) {
		return nil, ErrRegistrationNotFound
	}
	if err != nil {
		return nil, err
	}
	if reg.Status != "refund_requested" && reg.Status != "paid" {
		return nil, fmt.Errorf("%w: refund on %s registration", ErrInvalidTransition, reg.Status)
	}
	if reg.StripePaymentIntentID == nil {
		return nil, fmt.Errorf("%w: nothing was paid", ErrInvalidTransition)
	}
	return s.refundRegistration(ctx, registrationID, *reg.StripePaymentIntentID, false)
}

// DenyRefund resolves a refund_requested entry as denied: the row is
// cancelled, the money stays.
func (s *Service) DenyRefund(ctx context.Context, eventID, registrationID uuid.UUID) (*db.Registration, error) {
	var out db.Registration
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		reg, err := qtx.GetRegistration(ctx, registrationID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && reg.EventID != eventID) {
			return ErrRegistrationNotFound
		}
		if err != nil {
			return err
		}
		if reg.Status != "refund_requested" {
			return fmt.Errorf("%w: deny-refund on %s registration", ErrInvalidTransition, reg.Status)
		}
		ev, err := qtx.GetEvent(ctx, reg.EventID)
		if err != nil {
			return err
		}
		out, err = qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: registrationID, Status: "cancelled",
		})
		if err != nil {
			return err
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		emails = append(emails, refundDeniedEmail(u, ev, s.baseURL))
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.sendEmails(ctx, emails)
	return &out, nil
}

// cancelEventLocked transitions every no-money row to cancelled and
// returns the rows that need a Stripe refund (processed AFTER commit —
// no IO under the event lock).
func (s *Service) cancelEventLocked(ctx context.Context, qtx *db.Queries, ev db.Event) ([]pendingEmail, []db.ListActiveRegistrationsForEventRow, error) {
	rows, err := qtx.ListActiveRegistrationsForEvent(ctx, ev.ID)
	if err != nil {
		return nil, nil, err
	}
	var emails []pendingEmail
	var refunds []db.ListActiveRegistrationsForEventRow
	for _, r := range rows {
		if (r.Status == "paid" || r.Status == "refund_requested") && r.StripePaymentIntentID != nil {
			refunds = append(refunds, r)
			continue
		}
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: r.ID, Status: "cancelled",
		}); err != nil {
			return nil, nil, err
		}
		u := db.User{Email: r.Email, DisplayName: r.DisplayName}
		emails = append(emails, eventCancelledEmail(u, ev, false, s.baseURL))
	}
	return emails, refunds, nil
}

// refundCancelledEventRows runs after the cancel transaction commits.
// A failed Stripe call leaves that row untouched; re-running the cancel
// action retries exactly the leftovers.
func (s *Service) refundCancelledEventRows(ctx context.Context, ev db.Event, rows []db.ListActiveRegistrationsForEventRow) {
	for _, r := range rows {
		if err := s.stripe.RefundPaymentIntent(ctx, *r.StripePaymentIntentID); err != nil {
			s.log.Error("events: cancel refund failed, re-run cancel to retry",
				"registration", r.ID, "error", err)
			continue
		}
		err := s.withTx(ctx, func(qtx *db.Queries) error {
			_, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
				ID: r.ID, Status: "refunded",
			})
			return err
		})
		if err != nil {
			s.log.Error("events: mark refunded after cancel", "registration", r.ID, "error", err)
			continue
		}
		u := db.User{Email: r.Email, DisplayName: r.DisplayName}
		s.sendEmails(ctx, []pendingEmail{eventCancelledEmail(u, ev, true, s.baseURL)})
	}
}

// WebhookEvent is the minimal, SDK-independent shape the chi handler
// extracts from a verified Stripe event.
type WebhookEvent struct {
	ID                string
	Type              string
	CheckoutSessionID string
	PaymentIntentID   string
	// ClientReferenceID carries the registration id Pay stamped on the
	// Checkout session. It is the fallback identity for sessions the row
	// no longer stores (see findRegistrationForCheckout).
	ClientReferenceID string
}

// errAlreadySeen: duplicate delivery — acknowledged, never an error.
var errAlreadySeen = errors.New("stripe event already processed")

// HandleWebhookEvent processes a signature-verified Stripe event
// idempotently (stripe_events insert-or-skip in the same transaction as
// the state change). Unknown types are recorded and acknowledged.
func (s *Service) HandleWebhookEvent(ctx context.Context, we WebhookEvent) error {
	var err error
	switch we.Type {
	case "checkout.session.completed":
		err = s.handleCheckoutCompleted(ctx, we)
	case "checkout.session.expired":
		err = s.handleCheckoutExpired(ctx, we)
	case "charge.refunded":
		err = s.handleChargeRefunded(ctx, we)
	default:
		err = s.withTx(ctx, func(qtx *db.Queries) error {
			_, e := qtx.InsertStripeEvent(ctx, db.InsertStripeEventParams{
				StripeEventID: we.ID, Type: we.Type,
			})
			return e
		})
	}
	if errors.Is(err, errAlreadySeen) {
		return nil
	}
	return err
}

func markSeen(ctx context.Context, qtx *db.Queries, we WebhookEvent) error {
	n, err := qtx.InsertStripeEvent(ctx, db.InsertStripeEventParams{
		StripeEventID: we.ID, Type: we.Type,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errAlreadySeen
	}
	return nil
}

// findRegistrationForCheckout resolves the registration a
// checkout.session.* event refers to. Primary key: the stored session id.
// Fallback: two concurrent Pay calls can each create a session while the
// row stores only the last-written one — the user can still pay through
// the "orphaned" sibling, so a stored-id miss falls back to the
// registration id the session carries as client_reference_id. Returns nil
// (no error) when the event matches nothing we know.
func (s *Service) findRegistrationForCheckout(ctx context.Context, qtx *db.Queries, we WebhookEvent) (*db.Registration, error) {
	reg, err := qtx.GetRegistrationByCheckoutSession(ctx, &we.CheckoutSessionID)
	if err == nil {
		return &reg, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	regID, parseErr := uuid.Parse(we.ClientReferenceID)
	if parseErr != nil {
		return nil, nil //nolint:nilnil // "nothing matched" is a valid outcome, not an error
	}
	reg, err = qtx.GetRegistration(ctx, regID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // see above
	}
	if err != nil {
		return nil, err
	}
	return &reg, nil
}

func (s *Service) handleCheckoutCompleted(ctx context.Context, we WebhookEvent) error {
	var emails []pendingEmail
	lateRefund := ""
	dupRefund := ""
	var lateRegID uuid.UUID
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		if err := markSeen(ctx, qtx, we); err != nil {
			return err
		}
		found, err := s.findRegistrationForCheckout(ctx, qtx, we)
		if err != nil {
			return err
		}
		if found == nil {
			s.log.Warn("stripe webhook: unknown checkout session",
				"session", we.CheckoutSessionID, "client_reference", we.ClientReferenceID)
			return nil // acknowledge; nothing to do
		}
		ev, err := qtx.GetEventForUpdate(ctx, found.EventID)
		if err != nil {
			return err
		}
		reg, err := qtx.GetRegistration(ctx, found.ID) // re-read under the lock
		if err != nil {
			return err
		}
		// A second charge landing on an already-paid row is a duplicate
		// from a Pay race, not the winning payment: its pointers must NOT
		// clobber the winning ones (a dashboard refund of the first charge
		// would stop matching the row). It gets refunded below instead.
		duplicateCharge := reg.Status == "paid" && we.PaymentIntentID != "" &&
			reg.StripePaymentIntentID != nil && *reg.StripePaymentIntentID != we.PaymentIntentID
		if !duplicateCharge {
			// The paying session wins: if the row stores a sibling session
			// from a Pay race, overwrite it so this payment stays traceable
			// (URL/expiry cleared — the session is consumed).
			if reg.StripeCheckoutSessionID == nil || *reg.StripeCheckoutSessionID != we.CheckoutSessionID {
				if err := qtx.SetRegistrationCheckoutSession(ctx, db.SetRegistrationCheckoutSessionParams{
					ID: reg.ID, SessionID: &we.CheckoutSessionID,
				}); err != nil {
					return err
				}
			}
			// Always record the intent so dashboard refunds stay traceable.
			if we.PaymentIntentID != "" {
				if err := qtx.SetRegistrationPaymentIntent(ctx, db.SetRegistrationPaymentIntentParams{
					ID: reg.ID, PaymentIntentID: &we.PaymentIntentID,
				}); err != nil {
					return err
				}
			}
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		switch reg.Status {
		case "pending_payment":
			pi := we.PaymentIntentID
			paidAt := s.now()
			if _, err := qtx.MarkRegistrationPaid(ctx, db.MarkRegistrationPaidParams{
				ID: reg.ID, PaidAt: &paidAt, PaymentIntentID: &pi,
			}); err != nil {
				return err
			}
			emails = append(emails, confirmationEmail(u, ev, s.baseURL))
			return nil
		case "paid":
			if duplicateCharge {
				dupRefund = we.PaymentIntentID
			}
			return nil // redundant delivery of a different event id
		default:
			// Late payment: the registration already expired/cancelled.
			occupied, err := qtx.CountOccupiedSpots(ctx, ev.ID)
			if err != nil {
				return err
			}
			// Reclaiming is also blocked when the user re-registered after
			// cancelling: flipping the old row back to paid would violate
			// registrations_one_active_idx mid-tx, and that error would
			// 500 the webhook into a permanent Stripe retry loop. Checked
			// under the event lock, so Register can't race it.
			_, activeErr := qtx.GetActiveRegistration(ctx, db.GetActiveRegistrationParams{
				EventID: ev.ID, UserID: reg.UserID,
			})
			if activeErr != nil && !errors.Is(activeErr, pgx.ErrNoRows) {
				return activeErr
			}
			hasOtherActive := activeErr == nil
			if ev.Status == "published" && occupied < int64(ev.MaxParticipants) && !hasOtherActive {
				pi := we.PaymentIntentID
				paidAt := s.now()
				if _, err := qtx.MarkRegistrationPaid(ctx, db.MarkRegistrationPaidParams{
					ID: reg.ID, PaidAt: &paidAt, PaymentIntentID: &pi,
				}); err != nil {
					return err
				}
				emails = append(emails, confirmationEmail(u, ev, s.baseURL))
				return nil
			}
			lateRefund = we.PaymentIntentID
			lateRegID = reg.ID
			return nil
		}
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	if lateRefund != "" {
		if _, err := s.refundRegistration(ctx, lateRegID, lateRefund, true); err != nil {
			// The charge stays; a dashboard refund + charge.refunded webhook
			// will sync state. Log loudly.
			s.log.Error("events: late-payment auto-refund failed", "registration", lateRegID, "error", err)
		}
	}
	if dupRefund != "" {
		// Refund only the duplicate charge — the registration stays paid on
		// the winning intent, so refundRegistration (which flips the row to
		// refunded) must not run. Stripe notifies the cardholder itself.
		if err := s.stripe.RefundPaymentIntent(ctx, dupRefund); err != nil {
			s.log.Error("events: duplicate-charge auto-refund failed", "payment_intent", dupRefund, "error", err)
		}
	}
	return nil
}

func (s *Service) handleCheckoutExpired(ctx context.Context, we WebhookEvent) error {
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		if err := markSeen(ctx, qtx, we); err != nil {
			return err
		}
		// Deliberately NO client_reference_id fallback here (unlike
		// completion): an expired session only proves THAT session died.
		// When the stored-id lookup misses, the expired session is an
		// orphan from a Pay race and the registration's stored session may
		// still be live and payable — expiring the row would kill a spot
		// the user can still pay for. Genuine registration expiry is owned
		// by expires_at (the sweeper), not by orphan sessions.
		reg, err := qtx.GetRegistrationByCheckoutSession(ctx, &we.CheckoutSessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		reg, err = qtx.GetRegistration(ctx, reg.ID)
		if err != nil {
			return err
		}
		if reg.Status != "pending_payment" {
			return nil
		}
		// Same guard against the lookup→lock window: if a concurrent Pay
		// replaced the row's session after our unlocked lookup, the expired
		// session is no longer the live one — leave the row alone.
		if reg.StripeCheckoutSessionID == nil || *reg.StripeCheckoutSessionID != we.CheckoutSessionID {
			return nil
		}
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: reg.ID, Status: "expired",
		}); err != nil {
			return err
		}
		emails, err = s.promoteLocked(ctx, qtx, ev)
		return err
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	return nil
}

func (s *Service) handleChargeRefunded(ctx context.Context, we WebhookEvent) error {
	var emails []pendingEmail
	err := s.withTx(ctx, func(qtx *db.Queries) error {
		if err := markSeen(ctx, qtx, we); err != nil {
			return err
		}
		reg, err := qtx.GetRegistrationByPaymentIntent(ctx, &we.PaymentIntentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		ev, err := qtx.GetEventForUpdate(ctx, reg.EventID)
		if err != nil {
			return err
		}
		reg, err = qtx.GetRegistration(ctx, reg.ID)
		if err != nil {
			return err
		}
		if reg.Status == "refunded" {
			return nil // we issued this refund ourselves
		}
		wasHolding := reg.Status == "paid" || reg.Status == "pending_payment"
		if _, err := qtx.SetRegistrationTerminal(ctx, db.SetRegistrationTerminalParams{
			ID: reg.ID, Status: "refunded",
		}); err != nil {
			return err
		}
		u, err := qtx.GetUserByID(ctx, reg.UserID)
		if err != nil {
			return err
		}
		emails = append(emails, refundIssuedEmail(u, ev, s.baseURL))
		if wasHolding {
			more, err := s.promoteLocked(ctx, qtx, ev)
			if err != nil {
				return err
			}
			emails = append(emails, more...)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.sendEmails(ctx, emails)
	return nil
}
