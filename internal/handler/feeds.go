package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/chrisallenlane/rdr/internal/discover"
	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/poller"
)

// feedsPageData carries data for the feeds page template.
type feedsPageData struct {
	Feeds []model.Feed
	Error string
}

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	rows, err := s.db.Query(
		`SELECT f.id, f.url, f.title, f.site_url, f.favicon_url, f.last_fetched_at,
		        f.last_fetch_error, f.consecutive_failures, f.created_at,
		        (SELECT COUNT(*) FROM items WHERE feed_id = f.id) AS item_count
		 FROM feeds f WHERE f.user_id = ?
		 ORDER BY f.title ASC, f.url ASC`,
		user.ID,
	)
	if err != nil {
		slog.Error("querying feeds", "error", err)
		s.renderInternalError(w, r)
		return
	}
	defer func() { _ = rows.Close() }()

	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		var lastFetched sql.NullString
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.SiteURL, &f.FaviconURL, &lastFetched, &f.LastFetchError, &f.ConsecutiveFailures, &f.CreatedAt, &f.ItemCount); err != nil {
			slog.Error("scanning feed row", "error", err)
			s.renderInternalError(w, r)
			return
		}
		f.LastFetchedAt = parseTime(lastFetched)
		f.UserID = user.ID
		feeds = append(feeds, f)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating feed rows", "error", err)
		s.renderInternalError(w, r)
		return
	}

	s.render(w, r, "feeds.html", PageData{
		Content: feedsPageData{Feeds: feeds},
	})
}

func (s *Server) handleAddFeed(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	rawURL := strings.TrimSpace(r.FormValue("url"))

	// Validate URL.
	if rawURL == "" {
		s.render(w, r, "feeds.html", PageData{
			Content: feedsPageData{Error: "Feed URL is required."},
		})
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		s.render(w, r, "feeds.html", PageData{
			Content: feedsPageData{Error: "Feed URL must use http or https."},
		})
		return
	}

	// Resolve the URL to a feed URL (auto-discovery).
	feedURL, err := s.feedResolver(r.Context(), rawURL)
	if err != nil {
		if errors.Is(err, discover.ErrNoFeedFound) {
			s.render(w, r, "feeds.html", PageData{
				Content: feedsPageData{Error: "Could not find an RSS or Atom feed at this URL."},
			})
		} else {
			slog.Warn("feed discovery failed", "url", rawURL, "error", err)
			s.render(w, r, "feeds.html", PageData{
				Content: feedsPageData{Error: fmt.Sprintf("Could not fetch the URL: %v", err)},
			})
		}
		return
	}

	// Insert the (possibly discovered) feed URL.
	result, err := s.db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		user.ID, feedURL,
	)
	if err != nil {
		if isUniqueViolation(err) {
			s.render(w, r, "feeds.html", PageData{
				Content: feedsPageData{Error: "You have already added this feed."},
			})
			return
		}
		slog.Error("inserting feed", "error", err)
		s.renderInternalError(w, r)
		return
	}

	feedID, err := result.LastInsertId()
	if err != nil {
		slog.Error("getting feed id", "error", err)
		s.renderInternalError(w, r)
		return
	}

	feed := &model.Feed{ID: feedID, UserID: user.ID, URL: feedURL}
	if err := poller.FetchAndStoreFeed(r.Context(), s.db, feed, s.faviconsDir); err != nil {
		slog.Warn("initial feed fetch failed", "url", feedURL, "error", err)
		setFlash(w, fmt.Sprintf("Feed added but could not be fetched: %v", err))
		http.Redirect(w, r, "/feeds", http.StatusSeeOther)
		return
	}

	setFlash(w, "Feed added successfully.")
	http.Redirect(w, r, "/feeds", http.StatusSeeOther)
}

func (s *Server) handleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	s.deleteByID(w, r, "feeds", "Feed", "/feeds")
}
