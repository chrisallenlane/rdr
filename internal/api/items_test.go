package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/chrisallenlane/rdr/internal/token"
)

// itemFixture seeds users, feeds, and items, returning the first user's
// authentication token. Specifically:
//   - alice: 1 feed with 3 items (1 read, 2 unread, 1 starred)
//   - bob:   1 feed with 1 item
func itemFixture(t *testing.T) (db *sql.DB, aliceTok, bobTok string, aliceFeed, aliceItem int64) {
	t.Helper()
	db = testutil.OpenTestDB(t)

	alice := testutil.InsertUser(t, db, "alice")
	bob := testutil.InsertUser(t, db, "bob")

	res, err := db.Exec(
		`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		alice, "https://alice.example/feed", "Alice Blog",
	)
	if err != nil {
		t.Fatalf("insert alice feed: %v", err)
	}
	aliceFeed, _ = res.LastInsertId()

	now := time.Now()
	itemResults := []struct {
		title    string
		read     int
		starred  int
		hoursAgo int
	}{
		{"Alice unread starred", 0, 1, 1},
		{"Alice unread", 0, 0, 2},
		{"Alice read", 1, 0, 3},
	}
	for i, ir := range itemResults {
		res, err := db.Exec(
			`INSERT INTO items (feed_id, guid, title, content, url, published_at, read, starred)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			aliceFeed, "guid-"+ir.title, ir.title, "<p>body of "+ir.title+"</p>",
			"https://alice.example/post/"+ir.title,
			now.Add(-time.Duration(ir.hoursAgo)*time.Hour).Format(time.RFC3339),
			ir.read, ir.starred,
		)
		if err != nil {
			t.Fatalf("insert alice item %d: %v", i, err)
		}
		if i == 0 {
			aliceItem, _ = res.LastInsertId()
		}
	}

	bobFeedRes, err := db.Exec(
		`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		bob, "https://bob.example/feed", "Bob Blog",
	)
	if err != nil {
		t.Fatalf("insert bob feed: %v", err)
	}
	bobFeed, _ := bobFeedRes.LastInsertId()
	if _, err := db.Exec(
		`INSERT INTO items (feed_id, guid, title) VALUES (?, ?, ?)`,
		bobFeed, "guid-bob", "Bob secret",
	); err != nil {
		t.Fatalf("insert bob item: %v", err)
	}

	aliceTok, _, err = token.Generate(db, alice, "alice-test", time.Time{})
	if err != nil {
		t.Fatalf("alice token: %v", err)
	}
	bobTok, _, err = token.Generate(db, bob, "bob-test", time.Time{})
	if err != nil {
		t.Fatalf("bob token: %v", err)
	}
	return
}

func authedRequest(method, target, tok string, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Authorization", "Bearer "+tok)
	return r
}

func TestListItems_ScopedByUser(t *testing.T) {
	db, aliceTok, _, _, _ := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/items", aliceTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var items []Item
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("alice should see 3 items; got %d", len(items))
	}
	for _, it := range items {
		if !strings.HasPrefix(it.Title, "Alice") {
			t.Errorf("foreign item leaked: %q", it.Title)
		}
	}

	if got := rec.Header().Get("X-Total-Count"); got != "3" {
		t.Errorf("X-Total-Count: got %q, want 3", got)
	}
}

func TestListItems_FilterUnread(t *testing.T) {
	db, aliceTok, _, _, _ := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/items?unread=true", aliceTok, ""))

	var items []Item
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 2 {
		t.Errorf("got %d unread, want 2", len(items))
	}
	for _, it := range items {
		if it.Read {
			t.Errorf("read item leaked into unread filter: %q", it.Title)
		}
	}
}

func TestListItems_FilterStarred(t *testing.T) {
	db, aliceTok, _, _, _ := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/items?starred=true", aliceTok, ""))

	var items []Item
	_ = json.Unmarshal(rec.Body.Bytes(), &items)
	if len(items) != 1 {
		t.Errorf("got %d starred, want 1", len(items))
	}
}

func TestGetItem_OwnedReturnsFullContent(t *testing.T) {
	db, aliceTok, _, _, aliceItem := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, urlf("/api/v1/items/%d", aliceItem), aliceTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var it Item
	if err := json.Unmarshal(rec.Body.Bytes(), &it); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if it.Content == nil || !strings.Contains(*it.Content, "<p>body of") {
		t.Errorf("content missing or wrong: %v", it.Content)
	}
}

func TestGetItem_CrossUserReturns404(t *testing.T) {
	db, _, bobTok, _, aliceItem := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, urlf("/api/v1/items/%d", aliceItem), bobTok, ""))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 (IDOR check)", rec.Code)
	}
}

func TestStarUnstarItem(t *testing.T) {
	db, aliceTok, _, _, aliceItem := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodDelete, urlf("/api/v1/items/%d/star", aliceItem), aliceTok, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unstar: got %d, want 204; body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPut, urlf("/api/v1/items/%d/star", aliceItem), aliceTok, ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("star: got %d, want 204; body=%q", rec.Code, rec.Body.String())
	}

	// Verify via DB.
	var starred int
	if err := db.QueryRow(`SELECT starred FROM items WHERE id=?`, aliceItem).Scan(&starred); err != nil {
		t.Fatalf("query starred: %v", err)
	}
	if starred != 1 {
		t.Errorf("starred=%d, want 1 after PUT /star", starred)
	}
}

func TestStarItem_CrossUserReturns404(t *testing.T) {
	db, _, bobTok, _, aliceItem := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPut, urlf("/api/v1/items/%d/star", aliceItem), bobTok, ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}

	// Verify alice's item is unchanged.
	var starred int
	_ = db.QueryRow(`SELECT starred FROM items WHERE id=?`, aliceItem).Scan(&starred)
	if starred != 1 {
		t.Errorf("starred=%d after cross-user attempt; should remain 1", starred)
	}
}

func TestMarkRead_All(t *testing.T) {
	db, aliceTok, _, _, _ := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read", aliceTok, "{}"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var resp MarkReadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Marked != 2 {
		t.Errorf("marked: got %d, want 2 (alice had 2 unread)", resp.Marked)
	}
}

func TestMarkRead_FilterByFeed(t *testing.T) {
	db, aliceTok, _, aliceFeed, _ := itemFixture(t)
	h := New(db)

	body := `{"feed_id": 999999}` // wrong feed id; should mark 0
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read", aliceTok, body))

	var resp MarkReadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Marked != 0 {
		t.Errorf("marked: got %d, want 0 (wrong feed id)", resp.Marked)
	}

	body = urlf(`{"feed_id": %d}`, aliceFeed)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read", aliceTok, body))
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Marked != 2 {
		t.Errorf("marked: got %d, want 2", resp.Marked)
	}
}

func TestMarkRead_RejectsBothFilters(t *testing.T) {
	db, aliceTok, _, _, _ := itemFixture(t)
	h := New(db)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read", aliceTok,
		`{"feed_id": 1, "list_id": 1}`))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

// urlf is a tiny shim around fmt.Sprintf to keep request setup terse.
func urlf(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}
