package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/chrisallenlane/rdr/internal/token"
)

// listFixture seeds two users with one list and one feed each. Alice
// has a "tech" list with one feed assigned; bob has a "news" list
// with no feed assigned.
func listFixture(t *testing.T) (db *sql.DB, aliceTok, bobTok string, aliceList, aliceFeed, bobList, bobFeed int64) {
	t.Helper()
	db = testutil.OpenTestDB(t)

	alice := testutil.InsertUser(t, db, "alice")
	bob := testutil.InsertUser(t, db, "bob")

	res, err := db.Exec(`INSERT INTO lists (user_id, name) VALUES (?, ?)`, alice, "tech")
	if err != nil {
		t.Fatalf("insert alice list: %v", err)
	}
	aliceList, _ = res.LastInsertId()

	res, err = db.Exec(`INSERT INTO feeds (user_id, list_id, url, title) VALUES (?, ?, ?, ?)`,
		alice, aliceList, "https://alice.example/feed", "Alice Blog")
	if err != nil {
		t.Fatalf("insert alice feed: %v", err)
	}
	aliceFeed, _ = res.LastInsertId()

	res, err = db.Exec(`INSERT INTO lists (user_id, name) VALUES (?, ?)`, bob, "news")
	if err != nil {
		t.Fatalf("insert bob list: %v", err)
	}
	bobList, _ = res.LastInsertId()

	res, err = db.Exec(`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		bob, "https://bob.example/feed", "Bob Blog")
	if err != nil {
		t.Fatalf("insert bob feed: %v", err)
	}
	bobFeed, _ = res.LastInsertId()

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

func TestListLists_ScopedByUser(t *testing.T) {
	db, aliceTok, _, _, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/lists", aliceTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var lists []List
	if err := json.Unmarshal(rec.Body.Bytes(), &lists); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(lists) != 1 || lists[0].Name != "tech" {
		t.Errorf("alice should see only her list 'tech'; got %+v", lists)
	}
	if lists[0].FeedCount != 1 {
		t.Errorf("feed_count: got %d, want 1", lists[0].FeedCount)
	}
}

func TestGetList_OwnedReturnsFeeds(t *testing.T) {
	db, aliceTok, _, aliceList, aliceFeed, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, urlf("/api/v1/lists/%d", aliceList), aliceTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var d ListDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.List.Id != aliceList {
		t.Errorf("list id: got %d, want %d", d.List.Id, aliceList)
	}
	if len(d.Feeds) != 1 || d.Feeds[0].Id != aliceFeed {
		t.Errorf("feeds: got %+v, want one feed id=%d", d.Feeds, aliceFeed)
	}
}

func TestGetList_CrossUserReturns404(t *testing.T) {
	db, _, bobTok, aliceList, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, urlf("/api/v1/lists/%d", aliceList), bobTok, ""))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 (IDOR check)", rec.Code)
	}
}

func TestCreateList_Success(t *testing.T) {
	db, aliceTok, _, _, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/lists", aliceTok, `{"name":"science"}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%q", rec.Code, rec.Body.String())
	}
	var l List
	if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if l.Name != "science" {
		t.Errorf("name: got %q, want science", l.Name)
	}
}

func TestCreateList_DuplicateReturns409(t *testing.T) {
	db, aliceTok, _, _, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/lists", aliceTok, `{"name":"tech"}`))

	if rec.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rec.Code)
	}
}

func TestCreateList_RejectsEmptyName(t *testing.T) {
	db, aliceTok, _, _, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/lists", aliceTok, `{"name":"  "}`))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestRenameList_Success(t *testing.T) {
	db, aliceTok, _, aliceList, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPatch,
		urlf("/api/v1/lists/%d", aliceList), aliceTok, `{"name":"engineering"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var l List
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	if l.Name != "engineering" {
		t.Errorf("renamed: got %q, want engineering", l.Name)
	}
}

func TestRenameList_CrossUserReturns404(t *testing.T) {
	db, _, bobTok, aliceList, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPatch,
		urlf("/api/v1/lists/%d", aliceList), bobTok, `{"name":"hijacked"}`))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
}

func TestRenameList_DuplicateReturns409(t *testing.T) {
	db, aliceTok, _, aliceList, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	// Pre-create a second list so renaming "tech" → "another" then back
	// would conflict; simpler: create two lists then try to collide them.
	if _, err := db.Exec(`INSERT INTO lists (user_id, name) VALUES (?, ?)`,
		1, "another"); err != nil {
		t.Fatalf("seed second list: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPatch,
		urlf("/api/v1/lists/%d", aliceList), aliceTok, `{"name":"another"}`))

	if rec.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rec.Code)
	}
}

func TestDeleteList_OwnedSucceeds(t *testing.T) {
	db, aliceTok, _, aliceList, aliceFeed, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodDelete,
		urlf("/api/v1/lists/%d", aliceList), aliceTok, ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", rec.Code)
	}

	// Verify alice's feed remains, but with list_id NULL.
	var listID sql.NullInt64
	if err := db.QueryRow(`SELECT list_id FROM feeds WHERE id = ?`, aliceFeed).Scan(&listID); err != nil {
		t.Fatalf("query feed: %v", err)
	}
	if listID.Valid {
		t.Errorf("feeds.list_id after delete: got %d, want NULL", listID.Int64)
	}
}

func TestDeleteList_CrossUserReturns404(t *testing.T) {
	db, _, bobTok, aliceList, _, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodDelete,
		urlf("/api/v1/lists/%d", aliceList), bobTok, ""))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
}

func TestAddFeedToList_OwnedFeedAndList(t *testing.T) {
	db, aliceTok, _, aliceList, aliceFeed, _, _ := listFixture(t)
	// Detach alice's feed first so we can verify the add reattaches it.
	if _, err := db.Exec(`UPDATE feeds SET list_id = NULL WHERE id = ?`, aliceFeed); err != nil {
		t.Fatalf("detach feed: %v", err)
	}
	h := New(Config{DB: db})

	body := urlf(`{"feed_id":%d}`, aliceFeed)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost,
		urlf("/api/v1/lists/%d/feeds", aliceList), aliceTok, body))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204; body=%q", rec.Code, rec.Body.String())
	}

	var listID sql.NullInt64
	_ = db.QueryRow(`SELECT list_id FROM feeds WHERE id = ?`, aliceFeed).Scan(&listID)
	if !listID.Valid || listID.Int64 != aliceList {
		t.Errorf("feed not added to list: list_id=%v, want %d", listID, aliceList)
	}
}

// IDOR: alice tries to put bob's feed into her own list.
func TestAddFeedToList_RejectsCrossUserFeed(t *testing.T) {
	db, aliceTok, _, aliceList, _, _, bobFeed := listFixture(t)
	h := New(Config{DB: db})

	body := urlf(`{"feed_id":%d}`, bobFeed)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost,
		urlf("/api/v1/lists/%d/feeds", aliceList), aliceTok, body))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 (cross-user feed)", rec.Code)
	}

	// Confirm bob's feed wasn't touched.
	var listID sql.NullInt64
	_ = db.QueryRow(`SELECT list_id FROM feeds WHERE id = ?`, bobFeed).Scan(&listID)
	if listID.Valid {
		t.Errorf("bob's feed list_id was modified: got %d", listID.Int64)
	}
}

// AddFeedToList is idempotent: re-adding a feed already in the same list
// returns 204 (not 404). The single-statement UPDATE returns 0 rows
// affected for "feed already there", so the handler distinguishes that
// from "ownership failure" via an explicit existence check.
func TestAddFeedToList_AlreadyInList_Returns204(t *testing.T) {
	db, aliceTok, _, aliceList, aliceFeed, _, _ := listFixture(t)
	h := New(Config{DB: db})
	body := urlf(`{"feed_id":%d}`, aliceFeed)
	target := urlf("/api/v1/lists/%d/feeds", aliceList)

	// Seed state: alice's feed is already in alice's list (per listFixture).
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, target, aliceTok, body))
	if rec.Code != http.StatusNoContent {
		t.Errorf("idempotent re-add: got %d, want 204; body=%q",
			rec.Code, rec.Body.String())
	}
}

// IDOR: alice tries to put her own feed into bob's list.
func TestAddFeedToList_RejectsCrossUserList(t *testing.T) {
	db, aliceTok, _, _, aliceFeed, bobList, _ := listFixture(t)
	h := New(Config{DB: db})

	body := urlf(`{"feed_id":%d}`, aliceFeed)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost,
		urlf("/api/v1/lists/%d/feeds", bobList), aliceTok, body))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 (cross-user list)", rec.Code)
	}

	// Confirm alice's feed remains in her own list (the seeded state).
	var listID sql.NullInt64
	_ = db.QueryRow(`SELECT list_id FROM feeds WHERE id = ?`, aliceFeed).Scan(&listID)
	// listFixture put aliceFeed in aliceList; verify it's still there.
	if !listID.Valid {
		t.Errorf("alice's feed list_id became NULL")
	}
}

func TestRemoveFeedFromList_Owned(t *testing.T) {
	db, aliceTok, _, aliceList, aliceFeed, _, _ := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodDelete,
		urlf("/api/v1/lists/%d/feeds/%d", aliceList, aliceFeed), aliceTok, ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204", rec.Code)
	}

	var listID sql.NullInt64
	_ = db.QueryRow(`SELECT list_id FROM feeds WHERE id = ?`, aliceFeed).Scan(&listID)
	if listID.Valid {
		t.Errorf("feed list_id after remove: got %d, want NULL", listID.Int64)
	}
}

func TestRemoveFeedFromList_RejectsCrossUserList(t *testing.T) {
	db, aliceTok, _, _, _, bobList, bobFeed := listFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodDelete,
		urlf("/api/v1/lists/%d/feeds/%d", bobList, bobFeed), aliceTok, ""))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
}
