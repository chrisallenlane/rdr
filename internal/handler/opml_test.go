package handler

import (
	"bytes"
	"encoding/xml"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleExportOPML(t *testing.T) {
	t.Run("no feeds", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, authedRequest(t, s, userID, http.MethodGet, "/feeds/export"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/xml" {
			t.Errorf("Content-Type = %q, want application/xml", ct)
		}
		if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="rdr-feeds.opml"` {
			t.Errorf("Content-Disposition = %q, want attachment with filename", cd)
		}

		var doc opmlDoc
		if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("parsing OPML: %v", err)
		}
		if doc.Version != "2.0" {
			t.Errorf("version = %q, want 2.0", doc.Version)
		}
		if len(doc.Body.Outlines) != 0 {
			t.Errorf("outlines = %d, want 0", len(doc.Body.Outlines))
		}
	})

	t.Run("with feeds", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		feeds := []struct {
			url, title, siteURL string
		}{
			{"https://b.example.com/feed.xml", "B Feed", "https://b.example.com"},
			{"https://a.example.com/feed.xml", "A Feed", "https://a.example.com"},
		}
		for _, f := range feeds {
			if _, err := s.db.Exec(
				"INSERT INTO feeds (user_id, url, title, site_url) VALUES (?, ?, ?, ?)",
				userID, f.url, f.title, f.siteURL,
			); err != nil {
				t.Fatalf("inserting feed: %v", err)
			}
		}

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, authedRequest(t, s, userID, http.MethodGet, "/feeds/export"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var doc opmlDoc
		if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("parsing OPML: %v", err)
		}
		if len(doc.Body.Outlines) != 2 {
			t.Fatalf("outlines = %d, want 2", len(doc.Body.Outlines))
		}

		// Should be ordered by title: A Feed first.
		first := doc.Body.Outlines[0]
		if first.Text != "A Feed" {
			t.Errorf("first outline text = %q, want A Feed", first.Text)
		}
		if first.XMLURL != "https://a.example.com/feed.xml" {
			t.Errorf("first outline xmlUrl = %q", first.XMLURL)
		}
		if first.HTMLURL != "https://a.example.com" {
			t.Errorf("first outline htmlUrl = %q", first.HTMLURL)
		}
		if first.Type != "rss" {
			t.Errorf("first outline type = %q, want rss", first.Type)
		}

		second := doc.Body.Outlines[1]
		if second.Text != "B Feed" {
			t.Errorf("second outline text = %q, want B Feed", second.Text)
		}
	})

	t.Run("with feeds in lists", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Create two lists.
		var techID, newsID int64
		if err := s.db.QueryRow(
			"INSERT INTO lists (user_id, name) VALUES (?, ?) RETURNING id",
			userID, "Tech",
		).Scan(&techID); err != nil {
			t.Fatalf("creating Tech list: %v", err)
		}
		if err := s.db.QueryRow(
			"INSERT INTO lists (user_id, name) VALUES (?, ?) RETURNING id",
			userID, "News",
		).Scan(&newsID); err != nil {
			t.Fatalf("creating News list: %v", err)
		}

		// Insert feeds: two in Tech, one in News, one ungrouped.
		type feedRow struct {
			url, title string
			listID     *int64
		}
		techID64 := techID
		newsID64 := newsID
		feedRows := []feedRow{
			{"https://go.example.com/feed.xml", "Go Blog", &techID64},
			{"https://rust.example.com/feed.xml", "Rust Blog", &techID64},
			{"https://bbc.example.com/feed.xml", "BBC News", &newsID64},
			{"https://ungrouped.example.com/feed.xml", "Ungrouped", nil},
		}
		for _, f := range feedRows {
			if _, err := s.db.Exec(
				"INSERT INTO feeds (user_id, list_id, url, title) VALUES (?, ?, ?, ?)",
				userID, f.listID, f.url, f.title,
			); err != nil {
				t.Fatalf("inserting feed %q: %v", f.url, err)
			}
		}

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, authedRequest(t, s, userID, http.MethodGet, "/feeds/export"))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var doc opmlDoc
		if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("parsing OPML: %v", err)
		}

		// Expect: News folder, Tech folder, then ungrouped feed.
		// (ORDER BY list_name ASC puts News before Tech alphabetically.)
		if len(doc.Body.Outlines) != 3 {
			t.Fatalf("top-level outlines = %d, want 3 (News, Tech, Ungrouped)", len(doc.Body.Outlines))
		}

		newsFolder := doc.Body.Outlines[0]
		if newsFolder.Text != "News" {
			t.Errorf("first folder text = %q, want News", newsFolder.Text)
		}
		if newsFolder.XMLURL != "" {
			t.Errorf("folder should not have xmlUrl, got %q", newsFolder.XMLURL)
		}
		if newsFolder.Type != "" {
			t.Errorf("folder should not have type, got %q", newsFolder.Type)
		}
		if len(newsFolder.Outlines) != 1 {
			t.Fatalf("News folder children = %d, want 1", len(newsFolder.Outlines))
		}
		if newsFolder.Outlines[0].XMLURL != "https://bbc.example.com/feed.xml" {
			t.Errorf(
				"News child xmlUrl = %q, want BBC feed",
				newsFolder.Outlines[0].XMLURL,
			)
		}

		techFolder := doc.Body.Outlines[1]
		if techFolder.Text != "Tech" {
			t.Errorf("second folder text = %q, want Tech", techFolder.Text)
		}
		if len(techFolder.Outlines) != 2 {
			t.Fatalf("Tech folder children = %d, want 2", len(techFolder.Outlines))
		}
		// Feeds inside Tech ordered by title: Go Blog, Rust Blog.
		if techFolder.Outlines[0].Text != "Go Blog" {
			t.Errorf("Tech first child = %q, want Go Blog", techFolder.Outlines[0].Text)
		}
		if techFolder.Outlines[1].Text != "Rust Blog" {
			t.Errorf("Tech second child = %q, want Rust Blog", techFolder.Outlines[1].Text)
		}

		ungrouped := doc.Body.Outlines[2]
		if ungrouped.XMLURL != "https://ungrouped.example.com/feed.xml" {
			t.Errorf("ungrouped feed xmlUrl = %q", ungrouped.XMLURL)
		}
		if len(ungrouped.Outlines) != 0 {
			t.Errorf("ungrouped feed should have no children, got %d", len(ungrouped.Outlines))
		}
	})

	t.Run("feed without title falls back to URL", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			userID, "https://notitle.example.com/feed.xml",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, authedRequest(t, s, userID, http.MethodGet, "/feeds/export"))

		var doc opmlDoc
		if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("parsing OPML: %v", err)
		}
		if len(doc.Body.Outlines) != 1 {
			t.Fatalf("outlines = %d, want 1", len(doc.Body.Outlines))
		}
		if doc.Body.Outlines[0].Text != "https://notitle.example.com/feed.xml" {
			t.Errorf(
				"text = %q, want feed URL as fallback",
				doc.Body.Outlines[0].Text,
			)
		}
	})

	t.Run("does not leak other users feeds", func(t *testing.T) {
		s := newTestServer(t)
		user1 := createTestUser(t, s, "user1", "testpass1")
		user2 := createTestUser(t, s, "user2", "testpass2")

		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
			user1, "https://user1.example.com/feed.xml", "User1 Feed",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
			user2, "https://user2.example.com/feed.xml", "User2 Feed",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, authedRequest(t, s, user1, http.MethodGet, "/feeds/export"))

		var doc opmlDoc
		if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatalf("parsing OPML: %v", err)
		}
		if len(doc.Body.Outlines) != 1 {
			t.Fatalf("outlines = %d, want 1", len(doc.Body.Outlines))
		}
		if doc.Body.Outlines[0].XMLURL != "https://user1.example.com/feed.xml" {
			t.Errorf("got other user's feed: %q", doc.Body.Outlines[0].XMLURL)
		}
	})
}

// postOPMLImport creates an authenticated multipart POST /feeds/import request.
func postOPMLImport(t *testing.T, s *Server, userID int64, opmlContent string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("opml", "feeds.opml")
	if err != nil {
		t.Fatalf("creating form file: %v", err)
	}
	if _, err := part.Write([]byte(opmlContent)); err != nil {
		t.Fatalf("writing opml content: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing multipart writer: %v", err)
	}

	req := authedRequest(t, s, userID, http.MethodPost, "/feeds/import")
	req.Body = io.NopCloser(&buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func TestHandleImportOPML(t *testing.T) {
	t.Run("HTMX import returns fragment", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Feed A" title="Feed A" xmlUrl="https://a.example.com/feed.xml" htmlUrl="https://a.example.com"/>
  </body>
</opml>`

		req := postOPMLImport(t, s, userID, opml)
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}
	})

	t.Run("import flat OPML", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Feed A" title="Feed A" xmlUrl="https://a.example.com/feed.xml" htmlUrl="https://a.example.com"/>
    <outline type="rss" text="Feed B" title="Feed B" xmlUrl="https://b.example.com/feed.xml" htmlUrl="https://b.example.com"/>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/feeds" {
			t.Errorf("Location = %q, want /feeds", loc)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userID,
		).Scan(&count); err != nil {
			t.Fatalf("querying feeds: %v", err)
		}
		if count != 2 {
			t.Errorf("feed count = %d, want 2", count)
		}

		flash := flashFromResponse(t, rec)
		if !strings.Contains(flash, "2 new feed") {
			t.Errorf("flash = %q, want mention of 2 new feeds", flash)
		}
		if !strings.Contains(flash, "background") {
			t.Errorf("flash = %q, want mention of background fetching", flash)
		}
	})

	t.Run("import nested OPML creates lists and assigns feeds", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline text="Tech">
      <outline type="rss" text="Feed A" xmlUrl="https://a.example.com/feed.xml"/>
      <outline text="Deep Tech">
        <outline type="rss" text="Feed B" xmlUrl="https://b.example.com/feed.xml"/>
      </outline>
    </outline>
    <outline type="rss" text="Feed C" xmlUrl="https://c.example.com/feed.xml"/>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userID,
		).Scan(&count); err != nil {
			t.Fatalf("querying feeds: %v", err)
		}
		if count != 3 {
			t.Errorf("feed count = %d, want 3", count)
		}

		// Feed A is in Tech folder (immediate parent).
		var listNameA string
		if err := s.db.QueryRow(`
			SELECT COALESCE(l.name, '') FROM feeds f
			LEFT JOIN lists l ON f.list_id = l.id
			WHERE f.user_id = ? AND f.url = ?`,
			userID, "https://a.example.com/feed.xml",
		).Scan(&listNameA); err != nil {
			t.Fatalf("querying Feed A list: %v", err)
		}
		if listNameA != "Tech" {
			t.Errorf("Feed A list = %q, want Tech", listNameA)
		}

		// Feed B is in "Deep Tech" (its immediate parent folder).
		var listNameB string
		if err := s.db.QueryRow(`
			SELECT COALESCE(l.name, '') FROM feeds f
			LEFT JOIN lists l ON f.list_id = l.id
			WHERE f.user_id = ? AND f.url = ?`,
			userID, "https://b.example.com/feed.xml",
		).Scan(&listNameB); err != nil {
			t.Fatalf("querying Feed B list: %v", err)
		}
		if listNameB != "Deep Tech" {
			t.Errorf("Feed B list = %q, want Deep Tech", listNameB)
		}

		// Feed C is top-level — no list.
		var listNameC string
		if err := s.db.QueryRow(`
			SELECT COALESCE(l.name, '') FROM feeds f
			LEFT JOIN lists l ON f.list_id = l.id
			WHERE f.user_id = ? AND f.url = ?`,
			userID, "https://c.example.com/feed.xml",
		).Scan(&listNameC); err != nil {
			t.Fatalf("querying Feed C list: %v", err)
		}
		if listNameC != "" {
			t.Errorf("Feed C list = %q, want empty (ungrouped)", listNameC)
		}

		// Lists "Tech" and "Deep Tech" should exist; no others.
		var listCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM lists WHERE user_id = ?", userID,
		).Scan(&listCount); err != nil {
			t.Fatalf("querying lists: %v", err)
		}
		if listCount != 2 {
			t.Errorf("list count = %d, want 2 (Tech, Deep Tech)", listCount)
		}
	})

	t.Run("import updates list_id for existing feed", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Pre-insert feed with no list.
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
			userID, "https://a.example.com/feed.xml", "Feed A",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline text="Tech">
      <outline type="rss" text="Feed A" xmlUrl="https://a.example.com/feed.xml"/>
    </outline>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		// Feed should now have list_id pointing to "Tech".
		var listName string
		if err := s.db.QueryRow(`
			SELECT COALESCE(l.name, '') FROM feeds f
			LEFT JOIN lists l ON f.list_id = l.id
			WHERE f.user_id = ? AND f.url = ?`,
			userID, "https://a.example.com/feed.xml",
		).Scan(&listName); err != nil {
			t.Fatalf("querying feed list: %v", err)
		}
		if listName != "Tech" {
			t.Errorf("list name = %q, want Tech", listName)
		}

		flash := flashFromResponse(t, rec)
		if !strings.Contains(flash, "0 new feed") {
			t.Errorf("flash = %q, want 0 new feeds (was duplicate)", flash)
		}
		if !strings.Contains(flash, "1 already existed") {
			t.Errorf("flash = %q, want 1 already existed", flash)
		}
	})

	t.Run("import with empty folder name treats feeds as ungrouped", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Folder outline with whitespace-only text attribute.
		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline text="   ">
      <outline type="rss" text="Feed A" xmlUrl="https://a.example.com/feed.xml"/>
    </outline>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		// No lists should have been created.
		var listCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM lists WHERE user_id = ?", userID,
		).Scan(&listCount); err != nil {
			t.Fatalf("querying lists: %v", err)
		}
		if listCount != 0 {
			t.Errorf("list count = %d, want 0 (empty folder name)", listCount)
		}

		// Feed should be ungrouped.
		var listName string
		if err := s.db.QueryRow(`
			SELECT COALESCE(l.name, '') FROM feeds f
			LEFT JOIN lists l ON f.list_id = l.id
			WHERE f.user_id = ? AND f.url = ?`,
			userID, "https://a.example.com/feed.xml",
		).Scan(&listName); err != nil {
			t.Fatalf("querying feed list: %v", err)
		}
		if listName != "" {
			t.Errorf("list name = %q, want empty (ungrouped)", listName)
		}
	})

	t.Run("import OPML with empty folder body creates no list", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Folder with no feed children.
		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline text="EmptyFolder"/>
    <outline type="rss" text="Feed A" xmlUrl="https://a.example.com/feed.xml"/>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		// No list should have been created for the empty folder.
		var listCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM lists WHERE user_id = ?", userID,
		).Scan(&listCount); err != nil {
			t.Fatalf("querying lists: %v", err)
		}
		if listCount != 0 {
			t.Errorf("list count = %d, want 0 (empty folder should not create a list)", listCount)
		}
	})

	t.Run("round-trip export then import", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Create a list and populate it.
		var listID int64
		if err := s.db.QueryRow(
			"INSERT INTO lists (user_id, name) VALUES (?, ?) RETURNING id",
			userID, "Tech",
		).Scan(&listID); err != nil {
			t.Fatalf("creating list: %v", err)
		}
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, list_id, url, title, site_url) VALUES (?, ?, ?, ?, ?)",
			userID, listID, "https://go.example.com/feed.xml", "Go Blog", "https://go.example.com",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url, title, site_url) VALUES (?, ?, ?, ?)",
			userID, "https://ungrouped.example.com/feed.xml", "Ungrouped", "https://ungrouped.example.com",
		); err != nil {
			t.Fatalf("inserting ungrouped feed: %v", err)
		}

		// Export.
		exportRec := httptest.NewRecorder()
		s.ServeHTTP(
			exportRec,
			authedRequest(t, s, userID, http.MethodGet, "/feeds/export"),
		)
		if exportRec.Code != http.StatusOK {
			t.Fatalf("export status = %d, want %d", exportRec.Code, http.StatusOK)
		}
		opmlBytes := exportRec.Body.Bytes()

		// Set up a fresh server and import.
		s2 := newTestServer(t)
		userID2 := createTestUser(t, s2, "testuser", "testpass1")

		importRec := httptest.NewRecorder()
		s2.ServeHTTP(importRec, postOPMLImport(t, s2, userID2, string(opmlBytes)))
		if importRec.Code != http.StatusSeeOther {
			t.Fatalf("import status = %d, want %d", importRec.Code, http.StatusSeeOther)
		}

		// Verify structure on the new server.
		var feedCount int
		if err := s2.db.QueryRow(
			"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userID2,
		).Scan(&feedCount); err != nil {
			t.Fatalf("querying feeds: %v", err)
		}
		if feedCount != 2 {
			t.Errorf("feed count = %d, want 2", feedCount)
		}

		var techListName string
		if err := s2.db.QueryRow(`
			SELECT COALESCE(l.name, '') FROM feeds f
			LEFT JOIN lists l ON f.list_id = l.id
			WHERE f.user_id = ? AND f.url = ?`,
			userID2, "https://go.example.com/feed.xml",
		).Scan(&techListName); err != nil {
			t.Fatalf("querying Go Blog list: %v", err)
		}
		if techListName != "Tech" {
			t.Errorf("Go Blog list = %q, want Tech", techListName)
		}

		var ungroupedListName string
		if err := s2.db.QueryRow(`
			SELECT COALESCE(l.name, '') FROM feeds f
			LEFT JOIN lists l ON f.list_id = l.id
			WHERE f.user_id = ? AND f.url = ?`,
			userID2, "https://ungrouped.example.com/feed.xml",
		).Scan(&ungroupedListName); err != nil {
			t.Fatalf("querying Ungrouped list: %v", err)
		}
		if ungroupedListName != "" {
			t.Errorf("Ungrouped list = %q, want empty", ungroupedListName)
		}
	})

	t.Run("duplicate feeds are skipped", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Pre-insert one feed.
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			userID, "https://a.example.com/feed.xml",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Feed A" xmlUrl="https://a.example.com/feed.xml"/>
    <outline type="rss" text="Feed B" xmlUrl="https://b.example.com/feed.xml"/>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userID,
		).Scan(&count); err != nil {
			t.Fatalf("querying feeds: %v", err)
		}
		if count != 2 {
			t.Errorf("feed count = %d, want 2", count)
		}

		flash := flashFromResponse(t, rec)
		if !strings.Contains(flash, "1 new feed") {
			t.Errorf("flash = %q, want mention of 1 new feed", flash)
		}
		if !strings.Contains(flash, "1 already existed") {
			t.Errorf("flash = %q, want mention of 1 already existed", flash)
		}
	})

	t.Run("empty OPML body", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body></body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		flash := flashFromResponse(t, rec)
		if !strings.Contains(flash, "No feeds found") {
			t.Errorf("flash = %q, want 'No feeds found'", flash)
		}
	})

	t.Run("malformed XML", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, "this is not xml"))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/feeds" {
			t.Errorf("Location = %q, want /feeds", loc)
		}
		flash := flashFromResponse(t, rec)
		if !strings.Contains(flash, "Invalid OPML") {
			t.Errorf("flash = %q, want 'Invalid OPML'", flash)
		}
	})

	t.Run("no file uploaded", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(t, s, userID, http.MethodPost, "/feeds/import")
		req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
		req.Body = io.NopCloser(strings.NewReader("--xxx--\r\n"))

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/feeds" {
			t.Errorf("Location = %q, want /feeds", loc)
		}
		flash := flashFromResponse(t, rec)
		if !strings.Contains(flash, "Please select") {
			t.Errorf("flash = %q, want 'Please select'", flash)
		}
	})

	t.Run("invalid URL schemes are skipped", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Good" xmlUrl="https://good.example.com/feed.xml"/>
    <outline type="rss" text="Bad FTP" xmlUrl="ftp://bad.example.com/feed.xml"/>
    <outline type="rss" text="Bad JS" xmlUrl="javascript:alert(1)"/>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userID,
		).Scan(&count); err != nil {
			t.Fatalf("querying feeds: %v", err)
		}
		if count != 1 {
			t.Errorf("feed count = %d, want 1 (only https)", count)
		}

		flash := flashFromResponse(t, rec)
		if !strings.Contains(flash, "2 skipped") {
			t.Errorf("flash = %q, want mention of 2 skipped", flash)
		}
	})

	t.Run("imports title and site URL from OPML", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Use distinct text vs title to verify the handler persists title, not text.
		opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Display Name" title="Actual Title" xmlUrl="https://example.com/feed.xml" htmlUrl="https://example.com"/>
  </body>
</opml>`

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

		var title, siteURL string
		if err := s.db.QueryRow(
			"SELECT title, site_url FROM feeds WHERE user_id = ? AND url = ?",
			userID, "https://example.com/feed.xml",
		).Scan(&title, &siteURL); err != nil {
			t.Fatalf("querying feed: %v", err)
		}
		if title != "Actual Title" {
			t.Errorf("title = %q, want 'Actual Title'", title)
		}
		if siteURL != "https://example.com" {
			t.Errorf("site_url = %q, want 'https://example.com'", siteURL)
		}
	})
}

func TestHandleImportOPML_CrossUser(t *testing.T) {
	s := newTestServer(t)
	userA := createTestUser(t, s, "userA", "testpass1")
	userB := createTestUser(t, s, "userB", "testpass2")

	// User A already has this feed.
	if _, err := s.db.Exec(
		"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
		userA, "https://shared.example.com/feed.xml", "Shared Feed",
	); err != nil {
		t.Fatalf("inserting feed for user A: %v", err)
	}

	// User B imports the same URL.
	opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Shared Feed" xmlUrl="https://shared.example.com/feed.xml"/>
  </body>
</opml>`

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, postOPMLImport(t, s, userB, opml))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	flash := flashFromResponse(t, rec)
	if !strings.Contains(flash, "1 new feed") {
		t.Errorf("flash = %q, want mention of 1 new feed", flash)
	}

	// User B should have 1 feed.
	var countB int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userB,
	).Scan(&countB); err != nil {
		t.Fatalf("querying user B feeds: %v", err)
	}
	if countB != 1 {
		t.Errorf("user B feed count = %d, want 1", countB)
	}

	// User A should still have exactly 1 feed.
	var countA int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userA,
	).Scan(&countA); err != nil {
		t.Fatalf("querying user A feeds: %v", err)
	}
	if countA != 1 {
		t.Errorf("user A feed count = %d, want 1", countA)
	}
}

func TestHandleImportOPML_AllDuplicates(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Pre-insert both feeds.
	for _, u := range []string{
		"https://a.example.com/feed.xml",
		"https://b.example.com/feed.xml",
	} {
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			userID, u,
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
	}

	opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Feed A" xmlUrl="https://a.example.com/feed.xml"/>
    <outline type="rss" text="Feed B" xmlUrl="https://b.example.com/feed.xml"/>
  </body>
</opml>`

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	flash := flashFromResponse(t, rec)
	if !strings.Contains(flash, "0 new feed") {
		t.Errorf("flash = %q, want mention of 0 new feeds", flash)
	}
	if !strings.Contains(flash, "2 already existed") {
		t.Errorf("flash = %q, want mention of 2 already existed", flash)
	}
	// Should NOT mention background fetching when nothing was imported.
	if strings.Contains(flash, "background") {
		t.Errorf("flash = %q, should not mention background fetching", flash)
	}
}

func TestHandleImportOPML_WhitespaceURL(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="Padded" xmlUrl="  https://example.com/feed.xml  "/>
  </body>
</opml>`

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	// Feed should be stored with trimmed URL.
	var storedURL string
	if err := s.db.QueryRow(
		"SELECT url FROM feeds WHERE user_id = ?",
		userID,
	).Scan(&storedURL); err != nil {
		t.Fatalf("querying feed: %v", err)
	}
	if storedURL != "https://example.com/feed.xml" {
		t.Errorf("stored URL = %q, want trimmed URL", storedURL)
	}
}

func TestHandleImportOPML_FileTooLarge(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Create an OPML payload that exceeds maxOPMLSize (1 MB).
	header := `<?xml version="1.0" encoding="UTF-8"?><opml version="2.0"><head><title>Test</title></head><body><!--`
	footer := `--></body></opml>`
	padding := strings.Repeat("x", maxOPMLSize+1024)
	oversized := header + padding + footer

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, postOPMLImport(t, s, userID, oversized))

	// MaxBytesReader causes FormFile to fail before ReadAll, so the handler
	// redirects with a flash. The exact message depends on where the limit
	// is hit, but the key invariant is: no crash, redirect, no feeds created.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	var count int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userID,
	).Scan(&count); err != nil {
		t.Fatalf("querying feeds: %v", err)
	}
	if count != 0 {
		t.Errorf("feed count = %d, want 0 (oversized file should be rejected)", count)
	}
}

func TestHandleImportOPML_HTTPScheme(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	opml := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Test</title></head>
  <body>
    <outline type="rss" text="HTTP Feed" xmlUrl="http://example.com/feed.xml"/>
    <outline type="rss" text="HTTPS Feed" xmlUrl="https://secure.example.com/feed.xml"/>
  </body>
</opml>`

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, postOPMLImport(t, s, userID, opml))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	var count int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM feeds WHERE user_id = ?", userID,
	).Scan(&count); err != nil {
		t.Fatalf("querying feeds: %v", err)
	}
	if count != 2 {
		t.Errorf("feed count = %d, want 2 (both http and https)", count)
	}
}

func TestCollectFeedsWithFolder(t *testing.T) {
	t.Run("flat feeds have no folder", func(t *testing.T) {
		outlines := []opmlOutline{
			{Text: "Feed A", XMLURL: "https://a.example.com/feed.xml"},
			{Text: "Feed B", XMLURL: "https://b.example.com/feed.xml"},
		}
		got := collectFeedsWithFolder(outlines, "")
		if len(got) != 2 {
			t.Fatalf("got %d feeds, want 2", len(got))
		}
		for _, fw := range got {
			if fw.folderName != "" {
				t.Errorf("folderName = %q, want empty for top-level feed", fw.folderName)
			}
		}
	})

	t.Run("nested categories use immediate parent", func(t *testing.T) {
		outlines := []opmlOutline{
			{Text: "Tech", Outlines: []opmlOutline{
				{Text: "Feed A", XMLURL: "https://a.example.com/feed.xml"},
				{Text: "Deep Tech", Outlines: []opmlOutline{
					{Text: "Feed B", XMLURL: "https://b.example.com/feed.xml"},
				}},
			}},
			{Text: "Feed C", XMLURL: "https://c.example.com/feed.xml"},
		}

		got := collectFeedsWithFolder(outlines, "")
		if len(got) != 3 {
			t.Fatalf("got %d feeds, want 3", len(got))
		}

		byURL := make(map[string]string)
		for _, fw := range got {
			byURL[fw.outline.XMLURL] = fw.folderName
		}

		if byURL["https://a.example.com/feed.xml"] != "Tech" {
			t.Errorf(
				"Feed A folder = %q, want Tech",
				byURL["https://a.example.com/feed.xml"],
			)
		}
		if byURL["https://b.example.com/feed.xml"] != "Deep Tech" {
			t.Errorf(
				"Feed B folder = %q, want Deep Tech",
				byURL["https://b.example.com/feed.xml"],
			)
		}
		if byURL["https://c.example.com/feed.xml"] != "" {
			t.Errorf(
				"Feed C folder = %q, want empty",
				byURL["https://c.example.com/feed.xml"],
			)
		}
	})

	t.Run("outline with xmlUrl AND children is treated as feed", func(t *testing.T) {
		outlines := []opmlOutline{
			{
				Text:   "Parent Feed",
				XMLURL: "https://parent.example.com/feed.xml",
				Outlines: []opmlOutline{
					{Text: "Child Feed", XMLURL: "https://child.example.com/feed.xml"},
				},
			},
		}

		got := collectFeedsWithFolder(outlines, "")
		if len(got) != 2 {
			t.Fatalf("got %d feeds, want 2", len(got))
		}

		byURL := make(map[string]string)
		for _, fw := range got {
			byURL[fw.outline.XMLURL] = fw.folderName
		}

		// The parent has an xmlUrl so it is a feed, not a folder.
		// Its children inherit the current parentFolder (empty), not its text.
		if _, ok := byURL["https://parent.example.com/feed.xml"]; !ok {
			t.Error("missing parent feed")
		}
		if _, ok := byURL["https://child.example.com/feed.xml"]; !ok {
			t.Error("missing child feed")
		}
	})

	t.Run("whitespace-only folder name treated as ungrouped", func(t *testing.T) {
		outlines := []opmlOutline{
			{Text: "   ", Outlines: []opmlOutline{
				{Text: "Feed A", XMLURL: "https://a.example.com/feed.xml"},
			}},
		}

		got := collectFeedsWithFolder(outlines, "")
		if len(got) != 1 {
			t.Fatalf("got %d feeds, want 1", len(got))
		}
		if got[0].folderName != "" {
			t.Errorf("folderName = %q, want empty for whitespace folder", got[0].folderName)
		}
	})

	t.Run("nil input", func(t *testing.T) {
		got := collectFeedsWithFolder(nil, "")
		if len(got) != 0 {
			t.Errorf("got %d feeds, want 0", len(got))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		got := collectFeedsWithFolder([]opmlOutline{}, "")
		if len(got) != 0 {
			t.Errorf("got %d feeds, want 0", len(got))
		}
	})
}

func FuzzHandleImportOPML(f *testing.F) {
	// Seed corpus with representative OPML variants.
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body><outline xmlUrl="https://example.com/feed.xml"/></body></opml>`)
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body></body></opml>`)
	f.Add(`not xml at all`)
	f.Add(``)
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Cat"><outline xmlUrl="https://a.example.com/f"/></outline></body></opml>`)
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body><outline xmlUrl="ftp://bad.example.com"/><outline xmlUrl="javascript:alert(1)"/></body></opml>`)

	f.Fuzz(func(t *testing.T, data string) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postOPMLImport(t, s, userID, data))

		// The handler must never panic and must always redirect.
		if rec.Code != http.StatusSeeOther {
			t.Errorf(
				"status = %d, want %d for input %q",
				rec.Code, http.StatusSeeOther, data,
			)
		}

		// No non-http/https URL should ever make it into the database.
		rows, err := s.db.Query(
			"SELECT url FROM feeds WHERE user_id = ?", userID,
		)
		if err != nil {
			t.Fatalf("querying feeds: %v", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var u string
			if err := rows.Scan(&u); err != nil {
				t.Fatalf("scanning url: %v", err)
			}
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				t.Errorf("non-http(s) URL in database: %q", u)
			}
		}
	})
}

func FuzzCollectFeedsWithFolder(f *testing.F) {
	// Seed corpus: raw XML strings that get unmarshaled into opmlDoc.
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body><outline xmlUrl="https://example.com/feed.xml"/></body></opml>`)
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Cat"><outline xmlUrl="https://a.example.com/f"/><outline text="Sub"><outline xmlUrl="https://b.example.com/f"/></outline></outline></body></opml>`)
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body></body></opml>`)
	f.Add(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Empty"/></body></opml>`)
	f.Add(`not xml`)

	f.Fuzz(func(t *testing.T, data string) {
		var doc opmlDoc
		if err := xml.Unmarshal([]byte(data), &doc); err != nil {
			return // invalid XML is not interesting for this target
		}

		feeds := collectFeedsWithFolder(doc.Body.Outlines, "")

		// Every returned entry must have a non-whitespace xmlUrl.
		for _, fw := range feeds {
			if strings.TrimSpace(fw.outline.XMLURL) == "" {
				t.Errorf("collectFeedsWithFolder returned entry with empty xmlUrl")
			}
		}

		// Output count must not exceed total node count.
		totalNodes := countOutlineNodes(doc.Body.Outlines)
		if len(feeds) > totalNodes {
			t.Errorf(
				"collected %d feeds but only %d total nodes",
				len(feeds), totalNodes,
			)
		}
	})
}

// countOutlineNodes recursively counts the total number of outline nodes.
func countOutlineNodes(outlines []opmlOutline) int {
	count := len(outlines)
	for _, o := range outlines {
		count += countOutlineNodes(o.Outlines)
	}
	return count
}

// flashFromResponse extracts the rdr_flash cookie value from a response.
func flashFromResponse(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_flash" {
			return c.Value
		}
	}
	return ""
}
