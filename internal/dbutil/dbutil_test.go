package dbutil_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/chrisallenlane/rdr/internal/dbutil"
	"github.com/chrisallenlane/rdr/internal/testutil"
)

func TestBuildItemFilter_BaseClause(t *testing.T) {
	where, args := dbutil.BuildItemFilter(7, 0, 0, false, false)
	if where != "f.user_id = ?" {
		t.Errorf("base where = %q, want %q", where, "f.user_id = ?")
	}
	if !slices.Equal(args, []any{int64(7)}) {
		t.Errorf("base args = %v, want [7]", args)
	}
}

func TestBuildItemFilter_AllFiltersAppendInOrder(t *testing.T) {
	where, args := dbutil.BuildItemFilter(7, 11, 13, true, true)

	want := "f.user_id = ? AND f.id = ? AND i.read = 0 AND i.starred = 1 AND f.list_id = ? AND ? IN (SELECT id FROM lists WHERE user_id = ?)"
	if where != want {
		t.Errorf("where = %q\n want %q", where, want)
	}

	wantArgs := []any{int64(7), int64(11), int64(13), int64(13), int64(7)}
	if !slices.Equal(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}

func TestBuildItemFilter_FlagsOnly(t *testing.T) {
	where, args := dbutil.BuildItemFilter(7, 0, 0, true, true)

	want := "f.user_id = ? AND i.read = 0 AND i.starred = 1"
	if where != want {
		t.Errorf("where = %q, want %q", where, want)
	}
	if !slices.Equal(args, []any{int64(7)}) {
		t.Errorf("args = %v, want [7]", args)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	// Use a real database to generate an authentic UNIQUE constraint error.
	db := testutil.OpenTestDB(t)

	// Seed a user (feeds.user_id has a FK constraint).
	if _, err := db.Exec(
		"INSERT INTO users (username, password) VALUES (?, ?)",
		"alice", "x",
	); err != nil {
		t.Fatalf("seeding user: %v", err)
	}

	// Insert a feed.
	if _, err := db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		1, "https://example.com/feed.xml",
	); err != nil {
		t.Fatalf("inserting feed: %v", err)
	}

	// Insert the same (user_id, url) again to violate the UNIQUE constraint.
	_, uniqueErr := db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		1, "https://example.com/feed.xml",
	)
	if !dbutil.IsUniqueViolation(uniqueErr) {
		t.Errorf("IsUniqueViolation(UNIQUE error) = false, want true; got %v", uniqueErr)
	}

	if dbutil.IsUniqueViolation(errors.New("something else")) {
		t.Errorf("IsUniqueViolation(unrelated) = true, want false")
	}
	if dbutil.IsUniqueViolation(nil) {
		t.Errorf("IsUniqueViolation(nil) = true, want false")
	}
}
