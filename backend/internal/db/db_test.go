package db_test

import (
	"context"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestCreateAndGetUser(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	ctx := context.Background()

	hash := "fakehash"
	u, err := q.CreateUser(ctx, db.CreateUserParams{
		Email:        "Alice@Example.com",
		DisplayName:  "Alice",
		PasswordHash: &hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email = %q, want lowercased", u.Email)
	}

	got, err := q.GetUserByEmail(ctx, "ALICE@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != u.ID {
		t.Fatalf("got ID %v, want %v", got.ID, u.ID)
	}
	if got.EmailVerifiedAt != nil {
		t.Fatal("new user must not be verified")
	}
}
