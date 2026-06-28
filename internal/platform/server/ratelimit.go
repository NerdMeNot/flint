package server

import (
	"context"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"golang.org/x/time/rate"
)

// ipRateLimiter is a per-client-IP token-bucket limiter with idle eviction.
// In-memory (per-process) — sufficient for abuse protection on the auth/SCIM
// surface of a self-hosted instance; it is not a cross-replica quota.
type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	rps     rate.Limit
	burst   int
}

type ipBucket struct {
	lim  *rate.Limiter
	seen time.Time
}

func newIPRateLimiter(rps float64, burst int) *ipRateLimiter {
	l := &ipRateLimiter{buckets: make(map[string]*ipBucket), rps: rate.Limit(rps), burst: burst}
	go l.evictLoop()
	return l
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	b, ok := l.buckets[ip]
	if !ok {
		b = &ipBucket{lim: rate.NewLimiter(l.rps, l.burst)}
		l.buckets[ip] = b
	}
	b.seen = time.Now()
	l.mu.Unlock()
	return b.lim.Allow()
}

// evictLoop drops buckets unused for 10 minutes so the map can't grow unbounded.
func (l *ipRateLimiter) evictLoop() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		l.mu.Lock()
		for ip, b := range l.buckets {
			if b.seen.Before(cutoff) {
				delete(l.buckets, ip)
			}
		}
		l.mu.Unlock()
	}
}

// ipRateLimit returns middleware that throttles requests per client IP. On
// exhaustion it aborts with 429; the body is left empty so it works for both the
// HTML auth-callback flow and the JSON SCIM API.
func (s *Server) ipRateLimit(l *ipRateLimiter) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if !l.allow(extractClientIP(c).String()) {
			c.AbortWithStatus(consts.StatusTooManyRequests)
			return
		}
		c.Next(ctx)
	}
}
