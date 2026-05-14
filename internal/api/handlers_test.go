package api

import (
	"testing"
	"time"
)

// TestParseSQLiteTimestamp_AcceptedFormats pins the formats accepted by
// the JSON-API timestamp parser. This guards against silent drift
// between this parser, handler/request_helpers.go's parseTime, and
// token/token.go's parseSQLiteTime — all three nominally parse the
// same set of SQLite DATETIME strings but have drifted in practice.
//
// If you change the accepted format list, update the other two parsers
// to match (or centralize them), and update all three pinning tests.
func TestParseSQLiteTimestamp_AcceptedFormats(t *testing.T) {
	want := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	cases := []struct {
		name      string
		input     string
		wantOK    bool
		wantEqual time.Time
	}{
		{
			name:      "SQLite default (space separator, no zone)",
			input:     "2024-01-15 10:30:00",
			wantOK:    true,
			wantEqual: want,
		},
		{
			name:      "RFC 3339 with Z",
			input:     "2024-01-15T10:30:00Z",
			wantOK:    true,
			wantEqual: want,
		},
		{
			name:   "T separator without zone — accepted by handler.parseTime but NOT by api.parseSQLiteTimestamp",
			input:  "2024-01-15T10:30:00",
			wantOK: false,
		},
		{
			// time.RFC3339 in Go's stdlib actually accepts a fractional-
			// second tail even though the layout string doesn't have one
			// (the stdlib parser treats ".N" as optional). All three
			// parsers therefore handle this — pinning it here so a future
			// switch away from time.RFC3339 doesn't silently regress.
			name:      "RFC 3339 with fractional seconds (stdlib lenient)",
			input:     "2024-01-15T10:30:00.123Z",
			wantOK:    true,
			wantEqual: time.Date(2024, 1, 15, 10, 30, 0, 123000000, time.UTC),
		},
		{
			name:   "empty string",
			input:  "",
			wantOK: false,
		},
		{
			name:   "garbage",
			input:  "not-a-date",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSQLiteTimestamp(tc.input)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("parseSQLiteTimestamp(%q) error = %v, want nil", tc.input, err)
				}
				if !got.Equal(tc.wantEqual) {
					t.Errorf("parseSQLiteTimestamp(%q) = %v, want %v", tc.input, got, tc.wantEqual)
				}
				return
			}
			if err == nil {
				t.Errorf("parseSQLiteTimestamp(%q) succeeded with %v, want error", tc.input, got)
			}
		})
	}
}
