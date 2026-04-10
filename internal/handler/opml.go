package handler

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/poller"
)

// opmlDoc is the root element of an OPML 2.0 document.
type opmlDoc struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

// opmlHead contains metadata about the OPML document.
type opmlHead struct {
	Title string `xml:"title"`
}

// opmlBody contains the list of feed outlines.
type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

// opmlOutline represents a single feed entry (or category folder) in OPML.
type opmlOutline struct {
	Type     string        `xml:"type,attr,omitempty"`
	Text     string        `xml:"text,attr"`
	Title    string        `xml:"title,attr,omitempty"`
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string        `xml:"htmlUrl,attr,omitempty"`
	Outlines []opmlOutline `xml:"outline,omitempty"`
}

func (s *Server) handleExportOPML(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	rows, err := s.db.Query(
		"SELECT url, title, site_url FROM feeds WHERE user_id = ? ORDER BY title ASC, url ASC",
		user.ID,
	)
	if err != nil {
		slog.Error("querying feeds for OPML export", "error", err)
		s.renderInternalError(w, r)
		return
	}
	defer func() { _ = rows.Close() }()

	var outlines []opmlOutline
	for rows.Next() {
		var feedURL, title, siteURL string
		if err := rows.Scan(&feedURL, &title, &siteURL); err != nil {
			slog.Error("scanning feed for OPML export", "error", err)
			s.renderInternalError(w, r)
			return
		}

		text := title
		if text == "" {
			text = feedURL
		}

		outlines = append(outlines, opmlOutline{
			Type:    "rss",
			Text:    text,
			Title:   title,
			XMLURL:  feedURL,
			HTMLURL: siteURL,
		})
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating feeds for OPML export", "error", err)
		s.renderInternalError(w, r)
		return
	}

	doc := opmlDoc{
		Version: "2.0",
		Head:    opmlHead{Title: "rdr feeds"},
		Body:    opmlBody{Outlines: outlines},
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Disposition", `attachment; filename="rdr-feeds.opml"`)

	if _, err := w.Write([]byte(xml.Header)); err != nil {
		slog.Error("writing XML header", "error", err)
		return
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		slog.Error("encoding OPML", "error", err)
		return
	}
	if err := enc.Close(); err != nil {
		slog.Error("flushing OPML encoder", "error", err)
	}
}

// maxOPMLSize is the maximum allowed size for an uploaded OPML file (1 MB).
const maxOPMLSize = 1 << 20

func (s *Server) handleImportOPML(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	htmx := isHTMXRequest(r)

	flashAndRedirect := func(msg string) {
		if htmx {
			flash(w, r, msg)
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		setFlash(w, msg)
		http.Redirect(w, r, "/feeds", http.StatusSeeOther)
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxOPMLSize)

	file, _, err := r.FormFile("opml")
	if err != nil {
		flashAndRedirect("Please select an OPML file to upload.")
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		flashAndRedirect("File too large (max 1 MB).")
		return
	}

	var doc opmlDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		flashAndRedirect("Invalid OPML file.")
		return
	}

	outlines := collectFeedOutlines(doc.Body.Outlines)
	if len(outlines) == 0 {
		flashAndRedirect("No feeds found in the uploaded file.")
		return
	}

	var imported, duplicates, skipped int
	var newFeeds []*model.Feed
	for _, o := range outlines {
		feedURL := strings.TrimSpace(o.XMLURL)
		parsed, err := url.Parse(feedURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			skipped++
			continue
		}

		result, err := s.db.Exec(
			"INSERT OR IGNORE INTO feeds (user_id, url, title, site_url) VALUES (?, ?, ?, ?)",
			user.ID, feedURL, o.Title, o.HTMLURL,
		)
		if err != nil {
			slog.Error("inserting imported feed", "url", feedURL, "error", err)
			skipped++
			continue
		}

		rows, err := result.RowsAffected()
		if err != nil {
			slog.Error("checking rows affected", "error", err)
			skipped++
			continue
		}
		if rows == 0 {
			duplicates++
			continue
		}

		imported++
		feedID, err := result.LastInsertId()
		if err != nil {
			slog.Error("getting feed id", "error", err)
			continue
		}
		newFeeds = append(newFeeds, &model.Feed{
			ID:     feedID,
			UserID: user.ID,
			URL:    feedURL,
		})
	}

	msg := fmt.Sprintf("Imported %d new feed(s)", imported)
	if duplicates > 0 {
		msg += fmt.Sprintf(" (%d already existed)", duplicates)
	}
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d skipped)", skipped)
	}
	if len(newFeeds) > 0 {
		msg += ". Feeds are being fetched in the background."
		go s.fetchImportedFeeds(context.WithoutCancel(r.Context()), newFeeds)
	} else {
		msg += "."
	}

	if htmx {
		feeds, err := s.queryUserFeedsWithCounts(user.ID)
		if err != nil {
			slog.Error("querying feeds", "error", err)
			s.renderInternalError(w, r)
			return
		}
		flash(w, r, msg)
		s.renderFragment(w, "feeds_table.html", feeds)
		return
	}

	setFlash(w, msg)
	http.Redirect(w, r, "/feeds", http.StatusSeeOther)
}

// fetchImportedFeeds fetches each newly imported feed sequentially in the
// background. Errors are logged but do not block other feeds.
func (s *Server) fetchImportedFeeds(ctx context.Context, feeds []*model.Feed) {
	for _, f := range feeds {
		if err := poller.FetchAndStoreFeed(ctx, s.db, f, s.faviconsDir); err != nil {
			slog.Warn("fetching imported feed", "url", f.URL, "error", err)
		}
	}
}

// collectFeedOutlines recursively walks the outline tree and returns all
// outlines that have an xmlUrl attribute (i.e. actual feed entries, not
// category folders).
func collectFeedOutlines(outlines []opmlOutline) []opmlOutline {
	var feeds []opmlOutline
	for _, o := range outlines {
		if strings.TrimSpace(o.XMLURL) != "" {
			feeds = append(feeds, o)
		}
		if len(o.Outlines) > 0 {
			feeds = append(feeds, collectFeedOutlines(o.Outlines)...)
		}
	}
	return feeds
}
