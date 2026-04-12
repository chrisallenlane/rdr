package handler

import (
	"io/fs"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/middleware"
)

// routes registers all HTTP routes on the server's mux.
func (s *Server) routes() {
	// Static files
	staticFS, _ := fs.Sub(s.staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public routes
	s.mux.HandleFunc("GET /register", s.handleRegisterForm)
	s.mux.HandleFunc("POST /register", s.handleRegister)
	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLogin)
	s.mux.HandleFunc("POST /logout", s.handleLogout)
	s.mux.HandleFunc("POST /theme", s.handleThemeChange)
	s.mux.HandleFunc("GET /favicons/{slug}", s.handleFavicon)

	// Protected routes
	protected := middleware.RequireAuth(s.db)
	s.mux.Handle("GET /{$}", protected(http.HandlerFunc(s.handleIndex)))
	s.mux.Handle("GET /items", protected(http.HandlerFunc(s.handleItems)))
	s.mux.Handle("GET /items/{id}", protected(http.HandlerFunc(s.handleItemDetail)))
	s.mux.Handle("POST /items/{id}/star", protected(http.HandlerFunc(s.handleToggleStar)))
	s.mux.Handle("GET /feeds", protected(http.HandlerFunc(s.handleFeeds)))
	s.mux.Handle("POST /feeds", protected(http.HandlerFunc(s.handleAddFeed)))
	s.mux.Handle("POST /feeds/sync", protected(http.HandlerFunc(s.handleSync)))
	s.mux.Handle("GET /feeds/sync/status", protected(http.HandlerFunc(s.handleSyncStatus)))
	s.mux.Handle("POST /feeds/{id}/delete", protected(http.HandlerFunc(s.handleDeleteFeed)))
	s.mux.Handle("GET /feeds/export", protected(http.HandlerFunc(s.handleExportOPML)))
	s.mux.Handle("POST /feeds/import", protected(http.HandlerFunc(s.handleImportOPML)))
	s.mux.Handle("GET /lists", protected(http.HandlerFunc(s.handleLists)))
	s.mux.Handle("POST /lists", protected(http.HandlerFunc(s.handleCreateList)))
	s.mux.Handle("POST /lists/{id}/delete", protected(http.HandlerFunc(s.handleDeleteList)))
	s.mux.Handle("POST /lists/{id}/rename", protected(http.HandlerFunc(s.handleRenameList)))
	s.mux.Handle("GET /lists/{id}", protected(http.HandlerFunc(s.handleListDetail)))
	s.mux.Handle("POST /lists/{id}/feeds", protected(http.HandlerFunc(s.handleAddFeedToList)))
	s.mux.Handle("POST /lists/{id}/feeds/{feedID}/delete", protected(http.HandlerFunc(s.handleRemoveFeedFromList)))
	s.mux.Handle("GET /search", protected(http.HandlerFunc(s.handleSearch)))
	s.mux.Handle("GET /settings", protected(http.HandlerFunc(s.handleSettingsForm)))
	s.mux.Handle("POST /settings", protected(http.HandlerFunc(s.handleUpdateSettings)))
	s.mux.Handle("POST /items/mark-read", protected(http.HandlerFunc(s.handleMarkRead)))
}
