package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/chrisallenlane/rdr/internal/token"
)

// searchFixture seeds two users with searchable items. Alice has 3
// items mentioning "golang"; bob has 1 item mentioning "golang".
func searchFixture(t *testing.T) (db *sql.DB, aliceTok, bobTok string) {
	t.Helper()
	db = testutil.OpenTestDB(t)

	alice := testutil.InsertUser(t, db, "alice")
	bob := testutil.InsertUser(t, db, "bob")

	res, err := db.Exec(`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		alice, "https://alice.example/feed", "Alice Blog")
	if err != nil {
		t.Fatalf("insert alice feed: %v", err)
	}
	aliceFeed, _ := res.LastInsertId()

	for i, title := range []string{
		"Learning golang for beginners",
		"Advanced golang patterns",
		"golang concurrency tips",
	} {
		if _, err := db.Exec(
			`INSERT INTO items (feed_id, guid, title, content) VALUES (?, ?, ?, ?)`,
			aliceFeed, "guid-alice-"+string(rune('a'+i)), title,
			"All about golang and the Go programming language.",
		); err != nil {
			t.Fatalf("insert alice item %d: %v", i, err)
		}
	}

	res, err = db.Exec(`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		bob, "https://bob.example/feed", "Bob Blog")
	if err != nil {
		t.Fatalf("insert bob feed: %v", err)
	}
	bobFeed, _ := res.LastInsertId()
	if _, err := db.Exec(
		`INSERT INTO items (feed_id, guid, title, content) VALUES (?, ?, ?, ?)`,
		bobFeed, "guid-bob", "Bob's golang notes", "Just some private golang notes.",
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

func TestSearch_ScopedByUser(t *testing.T) {
	db, aliceTok, _ := searchFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/search?q=golang", aliceTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}
	var results []SearchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &results); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("alice should see 3 results; got %d", len(results))
	}
	for _, sr := range results {
		if sr.FeedTitle != "Alice Blog" {
			t.Errorf("foreign result leaked: feed_title=%q", sr.FeedTitle)
		}
	}

	if got := rec.Header().Get("X-Total-Count"); got != "3" {
		t.Errorf("X-Total-Count: got %q, want 3", got)
	}
}

func TestSearch_OtherUserOnlySeesOwn(t *testing.T) {
	db, _, bobTok := searchFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/search?q=golang", bobTok, ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var results []SearchResult
	_ = json.Unmarshal(rec.Body.Bytes(), &results)
	if len(results) != 1 || results[0].FeedTitle != "Bob Blog" {
		t.Errorf("bob should see only his 1 result; got %+v", results)
	}
}

func TestSearch_RejectsForbiddenChars(t *testing.T) {
	db, aliceTok, _ := searchFixture(t)
	h := New(Config{DB: db})

	for _, q := range []string{`"quoted"`, `gol*`, `(group)`} {
		t.Run(q, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/search?q="+q, aliceTok, ""))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("q=%q status: got %d, want 400", q, rec.Code)
			}
		})
	}
}

func TestSearch_RejectsEmptyQuery(t *testing.T) {
	db, aliceTok, _ := searchFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/search?q=", aliceTok, ""))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
}

func TestSearch_StructuredSnippets(t *testing.T) {
	db, aliceTok, _ := searchFixture(t)
	h := New(Config{DB: db})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/v1/search?q=golang", aliceTok, ""))

	var results []SearchResult
	_ = json.Unmarshal(rec.Body.Bytes(), &results)
	if len(results) == 0 {
		t.Fatal("no results")
	}
	// At least one segment should be highlighted.
	var sawHighlight bool
	for _, seg := range append(results[0].TitleSnippet, results[0].ContentSnippet...) {
		if seg.Highlight {
			sawHighlight = true
		}
		// No leftover sentinel markers should remain in any segment.
		if seg.Text == snippetMarkerOpen || seg.Text == snippetMarkerClose {
			t.Errorf("segment text contains unparsed marker: %q", seg.Text)
		}
	}
	if !sawHighlight {
		t.Errorf("no highlight=true segment in any snippet; result: %+v", results[0])
	}
}

func TestParseSnippet(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []SnippetSegment
	}{
		{
			name: "no markers",
			in:   "plain text",
			want: []SnippetSegment{{Text: "plain text", Highlight: false}},
		},
		{
			name: "single highlight",
			in:   "before [[HIGHLIGHT]]hit[[/HIGHLIGHT]] after",
			want: []SnippetSegment{
				{Text: "before ", Highlight: false},
				{Text: "hit", Highlight: true},
				{Text: " after", Highlight: false},
			},
		},
		{
			name: "leading highlight",
			in:   "[[HIGHLIGHT]]hit[[/HIGHLIGHT]] tail",
			want: []SnippetSegment{
				{Text: "hit", Highlight: true},
				{Text: " tail", Highlight: false},
			},
		},
		{
			name: "two highlights merged when adjacent",
			in:   "[[HIGHLIGHT]]a[[/HIGHLIGHT]][[HIGHLIGHT]]b[[/HIGHLIGHT]]",
			want: []SnippetSegment{
				{Text: "ab", Highlight: true},
			},
		},
		{
			name: "empty string yields empty slice",
			in:   "",
			want: []SnippetSegment{},
		},
		{
			name: "unmatched open marker treated as plain text",
			in:   "before [[HIGHLIGHT]]dangling",
			want: []SnippetSegment{
				{Text: "before dangling", Highlight: false},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSnippet(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseSnippet(%q):\n  got  %#v\n  want %#v", tc.in, got, tc.want)
			}
		})
	}
}
