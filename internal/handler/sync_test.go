package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// flashCookie extracts the value of the rdr_flash cookie from a recorded
// response, returning "" if the cookie is absent.
func flashCookie(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_flash" {
			return c.Value
		}
	}
	return ""
}

func TestHandleSync_SyncStarted(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	s.syncFeeds = func(_ context.Context) bool { return true }

	req := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if flashCookie(rec) == "" {
		t.Error("expected rdr_flash cookie to be set and non-empty")
	}
}

func TestHandleSync_AlreadyInProgress(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	s.syncFeeds = func(_ context.Context) bool { return false }

	req := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if flashCookie(rec) == "" {
		t.Error("expected rdr_flash cookie to be set and non-empty")
	}
}

func TestHandleSync_NilSyncFunc(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	// s.syncFeeds is nil by default from newTestServer

	req := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
	rec := httptest.NewRecorder()

	// Must not panic.
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if flashCookie(rec) == "" {
		t.Error("expected rdr_flash cookie to be set and non-empty")
	}
}

// TestHandleSync_DifferentOutcomesDifferentFlash verifies that a sync that
// starts produces a different flash message than one that is already in
// progress. This ensures the two paths are distinguishable to the user even
// though we don't assert the exact wording.
func TestHandleSync_DifferentOutcomesDifferentFlash(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	s.syncFeeds = func(_ context.Context) bool { return true }
	req := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
	recStarted := httptest.NewRecorder()
	s.ServeHTTP(recStarted, req)
	startedFlash := flashCookie(recStarted)

	s.syncFeeds = func(_ context.Context) bool { return false }
	req2 := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
	recInProgress := httptest.NewRecorder()
	s.ServeHTTP(recInProgress, req2)
	inProgressFlash := flashCookie(recInProgress)

	if startedFlash == inProgressFlash {
		t.Errorf(
			"started and in-progress flash values are identical (%q); expected different messages",
			startedFlash,
		)
	}
}

func TestHandleSync_RedirectUsesReferer(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	s.syncFeeds = func(_ context.Context) bool { return true }

	req := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
	req.Header.Set("Referer", "http://localhost/items")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/items" {
		t.Errorf("Location = %q, want %q", loc, "/items")
	}
}

func TestHandleSync_RedirectDefaultsToFeeds(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	s.syncFeeds = func(_ context.Context) bool { return true }

	req := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
	// No Referer header set.
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/feeds" {
		t.Errorf("Location = %q, want %q", loc, "/feeds")
	}
}

func TestHandleSync_HTMX(t *testing.T) {
	t.Run("HTMX sync returns 200", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		s.syncFeeds = func(_ context.Context) bool { return true }

		req := authedRequest(t, s, userID, http.MethodPost, "/feeds/sync")
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		// The HTMX sync path intentionally sends no HX-Trigger / flash — the
		// spinning sync icon is sufficient feedback while the poll is running.
		if trigger := rec.Header().Get("HX-Trigger"); trigger != "" {
			t.Errorf("HX-Trigger = %q, want empty (no flash on HTMX sync)", trigger)
		}
	})
}

func TestHandleSyncStatus(t *testing.T) {
	t.Run("returns JSON false when idle", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		// syncStatus is nil by default — treated as false.

		req := authedRequest(t, s, userID, http.MethodGet, "/feeds/sync/status")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if body := rec.Body.String(); !strings.Contains(body, `"syncing":false`) {
			t.Errorf("body = %q, want to contain %q", body, `"syncing":false`)
		}
	})

	t.Run("returns JSON true when syncing", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		s.syncStatus = func() bool { return true }

		req := authedRequest(t, s, userID, http.MethodGet, "/feeds/sync/status")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if body := rec.Body.String(); !strings.Contains(body, `"syncing":true`) {
			t.Errorf("body = %q, want to contain %q", body, `"syncing":true`)
		}
	})
}
