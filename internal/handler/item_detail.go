package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/sanitize"
)

// itemDetailData carries data for the item detail page template.
type itemDetailData struct {
	Item                model.Item
	Content             template.HTML // sanitized HTML
	PrevItemID          *int64
	NextItemID          *int64
	DateDisplayAbsolute bool
}

// adjacentItemID returns the ID of the item immediately before (prev=true)
// or after (prev=false) the given item, ordered by published_at then id.
func (s *Server) adjacentItemID(userID int64, pubStr string, itemID int64, prev bool) *int64 {
	cmp, order := ">", "ASC"
	if prev {
		cmp, order = "<", "DESC"
	}
	var id int64
	err := s.db.QueryRow(
		fmt.Sprintf(
			`SELECT id FROM items
			 WHERE feed_id IN (SELECT id FROM feeds WHERE user_id = ?)
			 AND (published_at %s ? OR (published_at = ? AND id %s ?))
			 ORDER BY published_at %s, id %s LIMIT 1`,
			cmp, cmp, order, order,
		),
		userID, pubStr, pubStr, itemID,
	).Scan(&id)
	if err != nil {
		return nil
	}
	return &id
}

func (s *Server) handleItemDetail(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	itemID, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	// Query item with ownership check.
	var item model.Item
	var publishedAt, readAt, createdAt sql.NullString
	var read, starred sqlBool
	err := s.db.QueryRow(
		`SELECT i.id, i.feed_id, i.guid, i.title, i.content, i.description,
		        i.url, i.published_at, i.read, i.read_at, i.created_at,
		        i.starred,
		        f.title AS feed_title, f.site_url AS feed_site_url,
		        f.url AS feed_url
		 FROM items i
		 JOIN feeds f ON i.feed_id = f.id
		 WHERE i.id = ? AND f.user_id = ?`,
		itemID, user.ID,
	).Scan(
		&item.ID, &item.FeedID, &item.GUID, &item.Title, &item.Content,
		&item.Description, &item.URL, &publishedAt, &read, &readAt, &createdAt,
		&starred,
		&item.FeedTitle, &item.FeedSiteURL, &item.FeedURL,
	)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "Item not found")
		return
	}
	if err != nil {
		slog.Error("querying item", "error", err)
		s.renderInternalError(w, r)
		return
	}
	item.PublishedAt = parseTime(publishedAt)
	item.ReadAt = parseTime(readAt)
	item.Read = bool(read)
	item.Starred = bool(starred)
	if t := parseTime(createdAt); t != nil {
		item.CreatedAt = *t
	}

	// Mark as read if not already.
	if !item.Read {
		if _, err := s.db.Exec(
			"UPDATE items SET read = 1, read_at = datetime('now') WHERE id = ? AND read = 0",
			item.ID,
		); err != nil {
			slog.Error("marking item read", "error", err)
		}
		item.Read = true
	}

	// Resolve relative URLs, sanitize, then highlight code blocks.
	content := sanitize.ResolveRelativeURLs(item.Content, item.URL)
	content = string(sanitize.HTML(content))
	content = sanitize.HighlightCodeBlocks(content)

	// Query adjacent items for prev/next navigation.
	var prevID, nextID *int64
	if item.PublishedAt != nil {
		pubStr := model.FormatTime(*item.PublishedAt)
		prevID = s.adjacentItemID(user.ID, pubStr, item.ID, true)
		nextID = s.adjacentItemID(user.ID, pubStr, item.ID, false)
	}

	settings := queryUserSettings(s.db, user.ID)

	s.render(w, r, "item.html", PageData{
		Content: itemDetailData{
			Item:                item,
			Content:             template.HTML(content),
			PrevItemID:          prevID,
			NextItemID:          nextID,
			DateDisplayAbsolute: settings.DateDisplayAbsolute,
		},
	})
}
