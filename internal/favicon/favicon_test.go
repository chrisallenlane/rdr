package favicon

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/mmcdole/gofeed"
)

func TestSlug(t *testing.T) {
	tests := []struct {
		name    string
		siteURL string
		feedURL string
		want    string
	}{
		{
			name:    "simple domain from siteURL",
			siteURL: "https://www.schneier.com",
			feedURL: "https://www.schneier.com/feed/atom",
			want:    "www-schneier-com",
		},
		{
			name:    "siteURL preferred over feedURL",
			siteURL: "https://example.com",
			feedURL: "https://feeds.feedburner.com/Example",
			want:    "example-com",
		},
		{
			name:    "falls back to feedURL when siteURL empty",
			siteURL: "",
			feedURL: "https://feeds.feedburner.com/Example",
			want:    "feeds-feedburner-com",
		},
		{
			name:    "domain with port",
			siteURL: "https://localhost:8080",
			feedURL: "",
			want:    "localhost-8080",
		},
		{
			name:    "both empty returns empty",
			siteURL: "",
			feedURL: "",
			want:    "",
		},
		{
			name:    "invalid URL falls back to feedURL",
			siteURL: "not a url",
			feedURL: "https://example.com/feed",
			want:    "example-com",
		},
		{
			name:    "uppercase domain is lowercased",
			siteURL: "https://WWW.Example.COM",
			feedURL: "",
			want:    "www-example-com",
		},
		{
			name:    "domain with subdomain",
			siteURL: "https://blog.chris-allen-lane.com",
			feedURL: "",
			want:    "blog-chris-allen-lane-com",
		},
		{
			name:    "IP address domain",
			siteURL: "http://192.168.1.100",
			feedURL: "",
			want:    "192-168-1-100",
		},
		{
			name:    "both invalid returns empty",
			siteURL: "not-a-url",
			feedURL: "also-not-a-url",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Slug(tt.siteURL, tt.feedURL)
			if got != tt.want {
				t.Errorf("Slug(%q, %q) = %q, want %q", tt.siteURL, tt.feedURL, got, tt.want)
			}
		})
	}
}

func TestSlugifyHost(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"www.schneier.com", "www-schneier-com"},
		{"example.com", "example-com"},
		{"EXAMPLE.COM", "example-com"},
		{"localhost:8080", "localhost-8080"},
		{"192.168.1.1", "192-168-1-1"},
		{"a", "a"},
		{"a.b", "a-b"},
		{".leading.dot.", "leading-dot"},
		{"---dashes---", "dashes"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			got := slugifyHost(tt.host)
			if got != tt.want {
				t.Errorf("slugifyHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()

	// No file should not exist.
	if FileExists(dir, "example-com") {
		t.Error("expected false for nonexistent slug")
	}

	// Create a file.
	if err := os.WriteFile(filepath.Join(dir, "example-com.ico"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !FileExists(dir, "example-com") {
		t.Error("expected true for existing slug file")
	}

	// Different slug should not match.
	if FileExists(dir, "other-com") {
		t.Error("expected false for different slug")
	}
}

func TestRemoveOld(t *testing.T) {
	dir := t.TempDir()

	// Create files with different extensions.
	for _, ext := range []string{".ico", ".png", ".jpg"} {
		if err := os.WriteFile(filepath.Join(dir, "example-com"+ext), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Remove all except .png.
	removeOld(dir, "example-com", ".png")

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
	if entries[0].Name() != "example-com.png" {
		t.Errorf("expected example-com.png, got %s", entries[0].Name())
	}
}

func TestExtensionFromContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/svg+xml", ".ico"},
		{"image/webp", ".webp"},
		{"image/x-icon", ".ico"},
		{"image/vnd.microsoft.icon", ".ico"},
		{"image/png; charset=utf-8", ".png"},
		{"unknown/type", ".ico"},
		{"", ".ico"},
	}

	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			got := extensionFromContentType(tt.ct)
			if got != tt.want {
				t.Errorf("extensionFromContentType(%q) = %q, want %q", tt.ct, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// candidates
// ---------------------------------------------------------------------------

func TestCandidates(t *testing.T) {
	tests := []struct {
		name    string
		parsed  *gofeed.Feed
		feedURL string
		want    []string
	}{
		{
			name: "image URL is first candidate",
			parsed: &gofeed.Feed{
				Image:    &gofeed.Image{URL: "https://example.com/logo.png"},
				Link:     "https://example.com",
				FeedLink: "https://example.com/feed",
			},
			feedURL: "https://example.com/feed.xml",
			// image first, then /favicon.ico (deduplicated — all three URLs
			// share the same origin).
			want: []string{
				"https://example.com/logo.png",
				"https://example.com/favicon.ico",
			},
		},
		{
			name: "falls back to favicon.ico from parsed.Link",
			parsed: &gofeed.Feed{
				Link: "https://example.com/blog",
			},
			feedURL: "https://example.com/feed",
			want:    []string{"https://example.com/favicon.ico"},
		},
		{
			name: "falls back to favicon.ico from parsed.FeedLink when Link empty",
			parsed: &gofeed.Feed{
				FeedLink: "https://feeds.example.com/rss",
			},
			feedURL: "https://feeds.example.com/other",
			// parsed.FeedLink and feedURL share the same origin, so only one
			// /favicon.ico entry.
			want: []string{"https://feeds.example.com/favicon.ico"},
		},
		{
			name:   "falls back to favicon.ico from feedURL",
			parsed: &gofeed.Feed{
				// No Link, no FeedLink, no Image.
			},
			feedURL: "https://example.org/atom.xml",
			want:    []string{"https://example.org/favicon.ico"},
		},
		{
			name: "duplicate origins are deduplicated",
			parsed: &gofeed.Feed{
				Link:     "https://example.com/",
				FeedLink: "https://example.com/rss",
			},
			feedURL: "https://example.com/atom",
			// All three share https://example.com — only one /favicon.ico.
			want: []string{"https://example.com/favicon.ico"},
		},
		{
			name: "different origins produce separate candidates",
			parsed: &gofeed.Feed{
				Link:     "https://example.com",
				FeedLink: "https://feeds.feedburner.com/example",
			},
			feedURL: "https://feeds.feedburner.com/example",
			want: []string{
				"https://example.com/favicon.ico",
				"https://feeds.feedburner.com/favicon.ico",
			},
		},
		{
			name: "no image and no resolvable URLs returns empty slice",
			parsed: &gofeed.Feed{
				// All empty / unparseable.
				Link:     "not-a-url",
				FeedLink: "",
			},
			feedURL: "also-not-a-url",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidates(tt.parsed, tt.feedURL)

			if len(got) != len(tt.want) {
				t.Fatalf(
					"candidates() = %v (len %d), want %v (len %d)",
					got, len(got), tt.want, len(tt.want),
				)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("candidates()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// download
// ---------------------------------------------------------------------------

func TestDownload(t *testing.T) {
	t.Run("200 response returns body and content type", func(t *testing.T) {
		body := []byte("\x00\x00\x01\x00") // minimal ICO header bytes
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/x-icon")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(body)
			}),
		)
		defer srv.Close()

		data, ct, err := download(context.Background(), srv.URL+"/favicon.ico")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(data, body) {
			t.Errorf("body = %v, want %v", data, body)
		}
		if ct != "image/x-icon" {
			t.Errorf("content-type = %q, want %q", ct, "image/x-icon")
		}
	})

	t.Run("non-200 response returns error", func(t *testing.T) {
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}),
		)
		defer srv.Close()

		_, _, err := download(context.Background(), srv.URL+"/favicon.ico")
		if err == nil {
			t.Fatal("expected error for 404, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("error %q should mention status code 404", err.Error())
		}
	})

	t.Run("body exceeding maxSize returns error", func(t *testing.T) {
		// Serve maxSize+1 bytes.
		oversized := bytes.Repeat([]byte("x"), maxSize+1)
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(oversized)
			}),
		)
		defer srv.Close()

		_, _, err := download(context.Background(), srv.URL+"/favicon.ico")
		if err == nil {
			t.Fatal("expected error for oversized body, got nil")
		}
		if !strings.Contains(err.Error(), "too large") {
			t.Errorf("error %q should mention 'too large'", err.Error())
		}
	})

	t.Run("missing Content-Type header falls back to DetectContentType", func(t *testing.T) {
		// A minimal 1x1 PNG.  http.DetectContentType should identify it as
		// "image/png".
		png1x1 := []byte(
			"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00" +
				"\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx" +
				"\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00" +
				"\x00IEND\xaeB`\x82",
		)
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Explicitly delete Content-Type so Go's auto-sniff is
				// suppressed and the response header arrives empty.  Setting
				// the header to the empty string alone is not enough; the
				// header must be deleted after WriteHeader to prevent the
				// server-side content sniffer from re-adding it.
				w.Header()["Content-Type"] = []string{""}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(png1x1)
			}),
		)
		defer srv.Close()

		_, ct, err := download(context.Background(), srv.URL+"/favicon.ico")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(ct, "image/png") {
			t.Errorf("content-type = %q, want image/png prefix", ct)
		}
	})
}

// ---------------------------------------------------------------------------
// Fetch
// ---------------------------------------------------------------------------

func TestFetch(t *testing.T) {
	// Minimal PNG bytes so http.DetectContentType returns "image/png".
	pngData := []byte(
		"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00" +
			"\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx" +
			"\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00" +
			"\x00IEND\xaeB`\x82",
	)

	t.Run("successful fetch writes file and updates DB", func(t *testing.T) {
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(pngData)
			}),
		)
		defer srv.Close()

		db := testutil.OpenTestDB(t)
		userID := testutil.InsertUser(t, db, "testuser")

		feedURL := srv.URL + "/feed.xml"
		siteURL := srv.URL

		res, err := db.Exec(
			`INSERT INTO feeds (user_id, url, site_url, title) VALUES (?, ?, ?, ?)`,
			userID, feedURL, siteURL, "Test Feed",
		)
		if err != nil {
			t.Fatalf("insert feed: %v", err)
		}
		feedID, _ := res.LastInsertId()

		feed := &model.Feed{
			ID:      feedID,
			UserID:  userID,
			URL:     feedURL,
			SiteURL: siteURL,
		}

		parsed := &gofeed.Feed{
			Link: siteURL,
		}

		faviconsDir := t.TempDir()
		Fetch(context.Background(), db, feed, faviconsDir, parsed)

		// A file should have been written.
		slug := Slug(feed.SiteURL, feed.URL)
		if !FileExists(faviconsDir, slug) {
			t.Errorf("expected favicon file for slug %q in %s", slug, faviconsDir)
		}

		// The favicon_url column should have been updated.
		var gotURL string
		if err := db.QueryRow(
			`SELECT favicon_url FROM feeds WHERE id = ?`, feedID,
		).Scan(&gotURL); err != nil {
			t.Fatalf("query favicon_url: %v", err)
		}
		if gotURL == "" {
			t.Error("expected favicon_url to be set in DB, got empty string")
		}
	})

	t.Run("all candidates fail: no file written, no DB update", func(t *testing.T) {
		// Serve 404 for every request.
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}),
		)
		defer srv.Close()

		db := testutil.OpenTestDB(t)
		userID := testutil.InsertUser(t, db, "testuser")

		feedURL := srv.URL + "/feed.xml"
		siteURL := srv.URL

		res, err := db.Exec(
			`INSERT INTO feeds (user_id, url, site_url, title) VALUES (?, ?, ?, ?)`,
			userID, feedURL, siteURL, "Test Feed",
		)
		if err != nil {
			t.Fatalf("insert feed: %v", err)
		}
		feedID, _ := res.LastInsertId()

		feed := &model.Feed{
			ID:      feedID,
			UserID:  userID,
			URL:     feedURL,
			SiteURL: siteURL,
		}

		parsed := &gofeed.Feed{
			Link: siteURL,
		}

		faviconsDir := t.TempDir()
		Fetch(context.Background(), db, feed, faviconsDir, parsed)

		slug := Slug(feed.SiteURL, feed.URL)
		if FileExists(faviconsDir, slug) {
			t.Error("expected no favicon file when all candidates fail")
		}

		var gotURL string
		if err := db.QueryRow(
			`SELECT favicon_url FROM feeds WHERE id = ?`, feedID,
		).Scan(&gotURL); err != nil {
			t.Fatalf("query favicon_url: %v", err)
		}
		if gotURL != "" {
			t.Errorf("expected favicon_url to remain empty, got %q", gotURL)
		}
	})

	t.Run("short-circuits when candidate matches FaviconURL and file exists", func(t *testing.T) {
		// This server must never be called — Fetch should return early.
		srv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("http server was called but should have short-circuited")
				w.WriteHeader(http.StatusInternalServerError)
			}),
		)
		defer srv.Close()

		db := testutil.OpenTestDB(t)
		userID := testutil.InsertUser(t, db, "testuser")

		siteURL := srv.URL
		feedURL := srv.URL + "/feed.xml"
		faviconURL := srv.URL + "/favicon.ico"

		res, err := db.Exec(
			`INSERT INTO feeds (user_id, url, site_url, favicon_url, title)
			 VALUES (?, ?, ?, ?, ?)`,
			userID, feedURL, siteURL, faviconURL, "Test Feed",
		)
		if err != nil {
			t.Fatalf("insert feed: %v", err)
		}
		feedID, _ := res.LastInsertId()

		feed := &model.Feed{
			ID:         feedID,
			UserID:     userID,
			URL:        feedURL,
			SiteURL:    siteURL,
			FaviconURL: faviconURL,
		}

		// candidates()[0] will be siteURL + "/favicon.ico" == faviconURL.
		// We must also place a file on disk so FileExists returns true.
		faviconsDir := t.TempDir()
		slug := Slug(feed.SiteURL, feed.URL)
		existingFile := filepath.Join(faviconsDir, slug+".ico")
		if err := os.WriteFile(existingFile, []byte("cached"), 0o644); err != nil {
			t.Fatalf("write existing favicon: %v", err)
		}

		parsed := &gofeed.Feed{
			Link: siteURL,
		}

		Fetch(context.Background(), db, feed, faviconsDir, parsed)
		// If the test server handler ran, t.Error was already called above.
	})
}

func TestSlugifyHost_BoundaryZ(t *testing.T) {
	// Verify 'z' itself is kept as alphanumeric, not replaced with a hyphen.
	got := slugifyHost("az")
	if got != "az" {
		t.Errorf("slugifyHost(%q) = %q, want %q", "az", got, "az")
	}
}

func TestDownload_ExactlyMaxSize(t *testing.T) {
	// A body of exactly maxSize bytes should succeed (not trigger "too large").
	body := bytes.Repeat([]byte("x"), maxSize)
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(body)
		}),
	)
	defer srv.Close()

	_, _, err := download(context.Background(), srv.URL)
	if err != nil {
		t.Errorf("body of exactly maxSize should succeed, got error: %v", err)
	}
}

func FuzzExtensionFromContentType(f *testing.F) {
	f.Add("image/png")
	f.Add("image/jpeg")
	f.Add("image/svg+xml")
	f.Add("image/png; charset=utf-8")
	f.Add("")
	f.Add("text/html")
	f.Add("application/octet-stream")

	f.Fuzz(func(t *testing.T, ct string) {
		ext := extensionFromContentType(ct)
		// Must always be one of the known extensions
		valid := map[string]bool{
			".png": true, ".jpg": true, ".gif": true,
			".ico": true, ".webp": true,
		}
		if !valid[ext] {
			t.Errorf(
				"extensionFromContentType(%q) = %q, not a known extension",
				ct, ext,
			)
		}
	})
}

func FuzzSlug(f *testing.F) {
	f.Add("https://example.com", "https://example.com/feed")
	f.Add("", "https://feeds.feedburner.com/Example")
	f.Add("not a url", "also not a url")
	f.Add("https://localhost:8080", "")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, siteURL, feedURL string) {
		result := Slug(siteURL, feedURL)
		// Result must be empty or contain only [a-z0-9-] with no
		// leading/trailing hyphens
		if result == "" {
			return
		}
		for _, r := range result {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				t.Errorf(
					"Slug(%q, %q) = %q contains invalid character %q",
					siteURL, feedURL, result, string(r),
				)
			}
		}
		if result[0] == '-' || result[len(result)-1] == '-' {
			t.Errorf(
				"Slug(%q, %q) = %q has leading/trailing hyphen",
				siteURL, feedURL, result,
			)
		}
	})
}
