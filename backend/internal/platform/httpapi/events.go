package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/events"
)

// Wire types are exported: huma derives OpenAPI schema names (and the
// generated TS type names) from Go type names.

type RegistrationInfo struct {
	ID          uuid.UUID  `json:"id"`
	Status      string     `json:"status" enum:"pending_payment,paid,waitlisted,cancelled,refund_requested,refunded,expired"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	WaitlistPos *int64     `json:"waitlistPos,omitempty"`
	PaidAt      *time.Time `json:"paidAt,omitempty"`
}

func registrationInfoFrom(r db.Registration) RegistrationInfo {
	return RegistrationInfo{
		ID: r.ID, Status: r.Status, ExpiresAt: r.ExpiresAt,
		WaitlistPos: r.WaitlistPos, PaidAt: r.PaidAt,
	}
}

type EventSummary struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	StartsAt             time.Time `json:"startsAt"`
	Location             string    `json:"location"`
	FeeCents             int32     `json:"feeCents"`
	Currency             string    `json:"currency"`
	MaxParticipants      int32     `json:"maxParticipants"`
	PaidCount            int32     `json:"paidCount"`
	PendingCount         int32     `json:"pendingCount"`
	WaitlistCount        int32     `json:"waitlistCount"`
	Status               string    `json:"status" enum:"draft,published,started,finished,cancelled"`
	MyRegistrationStatus *string   `json:"myRegistrationStatus,omitempty"`
}

func eventSummaryFrom(e events.EventInfo) EventSummary {
	return EventSummary{
		ID: e.Event.ID, Name: e.Event.Name, StartsAt: e.Event.StartsAt,
		Location: e.Event.Location, FeeCents: e.Event.FeeCents,
		Currency: e.Event.Currency, MaxParticipants: e.Event.MaxParticipants,
		PaidCount: e.PaidCount, PendingCount: e.PendingCount,
		WaitlistCount: e.WaitlistCount, Status: e.Event.Status,
		MyRegistrationStatus: e.MyStatus,
	}
}

type EventCubeEntry struct {
	CubeID        uuid.UUID  `json:"cubeId"`
	CubeName      string     `json:"cubeName"`
	CubeChangeID  *uuid.UUID `json:"cubeChangeId,omitempty"`
	PinnedVersion *int32     `json:"pinnedVersion,omitempty"`
	PinnedAt      *time.Time `json:"pinnedAt,omitempty"`
}

type EventDetailBody struct {
	EventSummary
	Description    string            `json:"description"`
	RefundDeadline *time.Time        `json:"refundDeadline,omitempty"`
	OrganizerName  string            `json:"organizerName"`
	Cubes          []EventCubeEntry  `json:"cubes"`
	Attendees      []string          `json:"attendees"`
	MyRegistration *RegistrationInfo `json:"myRegistration,omitempty"`
}

func eventProblem(status int, ptype, detail string) error {
	return &huma.ErrorModel{
		Status: status, Type: ptype, Title: http.StatusText(status), Detail: detail,
	}
}

func mapEventErr(err error) error {
	switch {
	case errors.Is(err, events.ErrEventNotFound):
		return huma.Error404NotFound("event not found")
	case errors.Is(err, events.ErrRegistrationNotFound):
		return eventProblem(http.StatusNotFound, "registration-not-found", "no active registration")
	case errors.Is(err, events.ErrAlreadyRegistered):
		return eventProblem(http.StatusConflict, "already-registered", err.Error())
	case errors.Is(err, events.ErrRegistrationClosed):
		return eventProblem(http.StatusConflict, "event-registration-closed", err.Error())
	case errors.Is(err, events.ErrRegistrationNotPayable):
		return eventProblem(http.StatusConflict, "registration-not-payable", err.Error())
	case errors.Is(err, events.ErrInvalidTransition):
		return eventProblem(http.StatusConflict, "invalid-event-transition", err.Error())
	case errors.Is(err, events.ErrEventLocked):
		return eventProblem(http.StatusConflict, "event-locked", err.Error())
	case errors.Is(err, events.ErrCubesLocked):
		return eventProblem(http.StatusConflict, "event-cubes-locked", err.Error())
	case errors.Is(err, events.ErrInvalidEventCube):
		return eventProblem(http.StatusUnprocessableEntity, "invalid-event-cube", err.Error())
	case errors.Is(err, events.ErrPaymentsUnconfigured):
		return eventProblem(http.StatusServiceUnavailable, "payments-unconfigured", "payments are not configured")
	default:
		return err
	}
}

func parseEventID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, huma.Error404NotFound("event not found")
	}
	return id, nil
}

// isAdmin resolves the session user's role. Missing session or lookup
// failure simply reads as non-admin.
func isAdmin(ctx context.Context, deps Deps) bool {
	uid, ok := CurrentUserID(ctx)
	if !ok {
		return false
	}
	u, err := deps.Queries.GetUserByID(ctx, uid)
	return err == nil && u.Role == "admin"
}

type eventIDInput struct {
	EventID string `path:"eventId"`
}

type listEventsOutput struct {
	Body struct {
		Events []EventSummary `json:"events"`
	}
}

type eventDetailOutput struct {
	Body EventDetailBody
}

type registrationOutput struct {
	Body RegistrationInfo
}

type payOutput struct {
	Body struct {
		CheckoutUrl string `json:"checkoutUrl"`
	}
}

func (deps Deps) eventDetailBody(ctx context.Context, eventID, callerID uuid.UUID, admin bool) (*EventDetailBody, error) {
	d, err := deps.Events.GetDetail(ctx, eventID, callerID, admin)
	if err != nil {
		return nil, mapEventErr(err)
	}
	organizer, err := deps.Queries.GetUserByID(ctx, d.Event.OrganizerID)
	if err != nil {
		return nil, err
	}
	body := &EventDetailBody{
		EventSummary:   eventSummaryFrom(d.EventInfo),
		Description:    d.Event.Description,
		RefundDeadline: d.Event.RefundDeadline,
		OrganizerName:  organizer.DisplayName,
		Cubes:          make([]EventCubeEntry, len(d.Cubes)),
		Attendees:      d.Attendees,
	}
	for i, c := range d.Cubes {
		body.Cubes[i] = EventCubeEntry{
			CubeID: c.CubeID, CubeName: c.CubeName, CubeChangeID: c.CubeChangeID,
			PinnedVersion: c.PinnedVersion, PinnedAt: c.PinnedAt,
		}
	}
	if d.MyRegistration != nil {
		info := registrationInfoFrom(*d.MyRegistration)
		body.MyRegistration = &info
	}
	return body, nil
}

func registerEvents(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "listEvents",
		Method:      http.MethodGet,
		Path:        "/api/events",
		Summary:     "All visible events (admins also see drafts)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, _ *struct{}) (*listEventsOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		infos, err := deps.Events.List(ctx, uid, isAdmin(ctx, deps))
		if err != nil {
			return nil, mapEventErr(err)
		}
		out := &listEventsOutput{}
		out.Body.Events = make([]EventSummary, len(infos))
		for i, e := range infos {
			out.Body.Events[i] = eventSummaryFrom(e)
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getEvent",
		Method:      http.MethodGet,
		Path:        "/api/events/{eventId}",
		Summary:     "Event detail with cubes, attendees, and the caller's registration",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*eventDetailOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		body, err := deps.eventDetailBody(ctx, id, uid, isAdmin(ctx, deps))
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "registerForEvent",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/register",
		Summary:     "Register (or join the waitlist when full)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*registrationOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		reg, err := deps.Events.Register(ctx, id, uid)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "payRegistration",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/registration/pay",
		Summary:     "Get a Stripe Checkout URL for the pending registration",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*payOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		url, err := deps.Events.Pay(ctx, id, uid)
		if err != nil {
			return nil, mapEventErr(err)
		}
		out := &payOutput{}
		out.Body.CheckoutUrl = url
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cancelMyRegistration",
		Method:      http.MethodDelete,
		Path:        "/api/events/{eventId}/registration",
		Summary:     "Cancel own registration (refund policy applies)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*registrationOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		reg, err := deps.Events.CancelRegistration(ctx, id, uid)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})
}
