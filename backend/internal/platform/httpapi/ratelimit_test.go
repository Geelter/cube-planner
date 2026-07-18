package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	now := time.Unix(0, 0)
	l := newRateLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := range 3 {
		if !l.allow("k") {
			t.Fatalf("request %d within the limit must pass", i+1)
		}
	}
	if l.allow("k") {
		t.Fatal("request over the limit must be blocked")
	}
	if !l.allow("other") {
		t.Fatal("different keys must not share a bucket")
	}
	now = now.Add(61 * time.Second)
	if !l.allow("k") {
		t.Fatal("a new window must reset the count")
	}
}

func TestClientIPIgnoresSpoofedForwardedHops(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/auth/login", nil)
	r.RemoteAddr = "10.0.0.5:44444"

	// No proxy header: socket address.
	if got := clientIP(r); got != "10.0.0.5" {
		t.Fatalf("no XFF: got %q", got)
	}

	// Caddy APPENDS the real peer to whatever the client sent, so only the
	// rightmost hop is trustworthy — the left entries are attacker-typed.
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8, 203.0.113.9")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Fatalf("spoofed XFF: got %q, want the rightmost hop", got)
	}
}
