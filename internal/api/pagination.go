package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// pageSize is the fixed page size for paginated list endpoints. Matches
// the HTML UI so callers can rely on consistent counts.
const pageSize = 50

// writePagination emits the X-Total-Count and Link (RFC 5988) response
// headers for a paginated list. page is the 1-based page returned. If
// total <= pageSize, only X-Total-Count is set (no Link is needed).
func writePagination(w http.ResponseWriter, r *http.Request, total, page int) {
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	if total <= pageSize {
		return
	}

	totalPages := (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}

	var rels []string
	if page > 1 {
		rels = append(rels,
			linkRel(r, "first", 1),
			linkRel(r, "prev", page-1),
		)
	}
	if page < totalPages {
		rels = append(rels,
			linkRel(r, "next", page+1),
			linkRel(r, "last", totalPages),
		)
	}
	if len(rels) > 0 {
		w.Header().Set("Link", strings.Join(rels, ", "))
	}
}

// linkRel formats a single Link header relation entry pointing at the
// current request path with `?page=<n>` (preserving other query params).
func linkRel(r *http.Request, rel string, page int) string {
	q := cloneQuery(r.URL.Query())
	q.Set("page", strconv.Itoa(page))
	u := r.URL.Path + "?" + q.Encode()
	return fmt.Sprintf("<%s>; rel=%q", u, rel)
}

func cloneQuery(q url.Values) url.Values {
	out := make(url.Values, len(q))
	for k, vs := range q {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// effectivePage clamps a requested page number to [1, totalPages] and
// returns the row offset.
func effectivePage(total, requested int) (page, totalPages, offset int) {
	totalPages = (total + pageSize - 1) / pageSize
	if totalPages < 1 {
		totalPages = 1
	}
	page = requested
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset = (page - 1) * pageSize
	return
}
