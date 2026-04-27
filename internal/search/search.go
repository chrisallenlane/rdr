// Package search provides FTS5 query input helpers shared between the
// HTML and JSON routes. Centralising the rejected-character list keeps
// the two code paths from drifting.
package search

import "strings"

// RejectedChars are FTS5 query characters that produce confusing
// failure modes for end users (unbalanced groups, prefix wildcards on
// tiny terms, etc). Both the HTML and JSON search routes reject queries
// containing any of these characters up front.
const RejectedChars = `"*()`

// IsRejected reports whether q contains any character in RejectedChars.
func IsRejected(q string) bool {
	return strings.ContainsAny(q, RejectedChars)
}
