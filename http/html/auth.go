package html

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/mwyvr/photo"
	"golang.org/x/crypto/bcrypt"
)

// realClientIP returns the client IP respecting X-Forwarded-For.
func realClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	// Already logged in — redirect to grid.
	if _, ok := s.authenticatedUserID(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", struct {
		baseData
		Error    string
		Username string
	}{
		baseData: s.newBase(r, "login"),
	})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ip := realClientIP(r)
	if !s.authLimiter.allow(ip) {
		s.render(w, r, "login.html", struct {
			baseData
			Error    string
			Username string
		}{
			baseData: s.newBase(r, "login"),
			Error:    "Too many failed attempts. Please wait a minute and try again.",
		})
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	next := r.FormValue("next")
	if next == "" {
		next = "/"
	}

	renderError := func(msg string) {
		s.authLimiter.record(ip)
		s.render(w, r, "login.html", struct {
			baseData
			Error    string
			Username string
		}{
			baseData: s.newBase(r, "login"),
			Error:    msg,
			Username: username,
		})
	}

	u, err := s.UserService.FindUserByUsername(r.Context(), username)
	if err != nil {
		renderError("Invalid username or password.")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		renderError("Invalid username or password.")
		return
	}

	token, err := s.issueJWT(u.ID)
	if err != nil {
		renderError("Could not create session.")
		return
	}

	sess := &photo.Session{
		UserID:    u.ID,
		TokenHash: tokenHash(token),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := s.SessionService.CreateSession(r.Context(), sess); err != nil {
		renderError("Could not create session.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err == nil {
		hash := tokenHash(cookie.Value)
		if sess, err := s.SessionService.FindSessionByTokenHash(r.Context(), hash); err == nil {
			s.SessionService.DeleteSession(r.Context(), sess.ID) //nolint:errcheck
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:    cookieName,
		Value:   "",
		Path:    "/",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// issueJWT creates a signed HS256 JWT — mirrors logic in http/auth.go.
func (s *Server) issueJWT(userID interface{ String() string }) (string, error) {
	// Delegate to a simple inline implementation to avoid circular imports.
	return issueJWTHS256(s.JWTSecret, userID.String(), 30*24*time.Hour)
}
