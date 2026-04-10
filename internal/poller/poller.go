package poller

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
)

// maxPollWorkers is the maximum number of feeds fetched concurrently.
const maxPollWorkers = 10

// Poller periodically fetches all feeds and stores new items.
type Poller struct {
	db            *sql.DB
	ctx           context.Context
	interval      time.Duration
	retentionDays int
	faviconsDir   string
	syncing       atomic.Bool
}

// NewPoller creates a new Poller with the given database, poll interval,
// retention period (in days), and favicons directory path. A retentionDays
// value of 0 disables pruning. The context controls the lifetime of all poll
// operations, including manually triggered syncs.
func NewPoller(ctx context.Context, db *sql.DB, interval time.Duration, retentionDays int, faviconsDir string) *Poller {
	return &Poller{ctx: ctx, db: db, interval: interval, retentionDays: retentionDays, faviconsDir: faviconsDir}
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
// is cancelled as soon as the response is sent.
func (p *Poller) TriggerSync(_ context.Context) bool {
	if p.syncing.Load() {
		return false
	}
	go p.poll(p.ctx)
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

	slog.Info("poll cycle complete")
}
