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
	"cancel":  {"published", "started"},
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
		if action == "cancel" {
			emails, err = s.cancelEventLocked(ctx, qtx, ev)
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
