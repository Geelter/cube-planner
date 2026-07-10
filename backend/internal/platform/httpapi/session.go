package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

type userIDKey struct{}

// sessionMiddleware resolves the session cookie into a user ID in context.
// It never rejects requests: handlers decide whether auth is required.
func sessionMiddleware(sessions *auth.Sessions) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if cookie, err := huma.ReadCookie(ctx, auth.SessionCookieName); err == nil && cookie.Value != "" {
			if uid, err := sessions.UserID(ctx.Context(), cookie.Value); err == nil {
				ctx = huma.WithValue(ctx, userIDKey{}, uid)
			}
		}
		next(ctx)
	}
}

// CurrentUserID returns the authenticated user's ID, if any.
func CurrentUserID(ctx context.Context) (uuid.UUID, bool) {
	uid, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return uid, ok
}

// UserBody is exported: huma derives the OpenAPI schema name (and thus the
// generated TS type name) from the Go type name.
type UserBody struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Providers   []string  `json:"providers"`
}

type loginInput struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password" minLength:"1"`
	}
}

type loginOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      UserBody
}

type meOutput struct {
	Body UserBody
}

type logoutInput struct {
	Cookie string `cookie:"cp_session"`
}

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func userBodyFor(ctx context.Context, deps Deps, u db.User) (UserBody, error) {
	providers, err := deps.Queries.ListProvidersForUser(ctx, u.ID)
	if err != nil {
		return UserBody{}, err
	}
	if providers == nil {
		providers = []string{}
	}
	return UserBody{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Providers: providers}, nil
}

func registerSession(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      http.MethodPost,
		Path:        "/api/auth/login",
		Summary:     "Log in with email and password",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, in *loginInput) (*loginOutput, error) {
		u, err := deps.Auth.Login(ctx, in.Body.Email, in.Body.Password)
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			return nil, huma.Error401Unauthorized("invalid email or password")
		case errors.Is(err, auth.ErrEmailNotVerified):
			return nil, huma.Error403Forbidden("email not verified")
		case err != nil:
			return nil, err
		}
		cookie, err := deps.Sessions.Create(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		body, err := userBodyFor(ctx, deps, u)
		if err != nil {
			return nil, err
		}
		return &loginOutput{SetCookie: *cookie, Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/api/auth/logout",
		Summary:       "Log out",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *logoutInput) (*logoutOutput, error) {
		if in.Cookie != "" {
			if err := deps.Sessions.Revoke(ctx, in.Cookie); err != nil {
				return nil, err
			}
		}
		return &logoutOutput{SetCookie: *deps.Sessions.ClearCookie()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/api/me",
		Summary:     "Current user",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, _ *struct{}) (*meOutput, error) {
		uid, ok := CurrentUserID(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		u, err := deps.Queries.GetUserByID(ctx, uid)
		if err != nil {
			return nil, err
		}
		body, err := userBodyFor(ctx, deps, u)
		if err != nil {
			return nil, err
		}
		return &meOutput{Body: body}, nil
	})
}
