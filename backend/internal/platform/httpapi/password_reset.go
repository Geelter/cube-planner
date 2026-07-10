package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
)

type forgotPasswordInput struct {
	Body struct {
		Email string `json:"email" format:"email"`
	}
}

type resetPasswordInput struct {
	Body struct {
		Token       string `json:"token" minLength:"1"`
		NewPassword string `json:"newPassword" minLength:"8" maxLength:"200"`
	}
}

func registerPasswordReset(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID:   "forgot-password",
		Method:        http.MethodPost,
		Path:          "/api/auth/forgot-password",
		Summary:       "Request a password reset email",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *forgotPasswordInput) (*struct{}, error) {
		// Always 204: do not reveal whether the email exists.
		return nil, deps.Auth.RequestPasswordReset(ctx, in.Body.Email)
	})

	huma.Register(api, huma.Operation{
		OperationID:   "reset-password",
		Method:        http.MethodPost,
		Path:          "/api/auth/reset-password",
		Summary:       "Reset password with a token",
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *resetPasswordInput) (*struct{}, error) {
		err := deps.Auth.ResetPassword(ctx, in.Body.Token, in.Body.NewPassword)
		if errors.Is(err, auth.ErrInvalidToken) {
			return nil, huma.Error422UnprocessableEntity("invalid or expired token")
		}
		return nil, err
	})
}
