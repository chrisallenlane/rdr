// Package background provides a Group type for tracking server-scoped
// background goroutines so that graceful shutdown can wait for them to
// complete before closing the database.
package background

import "sync"

// Group tracks a set of background goroutines. Callers start goroutines
// with Go and wait for all of them via Wait. Zero value is ready to use.
type Group struct {
	wg sync.WaitGroup
}

// Go starts fn in a new goroutine and registers it with the group so that
// Wait blocks until fn returns.
func (g *Group) Go(fn func()) {
	g.wg.Go(fn)
}

// Wait blocks until all goroutines started via Go have returned.
func (g *Group) Wait() {
	g.wg.Wait()
}
