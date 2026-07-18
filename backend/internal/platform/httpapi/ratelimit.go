package httpapi

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Auth endpoints get a per-IP, per-path fixed window: enough headroom for
// a venue full of players logging in from one NAT, far too little for
// credential stuffing or email-spam via register/forgot-password.
const (
	authRateLimit  = 20
	authRateWindow = time.Minute
)

// rateLimiter is a fixed-window counter. In-memory on purpose: the app
// deploys as a single instance (see deploy/README.md); a shared store
// would be dead weight until that changes.
type rateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string]*windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   map[string]*windowCount{},
	}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// Opportunistic pruning keeps the map bounded without a background
	// goroutine; 1024 live keys is far beyond a small community's traffic.
	if len(l.hits) > 1024 {
		for k, w := range l.hits {
			if now.Sub(w.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	w := l.hits[key]
	if w == nil || now.Sub(w.start) >= l.window {
		l.hits[key] = &windowCount{start: now, n: 1}
		return true
	}
	if w.n >= l.limit {
		return false
	}
	w.n++
	return true
}

// clientIP takes the RIGHTMOST X-Forwarded-For hop. Caddy (the only prod
// route to this API) appends the real peer address to whatever the client
// sent, so the left entries are attacker-typed and only the last one is
// trustworthy. Direct dev traffic has no header and falls back to the
// socket address.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		hops := strings.Split(xff, ",")
		return strings.TrimSpace(hops[len(hops)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authRateLimitMiddleware throttles POSTs under /api/auth/ (register,
// verify-email, login, logout, forgot/reset-password). GET /api/me and
// everything else pass through untouched.
func authRateLimitMiddleware(l *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/auth/") {
				if !l.allow(clientIP(r) + "|" + r.URL.Path) {
					w.Header().Set("Content-Type", "application/problem+json")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"title":"Too Many Requests","status":429,"detail":"rate limit exceeded, try again in a minute"}`))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
