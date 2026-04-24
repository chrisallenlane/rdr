package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
)

// listDetailData carries data for the list detail page template.
type listDetailData struct {
	List      model.List
	InList    []model.Feed
	NotInList []model.Feed
}

// queryListFeeds returns feeds in and not in the given list for the user.
func (s *Server) queryListFeeds(listID, userID int64) (inList, notInList []model.Feed, err error) {
	inRows, err := s.db.Query(
		`SELECT f.id, f.title, f.url FROM feeds f
		 WHERE f.list_id = ? AND f.user_id = ?
		 ORDER BY f.title ASC`,
		listID, userID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("querying feeds in list: %w", err)
	}
	defer func() { _ = inRows.Close() }()

	inList, err = scanFeeds(inRows)
	if err != nil {
		return nil, nil, fmt.Errorf("scanning feeds in list: %w", err)
	}

	outRows, err := s.db.Query(
		`SELECT f.id, f.title, f.url FROM feeds f
		 WHERE f.list_id IS NULL AND f.user_id = ?
		 ORDER BY f.title ASC`,
		userID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("querying feeds not in list: %w", err)
	}
	defer func() { _ = outRows.Close() }()

	notInList, err = scanFeeds(outRows)
	if err != nil {
		return nil, nil, fmt.Errorf("scanning feeds not in list: %w", err)
	}

	return inList, notInList, nil
}

func (s *Server) handleListDetail(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	listID, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	// Query list with ownership check.
	var list model.List
	var createdAt sql.NullString
	err := s.db.QueryRow(
		"SELECT id, user_id, name, created_at FROM lists WHERE id = ? AND user_id = ?",
		listID, user.ID,
	).Scan(&list.ID, &list.UserID, &list.Name, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderError(w, r, http.StatusNotFound, "List not found")
		return
	}
	if err != nil {
		slog.Error("querying list", "error", err)
		s.renderInternalError(w, r)
		return
	}
	if t := parseTime(createdAt); t != nil {
		list.CreatedAt = *t
	}

	inList, notInList, err := s.queryListFeeds(listID, user.ID)
	if err != nil {
		slog.Error("querying list feeds", "error", err)
		s.renderInternalError(w, r)
		return
	}

	s.render(w, r, "list_detail.html", PageData{
		Content: listDetailData{
			List:      list,
			InList:    inList,
			NotInList: notInList,
		},
	})
}

func (s *Server) handleAddFeedToList(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	listID, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	feedID, err := strconv.ParseInt(r.FormValue("feed_id"), 10, 64)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid feed ID")
		return
	}

	// Verify list ownership.
	if !s.verifyOwnership(w, r, "lists", listID, user.ID) {
		return
	}

	// Verify feed ownership.
	if !s.verifyOwnership(w, r, "feeds", feedID, user.ID) {
		return
	}

	_, err = s.db.Exec(
		"UPDATE feeds SET list_id = ? WHERE id = ? AND user_id = ?",
		listID, feedID, user.ID,
	)
	if err != nil {
		slog.Error("adding feed to list", "error", err)
		s.renderInternalError(w, r)
		return
	}

	if isHTMXRequest(r) {
		inList, notInList, err := s.queryListFeeds(listID, user.ID)
		if err != nil {
			slog.Error("querying list feeds", "error", err)
			s.renderInternalError(w, r)
			return
		}
		flash(w, r, "Feed added to list.")
		s.renderFragment(w, "list_detail_feeds.html", listDetailData{
			List:      model.List{ID: listID},
			InList:    inList,
			NotInList: notInList,
		})
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/lists/%d", listID), http.StatusSeeOther)
}

func (s *Server) handleRenameList(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	listID, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	if !s.verifyOwnership(w, r, "lists", listID, user.ID) {
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		if isHTMXRequest(r) {
			flash(w, r, "List name is required.")
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		setFlash(w, r, "List name is required.")
		http.Redirect(w, r, fmt.Sprintf("/lists/%d", listID), http.StatusSeeOther)
		return
	}

	_, err := s.db.Exec(
		"UPDATE lists SET name = ? WHERE id = ? AND user_id = ?",
		name, listID, user.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			if isHTMXRequest(r) {
				flash(w, r, "A list with that name already exists.")
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			setFlash(w, r, "A list with that name already exists.")
			http.Redirect(w, r, fmt.Sprintf("/lists/%d", listID), http.StatusSeeOther)
			return
		}
		slog.Error("renaming list", "error", err)
		s.renderInternalError(w, r)
		return
	}

	if isHTMXRequest(r) {
		setHTMXTriggers(w, htmxTriggers{
			"showFlash":    "List renamed.",
			"setPageTitle": name,
		})
		w.WriteHeader(http.StatusNoContent)
		return
	}

	setFlash(w, r, "List renamed.")
	http.Redirect(w, r, fmt.Sprintf("/lists/%d", listID), http.StatusSeeOther)
}

func (s *Server) handleRemoveFeedFromList(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	listID, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	feedID, ok := s.pathInt64(w, r, "feedID")
	if !ok {
		return
	}

	// Verify list ownership.
	if !s.verifyOwnership(w, r, "lists", listID, user.ID) {
		return
	}

	_, err := s.db.Exec(
		"UPDATE feeds SET list_id = NULL WHERE id = ? AND user_id = ?",
		feedID, user.ID,
	)
	if err != nil {
		slog.Error("removing feed from list", "error", err)
		s.renderInternalError(w, r)
		return
	}

	if isHTMXRequest(r) {
		inList, notInList, err := s.queryListFeeds(listID, user.ID)
		if err != nil {
			slog.Error("querying list feeds", "error", err)
			s.renderInternalError(w, r)
			return
		}
		flash(w, r, "Feed removed from list.")
		s.renderFragment(w, "list_detail_feeds.html", listDetailData{
			List:      model.List{ID: listID},
			InList:    inList,
			NotInList: notInList,
		})
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/lists/%d", listID), http.StatusSeeOther)
}
