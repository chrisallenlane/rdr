// Package handler contains HTTP request handlers and the server setup.
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

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
// and stores them in a map keyed by page filename.
func (s *Server) parseTemplates(templateFiles fs.FS) error {
	baseBytes, err := fs.ReadFile(templateFiles, "templates/layout/base.html")
	if err != nil {
		return fmt.Errorf("reading base layout: %w", err)
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
		s.templates["fragments/"+name] = tmpl
	}

	return nil
}

// render executes a named page template with the given PageData.
func (s *Server) render(w http.ResponseWriter, r *http.Request, tmpl string, data PageData) {
	// Read and clear flash cookie.
	if cookie, err := r.Cookie("rdr_flash"); err == nil {
		data.Flash = cookie.Value
		http.SetCookie(w, &http.Cookie{
			Name:   "rdr_flash",
			Value:  "",
			MaxAge: -1,
			Path:   "/",
		})
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

// renderInternalError renders a generic 500 error page.
func (s *Server) renderInternalError(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusInternalServerError, "Internal Server Error")
}

// setFlash sets a flash message cookie.
func setFlash(w http.ResponseWriter, message string) {
	http.SetCookie(w, &http.Cookie{
		Name:   "rdr_flash",
		Value:  message,
		MaxAge: 10,
		Path:   "/",
	})
}

// htmxTriggers maps event names to payloads for the HX-Trigger header.
type htmxTriggers map[string]any

// setHTMXTriggers writes a merged HX-Trigger response header as JSON.
func setHTMXTriggers(w http.ResponseWriter, triggers htmxTriggers) {
	b, _ := json.Marshal(triggers)
	w.Header().Set("HX-Trigger", string(b))
}

// flash sends a flash message via the appropriate mechanism: HX-Trigger
// header for HTMX requests, cookie for normal requests. Do not call both
// flash() and setHTMXTriggers() in the same handler — flash() calls
// setHTMXTriggers() internally and a second call would overwrite it.
func flash(w http.ResponseWriter, r *http.Request, message string) {
	if isHTMXRequest(r) {
		setHTMXTriggers(w, htmxTriggers{"showFlash": message})
	} else {
		setFlash(w, message)
	}
}

// handleIndex redirects to the items page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/items", http.StatusSeeOther)
}
