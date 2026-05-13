package background

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestGroup_BasicGoWait sanity-checks the happy path: Go schedules a
// goroutine, Wait blocks until it finishes.
func TestGroup_BasicGoWait(t *testing.T) {
	var g Group
	var ran atomic.Bool
	g.Go(func() {
		time.Sleep(10 * time.Millisecond)
		ran.Store(true)
	})
	g.Wait()
	if !ran.Load() {
		t.Fatal("Wait returned before Go-scheduled function completed")
	}
}

// TestGroup_GoAfterCloseIsDropped verifies the shutdown contract
// documented in the cycle 2 bug-hunt assessment.
//
// Scenario, mirroring cmd/rdr/main.go:
//
//  1. An HTTP handler is mid-flight doing synchronous work (e.g.
//     handleImportOPML's up-to-5000 sequential INSERTs).
//
//  2. SIGTERM fires. main.go calls httpServer.Shutdown(ctx) with a
//     10s deadline. Shutdown returns at the deadline while the handler
//     is still running.
//
//  3. main.go calls bg.Close() to mark the Group as no longer
//     accepting new work, then bg.Wait().
//
//  4. The handler eventually completes its synchronous work and calls
//     s.bg.Go(...). The Group must drop the call (logged, not
//     panicked) so that main.go can proceed to db.Close() without an
//     orphan goroutine racing it.
//
// Without Close(), this is sync.WaitGroup contract violation: a Wait()
// that returns on a zero counter does not set the waiter bit, so a
// subsequent Add() looks (to the runtime) like a fresh first Add. The
// race detector does NOT trip on this sequence — the bug is silent.
// The Close() flag is what makes the contract enforceable at the type
// level rather than relying on cooperative shutdown ordering.
func TestGroup_GoAfterCloseIsDropped(t *testing.T) {
	var g Group

	// Simulate main.go's Wait on an empty Group (handler hasn't yet
	// reached its s.bg.Go line). Then Close, mirroring the recommended
	// shutdown protocol.
	waitReturned := make(chan struct{})
	go func() {
		g.Wait()
		close(waitReturned)
	}()
	select {
	case <-waitReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return on an empty Group")
	}
	g.Close()

	// Simulate the in-flight handler finally reaching its s.bg.Go(...)
	// line, AFTER Close. The Group must drop the call rather than
	// scheduling work that would race the deferred db.Close().
	var ran atomic.Bool
	g.Go(func() {
		ran.Store(true)
	})

	// Give the (dropped) goroutine plenty of time to run if it was
	// (incorrectly) scheduled.
	time.Sleep(50 * time.Millisecond)

	if ran.Load() {
		t.Errorf(
			"background.Group ran a function scheduled via Go() AFTER Close(); " +
				"the dropped-after-Close contract was violated, and a real " +
				"main.go shutdown would race the deferred db.Close()",
		)
	}
}

// TestGroup_CloseIsIdempotent confirms Close can be called multiple
// times without panicking.
func TestGroup_CloseIsIdempotent(t *testing.T) {
	var g Group
	g.Close()
	g.Close()
	g.Close()
}
