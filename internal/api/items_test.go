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
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, urlf("/api/v1/items/%d", aliceItem), bobTok, ""))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 (IDOR check)", rec.Code)
	}
}

func TestStarUnstarItem(t *testing.T) {
	db, aliceTok, _, _, aliceItem := itemFixture(t)
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

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
	h := New(Config{DB: db})

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

// TestListItems_PaginationLinks verifies that the Link (RFC 5988) and
// X-Total-Count (RFC 6648-ish) headers wire up correctly when the
// result set spans multiple pages. Earlier tests use small fixtures
// where total <= pageSize, so the Link branches in writePagination
// were uncovered.
func TestListItems_PaginationLinks(t *testing.T) {
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, "alice")
	tok, _, err := token.Generate(db, uid, "test", time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Create a feed and 75 items so we cross the page-size threshold.
	res, err := db.Exec(
		`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		uid, "https://example.com/feed", "Big Feed",
	)
	if err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	const itemCount = 75
	now := time.Now()
	for i := 0; i < itemCount; i++ {
		if _, err := db.Exec(
			`INSERT INTO items (feed_id, guid, title, published_at)
			 VALUES (?, ?, ?, ?)`,
			feedID,
			fmt.Sprintf("guid-%d", i),
			fmt.Sprintf("Item %d", i),
			now.Add(-time.Duration(i)*time.Minute).Format(time.RFC3339),
		); err != nil {
			t.Fatalf("insert item %d: %v", i, err)
		}
	}

	h := New(Config{DB: db})

	// Page 1: expect next + last; should NOT carry first/prev.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/items?page=1", tok, ""))

	if got := rec.Header().Get("X-Total-Count"); got != "75" {
		t.Errorf("X-Total-Count: got %q, want 75", got)
	}
	link := rec.Header().Get("Link")
	if link == "" {
		t.Fatal("page 1: Link header missing")
	}
	if !strings.Contains(link, `rel="next"`) || !strings.Contains(link, `rel="last"`) {
		t.Errorf(`page 1 Link missing next/last rels: %q`, link)
	}
	if strings.Contains(link, `rel="prev"`) || strings.Contains(link, `rel="first"`) {
		t.Errorf(`page 1 Link should not contain prev/first: %q`, link)
	}
	// page=2 in next, page=2 in last (because 75 items / 50 = 2 pages)
	if !strings.Contains(link, "page=2") {
		t.Errorf(`page 1 Link should reference page=2: %q`, link)
	}

	// Page 2: expect first + prev; no next/last.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/items?page=2", tok, ""))

	link = rec.Header().Get("Link")
	if !strings.Contains(link, `rel="prev"`) || !strings.Contains(link, `rel="first"`) {
		t.Errorf(`page 2 Link missing prev/first rels: %q`, link)
	}
	if strings.Contains(link, `rel="next"`) || strings.Contains(link, `rel="last"`) {
		t.Errorf(`page 2 Link should not contain next/last: %q`, link)
	}
	if !strings.Contains(link, "page=1") {
		t.Errorf(`page 2 Link should reference page=1: %q`, link)
	}
}

// TestMarkRead_FilterByList covers the list_id branch of MarkItemsRead,
// including the "list belongs to another user" no-op case (which does
// NOT leak data — RowsAffected=0 because the IN-subquery is false).
func TestMarkRead_FilterByList(t *testing.T) {
	db, aliceTok, bobTok, aliceFeed, _ := itemFixture(t)

	// Alice creates a list and adds her feed to it.
	res, err := db.Exec(
		`INSERT INTO lists (user_id, name) VALUES (?, ?)`, 1, "alice-list",
	)
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	listID, _ := res.LastInsertId()
	if _, err := db.Exec(
		`UPDATE feeds SET list_id = ? WHERE id = ?`, listID, aliceFeed,
	); err != nil {
		t.Fatalf("attach feed to list: %v", err)
	}

	h := New(Config{DB: db})

	// Owned list: marks alice's 2 unread items.
	body := urlf(`{"list_id": %d}`, listID)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read", aliceTok, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("alice mark-read by list: status=%d body=%q", rec.Code, rec.Body.String())
	}
	var resp MarkReadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Marked != 2 {
		t.Errorf("alice: marked=%d, want 2", resp.Marked)
	}

	// Bob targets alice's list_id: silently no-ops (no data leak, no
	// 404 — this is the documented contract for batch mark-read).
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read", bobTok, body))
	if rec.Code != http.StatusOK {
		t.Fatalf("bob cross-user mark-read: status=%d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Marked != 0 {
		t.Errorf("bob cross-user: marked=%d, want 0", resp.Marked)
	}
}

// TestMarkRead_NoStarredFilterSupport documents the parallel bug on
// the JSON API path: MarkReadRequest accepts feed_id XOR list_id but
// has no `starred` (or `unread`) field. An API client that uses
// GET /api/v1/items?starred=true to fetch starred items and then issues
// POST /api/v1/items/mark-read with no filter (the only no-arg call the
// schema permits) marks ALL unread items account-wide, including
// non-starred items the client never saw.
//
// The schema is strict (additionalProperties: false plus
// DisallowUnknownFields), so a client cannot even attempt to send
// {"starred": true} — the request 400s. There is *no way* to scope
// mark-read to starred items via the API. The test asserts the
// behavior we want: marking only starred items should be possible and
// non-starred items must remain unread.
//
// Current behavior: the only call available is "no body" / "empty body",
// which marks all unread items account-wide. Test fails because item 2
// (unread, not starred) ends up read.
func TestMarkRead_NoStarredFilterSupport(t *testing.T) {
	db := testutil.OpenTestDB(t)
	alice := testutil.InsertUser(t, db, "alice")

	res, err := db.Exec(
		`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		alice, "https://alice.example/feed", "Alice Blog",
	)
	if err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	aliceFeed, _ := res.LastInsertId()

	// Two items:
	//   - "starred-unread": unread AND starred — the only item that
	//      GET /api/v1/items?starred=true returns.
	//   - "plain-unread":   unread, NOT starred — invisible on the
	//      starred view, must NOT be marked read.
	rows := []struct {
		guid    string
		read    int
		starred int
	}{
		{"starred-unread", 0, 1},
		{"plain-unread", 0, 0},
	}
	for _, row := range rows {
		if _, err := db.Exec(
			`INSERT INTO items (feed_id, guid, title, read, starred, published_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			aliceFeed, row.guid, "Item", row.read, row.starred,
			time.Now().Format(time.RFC3339),
		); err != nil {
			t.Fatalf("insert item %s: %v", row.guid, err)
		}
	}

	aliceTok, _, err := token.Generate(db, alice, "alice-test", time.Time{})
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	h := New(Config{DB: db})

	// Confirm GET /api/v1/items?starred=true returns only the starred item.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/items?starred=true", aliceTok, ""))
	var visible []Item
	_ = json.Unmarshal(rec.Body.Bytes(), &visible)
	if len(visible) != 1 {
		t.Fatalf("GET /api/v1/items?starred=true: got %d items, want 1", len(visible))
	}

	// Mark-read scoped to starred items using the new field.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read",
		aliceTok, `{"starred": true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("mark-read: status=%d body=%q", rec.Code, rec.Body.String())
	}

	// The non-starred item must still be unread — the client only
	// "saw" the starred item and (logically) only intended to clear
	// that one.
	var item2Read int
	if err := db.QueryRow(
		`SELECT read FROM items WHERE guid = 'plain-unread'`,
	).Scan(&item2Read); err != nil {
		t.Fatalf("query plain-unread: %v", err)
	}
	if item2Read != 0 {
		t.Errorf("non-starred item read = %d, want 0", item2Read)
	}

	// The starred item must now be read.
	var item1Read int
	if err := db.QueryRow(
		`SELECT read FROM items WHERE guid = 'starred-unread'`,
	).Scan(&item1Read); err != nil {
		t.Fatalf("query starred-unread: %v", err)
	}
	if item1Read != 1 {
		t.Errorf("starred item read = %d, want 1", item1Read)
	}
}

// TestMarkRead_StarredBodyRejected was renamed/repurposed: the `starred`
// field now exists on MarkReadRequest, so {"starred": true} is accepted
// and restricts mark-read to starred items only.
func TestMarkRead_StarredBodyRejected(t *testing.T) {
	db, aliceTok, _, _, _ := itemFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/items/mark-read",
		aliceTok, `{"starred": true}`))
	// Now 200: the schema has the `starred` field and the handler uses it.
	if rec.Code != http.StatusOK {
		t.Errorf("status=%d, want 200 (`starred` field is now part of MarkReadRequest)", rec.Code)
	}
}
