package handler

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefererPath(t *testing.T) {
	tests := []struct {
		name     string
		referer  string
		fallback string
		want     string
	}{
		{
			name:     "valid referer returns path",
			referer:  "http://localhost:8080/items",
			fallback: "/feeds",
			want:     "/items",
		},
		{
			name:     "empty referer returns fallback",
			referer:  "",
			fallback: "/feeds",
			want:     "/feeds",
		},
		{
			name:     "unparseable referer returns fallback",
			referer:  "://not-a-url",
			fallback: "/feeds",
			want:     "/feeds",
		},
		{
			name:     "referer with empty path returns fallback",
			referer:  "http://localhost",
			fallback: "/feeds",
			want:     "/feeds",
		},
		{
			name:     "referer with query string returns only path",
			referer:  "http://localhost:8080/items?feed=3&unread=1",
			fallback: "/feeds",
			want:     "/items",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.referer != "" {
				r.Header.Set("Referer", tt.referer)
			}
			got := refererPath(r, tt.fallback)
			if got != tt.want {
				t.Errorf("refererPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsHTMXRequest(t *testing.T) {
	t.Run("with HX-Request header", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("HX-Request", "true")
		if !isHTMXRequest(r) {
			t.Error("isHTMXRequest() = false, want true")
		}
	})

	t.Run("without HX-Request header", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		if isHTMXRequest(r) {
			t.Error("isHTMXRequest() = true, want false")
		}
	})

	t.Run("with wrong HX-Request value", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("HX-Request", "false")
		if isHTMXRequest(r) {
			t.Error("isHTMXRequest() = false, want false")
		}
	})
}

func TestPaginate(t *testing.T) {
	tests := []struct {
		name          string
		total         int
		perPage       int
		requestedPage int
		wantPage      int
		wantTotal     int
		wantOffset    int
	}{
		{
			name:          "zero total items",
			total:         0,
			perPage:       50,
			requestedPage: 1,
			wantPage:      1,
			wantTotal:     1,
			wantOffset:    0,
		},
		{
			name:          "150 items page 1",
			total:         150,
			perPage:       50,
			requestedPage: 1,
			wantPage:      1,
			wantTotal:     3,
			wantOffset:    0,
		},
		{
			name:          "150 items page 2",
			total:         150,
			perPage:       50,
			requestedPage: 2,
			wantPage:      2,
			wantTotal:     3,
			wantOffset:    50,
		},
		{
			name:          "150 items page 3",
			total:         150,
			perPage:       50,
			requestedPage: 3,
			wantPage:      3,
			wantTotal:     3,
			wantOffset:    100,
		},
		{
			name:          "page beyond last is clamped",
			total:         150,
			perPage:       50,
			requestedPage: 5,
			wantPage:      3,
			wantTotal:     3,
			wantOffset:    100,
		},
		{
			name:          "page 0 is clamped to 1",
			total:         150,
			perPage:       50,
			requestedPage: 0,
			wantPage:      1,
			wantTotal:     3,
			wantOffset:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, totalPages, offset := paginate(tt.total, tt.perPage, tt.requestedPage)
			if page != tt.wantPage {
				t.Errorf("page = %d, want %d", page, tt.wantPage)
			}
			if totalPages != tt.wantTotal {
				t.Errorf("totalPages = %d, want %d", totalPages, tt.wantTotal)
			}
			if offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", offset, tt.wantOffset)
			}
		})
	}
}

func TestParseTime(t *testing.T) {
	tests := []struct {
		name    string
		input   sql.NullString
		wantNil bool
		wantUTC time.Time
	}{
		{
			name:    "invalid NullString",
			input:   sql.NullString{Valid: false, String: "2024-01-15 10:30:00"},
			wantNil: true,
		},
		{
			name:    "empty string",
			input:   sql.NullString{Valid: true, String: ""},
			wantNil: true,
		},
		{
			name:    "SQLite datetime format",
			input:   sql.NullString{Valid: true, String: "2024-01-15 10:30:00"},
			wantNil: false,
			wantUTC: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:    "RFC 3339 format",
			input:   sql.NullString{Valid: true, String: "2024-01-15T10:30:00Z"},
			wantNil: false,
			wantUTC: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			name:    "unparseable string",
			input:   sql.NullString{Valid: true, String: "not-a-date"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTime(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("parseTime() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseTime() = nil, want non-nil")
			}
			if !got.Equal(tt.wantUTC) {
				t.Errorf("parseTime() = %v, want %v", got, tt.wantUTC)
			}
		})
	}
}

func TestParsePositiveInt64(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{name: "positive integer", input: "42", want: 42},
		{name: "zero", input: "0", want: 0},
		{name: "negative integer", input: "-5", want: 0},
		{name: "empty string", input: "", want: 0},
		{name: "non-numeric string", input: "abc", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePositiveInt64(tt.input)
			if got != tt.want {
				t.Errorf("parsePositiveInt64(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsUniqueViolation(t *testing.T) {
	// Use a real database to generate an authentic UNIQUE constraint error.
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Insert a feed.
	_, err := s.db.Exec(
		"INSERT INTO feeds (user_id, url, title, site_url) VALUES (?, ?, ?, ?)",
		userID, "https://example.com/feed.xml", "Test Feed", "https://example.com",
	)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}

	// Insert the same feed again to trigger a UNIQUE constraint violation.
	_, uniqueErr := s.db.Exec(
		"INSERT INTO feeds (user_id, url, title, site_url) VALUES (?, ?, ?, ?)",
		userID, "https://example.com/feed.xml", "Test Feed", "https://example.com",
	)

	if !isUniqueViolation(uniqueErr) {
		t.Errorf("isUniqueViolation(UNIQUE error) = false, want true")
	}

	if isUniqueViolation(errors.New("something else went wrong")) {
		t.Errorf("isUniqueViolation(unrelated error) = true, want false")
	}
	if isUniqueViolation(nil) {
		t.Errorf("isUniqueViolation(nil) = true, want false")
	}
}

func TestPageFromQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  int
	}{
		{name: "no page param", query: "", want: 1},
		{name: "page=3", query: "page=3", want: 3},
		{name: "page=0", query: "page=0", want: 1},
		{name: "page=-1", query: "page=-1", want: 1},
		{name: "page=abc", query: "page=abc", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "/items"
			if tt.query != "" {
				target += "?" + tt.query
			}
			r := httptest.NewRequest("GET", target, nil)
			got := pageFromQuery(r)
			if got != tt.want {
				t.Errorf("pageFromQuery(%q) = %d, want %d", tt.query, got, tt.want)
			}
		})
	}
}

func FuzzParseTime(f *testing.F) {
	f.Add("2024-01-15 10:30:00")
	f.Add("2024-01-15T10:30:00Z")
	f.Add("2024-01-15T10:30:00")
	f.Add("")
	f.Add("not-a-date")
	f.Add("9999-99-99 99:99:99")

	f.Fuzz(func(t *testing.T, s string) {
		result := parseTime(sql.NullString{Valid: true, String: s})
		_ = result // just verify no panics
	})
}
