package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

	// 5. Unverified provider email colliding with an existing account's email
	// must be rejected, not silently linked or shadow-created.
	eu := auth.ProviderUser{ID: "d777", Email: "other@x.y", DisplayName: "Evil", EmailVerified: false}
	_, err = o.CompleteLogin(ctx, "discord", eu, uuid.Nil)
	if !errors.Is(err, auth.ErrEmailCollision) {
		t.Fatalf("err = %v, want ErrEmailCollision", err)
	}
	if _, err := q.GetOAuthIdentity(ctx, db.GetOAuthIdentityParams{Provider: "discord", ProviderUserID: "d777"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected no oauth identity created for rejected collision, got err=%v", err)
	}

	// 6. Verified provider email matching an existing UNVERIFIED local account
	// links onto it, wipes its password (pre-account-hijack defense), and
	// marks the email verified.
	const oldPassword = "correct horse battery staple"
	hash, err := auth.HashPassword(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	victim, err := q.CreateUser(ctx, db.CreateUserParams{
		Email:        "victim@x.y",
		DisplayName:  "Victim",
		PasswordHash: &hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if victim.EmailVerifiedAt != nil {
		t.Fatalf("victim should start unverified, got %+v", victim)
	}

	vu := auth.ProviderUser{ID: "g555", Email: "victim@x.y", DisplayName: "Victim G", EmailVerified: true}
	uid6, err := o.CompleteLogin(ctx, "google", vu, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if uid6 != victim.ID {
		t.Fatalf("uid6 = %v, want same user id %v", uid6, victim.ID)
	}

	svc := auth.NewService(q, nil, "http://test")
	if _, err := svc.Login(ctx, "victim@x.y", oldPassword); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("login with old password: err = %v, want ErrInvalidCredentials", err)
	}

	victimAfter, err := q.GetUserByID(ctx, victim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if victimAfter.EmailVerifiedAt == nil {
		t.Fatal("victim account should be verified after linking")
	}
	if victimAfter.PasswordHash != nil {
		t.Fatal("victim account password hash should be wiped after linking")
	}
}
