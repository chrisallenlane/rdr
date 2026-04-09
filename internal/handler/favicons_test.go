package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleFavicon(t *testing.T) {
	s := newTestServer(t)

	// Write a test favicon file using slug-based naming.
	if err := os.WriteFile(filepath.Join(s.faviconsDir, "example-com.ico"), []byte("fake-icon"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		slug   string
		status int
	}{
		{"valid slug", "example-com", http.StatusOK},
		{"missing slug", "nonexistent-com", http.StatusNotFound},
		{"empty slug", "", http.StatusNotFound},
		{"path traversal attempt", "../etc/passwd", http.StatusNotFound},
		{"numeric (old-style)", "123", http.StatusNotFound},
		{"single char slug", "a", http.StatusNotFound}, // no file on disk
		{"slug with uppercase", "EXAMPLE-COM", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/favicons/"+tt.slug, nil)
			req.SetPathValue("slug", tt.slug)
			rr := httptest.NewRecorder()
			s.handleFavicon(rr, req)

			if rr.Code != tt.status {
				t.Errorf("status = %d, want %d", rr.Code, tt.status)
			}

			if tt.status == http.StatusOK {
				if got := rr.Header().Get("Cache-Control"); got != "public, max-age=86400" {
					t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=86400")
				}
			}
		})
	}
}

func TestHandleFavicon_MultipleExtensions(t *testing.T) {
	s := newTestServer(t)

	// Write favicon files with different extensions.
	if err := os.WriteFile(filepath.Join(s.faviconsDir, "multi-com.png"), []byte("png-data"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/favicons/multi-com", nil)
	req.SetPathValue("slug", "multi-com")
	rr := httptest.NewRecorder()
	s.handleFavicon(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleFavicon_ValidSlugRegex(t *testing.T) {
	s := newTestServer(t)

	// These should be rejected by the regex validator, not even reaching glob.
	invalidSlugs := []string{
		"-leading-dash",
		"trailing-dash-",
		"has spaces",
		"has.dots",
		"has/slash",
		"has\\backslash",
		"",
	}

	for _, slug := range invalidSlugs {
		t.Run("slug_"+slug, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/favicons/test", nil)
			req.SetPathValue("slug", slug)
			rr := httptest.NewRecorder()
			s.handleFavicon(rr, req)

			if rr.Code != http.StatusNotFound {
				t.Errorf("slug %q: status = %d, want %d", slug, rr.Code, http.StatusNotFound)
			}
		})
	}
}
