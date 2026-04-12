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

	rows, err := s.db.Query(`
		SELECT f.url, f.title, f.site_url, COALESCE(l.name, '') AS list_name
		FROM feeds f
		LEFT JOIN lists l ON f.list_id = l.id
		WHERE f.user_id = ?
		ORDER BY list_name ASC, f.title ASC, f.url ASC`,
		user.ID,
	)
	if err != nil {
		slog.Error("querying feeds for OPML export", "error", err)
		s.renderInternalError(w, r)
		return
	}
	defer func() { _ = rows.Close() }()

	// folderOutlines collects folder outlines keyed by list name.
	folderOutlines := make(map[string]*opmlOutline)
	// folderOrder preserves the insertion order of folder names.
	var folderOrder []string
	// topLevel collects feeds that belong to no list.
	var topLevel []opmlOutline

	for rows.Next() {
		var feedURL, title, siteURL, listName string
		if err := rows.Scan(&feedURL, &title, &siteURL, &listName); err != nil {
			slog.Error("scanning feed for OPML export", "error", err)
			s.renderInternalError(w, r)
			return
		}

		text := title
		if text == "" {
			text = feedURL
		}

		feedOutline := opmlOutline{
			Type:    "rss",
			Text:    text,
			Title:   title,
			XMLURL:  feedURL,
			HTMLURL: siteURL,
		}

		if listName == "" {
			topLevel = append(topLevel, feedOutline)
		} else {
			if _, exists := folderOutlines[listName]; !exists {
				folderOutlines[listName] = &opmlOutline{Text: listName}
				folderOrder = append(folderOrder, listName)
			}
			folderOutlines[listName].Outlines = append(
				folderOutlines[listName].Outlines,
				feedOutline,
			)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating feeds for OPML export", "error", err)
		s.renderInternalError(w, r)
		return
	}

	// Build the top-level outline list: folders first (alphabetical), then
	// ungrouped feeds.
	outlines := make([]opmlOutline, 0, len(folderOrder)+len(topLevel))
	for _, name := range folderOrder {
		outlines = append(outlines, *folderOutlines[name])
	}
	outlines = append(outlines, topLevel...)

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

	feeds := collectFeedsWithFolder(doc.Body.Outlines, "")
	if len(feeds) == 0 {
		flashAndRedirect("No feeds found in the uploaded file.")
		return
	}

	// Cache of folder name → list ID to avoid repeated queries.
	listCache := make(map[string]int64)

	// resolveListID returns the list ID for the given folder name, creating
	// the list if it does not already exist.
	resolveListID := func(folderName string) (int64, error) {
		if id, ok := listCache[folderName]; ok {
			return id, nil
		}
		if _, err := s.db.Exec(
			"INSERT OR IGNORE INTO lists (user_id, name) VALUES (?, ?)",
			user.ID, folderName,
		); err != nil {
			return 0, err
		}
		var id int64
		if err := s.db.QueryRow(
			"SELECT id FROM lists WHERE user_id = ? AND name = ?",
			user.ID, folderName,
		).Scan(&id); err != nil {
			return 0, err
		}
		listCache[folderName] = id
		return id, nil
	}

	var imported, duplicates, skipped int
	var newFeeds []*model.Feed

	for _, fw := range feeds {
		feedURL := strings.TrimSpace(fw.outline.XMLURL)
		parsed, err := url.Parse(feedURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			skipped++
			continue
		}

		result, err := s.db.Exec(
			"INSERT OR IGNORE INTO feeds (user_id, url, title, site_url) VALUES (?, ?, ?, ?)",
			user.ID, feedURL, fw.outline.Title, fw.outline.HTMLURL,
		)
		if err != nil {
			slog.Error("inserting imported feed", "url", feedURL, "error", err)
			skipped++
			continue
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			slog.Error("checking rows affected", "error", err)
			skipped++
			continue
		}

		isNew := rowsAffected > 0

		// Determine list_id for this feed (may be nil if no folder).
		var listID *int64
		if fw.folderName != "" {
			id, err := resolveListID(fw.folderName)
			if err != nil {
				slog.Error(
					"resolving list for imported feed",
					"folder", fw.folderName,
					"error", err,
				)
			} else {
				listID = &id
			}
		}

		// Update list_id for both new and existing feeds.
		if _, err := s.db.Exec(
			"UPDATE feeds SET list_id = ? WHERE user_id = ? AND url = ?",
			listID, user.ID, feedURL,
		); err != nil {
			slog.Error(
				"updating list_id for imported feed",
				"url", feedURL,
				"error", err,
			)
		}

		if !isNew {
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
		flash(w, r, msg)
		s.renderFeedsTableFragment(w, r, user.ID)
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

// feedWithFolder pairs a feed outline with its immediate parent folder name.
type feedWithFolder struct {
	outline    opmlOutline
	folderName string // empty if top-level (no folder)
}

// collectFeedsWithFolder recursively walks the outline tree and returns all
// feed outlines paired with their immediate parent folder name. A feed belongs
// to the innermost folder that directly contains it. Whitespace-only folder
// names are treated as no folder (top-level).
func collectFeedsWithFolder(
	outlines []opmlOutline,
	parentFolder string,
) []feedWithFolder {
	var result []feedWithFolder
	for _, o := range outlines {
		if strings.TrimSpace(o.XMLURL) != "" {
			result = append(result, feedWithFolder{
				outline:    o,
				folderName: parentFolder,
			})
		}
		if len(o.Outlines) > 0 {
			// If this outline has no xmlUrl it is a folder; use its text as
			// the folder name for children. If the text is whitespace-only,
			// fall back to the current parent so those feeds are treated as
			// ungrouped.
			folder := parentFolder
			if strings.TrimSpace(o.XMLURL) == "" {
				if name := strings.TrimSpace(o.Text); name != "" {
					folder = name
				}
			}
			result = append(
				result,
				collectFeedsWithFolder(o.Outlines, folder)...,
			)
		}
	}
	return result
}
