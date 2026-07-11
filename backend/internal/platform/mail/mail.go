// Package mail sends transactional email. Dev uses the log mailer; prod
// uses SMTP (config-gated by SMTP_HOST).
package mail

import (
	"context"
	"log/slog"

	gomail "github.com/wneessen/go-mail"

	"github.com/mjabloniec/cube-planner/backend/internal/platform/config"
)

type Mailer interface {
	Send(ctx context.Context, to, subject, textBody string) error
}

func FromConfig(cfg config.Config) Mailer {
	if cfg.SMTP.Host != "" {
		return NewSMTPMailer(cfg.SMTP)
	}
	return NewLogMailer(slog.Default())
}

type logMailer struct{ log *slog.Logger }

func NewLogMailer(logger *slog.Logger) Mailer { return &logMailer{log: logger} }

func (m *logMailer) Send(_ context.Context, to, subject, textBody string) error {
	m.log.Info("mail (not sent, log mailer)", "to", to, "subject", subject, "body", textBody)
	return nil
}

type smtpMailer struct{ cfg config.SMTPConfig }

func NewSMTPMailer(cfg config.SMTPConfig) Mailer { return &smtpMailer{cfg: cfg} }

// smtpClientOptions builds the go-mail client options for cfg. TLS is
// opportunistic (used when the server advertises STARTTLS, plain otherwise)
// so both Mailpit (no STARTTLS) and a real relay work. SMTP AUTH is only
// requested when credentials are configured, since Mailpit advertises no
// AUTH mechanisms and forcing PLAIN auth against it fails.
func smtpClientOptions(cfg config.SMTPConfig) []gomail.Option {
	opts := []gomail.Option{
		gomail.WithPort(cfg.Port),
		gomail.WithTLSPolicy(gomail.TLSOpportunistic),
	}
	if cfg.User != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(cfg.User),
			gomail.WithPassword(cfg.Pass))
	}
	return opts
}

func (m *smtpMailer) Send(ctx context.Context, to, subject, textBody string) error {
	msg := gomail.NewMsg()
	if err := msg.From(m.cfg.From); err != nil {
		return err
	}
	if err := msg.To(to); err != nil {
		return err
	}
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextPlain, textBody)

	client, err := gomail.NewClient(m.cfg.Host, smtpClientOptions(m.cfg)...)
	if err != nil {
		return err
	}
	return client.DialAndSendWithContext(ctx, msg)
}
