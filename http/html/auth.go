package html

import (
	"log"
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
		bcrypt.CompareHashAndPassword([]byte("$2a$10$dummyhashfortimingXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"), []byte(password)) //nolint:errcheck
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
		Secure:   isSecureRequest(r, s.TrustedProxy),
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// registerFormData holds the data passed to register.html.
type registerFormData struct {
	baseData
	Error       string
	Username    string
	FirstName   string
	LastName    string
	InviteToken string
	IsFirstUser bool
}

// handleRegisterForm renders the registration form.
// The invite token, if present, comes from the ?token= query parameter
// (the link an admin shares with the person being invited).
func (s *Server) handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticatedUserID(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	isFirstUser := false
	if n, err := s.UserService.CountUsers(r.Context()); err == nil {
		isFirstUser = n == 0
	}

	s.render(w, r, "register.html", registerFormData{
		baseData:    s.newBase(r, "register"),
		InviteToken: r.URL.Query().Get("token"),
		IsFirstUser: isFirstUser,
	})
}

// handleRegisterPost processes the registration form.
// Mirrors the validation logic in http/auth.go's handleRegister: the first
// user on the server becomes an admin automatically and needs no invite;
// every subsequent registration requires a valid, unused invite token.
func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	firstName := strings.TrimSpace(r.FormValue("firstName"))
	lastName := strings.TrimSpace(r.FormValue("lastName"))
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")
	inviteToken := r.FormValue("inviteToken")

	isFirstUser := false
	if n, err := s.UserService.CountUsers(r.Context()); err == nil {
		isFirstUser = n == 0
	}

	renderError := func(msg string) {
		s.render(w, r, "register.html", registerFormData{
			baseData:    s.newBase(r, "register"),
			Error:       msg,
			Username:    username,
			FirstName:   firstName,
			LastName:    lastName,
			InviteToken: inviteToken,
			IsFirstUser: isFirstUser,
		})
	}

	if username == "" || password == "" {
		renderError("Email and password are required.")
		return
	}
	if password != confirm {
		renderError("Passwords do not match.")
		return
	}
	if len(password) < 8 {
		renderError("Password must be at least 8 characters.")
		return
	}

	var inv *photo.Invite
	if !isFirstUser {
		if inviteToken == "" {
			renderError("An invite is required to register. Ask an administrator for an invite link.")
			return
		}
		var err error
		inv, err = s.InviteService.FindInviteByToken(r.Context(), inviteToken)
		if err != nil || !inv.IsValid() {
			renderError("This invite link is invalid or has expired. Ask for a new one.")
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		renderError("Could not create account. Please try again.")
		return
	}

	u := &photo.User{
		Username:     username,
		FirstName:    firstName,
		LastName:     lastName,
		PasswordHash: string(hash),
		IsAdmin:      isFirstUser,
	}
	if err := s.UserService.CreateUser(r.Context(), u); err != nil {
		if photo.ErrorCode(err) == photo.EINVALID {
			renderError("Please enter a valid email address.")
		} else if photo.ErrorCode(err) == photo.ECONFLICT {
			renderError("An account with this email already exists.")
		} else {
			renderError("Could not create account. Please try again.")
		}
		return
	}

	if inv != nil {
		if err := s.InviteService.MarkInviteUsed(r.Context(), inv.Token, u.ID); err != nil {
			log.Printf("register: mark invite used: %v", err)
		}
	}

	// Log the new user in immediately.
	token, err := s.issueJWT(u.ID)
	if err != nil {
		renderError("Account created, but could not sign you in. Please sign in manually.")
		return
	}
	sess := &photo.Session{
		UserID:    u.ID,
		TokenHash: tokenHash(token),
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	}
	if err := s.SessionService.CreateSession(r.Context(), sess); err != nil {
		renderError("Account created, but could not sign you in. Please sign in manually.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r, s.TrustedProxy),
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
