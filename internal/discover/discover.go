// Package discover provides feed URL auto-discovery from website URLs.
package discover

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/chrisallenlane/rdr/internal/httpclient"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html"
)

// ErrNoFeedFound is returned when no RSS or Atom feed link is found.
var ErrNoFeedFound = errors.New("no RSS or Atom feed found")

// feedLinkTypes are the MIME types that indicate a feed link.
var feedLinkTypes = map[string]bool{
	"application/rss+xml":  true,
	"application/atom+xml": true,
}

// ResolveFeedURL attempts to resolve a URL to an RSS/Atom feed URL.
// If the URL points directly to a feed, it is returned as-is.
// If it points to an HTML page, the function looks for
// <link rel="alternate"> tags pointing to feeds and returns the first
// one found. Returns ErrNoFeedFound if no feed can be located.
func ResolveFeedURL(ctx context.Context, rawURL string) (string, error) {
	body, err := fetchBody(ctx, rawURL)
	if err != nil {
		return "", err
	}

	// Try parsing as a feed first.
	if _, err := gofeed.NewParser().Parse(bytes.NewReader(body)); err == nil {
		return rawURL, nil
	}

	// Not a feed — try HTML discovery.
	return discoverInHTML(body, rawURL)
}

// fetchBody fetches the given URL and returns the response body (limited).
func fetchBody(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", httpclient.UserAgent)

	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: status %d", rawURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, httpclient.MaxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", rawURL, err)
	}
	return body, nil
}

// discoverInHTML scans an HTML document's <head> for feed link tags.
func discoverInHTML(body []byte, baseURL string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", ErrNoFeedFound
	}

	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			return "", ErrNoFeedFound

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tagName := string(tn)

			// Stop at <body> — don't parse beyond <head>.
			if tagName == "body" {
				return "", ErrNoFeedFound
			}

			if tagName != "link" || !hasAttr {
				continue
			}

			var rel, typ, href string
			for {
				key, val, more := tokenizer.TagAttr()
				switch string(key) {
				case "rel":
					rel = strings.ToLower(string(val))
				case "type":
					typ = strings.ToLower(string(val))
				case "href":
					href = string(val)
				}
				if !more {
					break
				}
			}

			if rel != "alternate" || !feedLinkTypes[typ] || href == "" {
				continue
			}

			// Resolve relative URL.
			resolved, err := base.Parse(href)
			if err != nil {
				continue
			}

			// Only allow http/https.
			if resolved.Scheme != "http" && resolved.Scheme != "https" {
				continue
			}

			return resolved.String(), nil
		}
	}
}
