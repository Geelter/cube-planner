package httpapi

import (
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
