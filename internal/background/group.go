// Package background provides a Group type for tracking server-scoped
// background goroutines so that graceful shutdown can wait for them to
// complete before closing the database.
package background

import (
	"log/slog"
	"sync"
	"sync/atomic"
)

// Group tracks a set of background goroutines. Callers start goroutines
// with Go and wait for all of them via Wait. Zero value is ready to use.
//
// Shutdown protocol: after the http.Server has stopped accepting new
// requests, call Close to prevent any straggling handler from
// registering new work, then Wait to block until in-flight goroutines
// have returned. Any Go call that arrives after Close is dropped with a
// log message — this protects the underlying sync.WaitGroup from the
// "Add after Wait returned" contract violation, which the race detector
// does not catch when Wait took its fast path on a zero counter.
type Group struct {
	wg     sync.WaitGroup
	closed atomic.Bool
}

// Go starts fn in a new goroutine and registers it with the group so
// that Wait blocks until fn returns. Go calls after Close are dropped.
func (g *Group) Go(fn func()) {
	if g.closed.Load() {
		slog.Warn("background.Group: Go called after Close; dropping work")
		return
	}
	g.wg.Go(fn)
}

// Close marks the group as no longer accepting new work. Subsequent Go
// calls return without scheduling. Close is idempotent and safe to call
// concurrently with Go.
func (g *Group) Close() {
	g.closed.Store(true)
}

// Wait blocks until all goroutines started via Go have returned.
func (g *Group) Wait() {
	g.wg.Wait()
}
