package flinterr_test

import (
	"testing"
	"time"

	"github.com/NerdMeNot/flint/internal/core/flinterr"
)

func TestRealClock(t *testing.T) {
	c := flinterr.RealClock{}

	before := time.Now()
	now := c.Now()
	after := time.Now()

	if now.Before(before) || now.After(after) {
		t.Errorf("RealClock.Now() = %v, expected between %v and %v", now, before, after)
	}

	past := time.Now().Add(-5 * time.Second)
	since := c.Since(past)
	if since < 5*time.Second {
		t.Errorf("RealClock.Since() = %v, expected >= 5s", since)
	}
}

func TestFixedClock(t *testing.T) {
	fixed := time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)
	c := flinterr.FixedClock{T: fixed}

	if got := c.Now(); !got.Equal(fixed) {
		t.Errorf("FixedClock.Now() = %v, want %v", got, fixed)
	}

	past := fixed.Add(-10 * time.Minute)
	if got := c.Since(past); got != 10*time.Minute {
		t.Errorf("FixedClock.Since() = %v, want 10m", got)
	}

	future := fixed.Add(5 * time.Minute)
	if got := c.Since(future); got != -5*time.Minute {
		t.Errorf("FixedClock.Since(future) = %v, want -5m", got)
	}
}
