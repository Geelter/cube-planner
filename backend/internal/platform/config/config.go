package config

import (
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
