package handler

import (
	"net/http"
	"path/filepath"
	"regexp"
)

// validSlug matches the slugified domain format: lowercase alphanumeric and hyphens.
var validSlug = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// handleFavicon serves a feed's favicon from the data directory.
// The favicon is stored as {faviconsDir}/{slug}.{ext} where slug is a
// slugified domain name. http.ServeFile handles Content-Type detection,
// ETag, If-Modified-Since, and range requests automatically.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !validSlug.MatchString(slug) {
		http.NotFound(w, r)
		return
	}

	matches, _ := filepath.Glob(filepath.Join(s.faviconsDir, slug+".*"))
	if len(matches) == 0 {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, matches[0])
}
