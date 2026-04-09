package handler

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// authPageData carries form state for login and register templates.
type authPageData struct {
	Error    string
	Username string
}

// --- Registration ---

func (s *Server) handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "register.html", PageData{
		Content: authPageData{},
	})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	passwordConfirm := r.FormValue("password_confirm")

	renderErr := func(msg string) {
		s.render(w, r, "register.html", PageData{
			Content: authPageData{Error: msg, Username: username},
		})
	}

	// Validation
	if len(username) < 3 {
		renderErr("Username must be at least 3 characters.")
		return
	}
	if len(password) < 8 {
		renderErr("Password must be at least 8 characters.")
		return
	}
	if password != passwordConfirm {
		renderErr("Passwords do not match.")
		return
	}

	hash, err := hashPassword(password)
	if err != nil {
		slog.Error("hashing password", "error", err)
		s.renderInternalError(w, r)
		return
	}

	result, err := s.db.Exec(
		"INSERT INTO users (username, password) VALUES (?, ?)",
		username, hash,
	)
	if err != nil {
		// UNIQUE constraint violation means duplicate username.
		if isUniqueViolation(err) {
			renderErr("Username is already taken.")
			return
		}
		slog.Error("inserting user", "error", err)
		s.renderInternalError(w, r)
		return
	}

	userID, err := result.LastInsertId()
	if err != nil {
		slog.Error("getting last insert id", "error", err)
		s.renderInternalError(w, r)
		return
	}

	if err := s.createSession(w, userID); err != nil {
		slog.Error("creating session", "error", err)
		s.renderInternalError(w, r)
		return
	}

	http.Redirect(w, r, "/items", http.StatusSeeOther)
}

// --- Login ---

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login.html", PageData{
		Content: authPageData{},
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	renderErr := func() {
		s.render(w, r, "login.html", PageData{
			Content: authPageData{Error: "Invalid username or password.", Username: username},
		})
	}

	var userID int64
	var hash string
	err := s.db.QueryRow(
		"SELECT id, password FROM users WHERE username = ?", username,
	).Scan(&userID, &hash)
	if err != nil {
		renderErr()
		return
	}

	if !checkPassword(hash, password) {
		renderErr()
		return
	}

	if err := s.createSession(w, userID); err != nil {
		slog.Error("creating session", "error", err)
		s.renderInternalError(w, r)
		return
	}

	http.Redirect(w, r, "/items", http.StatusSeeOther)
}

// --- Logout ---

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("rdr_session")
	if err == nil && cookie.Value != "" {
		if _, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", cookie.Value); err != nil {
			slog.Error("deleting session", "error", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "rdr_session",
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Session helper ---

const sessionDuration = 30 * 24 * time.Hour

// createSession generates a new session for the given user, stores it in the
// database, and sets the session cookie on the response.
func (s *Server) createSession(w http.ResponseWriter, userID int64) error {
	sessionID, err := generateRandomHex(32)
	if err != nil {
		return err
	}

	expiresAt := model.FormatTime(time.Now().Add(sessionDuration))
	if _, err := s.db.Exec(
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		sessionID, userID, expiresAt,
	); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "rdr_session",
		Value:    sessionID,
		MaxAge:   int(sessionDuration.Seconds()),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	return nil
}

// hashPassword returns a bcrypt hash of the given password.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// checkPassword reports whether the given password matches the bcrypt hash.
func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword(
		[]byte(hash),
		[]byte(password),
	) == nil
}

// generateRandomHex returns a hex-encoded string of n random bytes.
func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
