package flinterr

import "time"

// Clock abstracts time operations for testability.
// Inject RealClock in production, FixedClock in tests.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
}

// RealClock uses the real system clock.
type RealClock struct{}

func (RealClock) Now() time.Time                  { return time.Now() }
func (RealClock) Since(t time.Time) time.Duration { return time.Since(t) }

// FixedClock returns a fixed time. Useful in tests.
type FixedClock struct {
	T time.Time
}

func (c FixedClock) Now() time.Time                  { return c.T }
func (c FixedClock) Since(t time.Time) time.Duration { return c.T.Sub(t) }
