package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
)

var ErrNoSession = errors.New("no valid session")

const (
	SessionCookieName = "cp_session"
	sessionTTL        = 30 * 24 * time.Hour
)

type Sessions struct {
	q      *db.Queries
	secure bool
}

func NewSessions(q *db.Queries, secure bool) *Sessions {
	return &Sessions{q: q, secure: secure}
}

func (s *Sessions) Create(ctx context.Context, userID uuid.UUID) (*http.Cookie, error) {
	tok, hash, err := newToken()
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(sessionTTL)
	err = s.q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: hash,
		UserID:    userID,
		ExpiresAt: expires,
	})
	if err != nil {
		return nil, err
	}
	return s.cookie(tok, expires), nil
}

func (s *Sessions) UserID(ctx context.Context, rawToken string) (uuid.UUID, error) {
	uid, err := s.q.GetSessionUserID(ctx, hashToken(rawToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoSession
	}
	return uid, err
}

func (s *Sessions) Revoke(ctx context.Context, rawToken string) error {
	return s.q.DeleteSession(ctx, hashToken(rawToken))
}

func (s *Sessions) ClearCookie() *http.Cookie {
	return s.cookie("", time.Unix(0, 0))
}

func (s *Sessions) cookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	}
}
