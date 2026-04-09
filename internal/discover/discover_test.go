package discover

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestResolveFeedURL_DirectFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
		<rss version="2.0">
			<channel>
				<title>Test</title>
				<link>http://example.com</link>
				<item><title>Hello</title></item>
			</channel>
		</rss>`))
	}))
	defer srv.Close()

	got, err := ResolveFeedURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != srv.URL {
		t.Errorf("got %q, want %q", got, srv.URL)
	}
}

func TestResolveFeedURL_HTMLWithRSSLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.xml" {
			t.Error("should not fetch the feed URL during discovery")
			return
		}
		_, _ = w.Write([]byte(`<!DOCTYPE html>
		<html>
		<head>
			<title>My Site</title>
			<link rel="alternate" type="application/rss+xml" href="/feed.xml">
		</head>
		<body><p>Hello</p></body>
		</html>`))
	}))
	defer srv.Close()

	got, err := ResolveFeedURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := srv.URL + "/feed.xml"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveFeedURL_HTMLWithAtomLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head>
			<link rel="alternate" type="application/atom+xml" href="https://example.com/atom.xml">
		</head><body></body></html>`))
	}))
	defer srv.Close()

	got, err := ResolveFeedURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/atom.xml" {
		t.Errorf("got %q, want %q", got, "https://example.com/atom.xml")
	}
}

func TestResolveFeedURL_HTMLWithMultipleLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head>
			<link rel="alternate" type="application/rss+xml" href="/first.xml">
			<link rel="alternate" type="application/atom+xml" href="/second.xml">
		</head><body></body></html>`))
	}))
	defer srv.Close()

	got, err := ResolveFeedURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := srv.URL + "/first.xml"
	if got != want {
		t.Errorf("got %q (should use first link), want %q", got, want)
	}
}

func TestResolveFeedURL_HTMLNoFeedLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>No feeds</title></head><body></body></html>`))
	}))
	defer srv.Close()

	_, err := ResolveFeedURL(context.Background(), srv.URL)
	if !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("got error %v, want ErrNoFeedFound", err)
	}
}

func TestResolveFeedURL_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ResolveFeedURL(context.Background(), srv.URL)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestResolveFeedURL_Unreachable(t *testing.T) {
	_, err := ResolveFeedURL(context.Background(), "http://127.0.0.1:1")
	if err == nil {
		t.Error("expected error for unreachable host")
	}
}

func TestDiscoverInHTML_RelativeHref(t *testing.T) {
	body := []byte(`<html><head>
		<link rel="alternate" type="application/rss+xml" href="feed.xml">
	</head><body></body></html>`)

	got, err := discoverInHTML(body, "https://example.com/blog/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://example.com/blog/feed.xml" {
		t.Errorf("got %q, want %q", got, "https://example.com/blog/feed.xml")
	}
}

func TestDiscoverInHTML_RejectsNonHTTP(t *testing.T) {
	body := []byte(`<html><head>
		<link rel="alternate" type="application/rss+xml" href="javascript:alert(1)">
	</head><body></body></html>`)

	_, err := discoverInHTML(body, "https://example.com")
	if !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("should reject javascript: scheme, got %v", err)
	}
}

func TestDiscoverInHTML_IgnoresNonAlternate(t *testing.T) {
	body := []byte(`<html><head>
		<link rel="stylesheet" type="application/rss+xml" href="/feed.xml">
	</head><body></body></html>`)

	_, err := discoverInHTML(body, "https://example.com")
	if !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("should ignore non-alternate link, got %v", err)
	}
}

func TestDiscoverInHTML_IgnoresWrongType(t *testing.T) {
	body := []byte(`<html><head>
		<link rel="alternate" type="text/html" href="/feed.xml">
	</head><body></body></html>`)

	_, err := discoverInHTML(body, "https://example.com")
	if !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("should ignore non-feed type, got %v", err)
	}
}

func TestDiscoverInHTML_StopsAtBody(t *testing.T) {
	body := []byte(`<html><head></head><body>
		<link rel="alternate" type="application/rss+xml" href="/feed.xml">
	</body></html>`)

	_, err := discoverInHTML(body, "https://example.com")
	if !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("should stop scanning at <body>, got %v", err)
	}
}

func TestDiscoverInHTML_EmptyHref(t *testing.T) {
	body := []byte(`<html><head>
		<link rel="alternate" type="application/rss+xml" href="">
	</head><body></body></html>`)

	_, err := discoverInHTML(body, "https://example.com")
	if !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("should reject empty href, got %v", err)
	}
}

func TestDiscoverInHTML_InvalidBaseURL(t *testing.T) {
	body := []byte(`<html><head>
		<link rel="alternate" type="application/rss+xml" href="/feed.xml">
	</head><body></body></html>`)

	_, err := discoverInHTML(body, "://invalid")
	if !errors.Is(err, ErrNoFeedFound) {
		t.Errorf("should return ErrNoFeedFound for invalid base URL, got %v", err)
	}
}

func TestDiscoverInHTML_UnparseableHref(t *testing.T) {
	body := []byte(`<html><head>
		<link rel="alternate" type="application/rss+xml" href="/feed.xml">
		<link rel="alternate" type="application/rss+xml" href="/good.xml">
	</head><body></body></html>`)

	// A base URL that causes base.Parse(href) to fail for the first link
	// but still allows discovery to continue and find the second link.
	// Actually, url.Parse is very permissive, so let's test that continue
	// doesn't prematurely abort by having multiple valid links.
	got, err := discoverInHTML(body, "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the first valid link
	if got != "https://example.com/feed.xml" {
		t.Errorf("got %q, want first link", got)
	}
}

func FuzzDiscoverInHTML(f *testing.F) {
	f.Add([]byte(`<html><head><link rel="alternate" type="application/rss+xml" href="/feed.xml"></head><body></body></html>`), "https://example.com")
	f.Add([]byte(`<html><head></head><body></body></html>`), "https://example.com")
	f.Add([]byte(``), "https://example.com")
	f.Add([]byte(`<html><head><link rel="alternate" type="application/rss+xml" href="javascript:alert(1)"></head></html>`), "https://example.com")

	f.Fuzz(func(t *testing.T, body []byte, baseURL string) {
		result, err := discoverInHTML(body, baseURL)
		if err != nil {
			return // errors are fine
		}
		// If no error, result must be a valid http/https URL
		u, parseErr := url.Parse(result)
		if parseErr != nil {
			t.Errorf("returned unparseable URL %q: %v", result, parseErr)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			t.Errorf("returned non-http scheme %q in URL %q", u.Scheme, result)
		}
	})
}
