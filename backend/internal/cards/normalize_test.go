package cards

import "testing"

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Lightning Bolt", "lightning bolt"},
		{"Lim-Dûl's Vault", "lim-dul's vault"},
		{"Séance", "seance"},
		{"Jötun Grunt", "jotun grunt"},
		{"Fire // Ice", "fire // ice"},
		// Æ is a distinct letter, not a diacritic — it stays. Scryfall
		// renamed these cards to "Aether…" anyway.
		{"Æther Vial", "æther vial"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeName(tt.in); got != tt.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"50% off_now", `50\% off\_now`},
		{`back\slash`, `back\\slash`},
	}
	for _, tt := range tests {
		if got := escapeLike(tt.in); got != tt.want {
			t.Errorf("escapeLike(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
