// Package ratelimit provides keyed token-bucket rate limiting.
//
// State is kept in memory per process. With several API replicas each one
// enforces its own budget, which is acceptable for abuse protection. Swap in
// a Redis-backed implementation of the same method set if exact global limits
// are ever required.
package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type entry struct {
	lim  *rate.Limiter
	seen time.Time
}

type Limiter struct {
	mu        sync.Mutex
	every     rate.Limit
	burst     int
	entries   map[string]*entry
	ttl       time.Duration
	lastSweep time.Time
}

// New allows `burst` events immediately and then one event every `per`.
func New(per time.Duration, burst int) *Limiter {
	return &Limiter{
		every:   rate.Every(per),
		burst:   burst,
		entries: map[string]*entry{},
		ttl:     30 * time.Minute,
	}
}

// Allow reports whether one more event for key is permitted now.
func (l *Limiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > time.Minute {
		for k, e := range l.entries {
			if now.Sub(e.seen) > l.ttl {
				delete(l.entries, k)
			}
		}
		l.lastSweep = now
	}
	e, ok := l.entries[key]
	if !ok {
		e = &entry{lim: rate.NewLimiter(l.every, l.burst)}
		l.entries[key] = e
	}
	e.seen = now
	return e.lim.AllowN(now, 1)
}
