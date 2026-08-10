package agentd

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// A step's result is work already done and paid for. Shutdown must let an
// in-flight execution finish reporting it: Run used to return the instant its
// context was cancelled, and the deferred client and runtime teardown then ran
// while steps were still executing — so a step that had just succeeded lost its
// completion report. The engine heard nothing, swept the step at its deadline,
// and (now that a timeout honours the retry policy) ran the whole thing again.
func TestAwaitExecutions_WaitsForInFlightWork(t *testing.T) {
	d := &Daemon{active: map[string]*execution{}}

	reported := atomic.Bool{}
	d.running.Add(1)
	go func() {
		defer d.running.Done()
		time.Sleep(50 * time.Millisecond) // the completion report
		reported.Store(true)
	}()

	start := time.Now()
	d.awaitExecutions()

	assert.True(t, reported.Load(), "shutdown must not tear down while a step is still reporting")
	assert.Less(t, time.Since(start), shutdownGrace, "and must not wait the full grace when work finishes")
}

// It must not block forever on a step that never finishes.
func TestAwaitExecutions_GivesUpAfterTheGrace(t *testing.T) {
	if testing.Short() {
		t.Skip("bounded-wait check takes the full grace period")
	}
	d := &Daemon{active: map[string]*execution{}}

	d.running.Add(1) // never released — a step that will not return
	defer d.running.Done()

	done := make(chan struct{})
	go func() {
		d.awaitExecutions()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(shutdownGrace + 5*time.Second):
		t.Fatal("awaitExecutions did not respect its grace period")
	}
}

// With nothing running, shutdown is immediate.
func TestAwaitExecutions_ReturnsImmediatelyWhenIdle(t *testing.T) {
	d := &Daemon{active: map[string]*execution{}}

	start := time.Now()
	d.awaitExecutions()
	assert.Less(t, time.Since(start), time.Second)
}
