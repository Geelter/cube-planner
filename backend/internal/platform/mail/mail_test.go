package mail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	gomail "github.com/wneessen/go-mail"

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

func TestSMTPClientOptionsUsesOpportunisticTLS(t *testing.T) {
	cfg := config.SMTPConfig{Host: "mailpit", Port: 1025}
	client, err := gomail.NewClient(cfg.Host, smtpClientOptions(cfg)...)
	if err != nil {
		t.Fatal(err)
	}
	if got := client.TLSPolicy(); got != gomail.TLSOpportunistic.String() {
		t.Fatalf("want TLSOpportunistic policy, got %q", got)
	}
}

func TestSMTPClientOptionsOmitsAuthWithoutUser(t *testing.T) {
	cfg := config.SMTPConfig{Host: "mailpit", Port: 1025}
	opts := smtpClientOptions(cfg)
	if len(opts) != 2 {
		t.Fatalf("want 2 options (port, tls policy) when no user is set, got %d", len(opts))
	}
}

func TestSMTPClientOptionsAppliesAuthWithUser(t *testing.T) {
	cfg := config.SMTPConfig{Host: "smtp.example.com", Port: 587, User: "bob", Pass: "secret"}
	opts := smtpClientOptions(cfg)
	if len(opts) != 5 {
		t.Fatalf("want 5 options (port, tls policy, auth, username, password) when user is set, got %d", len(opts))
	}
	// Applying the options should not error, proving they are well-formed
	// go-mail options (e.g. WithUsername/WithPassword do not reject values).
	if _, err := gomail.NewClient(cfg.Host, opts...); err != nil {
		t.Fatalf("unexpected error constructing client with auth options: %v", err)
	}
}
