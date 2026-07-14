package config

import (
	"strings"
	"testing"
)

func TestValidateStripe(t *testing.T) {
	tests := []struct {
		name          string
		secretKey     string
		webhookSecret string
		wantErr       string // substring naming the missing variable; "" = ok
	}{
		{"neither set (free events only)", "", "", ""},
		{"both set", "sk_test_x", "whsec_x", ""},
		{"key without webhook secret", "sk_test_x", "", "STRIPE_WEBHOOK_SECRET is missing"},
		{"webhook secret without key", "", "whsec_x", "STRIPE_SECRET_KEY is missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{StripeSecretKey: tt.secretKey, StripeWebhookSecret: tt.webhookSecret}
			err := cfg.ValidateStripe()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error naming %q, got %v", tt.wantErr, err)
			}
		})
	}
}
