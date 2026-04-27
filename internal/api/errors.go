package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeProblem emits an RFC 7807 Problem Details response. typ may be
// "about:blank" for problems without a dedicated type URI.
func writeProblem(w http.ResponseWriter, status int, typ, title, detail string) {
	if typ == "" {
		typ = "about:blank"
	}
	if title == "" {
		title = http.StatusText(status)
	}
	body := Problem{
		Type:   typ,
		Title:  title,
		Status: status,
	}
	if detail != "" {
		body.Detail = &detail
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Warn("api: writing problem response", "error", err)
	}
}
