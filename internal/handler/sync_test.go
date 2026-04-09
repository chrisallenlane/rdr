package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

	var flashValue string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_flash" {
			flashValue = c.Value
			break
		}
	}
	if !strings.Contains(flashValue, "started") {
		t.Errorf("flash = %q, want it to contain %q", flashValue, "started")
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

	var flashValue string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_flash" {
			flashValue = c.Value
			break
		}
	}
	if !strings.Contains(flashValue, "already in progress") {
		t.Errorf("flash = %q, want it to contain %q", flashValue, "already in progress")
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

	var flashValue string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_flash" {
			flashValue = c.Value
			break
		}
	}
	if !strings.Contains(flashValue, "already in progress") {
		t.Errorf("flash = %q, want it to contain %q", flashValue, "already in progress")
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
