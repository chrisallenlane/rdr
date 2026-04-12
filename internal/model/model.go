// Package model defines data structures and domain types.
package model

import "time"

// SQLiteDatetimeFmt is the datetime format used by SQLite DATETIME columns.
const SQLiteDatetimeFmt = "2006-01-02 15:04:05"

// FormatTime formats t as a SQLite DATETIME string in UTC.
func FormatTime(t time.Time) string {
	return t.UTC().Format(SQLiteDatetimeFmt)
}

// FormatNow returns the current time as a SQLite DATETIME string in UTC.
func FormatNow() string {
	return FormatTime(time.Now())
}

// User represents a registered user account.
type User struct {
	ID        int64
	Username  string
	Password  string // bcrypt hash
	CreatedAt time.Time
}

// Feed represents an RSS/Atom feed subscription.
type Feed struct {
	ID                  int64
	UserID              int64
	URL                 string
	Title               string
	SiteURL             string
	FaviconURL          string
	LastFetchedAt       *time.Time
	LastFetchError      string
	ConsecutiveFailures int
	CreatedAt           time.Time
	ItemCount           int // computed, not stored
	UnreadCount         int // computed, not stored
}

// List represents a user-defined grouping of feeds.
type List struct {
	ID          int64
	UserID      int64
	Name        string
	CreatedAt   time.Time
	FeedCount   int // computed, not stored
	UnreadCount int // computed, not stored
}

// Item represents a single entry within a feed.
type Item struct {
	ID          int64
	FeedID      int64
	GUID        string
	Title       string
	Content     string
	Description string
	URL         string
	PublishedAt *time.Time
	Read        bool
	ReadAt      *time.Time
	Starred     bool
	CreatedAt   time.Time
	FeedTitle   string // joined from feeds table, for display
	FeedSiteURL string // joined from feeds table, for display
	FeedURL     string // joined from feeds table, for favicon slug
}

// UserSettings holds per-user configuration.
type UserSettings struct {
	UserID              int64
	ShowDescriptions    bool
	DateDisplayAbsolute bool
}
