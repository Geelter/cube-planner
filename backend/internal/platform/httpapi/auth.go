package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
)

type registerInput struct {
	Body struct {
		Email       string `json:"email" format:"email" maxLength:"254"`
		DisplayName string `json:"displayName" minLength:"1" maxLength:"50"`
		Password    string `json:"password" minLength:"8" maxLength:"200"`
	}
}

type verifyEmailInput struct {
	Body struct {
		Token string `json:"token" minLength:"1"`
	}
}

func registerAuth(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID:   "register",
		Method:        http.MethodPost,
		Path:          "/api/auth/register",
		Summary:       "Register with email and password",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *registerInput) (*struct{}, error) {
		err := deps.Auth.Register(ctx, in.Body.Email, in.Body.DisplayName, in.Body.Password)
		if errors.Is(err, auth.ErrEmailTaken) {
			return nil, huma.Error409Conflict("email already registered")
		}
		if errors.Is(err, auth.ErrInvalidDisplayName) {
			return nil, huma.Error422UnprocessableEntity("display name contains invalid characters")
		}
		return nil, err
	})

	huma.Register(api, huma.Operation{
		OperationID:   "verify-email",
		Method:        http.MethodPost,
		Path:          "/api/auth/verify-email",
		Summary:       "Verify email address with a token",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *verifyEmailInput) (*struct{}, error) {
		err := deps.Auth.VerifyEmail(ctx, in.Body.Token)
		if errors.Is(err, auth.ErrInvalidToken) {
			return nil, huma.Error422UnprocessableEntity("invalid or expired token")
		}
		return nil, err
	})
}
