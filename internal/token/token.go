// Package token implements GitHub-style Personal Access Tokens for the
// rdr JSON API.
//
// Format: rdr_pat_<64 hex chars> — the prefix is grep-able for log
// detection; the body is 32 random bytes (~256 bits of entropy).
//
// Storage: SHA-256 of the full token string. Bcrypt is overkill for
// random secrets (unrelated to password hashing's reason for slow KDFs)
// and would make every authenticated request expensive. SHA-256 indexed
// in SQLite gives O(1) validation lookups.
//
// Tokens are presented to the user exactly once at creation time. The
// database stores only the hash, so a DB compromise cannot yield active
// tokens.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Prefix is the human-readable identifier for an rdr Personal Access
// Token. Logs and secret-scanners can pattern-match this string.
const Prefix = "rdr_pat_"

// rawByteLen is the number of cryptographically random bytes that back
// the token; they are hex-encoded into the token string. 32 bytes →
// ~256 bits of entropy.
const rawByteLen = 32

// ErrInvalid is returned when a token cannot be validated. Returned for
// every failure mode — unknown, expired, malformed — so callers can
// surface a single generic 401 without enabling enumeration.
var ErrInvalid = errors.New("token: invalid or expired")

// Token is the row representation as displayed to the owner. The raw
// token string is never present here; it is only known to the caller of
// Generate at creation time.
type Token struct {
	ID         int64
	UserID     int64
	Name       string
	CreatedAt  time.Time
	LastUsedAt sql.NullTime
	ExpiresAt  sql.NullTime
}

// Generate creates a new token for userID with the given name. Optional
// expiresAt may be the zero time for a never-expiring token. Returns
// the raw token string (which the caller must show to the user
// immediately and not persist), the database id of the new row, and any
// error. The DB stores only the SHA-256 hash.
func Generate(db *sql.DB, userID int64, name string, expiresAt time.Time) (raw string, id int64, err error) {
	if name = strings.TrimSpace(name); name == "" {
		return "", 0, errors.New("token name is required")
	}

	buf := make([]byte, rawByteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", 0, fmt.Errorf("reading random bytes: %w", err)
	}
	raw = Prefix + hex.EncodeToString(buf)
	hash := hashToken(raw)

	var expiresAtVal any
	if !expiresAt.IsZero() {
		expiresAtVal = expiresAt.UTC().Format(time.RFC3339)
	}

	res, err := db.Exec(
		`INSERT INTO api_tokens (user_id, name, token_hash, expires_at) VALUES (?, ?, ?, ?)`,
		userID, name, hash, expiresAtVal,
	)
	if err != nil {
		return "", 0, fmt.Errorf("inserting api_token: %w", err)
	}
	id, err = res.LastInsertId()
	if err != nil {
		return "", 0, fmt.Errorf("LastInsertId: %w", err)
	}
	return raw, id, nil
}

// Validate looks up the given raw token string. On success it returns
// the user ID the token belongs to and the token id (so callers can
// update last_used_at out of band if desired) and updates last_used_at
// to the current time. On any failure (unknown token, expired,
// malformed input) it returns ErrInvalid.
func Validate(db *sql.DB, raw string) (userID int64, tokenID int64, err error) {
	if !strings.HasPrefix(raw, Prefix) || len(raw) != len(Prefix)+rawByteLen*2 {
		return 0, 0, ErrInvalid
	}
	hash := hashToken(raw)

	var (
		expiresAt sql.NullString
	)
	row := db.QueryRow(
		`SELECT id, user_id, expires_at FROM api_tokens WHERE token_hash = ?`,
		hash,
	)
	if err := row.Scan(&tokenID, &userID, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrInvalid
		}
		return 0, 0, fmt.Errorf("looking up token: %w", err)
	}
	if expiresAt.Valid {
		t, perr := time.Parse(time.RFC3339, expiresAt.String)
		if perr != nil {
			// Stored value is unparseable — treat as expired rather than 500.
			return 0, 0, ErrInvalid
		}
		if !t.After(time.Now()) {
			return 0, 0, ErrInvalid
		}
	}

	// Best-effort last_used_at touch; failure here does not deny auth.
	_, _ = db.Exec(
		`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), tokenID,
	)

	return userID, tokenID, nil
}

// List returns all tokens belonging to userID, most recent first.
func List(db *sql.DB, userID int64) ([]Token, error) {
	rows, err := db.Query(
		`SELECT id, user_id, name, created_at, last_used_at, expires_at
		   FROM api_tokens
		  WHERE user_id = ?
		  ORDER BY created_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Token
	for rows.Next() {
		var (
			t                     Token
			createdAt             string
			lastUsedAt, expiresAt sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &createdAt, &lastUsedAt, &expiresAt); err != nil {
			return nil, fmt.Errorf("scanning token: %w", err)
		}
		if t.CreatedAt, err = parseSQLiteTime(createdAt); err != nil {
			return nil, fmt.Errorf("parsing created_at: %w", err)
		}
		if lastUsedAt.Valid {
			lt, perr := parseSQLiteTime(lastUsedAt.String)
			if perr == nil {
				t.LastUsedAt = sql.NullTime{Time: lt, Valid: true}
			}
		}
		if expiresAt.Valid {
			et, perr := parseSQLiteTime(expiresAt.String)
			if perr == nil {
				t.ExpiresAt = sql.NullTime{Time: et, Valid: true}
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke deletes the token with id (scoped to userID for safety). It
// returns sql.ErrNoRows if no matching row exists, which the caller can
// treat as a 404.
func Revoke(db *sql.DB, userID, id int64) error {
	res, err := db.Exec(
		`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("deleting token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("RowsAffected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// hashToken returns the lowercase hex SHA-256 of raw.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// parseSQLiteTime parses the timestamp formats SQLite emits for DATETIME
// columns: the default `YYYY-MM-DD HH:MM:SS` and the RFC 3339 form we
// write explicitly.
func parseSQLiteTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}
