package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/auth"
	"github.com/mjabloniec/cube-planner/backend/internal/db"
	"github.com/mjabloniec/cube-planner/backend/internal/platform/testdb"
)

func TestSessionLifecycle(t *testing.T) {
	pool := testdb.New(t)
	q := db.New(pool)
	sessions := auth.NewSessions(q, false)
	ctx := context.Background()

	u, err := q.CreateUser(ctx, db.CreateUserParams{Email: "s@x.y", DisplayName: "S"})
	if err != nil {
		t.Fatal(err)
	}

	cookie, err := sessions.Create(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cookie.Name != "cp_session" || !cookie.HttpOnly {
		t.Fatalf("bad cookie: %+v", cookie)
	}

	uid, err := sessions.UserID(ctx, cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	if uid != u.ID {
		t.Fatalf("uid = %v, want %v", uid, u.ID)
	}

	if err := sessions.Revoke(ctx, cookie.Value); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.UserID(ctx, cookie.Value); !errors.Is(err, auth.ErrNoSession) {
		t.Fatalf("err = %v, want ErrNoSession after revoke", err)
	}
}
