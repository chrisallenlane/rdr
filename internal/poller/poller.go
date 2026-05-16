package poller

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrisallenlane/rdr/internal/background"
	"github.com/chrisallenlane/rdr/internal/model"
)

// maxPollWorkers is the maximum number of feeds fetched concurrently.
const maxPollWorkers = 10

// Poller periodically fetches all feeds and stores new items.
type Poller struct {
	db            *sql.DB
	ctx           context.Context
	bg            *background.Group
	interval      time.Duration
	retentionDays int
	faviconsDir   string
	syncing       atomic.Bool

	// VACUUM scheduling. lastVacuum gates the 24h cadence; on process
	// restart it resets to zero, so the first poll cycle of every new
	// process runs VACUUM once. consecutiveVacuumFailures shortens the
	// retry interval after the first failure and escalates the log
	// level after the third (see vacuumFailureBackoff).
	lastVacuum                time.Time
	consecutiveVacuumFailures int
}

// NewPoller creates a new Poller with the given database, poll interval,
// retention period (in days), and favicons directory path. A retentionDays
// value of 0 disables pruning. The context controls the lifetime of all poll
// operations, including manually triggered syncs. bg tracks goroutines
// started by TriggerSync so graceful shutdown can wait for them.
func NewPoller(
	ctx context.Context,
	bg *background.Group,
	db *sql.DB,
	interval time.Duration,
	retentionDays int,
	faviconsDir string,
) *Poller {
	return &Poller{
		ctx:           ctx,
		bg:            bg,
		db:            db,
		interval:      interval,
		retentionDays: retentionDays,
		faviconsDir:   faviconsDir,
	}
}

// Start blocks until the poller's context is cancelled. It runs an immediate
// poll cycle, then polls on the configured interval. Run in a goroutine.
func (p *Poller) Start() {
	p.poll(p.ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.poll(p.ctx)
		case <-p.ctx.Done():
			return
		}
	}
}

// IsSyncing reports whether a poll cycle is currently in progress.
func (p *Poller) IsSyncing() bool {
	return p.syncing.Load()
}

// TriggerSync starts an async poll cycle if one is not already running.
// Returns true if a sync was started, false if one is already in progress.
// It uses the application context (from NewPoller) rather than the caller's
// context, because the caller is typically an HTTP handler whose context
// is cancelled as soon as the response is sent. The goroutine is tracked
// via the background Group so graceful shutdown waits for it.
func (p *Poller) TriggerSync(_ context.Context) bool {
	if p.syncing.Load() {
		return false
	}
	p.bg.Go(func() { p.poll(p.ctx) })
	return true
}

// poll fetches all feeds from the database and updates each one.
// The syncing flag prevents concurrent poll cycles.
func (p *Poller) poll(ctx context.Context) {
	if !p.syncing.CompareAndSwap(false, true) {
		slog.Info("poll: skipping, already in progress")
		return
	}
	defer p.syncing.Store(false)

	rows, err := p.db.QueryContext(ctx,
		`SELECT id, user_id, url, title, site_url, favicon_url, last_fetched_at,
		        last_fetch_error, consecutive_failures, created_at
		 FROM feeds`,
	)
	if err != nil {
		slog.Error("poll: failed to query feeds", "error", err)
		return
	}
	defer func() { _ = rows.Close() }()

	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		if err := rows.Scan(
			&f.ID, &f.UserID, &f.URL, &f.Title, &f.SiteURL, &f.FaviconURL,
			&f.LastFetchedAt, &f.LastFetchError, &f.ConsecutiveFailures, &f.CreatedAt,
		); err != nil {
			slog.Error("poll: failed to scan feed", "error", err)
			continue
		}
		feeds = append(feeds, f)
	}
	if err := rows.Err(); err != nil {
		slog.Error("poll: row iteration error", "error", err)
		return
	}

	slog.Info("starting poll cycle", "feeds", len(feeds))

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxPollWorkers)

	for i := range feeds {
		if ctx.Err() != nil {
			slog.Info("poll cycle interrupted by shutdown")
			break
		}

		wg.Add(1)
		sem <- struct{}{} // acquire worker slot

		go func(feed *model.Feed) {
			defer wg.Done()
			defer func() { <-sem }() // release worker slot

			if err := FetchAndStoreFeed(ctx, p.db, feed, p.faviconsDir); err != nil {
				slog.Error("poll: failed to fetch feed",
					"feed_id", feed.ID,
					"url", feed.URL,
					"error", err,
				)
			} else {
				slog.Info("poll: fetched feed",
					"feed_id", feed.ID,
					"url", feed.URL,
				)
			}
		}(&feeds[i])
	}

	wg.Wait()

	// Run data-retention cleanup at the end of each poll cycle.
	runRetention(p.db, p.retentionDays)

	// VACUUM at most once per 24h per process lifetime. TriggerSync
	// also routes through poll(), so user-triggered syncs may run
	// maybeVacuum too — the 24h gate keeps this from being abusive.
	p.maybeVacuum(ctx)

	slog.Info("poll cycle complete")
}

// vacuumFailureBackoff returns the wait time before retrying VACUUM
// after repeated failures. Precondition: only called when
// consecutiveVacuumFailures > 0 (case 1 is the first retry attempt).
// The schedule (0 → 1h → 6h → 24h) means a persistent failure
// surfaces as WARN, then ERROR, then settles at once-per-day logging
// — no per-poll-cycle log storm.
func (p *Poller) vacuumFailureBackoff() time.Duration {
	switch p.consecutiveVacuumFailures {
	case 1:
		// Zero interval means the gate in maybeVacuum always passes,
		// so VACUUM is retried on the very next poll cycle.
		return 0
	case 2:
		return 1 * time.Hour
	case 3:
		return 6 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// maybeVacuum runs VACUUM if the configured interval has elapsed since
// the last attempt. Successful VACUUM emits an INFO log with a duration
// field; failures emit WARN (first two) or ERROR (third+) and update
// lastVacuum to gate the retry backoff window.
func (p *Poller) maybeVacuum(ctx context.Context) {
	interval := 24 * time.Hour
	if p.consecutiveVacuumFailures > 0 {
		interval = p.vacuumFailureBackoff()
	}
	if time.Since(p.lastVacuum) < interval {
		return
	}

	start := time.Now()
	if _, err := p.db.ExecContext(ctx, "VACUUM"); err != nil {
		p.consecutiveVacuumFailures++
		level := slog.LevelWarn
		if p.consecutiveVacuumFailures >= 3 {
			level = slog.LevelError
		}
		slog.Log(ctx, level, "maintenance: VACUUM failed",
			"error", err,
			"consecutive_failures", p.consecutiveVacuumFailures,
		)
		// Record the attempt so the backoff window applies.
		p.lastVacuum = time.Now()
		return
	}
	p.lastVacuum = time.Now()
	p.consecutiveVacuumFailures = 0
	slog.Info("maintenance: VACUUM complete", "duration", time.Since(start))
}
