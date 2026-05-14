package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"

	"github.com/chrisallenlane/rdr/internal/dbutil"
	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
)

// itemsPageData carries data for the items page template.
type itemsPageData struct {
	Items               []model.Item
	TotalItems          int
	UnreadCount         int
	Page                int
	TotalPages          int
	Heading             string
	FilterFeed          int64
	FilterList          int64
	FilterUnread        bool
	FilterStarred       bool
	ShowDescriptions    bool
	DateDisplayAbsolute bool
	Feeds               []model.Feed // for sidebar filter links
	Lists               []model.List // for sidebar filter links
}

// queryItemsPageData builds the full itemsPageData for the given filters.
func (s *Server) queryItemsPageData(
	userID int64, page int,
	filterFeed, filterList int64,
	filterUnread, filterStarred bool,
) (itemsPageData, error) {
	where, args := dbutil.BuildItemFilter(userID, filterFeed, filterList, filterUnread, filterStarred)

	countQuery := "SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id WHERE " + where
	var totalItems int
	if err := s.db.QueryRow(countQuery, args...).Scan(&totalItems); err != nil {
		return itemsPageData{}, fmt.Errorf("counting items: %w", err)
	}

	var totalPages, offset int
	page, totalPages, offset = paginate(totalItems, itemsPerPage, page)

	itemQuery := `SELECT i.id, i.feed_id, i.title,
	                     COALESCE(NULLIF(i.description, ''), i.content) AS description,
	                     i.url,
	                     i.published_at, i.read, i.starred,
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
		return itemsPageData{}, fmt.Errorf("querying items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []model.Item
	for rows.Next() {
		var item model.Item
		var publishedAt sql.NullString
		var read, starred sqlBool
		if err := rows.Scan(
			&item.ID, &item.FeedID, &item.Title, &item.Description, &item.URL,
			&publishedAt, &read, &starred,
			&item.FeedTitle, &item.FeedSiteURL, &item.FeedURL,
		); err != nil {
			return itemsPageData{}, fmt.Errorf("scanning item: %w", err)
		}
		item.PublishedAt = parseTime(publishedAt)
		item.Read = bool(read)
		item.Starred = bool(starred)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return itemsPageData{}, fmt.Errorf("iterating items: %w", err)
	}

	feeds, err := queryUserFeeds(s.db, userID)
	if err != nil {
		return itemsPageData{}, fmt.Errorf("querying feeds: %w", err)
	}

	lists, err := queryUserLists(s.db, userID)
	if err != nil {
		return itemsPageData{}, fmt.Errorf("querying lists: %w", err)
	}

	heading := itemsHeading(filterFeed, filterList, feeds, lists)

	unreadWhere, unreadArgs := dbutil.BuildItemFilter(userID, filterFeed, filterList, true, false)
	var unreadCount int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id WHERE "+unreadWhere,
		unreadArgs...,
	).Scan(&unreadCount); err != nil {
		return itemsPageData{}, fmt.Errorf("counting unread: %w", err)
	}

	return itemsPageData{
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
	}, nil
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	data, err := s.queryItemsPageData(
		user.ID,
		pageFromQuery(r),
		parsePositiveInt64(r.URL.Query().Get("feed")),
		parsePositiveInt64(r.URL.Query().Get("list")),
		r.URL.Query().Get("unread") == "1",
		r.URL.Query().Get("starred") == "1",
	)
	if err != nil {
		s.internalError(w, r, "querying items", err)
		return
	}

	settings := queryUserSettings(s.db, user.ID)
	data.ShowDescriptions = settings.ShowDescriptions
	data.DateDisplayAbsolute = settings.DateDisplayAbsolute

	s.render(w, r, "items.html", PageData{Content: data})
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	// Parse optional filter params from the form. For unread/starred we
	// also accept the Referer URL as a fallback so that a client not yet
	// sending hidden inputs still gets correct scoping.
	filterFeed := parsePositiveInt64(r.FormValue("feed"))
	filterList := parsePositiveInt64(r.FormValue("list"))
	filterUnread := formBoolWithRefererFallback(r, "unread")
	filterStarred := formBoolWithRefererFallback(r, "starred")

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
	where, args := dbutil.BuildItemFilter(
		user.ID, filterFeed, filterList, filterUnread, filterStarred,
	)
	query := `UPDATE items SET read = 1, read_at = datetime('now')
		WHERE read = 0 AND id IN (
			SELECT i.id FROM items i JOIN feeds f ON i.feed_id = f.id WHERE ` + where + `
		)`

	result, err := s.db.Exec(query, args...)
	if err != nil {
		s.internalError(w, r, "marking items as read", err)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		s.internalError(w, r, "getting rows affected", err)
		return
	}

	if isHTMXRequest(r) {
		data, err := s.queryItemsPageData(
			user.ID, 1, filterFeed, filterList, filterUnread, filterStarred,
		)
		if err != nil {
			s.internalError(w, r, "querying items for HTMX", err)
			return
		}
		htmxSettings := queryUserSettings(s.db, user.ID)
		data.ShowDescriptions = htmxSettings.ShowDescriptions
		data.DateDisplayAbsolute = htmxSettings.DateDisplayAbsolute
		flash(w, r, fmt.Sprintf("Marked %d items as read.", affected))
		s.renderFragment(w, "items_section.html", data)
		return
	}

	setFlash(w, r, fmt.Sprintf("Marked %d items as read.", affected))

	// Redirect back preserving all active filters.
	redirect := buildItemsRedirect(filterFeed, filterList, filterUnread, filterStarred)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// formBoolWithRefererFallback reads a "1" flag from the POST form body.
// If the key is absent from the form, it falls back to the Referer URL's
// query params so that legacy forms without the hidden input still work.
func formBoolWithRefererFallback(r *http.Request, key string) bool {
	if v := r.FormValue(key); v != "" {
		return v == "1"
	}
	// Fall back to the Referer URL query string.
	ref, err := url.Parse(r.Header.Get("Referer"))
	if err != nil || ref == nil {
		return false
	}
	return ref.Query().Get(key) == "1"
}

// buildItemsRedirect constructs the /items redirect URL preserving all
// active filter parameters.
func buildItemsRedirect(
	filterFeed, filterList int64,
	filterUnread, filterStarred bool,
) string {
	q := url.Values{}
	if filterFeed > 0 {
		q.Set("feed", fmt.Sprintf("%d", filterFeed))
	}
	if filterList > 0 {
		q.Set("list", fmt.Sprintf("%d", filterList))
	}
	if filterUnread {
		q.Set("unread", "1")
	}
	if filterStarred {
		q.Set("starred", "1")
	}
	if len(q) == 0 {
		return "/items"
	}
	return "/items?" + q.Encode()
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
