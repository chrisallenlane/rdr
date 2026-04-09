package handler

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
)

// itemsPageData carries data for the items page template.
type itemsPageData struct {
	Items         []model.Item
	TotalItems    int
	UnreadCount   int
	Page          int
	TotalPages    int
	Heading       string
	FilterFeed    int64
	FilterList    int64
	FilterUnread  bool
	FilterStarred bool
	Feeds         []model.Feed // for sidebar filter links
	Lists         []model.List // for sidebar filter links
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	// Parse query parameters.
	page := pageFromQuery(r)

	filterFeed := parsePositiveInt64(r.URL.Query().Get("feed"))
	filterList := parsePositiveInt64(r.URL.Query().Get("list"))
	filterUnread := r.URL.Query().Get("unread") == "1"
	filterStarred := r.URL.Query().Get("starred") == "1"

	// Build WHERE clauses.
	where, args := buildItemFilter(
		user.ID, filterFeed, filterList, filterUnread, filterStarred,
	)

	// Query total count.
	countQuery := "SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id WHERE " + where
	var totalItems int
	if err := s.db.QueryRow(countQuery, args...).Scan(&totalItems); err != nil {
		slog.Error("counting items", "error", err)
		s.renderInternalError(w, r)
		return
	}

	var totalPages, offset int
	page, totalPages, offset = paginate(totalItems, itemsPerPage, page)

	// Query items.
	itemQuery := `SELECT i.id, i.feed_id, i.title, i.url, i.published_at, i.read,
	                     i.starred,
	                     f.title AS feed_title, f.site_url AS feed_site_url,
	                     f.url AS feed_url
	              FROM items i
	              JOIN feeds f ON i.feed_id = f.id
	              WHERE ` + where + `
	              ORDER BY i.published_at DESC, i.id DESC
	              LIMIT ? OFFSET ?`
	itemArgs := append(args, itemsPerPage, offset)

	rows, err := s.db.Query(itemQuery, itemArgs...)
	if err != nil {
		slog.Error("querying items", "error", err)
		s.renderInternalError(w, r)
		return
	}
	defer func() { _ = rows.Close() }()

	var items []model.Item
	for rows.Next() {
		var item model.Item
		var publishedAt sql.NullString
		var read, starred sqlBool
		if err := rows.Scan(
			&item.ID, &item.FeedID, &item.Title, &item.URL, &publishedAt,
			&read, &starred,
			&item.FeedTitle, &item.FeedSiteURL, &item.FeedURL,
		); err != nil {
			slog.Error("scanning item row", "error", err)
			s.renderInternalError(w, r)
			return
		}
		item.PublishedAt = parseTime(publishedAt)
		item.Read = bool(read)
		item.Starred = bool(starred)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating item rows", "error", err)
		s.renderInternalError(w, r)
		return
	}

	// Query user's feeds and lists for the sidebar.
	feeds, err := queryUserFeeds(s.db, user.ID)
	if err != nil {
		slog.Error("querying feeds for sidebar", "error", err)
		s.renderInternalError(w, r)
		return
	}

	lists, err := queryUserLists(s.db, user.ID)
	if err != nil {
		slog.Error("querying lists for sidebar", "error", err)
		s.renderInternalError(w, r)
		return
	}

	heading := itemsHeading(filterFeed, filterList, feeds, lists)

	// Query unread count (with same feed/list filters, but always unread).
	unreadWhere, unreadArgs := buildItemFilter(
		user.ID, filterFeed, filterList, true, false,
	)

	var unreadCount int
	unreadQuery := "SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id WHERE " + unreadWhere
	if err := s.db.QueryRow(unreadQuery, unreadArgs...).Scan(&unreadCount); err != nil {
		slog.Error("counting unread items", "error", err)
		s.renderInternalError(w, r)
		return
	}

	s.render(w, r, "items.html", PageData{
		Content: itemsPageData{
			Items:         items,
			TotalItems:    totalItems,
			UnreadCount:   unreadCount,
			Page:          page,
			TotalPages:    totalPages,
			Heading:       heading,
			FilterFeed:    filterFeed,
			FilterList:    filterList,
			FilterUnread:  filterUnread,
			FilterStarred: filterStarred,
			Feeds:         feeds,
			Lists:         lists,
		},
	})
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	// Parse optional filter params from the form.
	filterFeed := parsePositiveInt64(r.FormValue("feed"))
	filterList := parsePositiveInt64(r.FormValue("list"))

	// Validate ownership of the specified feed.
	if filterFeed > 0 {
		if !s.verifyOwnership(w, r, "feeds", filterFeed, user.ID) {
			return
		}
	}

	// Validate ownership of the specified list.
	if filterList > 0 {
		if !s.verifyOwnership(w, r, "lists", filterList, user.ID) {
			return
		}
	}

	// Build the UPDATE query reusing the same filter logic as the items list.
	where, args := buildItemFilter(user.ID, filterFeed, filterList, false, false)
	query := `UPDATE items SET read = 1, read_at = datetime('now')
		WHERE read = 0 AND id IN (
			SELECT i.id FROM items i JOIN feeds f ON i.feed_id = f.id WHERE ` + where + `
		)`

	result, err := s.db.Exec(query, args...)
	if err != nil {
		slog.Error("marking items as read", "error", err)
		s.renderInternalError(w, r)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		slog.Error("getting rows affected", "error", err)
		s.renderInternalError(w, r)
		return
	}

	setFlash(w, fmt.Sprintf("Marked %d items as read.", affected))

	// Redirect back preserving filters.
	redirect := "/items"
	if filterFeed > 0 {
		redirect = fmt.Sprintf("/items?feed=%d", filterFeed)
	} else if filterList > 0 {
		redirect = fmt.Sprintf("/items?list=%d", filterList)
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// itemsHeading returns the page heading for the items list based on active filters.
func itemsHeading(filterFeed, filterList int64, feeds []model.Feed, lists []model.List) string {
	if filterFeed > 0 {
		for _, f := range feeds {
			if f.ID == filterFeed {
				return f.Title
			}
		}
	}
	if filterList > 0 {
		for _, l := range lists {
			if l.ID == filterList {
				return "List: " + l.Name
			}
		}
	}
	return "All Items"
}

// buildItemFilter constructs the WHERE clause and argument list for item
// queries scoped to a user, optionally filtered by feed, list, read state,
// and starred state.
func buildItemFilter(
	userID int64,
	filterFeed int64,
	filterList int64,
	filterUnread bool,
	filterStarred bool,
) (string, []any) {
	where := "f.user_id = ?"
	args := []any{userID}
	if filterFeed > 0 {
		where += " AND f.id = ?"
		args = append(args, filterFeed)
	}
	if filterUnread {
		where += " AND i.read = 0"
	}
	if filterStarred {
		where += " AND i.starred = 1"
	}
	if filterList > 0 {
		where += " AND f.id IN (SELECT feed_id FROM list_feeds WHERE list_id = ?)"
		where += " AND ? IN (SELECT id FROM lists WHERE user_id = ?)"
		args = append(args, filterList, filterList, userID)
	}
	return where, args
}
