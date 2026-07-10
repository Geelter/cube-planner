package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestCompleteLogin(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	sessions := auth.NewSessions(q, false)
	o := auth.NewOAuth(q, sessions, "http://test", false, nil)
	ctx := context.Background()

	pu := auth.ProviderUser{ID: "d123", Email: "Dana@X.Y", DisplayName: "Dana", EmailVerified: true}

	// 1. New identity, no matching user → creates a verified user.
	uid, err := o.CompleteLogin(ctx, "discord", pu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := q.GetUserByID(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "dana@x.y" || u.EmailVerifiedAt == nil {
		t.Fatalf("user = %+v, want lowercased verified email", u)
	}

	// 2. Same identity again → same user (login path).
	uid2, err := o.CompleteLogin(ctx, "discord", pu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if uid2 != uid {
		t.Fatalf("second login uid = %v, want %v", uid2, uid)
	}

	// 3. Different provider, same verified email → links to same user.
	gu := auth.ProviderUser{ID: "g999", Email: "dana@x.y", DisplayName: "Dana G", EmailVerified: true}
	uid3, err := o.CompleteLogin(ctx, "google", gu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if uid3 != uid {
		t.Fatalf("google login uid = %v, want %v", uid3, uid)
	}
	providers, err := q.ListProvidersForUser(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 2 {
		t.Fatalf("providers = %v, want discord+google", providers)
	}

	// 4. Explicit linking of an identity already owned by another user fails.
	other, err := q.CreateUser(ctx, db.CreateUserParams{Email: "other@x.y", DisplayName: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = o.CompleteLogin(ctx, "discord", pu, other.ID)
	if !errors.Is(err, auth.ErrIdentityTaken) {
		t.Fatalf("err = %v, want ErrIdentityTaken", err)
	}

	// 5. Unverified provider email must NOT auto-link to an existing account.
	eu := auth.ProviderUser{ID: "d777", Email: "other@x.y", DisplayName: "Evil", EmailVerified: false}
	uid5, err := o.CompleteLogin(ctx, "discord", eu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if uid5 == other.ID {
		t.Fatal("unverified email must not attach to existing account")
	}
}
