package token

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
)

func TestGenerate_FormatAndUniqueness(t *testing.T) {
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, "alice")

	seen := make(map[string]bool)
	for i := 0; i < 8; i++ {
		raw, _, err := Generate(db, uid, "key", time.Time{})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if !strings.HasPrefix(raw, Prefix) {
			t.Errorf("missing prefix: %q", raw)
		}
		if got := len(raw); got != len(Prefix)+rawByteLen*2 {
			t.Errorf("length: got %d, want %d", got, len(Prefix)+rawByteLen*2)
		}
		if seen[raw] {
			t.Errorf("collision on iteration %d: %q", i, raw)
		}
		seen[raw] = true
	}
}

func TestGenerate_RequiresName(t *testing.T) {
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, "alice")

	if _, _, err := Generate(db, uid, "", time.Time{}); err == nil {
		t.Error("expected error for empty name; got nil")
	}
	if _, _, err := Generate(db, uid, "   ", time.Time{}); err == nil {
		t.Error("expected error for whitespace name; got nil")
	}
}

func TestValidate_ValidToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, "alice")
	raw, id, err := Generate(db, uid, "ci-token", time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	gotUID, gotTID, err := Validate(db, raw)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if gotUID != uid {
		t.Errorf("user id: got %d, want %d", gotUID, uid)
	}
	if gotTID != id {
		t.Errorf("token id: got %d, want %d", gotTID, id)
	}

	// last_used_at should be populated after a successful validate.
	tokens, err := List(db, uid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !tokens[0].LastUsedAt.Valid {
		t.Error("last_used_at not updated by Validate")
	}
}

func TestValidate_UnknownToken(t *testing.T) {
	db := testutil.OpenTestDB(t)

	_, _, err := Validate(db, Prefix+strings.Repeat("a", rawByteLen*2))
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestValidate_MalformedToken(t *testing.T) {
	db := testutil.OpenTestDB(t)

	cases := []string{
		"",
		"not-a-token",
		"rdr_pat_short",
		"WRONGPREFIX_" + strings.Repeat("a", rawByteLen*2),
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, _, err := Validate(db, c)
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("expected ErrInvalid for %q, got %v", c, err)
			}
		})
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, "alice")
	raw, _, err := Generate(db, uid, "expired", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	_, _, err = Validate(db, raw)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid for expired token, got %v", err)
	}
}

func TestList_ScopedByUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	alice := testutil.InsertUser(t, db, "alice")
	bob := testutil.InsertUser(t, db, "bob")

	if _, _, err := Generate(db, alice, "alice-key-1", time.Time{}); err != nil {
		t.Fatalf("Generate alice-1: %v", err)
	}
	if _, _, err := Generate(db, alice, "alice-key-2", time.Time{}); err != nil {
		t.Fatalf("Generate alice-2: %v", err)
	}
	if _, _, err := Generate(db, bob, "bob-key", time.Time{}); err != nil {
		t.Fatalf("Generate bob: %v", err)
	}

	tokens, err := List(db, alice)
	if err != nil {
		t.Fatalf("List alice: %v", err)
	}
	if len(tokens) != 2 {
		t.Errorf("alice tokens: got %d, want 2", len(tokens))
	}
	for _, tok := range tokens {
		if tok.UserID != alice {
			t.Errorf("foreign token leaked: tok.UserID=%d, want %d", tok.UserID, alice)
		}
	}
}

func TestRevoke_DeletesAndDenies(t *testing.T) {
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, "alice")
	raw, id, err := Generate(db, uid, "doomed", time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if err := Revoke(db, uid, id); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, _, err := Validate(db, raw); !errors.Is(err, ErrInvalid) {
		t.Errorf("expected ErrInvalid after revoke, got %v", err)
	}
}

func TestRevoke_RejectsCrossUser(t *testing.T) {
	db := testutil.OpenTestDB(t)
	alice := testutil.InsertUser(t, db, "alice")
	bob := testutil.InsertUser(t, db, "bob")
	_, id, err := Generate(db, alice, "alice-key", time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Bob tries to revoke Alice's token.
	if err := Revoke(db, bob, id); err == nil {
		t.Error("expected error revoking another user's token; got nil")
	}
}

func TestHash_Deterministic(t *testing.T) {
	a := hashToken("rdr_pat_test")
	b := hashToken("rdr_pat_test")
	if a != b {
		t.Errorf("hashToken not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("sha256 hex length: got %d, want 64", len(a))
	}
}

// TestParseSQLiteTime_AcceptedFormats pins the formats accepted by the
// token package's timestamp parser. This guards against silent drift
// between this parser, api.parseSQLiteTimestamp, and handler.parseTime
// — all three nominally parse the same set of SQLite DATETIME strings
// but have drifted in practice.
//
// If you change the accepted format list, update the other two parsers
// to match (or centralize them), and update all three pinning tests.
func TestParseSQLiteTime_AcceptedFormats(t *testing.T) {
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
			name:   "T separator without zone — accepted by handler.parseTime but NOT by token.parseSQLiteTime",
			input:  "2024-01-15T10:30:00",
			wantOK: false,
		},
		{
			// time.RFC3339 in Go's stdlib is lenient about trailing
			// fractional seconds; pinning to flag a future regression
			// if any parser switches off RFC3339.
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
			got, err := parseSQLiteTime(tc.input)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("parseSQLiteTime(%q) error = %v, want nil", tc.input, err)
				}
				if !got.Equal(tc.wantEqual) {
					t.Errorf("parseSQLiteTime(%q) = %v, want %v", tc.input, got, tc.wantEqual)
				}
				return
			}
			if err == nil {
				t.Errorf("parseSQLiteTime(%q) succeeded with %v, want error", tc.input, got)
			}
		})
	}
}
