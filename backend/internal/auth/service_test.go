package auth_test

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

// capturingMailer records sent mail for assertions.
type capturingMailer struct {
	mu   sync.Mutex
	last string
}

func (m *capturingMailer) Send(_ context.Context, _, _, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.last = body
	return nil
}

var tokenRe = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func TestRegisterVerifyLogin(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	mailer := &capturingMailer{}
	svc := auth.NewService(q, mailer, "http://localhost:5173")
	ctx := context.Background()

	if err := svc.Register(ctx, "Bob@Example.com", "Bob", "password123"); err != nil {
		t.Fatal(err)
	}

	// Re-registering the same, still-unverified email overwrites the
	// pending account rather than failing (see TestRegisterUnverifiedOverwrite
	// for a dedicated test of this behavior); the rest of this test
	// continues using the original password, which remains valid because
	// this second call reuses it.
	if err := svc.Register(ctx, "bob@example.com", "Bob2", "password123"); err != nil {
		t.Fatal(err)
	}

	// Login before verification is rejected.
	if _, err := svc.Login(ctx, "bob@example.com", "password123"); !errors.Is(err, auth.ErrEmailNotVerified) {
		t.Fatalf("err = %v, want ErrEmailNotVerified", err)
	}

	// Verify with the token from the email.
	m := tokenRe.FindStringSubmatch(mailer.last)
	if m == nil {
		t.Fatalf("no token in mail body: %q", mailer.last)
	}
	if err := svc.VerifyEmail(ctx, m[1]); err != nil {
		t.Fatal(err)
	}
	// Token is single-use.
	if err := svc.VerifyEmail(ctx, m[1]); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken on reuse", err)
	}

	// Login now works; wrong password still fails.
	u, err := svc.Login(ctx, "bob@example.com", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "bob@example.com" {
		t.Fatalf("email = %q", u.Email)
	}
	if _, err := svc.Login(ctx, "bob@example.com", "nope"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestRegisterUnverifiedOverwrite(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	mailer := &capturingMailer{}
	svc := auth.NewService(q, mailer, "http://localhost:5173")
	ctx := context.Background()

	if err := svc.Register(ctx, "alice@example.com", "Alice", "passwordA"); err != nil {
		t.Fatal(err)
	}

	// Re-register the same, still-unverified email with a new password and
	// display name. This should succeed and overwrite the pending account.
	if err := svc.Register(ctx, "alice@example.com", "Alice2", "passwordB"); err != nil {
		t.Fatalf("re-register unverified: %v", err)
	}

	// Verify using the SECOND token from the mailer.
	m := tokenRe.FindStringSubmatch(mailer.last)
	if m == nil {
		t.Fatalf("no token in mail body: %q", mailer.last)
	}
	if err := svc.VerifyEmail(ctx, m[1]); err != nil {
		t.Fatal(err)
	}

	// New password works.
	u, err := svc.Login(ctx, "alice@example.com", "passwordB")
	if err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if u.DisplayName != "Alice2" {
		t.Fatalf("display name = %q, want Alice2", u.DisplayName)
	}

	// Old password no longer works.
	if _, err := svc.Login(ctx, "alice@example.com", "passwordA"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestRegisterVerifiedEmailTaken(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	mailer := &capturingMailer{}
	svc := auth.NewService(q, mailer, "http://localhost:5173")
	ctx := context.Background()

	if err := svc.Register(ctx, "carol@example.com", "Carol", "password123"); err != nil {
		t.Fatal(err)
	}
	m := tokenRe.FindStringSubmatch(mailer.last)
	if m == nil {
		t.Fatalf("no token in mail body: %q", mailer.last)
	}
	if err := svc.VerifyEmail(ctx, m[1]); err != nil {
		t.Fatal(err)
	}

	// Once verified, re-registering must fail with ErrEmailTaken.
	if err := svc.Register(ctx, "carol@example.com", "Carol2", "password456"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
}

func TestRegisterInvalidDisplayName(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	mailer := &capturingMailer{}
	svc := auth.NewService(q, mailer, "http://localhost:5173")
	ctx := context.Background()

	cases := []struct {
		name        string
		email       string
		displayName string
	}{
		{"crlf injection", "dave@example.com", "Evil\r\nname"},
		{"nul byte", "erin@example.com", "Evil\x00name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := svc.Register(ctx, tc.email, tc.displayName, "password123"); !errors.Is(err, auth.ErrInvalidDisplayName) {
				t.Fatalf("err = %v, want ErrInvalidDisplayName", err)
			}
			if _, err := q.GetUserByEmail(ctx, tc.email); err == nil {
				t.Fatalf("expected no user row created for %s", tc.email)
			}
		})
	}
}
