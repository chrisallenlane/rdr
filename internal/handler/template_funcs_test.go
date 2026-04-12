package handler

import (
	"testing"
	"time"
)

func TestFormatDate(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-25 * time.Hour)
	old := now.Add(-8 * 24 * time.Hour)

	tests := []struct {
		name     string
		input    any
		absolute bool
		want     string
	}{
		// nil pointer → empty string regardless of mode
		{"nil pointer relative", (*time.Time)(nil), false, ""},
		{"nil pointer absolute", (*time.Time)(nil), true, ""},

		// unsupported type → empty string
		{"unsupported type", "not a time", false, ""},

		// absolute mode always uses the fixed format
		{"absolute time.Time", old, true, old.Format("Jan 2, 2006")},
		{"absolute *time.Time", &old, true, old.Format("Jan 2, 2006")},
		{"absolute recent", yesterday, true, yesterday.Format("Jan 2, 2006")},

		// relative mode: recent → relative string
		{"relative just now", now, false, "just now"},
		{"relative yesterday", yesterday, false, "1d ago"},

		// relative mode: old → falls back to absolute format
		{"relative old date", old, false, old.Format("Jan 2, 2006")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDate(tt.input, tt.absolute)
			if got != tt.want {
				t.Errorf("formatDate(%v, %v) = %q, want %q",
					tt.input, tt.absolute, got, tt.want)
			}
		})
	}
}
