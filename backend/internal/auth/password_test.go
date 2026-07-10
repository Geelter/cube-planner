package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := HashPassword("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected format: %s", encoded)
	}
	if !VerifyPassword("hunter22", encoded) {
		t.Fatal("correct password must verify")
	}
	if VerifyPassword("wrong", encoded) {
		t.Fatal("wrong password must not verify")
	}
	if VerifyPassword("hunter22", "garbage") {
		t.Fatal("garbage hash must not verify")
	}
}

func TestHashPasswordUsesRandomSalt(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
}
