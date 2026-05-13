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

// TestGroup_GoAfterWait reproduces the shutdown-ordering race documented
// in the cycle 2 bug-hunt assessment.
//
// Scenario, mirroring cmd/rdr/main.go:
//
//  1. An HTTP handler is mid-flight doing synchronous work (e.g.
//     handleImportOPML's 5000 sequential INSERTs).
//
//  2. SIGTERM fires. main.go calls httpServer.Shutdown(ctx) with a
//     10s deadline. Shutdown returns at the deadline while the handler
//     is still running.
//
//  3. main.go calls bg.Wait(). Because the handler has not yet reached
//     its s.bg.Go(...) line, the WaitGroup counter is 0 and Wait
//     returns immediately.
//
//  4. The handler finally completes its synchronous work and calls
//     s.bg.Go(...). This is WaitGroup.Add(1) AFTER Wait returned 0 —
//     sync.WaitGroup explicitly forbids this:
//
//     "If a WaitGroup is reused to wait for several independent sets
//     of events, new Add calls must happen after all previous Wait
//     calls have returned."
//
//     In other words, once you've called Wait() and it returned, you
//     are not allowed to add more work — the Group is "consumed".
//     Subsequent Adds race with the next Wait (or with the program
//     shutting down) and the spawned goroutine runs against a DB that
//     main.go is about to defer-Close.
//
// This test reproduces the misuse without invoking the HTTP stack: a
// "shutdown" goroutine calls Wait() (which returns immediately because
// no work has been registered yet), and a "handler" goroutine then
// calls Go() concurrently. The Group is left observable so a fix can
// be verified (e.g. a Close() method that makes post-Wait Go calls a
// no-op, or a panic with a clear message).
//
// EXPECTED FAILURE MODE on current HEAD:
//   - The spawned goroutine runs to completion AFTER Wait already
//     returned. In production this means a goroutine using s.db
//     AFTER main.go has run its deferred db.Close(). The test
//     asserts this directly.
//
// Note on -race: a Wait() that returns immediately because the
// counter is zero never sets the waiter bit, so a subsequent Add()
// looks (to the runtime) like a fresh first Add. The race detector
// does NOT trip on this sequence empirically — which makes the bug
// silent, not louder. The semantic assertion is the only signal.
func TestGroup_GoAfterWaitIsMisuse(t *testing.T) {
	var g Group

	// Step 1: simulate main.go's bg.Wait() running while no work is
	// currently registered. This returns immediately because the
	// counter is 0.
	waitReturned := make(chan struct{})
	go func() {
		g.Wait()
		close(waitReturned)
	}()

	// Make sure Wait() really has returned before we register new work,
	// to faithfully reproduce the shutdown ordering. (In production,
	// main.go has already moved past bg.Wait() and is about to run
	// deferred db.Close().)
	select {
	case <-waitReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return on an empty Group")
	}

	// Step 2: simulate the in-flight handler finally reaching its
	// s.bg.Go(...) line, AFTER Wait already returned. This is the
	// misuse: a sync.WaitGroup may not be reused once Wait has
	// completed. Under -race this should produce a data-race report
	// (Add after Wait, no happens-before relationship).
	var ran atomic.Bool
	done := make(chan struct{})
	g.Go(func() {
		ran.Store(true)
		close(done)
	})

	// Wait for the spawned goroutine to complete so the test is
	// deterministic and so the -race detector has a chance to flush
	// its findings.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("post-Wait Go goroutine never ran")
	}

	// SEMANTIC ASSERTION (independent of -race):
	//
	// The goroutine scheduled after Wait() returned was still able to
	// run. In production this means main.go's bg.Wait() did not in
	// fact wait for this goroutine, the deferred db.Close() will fire
	// next, and this goroutine would race with / outlive the DB it
	// needs.
	//
	// A correct background.Group should either:
	//   - reject post-shutdown Go calls (return without scheduling, or
	//     panic with a clear message), OR
	//   - have Wait() that, once observed to return, makes a hard
	//     guarantee that no further work can ever be enqueued
	//     (typically via an explicit Close()/Shutdown() step).
	//
	// On current HEAD, neither is true: ran == true means the
	// goroutine ran behind main.go's back.
	if ran.Load() {
		t.Errorf(
			"background.Group ran a function scheduled via Go() AFTER Wait() returned; " +
				"this means main.go's bg.Wait() did not actually wait for it, " +
				"and the deferred db.Close() will fire before the goroutine finishes",
		)
	}
}
