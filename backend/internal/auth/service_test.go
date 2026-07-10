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

	// Duplicate email is rejected.
	if err := svc.Register(ctx, "bob@example.com", "Bob2", "password123"); !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
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
