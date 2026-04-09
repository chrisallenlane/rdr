package handler

import (
	"fmt"
	"html/template"
	"net/url"
	"time"

	"github.com/chrisallenlane/rdr/internal/favicon"
	"github.com/chrisallenlane/rdr/internal/model"
)

// templateFuncMap returns the shared FuncMap used by all templates.
func templateFuncMap(faviconsDir string) template.FuncMap {
	return template.FuncMap{
		"timeAgo":  timeAgo,
		"add":      func(a, b int) int { return a + b },
		"subtract": func(a, b int) int { return a - b },
		"deref": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
		"feedName": func(f model.Feed) string {
			if f.Title != "" {
				return f.Title
			}
			return f.URL
		},
		"faviconSlug": func(item model.Item) string {
			return favicon.Slug(item.FeedSiteURL, item.FeedURL)
		},
		"hasFavicon": func(slug string) bool {
			if slug == "" {
				return false
			}
			return favicon.FileExists(faviconsDir, slug)
		},
		"itemFeedName": func(item model.Item) string {
			if item.FeedTitle != "" {
				return item.FeedTitle
			}
			for _, raw := range []string{item.FeedSiteURL, item.FeedURL} {
				if u, err := url.Parse(raw); err == nil && u.Host != "" {
					return u.Host
				}
			}
			return item.FeedURL
		},
	}
}

// timeAgo returns a human-friendly relative time string.
// It accepts time.Time or *time.Time; nil pointers return an empty string.
func timeAgo(v any) string {
	var t time.Time
	switch val := v.(type) {
	case time.Time:
		t = val
	case *time.Time:
		if val == nil {
			return ""
		}
		t = *val
	default:
		return ""
	}

	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("Jan 2, 2006")
	}
}
