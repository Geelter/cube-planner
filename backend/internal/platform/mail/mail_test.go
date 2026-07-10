package mail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
)

func TestLogMailerLogsRecipientAndBody(t *testing.T) {
	var buf bytes.Buffer
	m := NewLogMailer(slog.New(slog.NewTextHandler(&buf, nil)))
	if err := m.Send(context.Background(), "a@b.c", "Verify", "http://x/verify?token=t"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "a@b.c") || !strings.Contains(out, "token=t") {
		t.Fatalf("log output missing fields: %s", out)
	}
}

func TestFromConfigPicksLogMailerWithoutSMTPHost(t *testing.T) {
	m := FromConfig(config.Config{})
	if _, ok := m.(*logMailer); !ok {
		t.Fatalf("want *logMailer, got %T", m)
	}
}
