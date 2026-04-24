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

// renderFeedsTableFragment queries the user's feeds and renders the
// feeds_table fragment. It returns false if an error was rendered.
func (s *Server) renderFeedsTableFragment(w http.ResponseWriter, r *http.Request, userID int64) bool {
	feeds, err := s.queryUserFeedsWithCounts(userID)
	if err != nil {
		slog.Error("querying feeds", "error", err)
		s.renderInternalError(w, r)
		return false
	}
	s.renderFragment(w, "feeds_table.html", feeds)
	return true
}

// queryUserFeedsWithCounts returns feeds with item counts and health data.
func (s *Server) queryUserFeedsWithCounts(userID int64) ([]model.Feed, error) {
	rows, err := s.db.Query(
		`SELECT f.id, f.url, f.title, f.site_url, f.favicon_url, f.last_fetched_at,
		        f.last_fetch_error, f.consecutive_failures, f.created_at,
		        (SELECT COUNT(*) FROM items WHERE feed_id = f.id) AS item_count
		 FROM feeds f WHERE f.user_id = ?
		 ORDER BY f.title ASC, f.url ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		var lastFetched sql.NullString
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &f.SiteURL, &f.FaviconURL, &lastFetched, &f.LastFetchError, &f.ConsecutiveFailures, &f.CreatedAt, &f.ItemCount); err != nil {
			return nil, err
		}
		f.LastFetchedAt = parseTime(lastFetched)
		f.UserID = userID
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	feeds, err := s.queryUserFeedsWithCounts(user.ID)
	if err != nil {
		slog.Error("querying feeds", "error", err)
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
	htmx := isHTMXRequest(r)

	renderErr := func(msg string) {
		if htmx {
			flash(w, r, msg)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		s.render(w, r, "feeds.html", PageData{
			Content: feedsPageData{Error: msg},
		})
	}

	// Validate URL.
	if rawURL == "" {
		renderErr("Feed URL is required.")
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		renderErr("Feed URL must use http or https.")
		return
	}

	// Resolve the URL to a feed URL (auto-discovery).
	feedURL, err := s.feedResolver(r.Context(), rawURL)
	if err != nil {
		if errors.Is(err, discover.ErrNoFeedFound) {
			renderErr("Could not find an RSS or Atom feed at this URL.")
		} else {
			slog.Warn("feed discovery failed", "url", rawURL, "error", err)
			renderErr(fmt.Sprintf("Could not fetch the URL: %v", err))
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
			renderErr("You have already added this feed.")
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
	fetchErr := poller.FetchAndStoreFeed(r.Context(), s.db, feed, s.faviconsDir)

	if htmx {
		if fetchErr != nil {
			flash(w, r, fmt.Sprintf("Feed added but could not be fetched: %v", fetchErr))
		} else {
			flash(w, r, "Feed added successfully.")
		}
		s.renderFeedsTableFragment(w, r, user.ID)
		return
	}

	if fetchErr != nil {
		slog.Warn("initial feed fetch failed", "url", feedURL, "error", fetchErr)
		setFlash(w, r, fmt.Sprintf("Feed added but could not be fetched: %v", fetchErr))
		http.Redirect(w, r, "/feeds", http.StatusSeeOther)
		return
	}

	setFlash(w, r, "Feed added successfully.")
	http.Redirect(w, r, "/feeds", http.StatusSeeOther)
}

func (s *Server) handleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	if isHTMXRequest(r) {
		user := middleware.UserFromContext(r.Context())
		id, ok := s.pathInt64(w, r, "id")
		if !ok {
			return
		}
		if !s.verifyOwnership(w, r, "feeds", id, user.ID) {
			return
		}
		if _, err := s.db.Exec("DELETE FROM feeds WHERE id = ? AND user_id = ?", id, user.ID); err != nil {
			slog.Error("deleting feed", "error", err)
			s.renderInternalError(w, r)
			return
		}
		flash(w, r, "Feed removed.")
		s.renderFeedsTableFragment(w, r, user.ID)
		return
	}
	s.deleteByID(w, r, "feeds", "Feed", "/feeds")
}
