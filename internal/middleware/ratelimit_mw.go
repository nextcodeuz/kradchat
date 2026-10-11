package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"kradchat/internal/security"
)

// RateLimit per-IP token bucket.
func RateLimit(limiter *security.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)
			// Statik fayllar limitga tushmaydi (chat sohasida faol).
			if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/" {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.Allow("ip:" + ip) {
				w.Header().Set("Retry-After", "60")
				WriteError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PeriodicGC har rateLimitWindow ichida limiter bucketlarini tozalaydi.
type PeriodicGC struct {
	limiter *security.RateLimiter
	ticker  *time.Ticker
	stop    chan struct{}
	once    sync.Once
}

func NewPeriodicGC(limiter *security.RateLimiter, window time.Duration) *PeriodicGC {
	if window < time.Second {
		window = time.Minute
	}
	return &PeriodicGC{
		limiter: limiter,
		ticker:  time.NewTicker(window),
		stop:    make(chan struct{}),
	}
}

func (g *PeriodicGC) Start() {
	go func() {
		for {
			select {
			case <-g.ticker.C:
				g.limiter.GCSweep()
			case <-g.stop:
				return
			}
		}
	}()
}

func (g *PeriodicGC) Stop() {
	g.once.Do(func() {
		g.ticker.Stop()
		close(g.stop)
	})
}
