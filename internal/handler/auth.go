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

	if err := s.createSession(w, r, userID); err != nil {
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
		// Equalize response time against the valid-username path by running
		// bcrypt against a decoy hash. Prevents username enumeration via a
		// timing side channel.
		_ = checkPassword(decoyPasswordHash, password)
		renderErr()
		return
	}

	if !checkPassword(hash, password) {
		renderErr()
		return
	}

	if err := s.createSession(w, r, userID); err != nil {
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

	setCookie(w, r, "rdr_session", "", -1, true)

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// --- Session helper ---

const sessionDuration = 30 * 24 * time.Hour

// createSession generates a new session for the given user, stores it in the
// database, and sets the session cookie on the response. The Secure flag is
// set when the request arrived over TLS (directly or via a reverse proxy
// that sets X-Forwarded-Proto).
func (s *Server) createSession(w http.ResponseWriter, r *http.Request, userID int64) error {
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

	setCookie(w, r, "rdr_session", sessionID, int(sessionDuration.Seconds()), true)

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

// decoyPasswordHash is a bcrypt hash used to equalize login response time
// when the submitted username does not exist. Computed once at package
// initialization so the per-login cost matches the valid-username path.
var decoyPasswordHash = func() string {
	h, err := hashPassword("decoy-password-no-user-will-ever-match-this")
	if err != nil {
		panic("bcrypt failed on decoy hash: " + err.Error())
	}
	return h
}()

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
