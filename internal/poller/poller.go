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
	interval      time.Duration
	retentionDays int
	faviconsDir   string
	syncing       atomic.Bool
}

// NewPoller creates a new Poller with the given database, poll interval,
// retention period (in days), and favicons directory path. A retentionDays
// value of 0 disables pruning.
func NewPoller(db *sql.DB, interval time.Duration, retentionDays int, faviconsDir string) *Poller {
	return &Poller{db: db, interval: interval, retentionDays: retentionDays, faviconsDir: faviconsDir}
}

// Start blocks until ctx is cancelled. It runs an immediate poll cycle, then
// polls on the configured interval. Run in a goroutine.
func (p *Poller) Start(ctx context.Context) {
	p.poll(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.poll(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// TriggerSync starts an async poll cycle if one is not already running.
// Returns true if a sync was started, false if one is already in progress.
func (p *Poller) TriggerSync(ctx context.Context) bool {
	if p.syncing.Load() {
		return false
	}
	go p.poll(ctx)
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
