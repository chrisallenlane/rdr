// Package favicon handles downloading, storing, and managing feed favicons.
package favicon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisallenlane/rdr/internal/httpclient"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/mmcdole/gofeed"
)

// maxSize is the maximum number of bytes to download for a favicon.
const maxSize = 1 << 20 // 1 MB

// Slug returns a filesystem-safe slug derived from the domain of the
// feed's site URL (preferred) or feed URL (fallback). The slug is the first
// 16 hex characters of SHA-256(lowercase(host)) — collision-resistant,
// safe for use as a filename, and immune to path-traversal attacks by
// construction. Two feeds at the same host produce the same slug, which
// intentionally lets multiple users share a cached favicon file.
func Slug(siteURL, feedURL string) string {
	for _, raw := range []string{siteURL, feedURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err == nil && u.Host != "" {
			return hashHost(u.Host)
		}
	}
	return ""
}

// hashHost returns the first 16 hex chars of SHA-256(lowercase(host)).
// A pure-slugification scheme (lowercase + non-alphanumeric → "-") is
// ambiguous: foo-bar.com and foo.bar.com would collide to foo-bar-com,
// letting one user observe or overwrite another user's favicon file for
// a different-but-colliding domain.
func hashHost(host string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(host)))
	return hex.EncodeToString(sum[:])[:16]
}

// Fetch attempts to discover and download a favicon for the given feed.
// It stores the file under faviconsDir/{slug}.{ext} (where slug is derived
// from the feed's domain) and updates the favicon_url column in the database.
// Errors are logged but never returned — favicon failures must not block feed
// polling.
func Fetch(ctx context.Context, db *sql.DB, feed *model.Feed, faviconsDir string, parsed *gofeed.Feed) {
	candidates := candidates(parsed, feed.URL)
	if len(candidates) == 0 {
		return
	}

	slug := Slug(feed.SiteURL, feed.URL)
	if slug == "" {
		return
	}

	// Skip if the first candidate hasn't changed and the file already exists.
	if candidates[0] == feed.FaviconURL && FileExists(faviconsDir, slug) {
		return
	}

	// Try each candidate URL until one downloads successfully.
	var data []byte
	var contentType, faviconURL string
	for _, candidate := range candidates {
		var err error
		data, contentType, err = download(ctx, candidate)
		if err == nil {
			faviconURL = candidate
			break
		}
		slog.Debug("favicon candidate failed", "feed_id", feed.ID, "url", candidate, "error", err)
	}
	if faviconURL == "" {
		return
	}

	ext := extensionFromContentType(contentType)

	// Remove any old favicon with a different extension.
	removeOld(faviconsDir, slug, ext)

	path := filepath.Join(faviconsDir, slug+ext)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		slog.Warn("failed to write favicon", "feed_id", feed.ID, "path", path, "error", err)
		return
	}

	if _, err := db.Exec("UPDATE feeds SET favicon_url = ? WHERE id = ?", faviconURL, feed.ID); err != nil {
		slog.Warn("failed to update favicon_url", "feed_id", feed.ID, "error", err)
		return
	}

	feed.FaviconURL = faviconURL
	slog.Info("saved favicon", "feed_id", feed.ID, "slug", slug, "url", faviconURL)
}

// candidates returns a prioritized list of favicon URLs to try.
func candidates(parsed *gofeed.Feed, feedURL string) []string {
	var result []string
	seen := make(map[string]bool)

	add := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			result = append(result, u)
		}
	}

	// Prefer the feed's declared image.
	if parsed.Image != nil {
		add(parsed.Image.URL)
	}

	// Fall back to /favicon.ico at the site URL, resolved feed link, or
	// original feed URL origin (deduplicated).
	for _, raw := range []string{parsed.Link, parsed.FeedLink, feedURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err == nil && u.Scheme != "" && u.Host != "" {
			add(fmt.Sprintf("%s://%s/favicon.ico", u.Scheme, u.Host))
		}
	}

	return result
}

// download fetches the given URL and returns the body bytes and
// content type. Returns an error if the response is not successful or the
// body exceeds maxSize.
func download(ctx context.Context, faviconURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", faviconURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", httpclient.UserAgent)

	resp, err := httpclient.Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxSize {
		return nil, "", fmt.Errorf("favicon too large (>%d bytes)", maxSize)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	return data, contentType, nil
}

// extensionFromContentType maps a content type to a file extension.
func extensionFromContentType(ct string) string {
	ct = strings.ToLower(ct)
	// Strip parameters (e.g., "image/png; charset=utf-8").
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}

	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		// SVGs can contain JavaScript; serve as .ico to prevent execution.
		return ".ico"
	case "image/webp":
		return ".webp"
	case "image/x-icon", "image/vnd.microsoft.icon":
		return ".ico"
	default:
		return ".ico"
	}
}

// FileExists checks whether any favicon file exists for the given slug.
func FileExists(faviconsDir string, slug string) bool {
	matches, _ := filepath.Glob(filepath.Join(faviconsDir, slug+".*"))
	return len(matches) > 0
}

// removeOld removes any existing favicon files for slug that don't
// match the given extension.
func removeOld(faviconsDir string, slug string, keepExt string) {
	matches, _ := filepath.Glob(filepath.Join(faviconsDir, slug+".*"))
	for _, m := range matches {
		if filepath.Ext(m) != keepExt {
			_ = os.Remove(m)
		}
	}
}
