package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/discover"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/chrisallenlane/rdr/internal/token"
)

// feedFixture seeds two users, one feed each (alice's has 2 items, bob's
// has 0), and returns auth tokens plus alice's feed id.
func feedFixture(t *testing.T) (db *sql.DB, aliceTok, bobTok string, aliceFeed int64) {
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

	for i, read := range []int{0, 1} {
		if _, err := db.Exec(
			`INSERT INTO items (feed_id, guid, title, read) VALUES (?, ?, ?, ?)`,
			aliceFeed, "guid-alice-"+string(rune('a'+i)), "alice item", read,
		); err != nil {
			t.Fatalf("insert alice item %d: %v", i, err)
		}
	}

	if _, err := db.Exec(
		`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		bob, "https://bob.example/feed", "Bob Blog",
	); err != nil {
		t.Fatalf("insert bob feed: %v", err)
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

// stubResolver returns a FeedResolver that returns the given URL or
// error verbatim. Useful for steering AddFeed test paths.
func stubResolver(target string, err error) func(context.Context, string) (string, error) {
	return func(context.Context, string) (string, error) {
		if err != nil {
			return "", err
		}
		return target, nil
	}
}

// noopFetcher is a FeedFetcher that returns nil without doing any work.
// Used in api tests so the AddFeed background goroutine doesn't issue a
// real outbound HTTP request through poller.FetchAndStoreFeed.
func noopFetcher(_ context.Context, _ *sql.DB, _ *model.Feed, _ string) error {
	return nil
}

func TestListFeeds_ScopedByUser(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/feeds", aliceTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var feeds []Feed
	if err := json.Unmarshal(rec.Body.Bytes(), &feeds); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(feeds) != 1 {
		t.Fatalf("alice should see exactly 1 feed; got %d", len(feeds))
	}
	if feeds[0].Title != "Alice Blog" {
		t.Errorf("title: got %q, want %q", feeds[0].Title, "Alice Blog")
	}
	if feeds[0].ItemCount != 2 {
		t.Errorf("item_count: got %d, want 2", feeds[0].ItemCount)
	}
	if feeds[0].UnreadCount != 1 {
		t.Errorf("unread_count: got %d, want 1", feeds[0].UnreadCount)
	}
}

func TestGetFeed_Owned(t *testing.T) {
	db, aliceTok, _, aliceFeed := feedFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, urlf("/api/v1/feeds/%d", aliceFeed), aliceTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var f Feed
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Id != aliceFeed {
		t.Errorf("id: got %d, want %d", f.Id, aliceFeed)
	}
}

func TestGetFeed_CrossUserReturns404(t *testing.T) {
	db, _, bobTok, aliceFeed := feedFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, urlf("/api/v1/feeds/%d", aliceFeed), bobTok, ""))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404 (IDOR check)", rec.Code)
	}
}

func TestAddFeed_Success(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{
		DB:           db,
		FeedResolver: stubResolver("https://new.example/feed.xml", nil),
		FeedFetcher:  noopFetcher,
	})

	body := `{"url": "https://new.example"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", aliceTok, body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%q", rec.Code, rec.Body.String())
	}
	var f Feed
	if err := json.Unmarshal(rec.Body.Bytes(), &f); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Url != "https://new.example/feed.xml" {
		t.Errorf("returned url: got %q, want resolved URL", f.Url)
	}

	// Confirm row exists in DB and is scoped to alice.
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM feeds WHERE url = ? AND user_id = (SELECT id FROM users WHERE username='alice')`,
		"https://new.example/feed.xml",
	).Scan(&count); err != nil {
		t.Fatalf("verify insert: %v", err)
	}
	if count != 1 {
		t.Errorf("feed row count: got %d, want 1", count)
	}
}

func TestAddFeed_DuplicateReturns409(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{
		// Resolver returns the URL alice already has subscribed.
		DB:           db,
		FeedResolver: stubResolver("https://alice.example/feed", nil),
		FeedFetcher:  noopFetcher,
	})

	body := `{"url": "https://alice.example/feed"}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", aliceTok, body))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status: got %d, want 409; body=%q", rec.Code, rec.Body.String())
	}
}

func TestAddFeed_InvalidScheme(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", aliceTok,
		`{"url": "ftp://example.com/feed"}`))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestAddFeed_DiscoveryFailureReturns422(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{
		DB:           db,
		FeedResolver: stubResolver("", discover.ErrNoFeedFound),
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", aliceTok,
		`{"url": "https://no-feed.example"}`))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", rec.Code)
	}
}

func TestAddFeed_FetchFailureReturns422(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{
		DB:           db,
		FeedResolver: stubResolver("", errors.New("connection refused")),
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", aliceTok,
		`{"url": "https://unreachable.example"}`))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status: got %d, want 422", rec.Code)
	}
}

func TestAddFeed_RejectsBadJSON(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", aliceTok, `{not json`))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestDeleteFeed_Owned(t *testing.T) {
	db, aliceTok, _, aliceFeed := feedFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodDelete, urlf("/api/v1/feeds/%d", aliceFeed), aliceTok, ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204; body=%q", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feeds WHERE id = ?`, aliceFeed).Scan(&count); err != nil {
		t.Fatalf("verify delete: %v", err)
	}
	if count != 0 {
		t.Errorf("feed row count after delete: got %d, want 0", count)
	}
}

func TestDeleteFeed_CrossUserReturns404(t *testing.T) {
	db, _, bobTok, aliceFeed := feedFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodDelete, urlf("/api/v1/feeds/%d", aliceFeed), bobTok, ""))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}

	// Confirm alice's feed still exists.
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM feeds WHERE id = ?`, aliceFeed).Scan(&count)
	if count != 1 {
		t.Errorf("alice's feed row count after cross-user delete attempt: got %d, want 1", count)
	}
}

func TestSyncFeeds_TriggersCallback(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	var called atomic.Bool
	var mu sync.Mutex
	cfg := Config{
		DB: db,
		SyncFeeds: func(_ context.Context) bool {
			mu.Lock()
			defer mu.Unlock()
			called.Store(true)
			return true
		},
	}
	h := New(cfg)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds/sync", aliceTok, ""))

	if rec.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", rec.Code)
	}
	if !called.Load() {
		t.Errorf("syncFeeds callback was not invoked")
	}
}

func TestSyncFeeds_NilCallbackStillReturns202(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	h := New(Config{DB: db}) // no SyncFeeds wired

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds/sync", aliceTok, ""))

	if rec.Code != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", rec.Code)
	}
}

func TestGetSyncStatus(t *testing.T) {
	db, aliceTok, _, _ := feedFixture(t)
	cases := []struct {
		name   string
		status func() bool
		want   bool
	}{
		{"nil reports false", nil, false},
		{"false", func() bool { return false }, false},
		{"true", func() bool { return true }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := New(Config{DB: db, SyncStatus: tc.status})

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/feeds/sync/status", aliceTok, ""))

			if rec.Code != http.StatusOK {
				t.Fatalf("status: got %d, want 200", rec.Code)
			}
			var resp SyncStatus
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Syncing != tc.want {
				t.Errorf("syncing: got %v, want %v", resp.Syncing, tc.want)
			}
			if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				t.Errorf("content-type: got %q, want application/json", rec.Header().Get("Content-Type"))
			}
		})
	}
}
