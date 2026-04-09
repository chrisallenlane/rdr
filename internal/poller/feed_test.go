package poller

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/mmcdole/gofeed"
)

func TestItemGUID(t *testing.T) {
	tests := []struct {
		name string
		item *gofeed.Item
		want string
	}{
		{
			name: "GUID set",
			item: &gofeed.Item{GUID: "abc-123", Link: "http://example.com", Title: "Title"},
			want: "abc-123",
		},
		{
			name: "empty GUID, Link set",
			item: &gofeed.Item{GUID: "", Link: "http://example.com/post", Title: "Title"},
			want: "http://example.com/post",
		},
		{
			name: "empty GUID and Link, Title set",
			item: &gofeed.Item{GUID: "", Link: "", Title: "My Title"},
			want: "My Title",
		},
		{
			name: "all empty",
			item: &gofeed.Item{GUID: "", Link: "", Title: "", Content: ""},
			want: func() string {
				h := sha256.Sum256([]byte(""))
				return fmt.Sprintf("%x", h)
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := itemGUID(tt.item)
			if got != tt.want {
				t.Errorf("itemGUID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItemPublishedAt(t *testing.T) {
	pub := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
	upd := time.Date(2024, 3, 11, 8, 30, 0, 0, time.UTC)

	tests := []struct {
		name        string
		item        *gofeed.Item
		want        string
		checkRecent bool // when true, verify result parses and is within 1s of now
	}{
		{
			name: "PublishedParsed set",
			item: &gofeed.Item{PublishedParsed: &pub},
			want: model.FormatTime(pub),
		},
		{
			name: "only UpdatedParsed set",
			item: &gofeed.Item{UpdatedParsed: &upd},
			want: model.FormatTime(upd),
		},
		{
			name:        "neither set",
			item:        &gofeed.Item{},
			checkRecent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now().UTC()
			got := itemPublishedAt(tt.item)

			if tt.checkRecent {
				parsed, err := time.Parse(model.SQLiteDatetimeFmt, got)
				if err != nil {
					t.Fatalf("itemPublishedAt() returned unparseable time %q: %v", got, err)
				}
				after := time.Now().UTC()
				if parsed.Before(before.Add(-time.Second)) || parsed.After(after.Add(time.Second)) {
					t.Errorf(
						"itemPublishedAt() = %q, expected a time near now (%s..%s)",
						got, before.Format(time.RFC3339), after.Format(time.RFC3339),
					)
				}
			} else if got != tt.want {
				t.Errorf("itemPublishedAt() = %q, want %q", got, tt.want)
			}
		})
	}
}
