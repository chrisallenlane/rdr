// Package handler contains HTTP request handlers and the server setup.
package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chrisallenlane/rdr/internal/discover"
	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
)

// Server holds application dependencies and implements http.Handler.
type Server struct {
	db           *sql.DB
	templates    map[string]*template.Template
	mux          *http.ServeMux
	staticFS     fs.FS
	faviconsDir  string
	feedResolver func(context.Context, string) (string, error) // resolves a URL to a feed URL
	syncFeeds    func(ctx context.Context) bool                // triggers an async feed sync
	syncStatus   func() bool                                   // returns true if sync in progress
}

// PageData is the common data structure passed to every template render.
type PageData struct {
	User    *model.User
	Theme   string
	Content any
	Flash   string
}

// NewServer initialises the server, parses templates, registers routes,
// and returns a ready-to-use *Server.
func NewServer(db *sql.DB, staticFiles fs.FS, templateFiles fs.FS, faviconsDir string) (*Server, error) {
	s := &Server{
		db:           db,
		mux:          http.NewServeMux(),
		staticFS:     staticFiles,
		faviconsDir:  faviconsDir,
		feedResolver: discover.ResolveFeedURL,
	}

	if err := s.parseTemplates(templateFiles); err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	s.routes()

	return s, nil
}

// SetSyncFunc sets the function used to trigger a feed sync.
func (s *Server) SetSyncFunc(fn func(ctx context.Context) bool) {
	s.syncFeeds = fn
}

// SetSyncStatusFunc sets the function used to check if a sync is in progress.
func (s *Server) SetSyncStatusFunc(fn func() bool) {
	s.syncStatus = fn
}

// ServeHTTP applies security headers and delegates to the internal ServeMux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	middleware.SecurityHeaders(s.mux).ServeHTTP(w, r)
}

// parseTemplates parses each page template together with the base layout
// and stores them in a map keyed by page filename. Fragment templates are
// parsed standalone (no base layout). Partial templates from
// templates/partials/ are parsed into both page and fragment templates so
// that {{template "partial_name" .}} calls resolve in either context.
func (s *Server) parseTemplates(templateFiles fs.FS) error {
	baseBytes, err := fs.ReadFile(templateFiles, "templates/layout/base.html")
	if err != nil {
		return fmt.Errorf("reading base layout: %w", err)
	}

	// Read partial templates (shared between pages and fragments).
	var partialContents [][]byte
	if partials, err := fs.ReadDir(templateFiles, "templates/partials"); err == nil {
		for _, entry := range partials {
			if entry.IsDir() {
				continue
			}
			b, err := fs.ReadFile(templateFiles, "templates/partials/"+entry.Name())
			if err != nil {
				return fmt.Errorf("reading partial %q: %w", entry.Name(), err)
			}
			partialContents = append(partialContents, b)
		}
	}

	// parsePartials parses all partial definitions into an existing template.
	parsePartials := func(tmpl *template.Template) error {
		for _, p := range partialContents {
			if _, err := tmpl.Parse(string(p)); err != nil {
				return err
			}
		}
		return nil
	}

	pages, err := fs.ReadDir(templateFiles, "templates/pages")
	if err != nil {
		return fmt.Errorf("reading pages directory: %w", err)
	}

	s.templates = make(map[string]*template.Template, len(pages))

	for _, entry := range pages {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()

		pageBytes, err := fs.ReadFile(templateFiles, "templates/pages/"+name)
		if err != nil {
			return fmt.Errorf("reading page template %q: %w", name, err)
		}

		tmpl, err := template.New("base.html").Funcs(templateFuncMap(s.faviconsDir)).Parse(string(baseBytes))
		if err != nil {
			return fmt.Errorf("parsing base layout for %q: %w", name, err)
		}

		if _, err := tmpl.Parse(string(pageBytes)); err != nil {
			return fmt.Errorf("parsing page template %q: %w", name, err)
		}

		if err := parsePartials(tmpl); err != nil {
			return fmt.Errorf("parsing partials for page %q: %w", name, err)
		}

		s.templates[name] = tmpl
	}

	// Parse fragment templates (standalone, no base layout).
	fragments, err := fs.ReadDir(templateFiles, "templates/fragments")
	if err != nil {
		// Directory may not exist yet — that's fine, skip.
		return nil
	}
	for _, entry := range fragments {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		fragBytes, err := fs.ReadFile(templateFiles, "templates/fragments/"+name)
		if err != nil {
			return fmt.Errorf("reading fragment template %q: %w", name, err)
		}
		tmpl, err := template.New(name).Funcs(templateFuncMap(s.faviconsDir)).Parse(string(fragBytes))
		if err != nil {
			return fmt.Errorf("parsing fragment template %q: %w", name, err)
		}

		if err := parsePartials(tmpl); err != nil {
			return fmt.Errorf("parsing partials for fragment %q: %w", name, err)
		}

		s.templates["fragments/"+name] = tmpl
	}

	return nil
}

// render executes a named page template with the given PageData.
func (s *Server) render(w http.ResponseWriter, r *http.Request, tmpl string, data PageData) {
	// Read and clear flash cookie.
	if cookie, err := r.Cookie("rdr_flash"); err == nil {
		data.Flash = cookie.Value
		setCookie(w, r, "rdr_flash", "", -1, true)
	}

	// Populate user from context (set by auth middleware).
	if user := middleware.UserFromContext(r.Context()); user != nil {
		data.User = user
	}

	// Read theme from cookie, default to "solarized-light".
	data.Theme = themeFromRequest(r)

	t, ok := s.templates[tmpl]
	if !ok {
		slog.Error("template not found", "template", tmpl)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		slog.Error("template execution error", "template", tmpl, "error", err)
	}
}

// renderFragment executes a standalone fragment template (no base layout).
// Fragment templates are stored under the "fragments/" prefix.
func (s *Server) renderFragment(w http.ResponseWriter, tmpl string, data any) {
	t, ok := s.templates["fragments/"+tmpl]
	if !ok {
		slog.Error("fragment template not found", "template", tmpl)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		slog.Error("fragment execution error", "template", tmpl, "error", err)
	}
}

// renderError renders the error page with the given status code and message.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	w.WriteHeader(statusCode)
	s.render(w, r, "error.html", PageData{
		Content: message,
	})
}

// internalError logs at Error level and renders a generic 500 page. Pass
// err=nil for defensive-programming paths where no Go error is available.
// extraAttrs are forwarded to slog as key/value pairs alongside the error.
// Callers should return immediately after invoking this helper.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, msg string, err error, extraAttrs ...any) {
	if err != nil {
		slog.Error(msg, append(extraAttrs, "error", err)...)
	} else {
		slog.Error(msg, extraAttrs...)
	}
	s.renderError(w, r, http.StatusInternalServerError, "Internal Server Error")
}

// setFlash writes a short-lived flash message cookie with the application's
// standard cookie security defaults (see setCookie).
func setFlash(w http.ResponseWriter, r *http.Request, message string) {
	setCookie(w, r, "rdr_flash", message, 10, true)
}

// htmxTriggers maps event names to payloads for the HX-Trigger header.
type htmxTriggers map[string]any

// setHTMXTriggers writes a merged HX-Trigger response header as JSON.
// Non-ASCII characters are escaped to \uXXXX sequences because HTTP
// headers only support ASCII reliably.
func setHTMXTriggers(w http.ResponseWriter, triggers htmxTriggers) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(triggers)
	// Encode appends a newline; also escape non-ASCII to \uXXXX.
	s := strings.TrimSpace(buf.String())
	var ascii strings.Builder
	for _, r := range s {
		if r > 127 {
			fmt.Fprintf(&ascii, "\\u%04x", r)
		} else {
			ascii.WriteRune(r)
		}
	}
	w.Header().Set("HX-Trigger", ascii.String())
}

// flash sends a flash message via the appropriate mechanism: HX-Trigger
// header for HTMX requests, cookie for normal requests. Do not call both
// flash() and setHTMXTriggers() in the same handler — flash() calls
// setHTMXTriggers() internally and a second call would overwrite it.
func flash(w http.ResponseWriter, r *http.Request, message string) {
	if isHTMXRequest(r) {
		setHTMXTriggers(w, htmxTriggers{"showFlash": message})
	} else {
		setFlash(w, r, message)
	}
}

// handleIndex redirects to the items page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/items", http.StatusSeeOther)
}
