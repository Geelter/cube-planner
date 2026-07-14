package config

import (
	"errors"
	"os"
	"strconv"
)

type SMTPConfig struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

type OAuthCredentials struct {
	ClientID     string
	ClientSecret string
}

type Config struct {
	Port                int
	Env                 string
	DatabaseURL         string
	BaseURL             string
	SMTP                SMTPConfig
	Discord             OAuthCredentials
	Google              OAuthCredentials
	CardsSyncEnabled    bool
	ScryfallBaseURL     string
	StripeSecretKey     string
	StripeWebhookSecret string
}

func (c Config) Secure() bool { return c.Env == "prod" }

// ValidateStripe rejects a half-configured payment setup, which is worse
// than none: with only the secret key, paid events publish and cards get
// charged, but the webhook route never mounts so nothing ever flips to
// paid; with only the webhook secret, no session can be created at all.
// Both keys or neither.
func (c Config) ValidateStripe() error {
	switch {
	case c.StripeSecretKey != "" && c.StripeWebhookSecret == "":
		return errors.New("STRIPE_SECRET_KEY is set but STRIPE_WEBHOOK_SECRET is missing: payments would be charged but never confirmed — set both or neither")
	case c.StripeSecretKey == "" && c.StripeWebhookSecret != "":
		return errors.New("STRIPE_WEBHOOK_SECRET is set but STRIPE_SECRET_KEY is missing — set both or neither")
	default:
		return nil
	}
}

func Load() Config {
	return Config{
		Port:        envInt("PORT", 8080),
		Env:         env("ENV", "dev"),
		DatabaseURL: env("DATABASE_URL", ""),
		BaseURL:     env("BASE_URL", "http://localhost:5173"),
		SMTP: SMTPConfig{
			Host: env("SMTP_HOST", ""),
			Port: envInt("SMTP_PORT", 587),
			User: env("SMTP_USER", ""),
			Pass: env("SMTP_PASS", ""),
			From: env("SMTP_FROM", "cube-planner@localhost"),
		},
		Discord:             OAuthCredentials{ClientID: env("DISCORD_CLIENT_ID", ""), ClientSecret: env("DISCORD_CLIENT_SECRET", "")},
		Google:              OAuthCredentials{ClientID: env("GOOGLE_CLIENT_ID", ""), ClientSecret: env("GOOGLE_CLIENT_SECRET", "")},
		CardsSyncEnabled:    envBool("CARDS_SYNC_ENABLED", true),
		ScryfallBaseURL:     env("SCRYFALL_BASE_URL", "https://api.scryfall.com"),
		StripeSecretKey:     env("STRIPE_SECRET_KEY", ""),
		StripeWebhookSecret: env("STRIPE_WEBHOOK_SECRET", ""),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
