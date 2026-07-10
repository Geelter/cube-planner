package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/mail"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailNotVerified   = errors.New("email not verified")
)

const (
	purposeEmailVerification = "email_verification"
	purposePasswordReset     = "password_reset"

	verificationTTL = 24 * time.Hour
	resetTTL        = time.Hour
)

type Service struct {
	q       *db.Queries
	mailer  mail.Mailer
	baseURL string
}

func NewService(q *db.Queries, mailer mail.Mailer, baseURL string) *Service {
	return &Service{q: q, mailer: mailer, baseURL: baseURL}
}

func (s *Service) Register(ctx context.Context, email, displayName, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	u, err := s.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: &hash,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return ErrEmailTaken
		}
		return err
	}
	return s.sendVerification(ctx, u)
}

func (s *Service) sendVerification(ctx context.Context, u db.User) error {
	tok, hash, err := newToken()
	if err != nil {
		return err
	}
	err = s.q.CreateAuthToken(ctx, db.CreateAuthTokenParams{
		TokenHash: hash,
		UserID:    u.ID,
		Purpose:   purposeEmailVerification,
		ExpiresAt: time.Now().Add(verificationTTL),
	})
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Hi %s,\n\nVerify your Cube Planner account:\n%s/verify-email?token=%s\n\nThe link expires in 24 hours.",
		u.DisplayName, s.baseURL, tok)
	return s.mailer.Send(ctx, u.Email, "Verify your Cube Planner account", body)
}

func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	userID, err := s.q.ConsumeAuthToken(ctx, db.ConsumeAuthTokenParams{
		TokenHash: hashToken(token),
		Purpose:   purposeEmailVerification,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		return err
	}
	return s.q.MarkEmailVerified(ctx, userID)
}

func (s *Service) Login(ctx context.Context, email, password string) (db.User, error) {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.User{}, ErrInvalidCredentials
		}
		return db.User{}, err
	}
	if u.PasswordHash == nil || !VerifyPassword(password, *u.PasswordHash) {
		return db.User{}, ErrInvalidCredentials
	}
	if u.EmailVerifiedAt == nil {
		return db.User{}, ErrEmailNotVerified
	}
	return u, nil
}

func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // do not reveal whether the email exists
		}
		return err
	}
	tok, hash, err := newToken()
	if err != nil {
		return err
	}
	err = s.q.CreateAuthToken(ctx, db.CreateAuthTokenParams{
		TokenHash: hash,
		UserID:    u.ID,
		Purpose:   purposePasswordReset,
		ExpiresAt: time.Now().Add(resetTTL),
	})
	if err != nil {
		return err
	}
	body := fmt.Sprintf("Hi %s,\n\nReset your Cube Planner password:\n%s/reset-password?token=%s\n\nThe link expires in 1 hour. If you didn't request this, ignore this mail.",
		u.DisplayName, s.baseURL, tok)
	return s.mailer.Send(ctx, u.Email, "Reset your Cube Planner password", body)
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	userID, err := s.q.ConsumeAuthToken(ctx, db.ConsumeAuthTokenParams{
		TokenHash: hashToken(token),
		Purpose:   purposePasswordReset,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.q.SetPasswordHash(ctx, db.SetPasswordHashParams{PasswordHash: &hash, ID: userID}); err != nil {
		return err
	}
	// Changing the password invalidates all existing sessions.
	return s.q.DeleteSessionsForUser(ctx, userID)
}
