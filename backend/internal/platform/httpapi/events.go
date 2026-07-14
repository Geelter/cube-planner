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

	registerEventAdmin(api, deps)
}

func requireAdmin(ctx context.Context, deps Deps) (uuid.UUID, error) {
	uid, ok := CurrentUserID(ctx)
	if !ok {
		return uuid.Nil, huma.Error401Unauthorized("authentication required")
	}
	u, err := deps.Queries.GetUserByID(ctx, uid)
	if err != nil {
		return uuid.Nil, err
	}
	if u.Role != "admin" {
		return uuid.Nil, eventProblem(http.StatusForbidden, "admin-required", "organizer access required")
	}
	return uid, nil
}

type createEventInput struct {
	Body struct {
		Name            string     `json:"name" minLength:"1" maxLength:"200"`
		Description     string     `json:"description,omitempty" maxLength:"5000"`
		Location        string     `json:"location,omitempty" maxLength:"200"`
		StartsAt        time.Time  `json:"startsAt"`
		FeeCents        int32      `json:"feeCents" minimum:"0" maximum:"10000000"`
		Currency        string     `json:"currency,omitempty" pattern:"^[a-z]{3}$"`
		MaxParticipants int32      `json:"maxParticipants" minimum:"1" maximum:"1000"`
		RefundDeadline  *time.Time `json:"refundDeadline,omitempty"`
	}
}

type updateEventInput struct {
	EventID string `path:"eventId"`
	Body    struct {
		Name            *string    `json:"name,omitempty" minLength:"1" maxLength:"200"`
		Description     *string    `json:"description,omitempty" maxLength:"5000"`
		Location        *string    `json:"location,omitempty" maxLength:"200"`
		StartsAt        *time.Time `json:"startsAt,omitempty"`
		FeeCents        *int32     `json:"feeCents,omitempty" minimum:"0" maximum:"10000000"`
		Currency        *string    `json:"currency,omitempty" pattern:"^[a-z]{3}$"`
		MaxParticipants *int32     `json:"maxParticipants,omitempty" minimum:"1" maximum:"1000"`
		RefundDeadline  *time.Time `json:"refundDeadline,omitempty"`
	}
}

// EventCubeLink is exported (rather than inlined) so huma's schema
// registry gives it a distinct name: an anonymous `[]struct{...}` here
// would be named "Item" like importItemsInput's element, which collides
// in huma's global registry (huma names slice elements "<ParentName>Item",
// and anonymous parents all resolve to the empty name).
type EventCubeLink struct {
	CubeID       uuid.UUID  `json:"cubeId"`
	CubeChangeID *uuid.UUID `json:"cubeChangeId,omitempty"`
}

type setEventCubesInput struct {
	EventID string `path:"eventId"`
	Body    struct {
		Cubes []EventCubeLink `json:"cubes" maxItems:"20"`
	}
}

type EventRegistrationRow struct {
	RegistrationInfo
	DisplayName string    `json:"displayName"`
	Email       string    `json:"email"`
	CreatedAt   time.Time `json:"createdAt"`
}

type listRegistrationsOutput struct {
	Body struct {
		Registrations []EventRegistrationRow `json:"registrations"`
	}
}

type registrationActionInput struct {
	EventID        string `path:"eventId"`
	RegistrationID string `path:"registrationId"`
}

func registerEventAdmin(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "createEvent",
		Method:      http.MethodPost,
		Path:        "/api/events",
		Summary:     "Create a draft event (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *createEventInput) (*eventDetailOutput, error) {
		uid, err := requireAdmin(ctx, deps)
		if err != nil {
			return nil, err
		}
		ev, err := deps.Events.Create(ctx, uid, events.CreateEventParams{
			Name: in.Body.Name, Description: in.Body.Description,
			Location: in.Body.Location, StartsAt: in.Body.StartsAt,
			FeeCents: in.Body.FeeCents, Currency: in.Body.Currency,
			MaxParticipants: in.Body.MaxParticipants,
			RefundDeadline:  in.Body.RefundDeadline,
		})
		if err != nil {
			return nil, mapEventErr(err)
		}
		body, err := deps.eventDetailBody(ctx, ev.ID, uid, true)
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateEvent",
		Method:      http.MethodPatch,
		Path:        "/api/events/{eventId}",
		Summary:     "Edit an event (field whitelist depends on lifecycle)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *updateEventInput) (*eventDetailOutput, error) {
		uid, err := requireAdmin(ctx, deps)
		if err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		if _, err := deps.Events.Update(ctx, id, events.UpdateEventParams{
			Name: in.Body.Name, Description: in.Body.Description,
			Location: in.Body.Location, StartsAt: in.Body.StartsAt,
			FeeCents: in.Body.FeeCents, Currency: in.Body.Currency,
			MaxParticipants: in.Body.MaxParticipants,
			RefundDeadline:  in.Body.RefundDeadline,
		}); err != nil {
			return nil, mapEventErr(err)
		}
		body, err := deps.eventDetailBody(ctx, id, uid, true)
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	for _, action := range []string{"publish", "start", "finish", "cancel"} {
		huma.Register(api, huma.Operation{
			OperationID: action + "Event",
			Method:      http.MethodPost,
			Path:        "/api/events/{eventId}/" + action,
			Summary:     "Lifecycle: " + action,
			Tags:        []string{"events"},
		}, func(ctx context.Context, in *eventIDInput) (*eventDetailOutput, error) {
			uid, err := requireAdmin(ctx, deps)
			if err != nil {
				return nil, err
			}
			id, err := parseEventID(in.EventID)
			if err != nil {
				return nil, err
			}
			if _, err := deps.Events.Transition(ctx, id, action); err != nil {
				return nil, mapEventErr(err)
			}
			body, err := deps.eventDetailBody(ctx, id, uid, true)
			if err != nil {
				return nil, err
			}
			return &eventDetailOutput{Body: *body}, nil
		})
	}

	huma.Register(api, huma.Operation{
		OperationID: "setEventCubes",
		Method:      http.MethodPut,
		Path:        "/api/events/{eventId}/cubes",
		Summary:     "Replace the event's linked cubes (draft only)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *setEventCubesInput) (*eventDetailOutput, error) {
		uid, err := requireAdmin(ctx, deps)
		if err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		links := make([]events.CubeLinkInput, len(in.Body.Cubes))
		for i, c := range in.Body.Cubes {
			links[i] = events.CubeLinkInput{CubeID: c.CubeID, CubeChangeID: c.CubeChangeID}
		}
		if err := deps.Events.SetCubes(ctx, id, links); err != nil {
			return nil, mapEventErr(err)
		}
		body, err := deps.eventDetailBody(ctx, id, uid, true)
		if err != nil {
			return nil, err
		}
		return &eventDetailOutput{Body: *body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listEventRegistrations",
		Method:      http.MethodGet,
		Path:        "/api/events/{eventId}/registrations",
		Summary:     "Every registration incl. waitlist order and refund queue (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *eventIDInput) (*listRegistrationsOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		id, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		rows, err := deps.Queries.ListEventRegistrations(ctx, id)
		if err != nil {
			return nil, err
		}
		out := &listRegistrationsOutput{}
		out.Body.Registrations = make([]EventRegistrationRow, len(rows))
		for i, r := range rows {
			out.Body.Registrations[i] = EventRegistrationRow{
				RegistrationInfo: RegistrationInfo{
					ID: r.ID, Status: r.Status, ExpiresAt: r.ExpiresAt,
					WaitlistPos: r.WaitlistPos, PaidAt: r.PaidAt,
				},
				DisplayName: r.DisplayName, Email: r.Email, CreatedAt: r.CreatedAt,
			}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "refundRegistration",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/registrations/{registrationId}/refund",
		Summary:     "Refund a queued or paid registration (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *registrationActionInput) (*registrationOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		eventID, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		regID, err := uuid.Parse(in.RegistrationID)
		if err != nil {
			return nil, eventProblem(http.StatusNotFound, "registration-not-found", "no such registration")
		}
		reg, err := deps.Events.OrganizerRefund(ctx, eventID, regID)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "denyRefund",
		Method:      http.MethodPost,
		Path:        "/api/events/{eventId}/registrations/{registrationId}/deny-refund",
		Summary:     "Deny a queued refund request (organizer)",
		Tags:        []string{"events"},
	}, func(ctx context.Context, in *registrationActionInput) (*registrationOutput, error) {
		if _, err := requireAdmin(ctx, deps); err != nil {
			return nil, err
		}
		eventID, err := parseEventID(in.EventID)
		if err != nil {
			return nil, err
		}
		regID, err := uuid.Parse(in.RegistrationID)
		if err != nil {
			return nil, eventProblem(http.StatusNotFound, "registration-not-found", "no such registration")
		}
		reg, err := deps.Events.DenyRefund(ctx, eventID, regID)
		if err != nil {
			return nil, mapEventErr(err)
		}
		return &registrationOutput{Body: registrationInfoFrom(*reg)}, nil
	})
}
