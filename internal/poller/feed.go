// Package poller provides background feed polling and data-retention helpers.
package poller

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/favicon"
	"github.com/chrisallenlane/rdr/internal/httpclient"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/mmcdole/gofeed"
)

// FetchAndStoreFeed fetches the given feed URL, parses its contents, and
// stores new items in the database. It also updates the feed's metadata
// (title, site_url) and last_fetched_at timestamp. If faviconsDir is
// non-empty, it also attempts to download a favicon for the feed.
//
// On failure the feed's last_fetch_error and consecutive_failures columns
// are updated; on success they are cleared.
func FetchAndStoreFeed(ctx context.Context, db *sql.DB, feed *model.Feed, faviconsDir string) (retErr error) {
	defer func() {
		if retErr != nil {
			recordFetchFailure(db, feed.ID, retErr.Error())
		}
	}()

	req, err := http.NewRequestWithContext(ctx, "GET", feed.URL, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", feed.URL, err)
	}
	req.Header.Set("User-Agent", httpclient.UserAgent)

	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return fmt.Errorf("requesting %s: %w", feed.URL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("requesting %s: status %d", feed.URL, resp.StatusCode)
	}

	parsed, err := gofeed.NewParser().Parse(io.LimitReader(resp.Body, httpclient.MaxResponseSize))
	if err != nil {
		return fmt.Errorf("parsing %s: %w", feed.URL, err)
	}

	// Update feed metadata and clear error state.
	if _, err := db.Exec(
		`UPDATE feeds SET title = ?, site_url = ?, last_fetched_at = ?,
		        last_fetch_error = '', consecutive_failures = 0
		 WHERE id = ?`,
		parsed.Title, parsed.Link, model.FormatNow(), feed.ID,
	); err != nil {
		return fmt.Errorf("updating metadata for %s: %w", feed.URL, err)
	}

	// Store items.
	for _, item := range parsed.Items {
		content := item.Content
		description := item.Description
		if content == "" {
			content = description
		}

		// When both content and description are empty, attempt to synthesize
		// HTML from Media RSS extension data (e.g. YouTube, Vimeo, podcasts).
		if content == "" && description == "" {
			if syn, desc := synthesizeMediaContent(item); syn != "" {
				content, description = syn, desc
			}
		}

		if _, err := db.Exec(
			`INSERT OR IGNORE INTO items (feed_id, guid, title, content, description, url, published_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			feed.ID, itemGUID(item), item.Title, content, description, item.Link, itemPublishedAt(item),
		); err != nil {
			return fmt.Errorf("storing item for %s: %w", feed.URL, err)
		}
	}

	// Best-effort favicon download.
	if faviconsDir != "" {
		favicon.Fetch(ctx, db, feed, faviconsDir, parsed)
	}

	return nil
}

// recordFetchFailure updates a feed's error columns after a failed fetch.
func recordFetchFailure(db *sql.DB, feedID int64, errMsg string) {
	if _, err := db.Exec(
		`UPDATE feeds SET last_fetch_error = ?,
		        consecutive_failures = consecutive_failures + 1
		 WHERE id = ?`,
		errMsg, feedID,
	); err != nil {
		slog.Error("recording fetch failure", "feed_id", feedID, "error", err)
	}
}

// itemGUID determines the GUID for a feed item, falling back through
// several options if the primary GUID is empty.
func itemGUID(item *gofeed.Item) string {
	if item.GUID != "" {
		return item.GUID
	}
	if item.Link != "" {
		return item.Link
	}
	if item.Title != "" {
		return item.Title
	}
	// Last resort: SHA-256 hash of title+content+url.
	h := sha256.Sum256([]byte(item.Title + item.Content + item.Link))
	return fmt.Sprintf("%x", h)
}

// itemPublishedAt determines the published date for a feed item, falling
// back to the current time if no date is available.
func itemPublishedAt(item *gofeed.Item) string {
	if item.PublishedParsed != nil {
		return model.FormatTime(*item.PublishedParsed)
	}
	if item.UpdatedParsed != nil {
		return model.FormatTime(*item.UpdatedParsed)
	}
	return model.FormatNow()
}
