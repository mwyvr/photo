// Package html implements the server-side rendered web UI for the photo library.
// It depends only on the root photo package (domain interfaces) and stdlib.
// Authentication uses httpOnly cookies storing JWTs, reusing the JWT logic
// from the parent http package.
//
// Routes:
//
//	GET  /              photo grid (public: published only; authed: all)
//	GET  /albums        album list
//	GET  /albums/:id    album detail
//	GET  /p/:id         full image (public if published)
//	GET  /p/:id/thumb   thumbnail  (public if published)
//	GET  /login         login form
//	POST /login         process login
//	GET  /logout        clear session cookie and redirect
//	GET  /status        library status (authenticated only)
package html

import (
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

//go:embed templates/* static/*
var assets embed.FS

// Server serves the HTML web UI.
type Server struct {
	PhotoService   photo.PhotoService
	Importer       photo.Importer

	// PublishDefault controls default visibility for uploads where the user
	// hasn't explicitly chosen. RAW files are always unpublished regardless.
	PublishDefault bool
	AlbumService   photo.AlbumService
	SessionService photo.SessionService
	UserService    photo.UserService
	StatusService  photo.StatusService
	BackupService  photo.BackupService
	InviteService  photo.InviteService

	// JWTSecret must match the API server's secret.
	JWTSecret []byte

	// LibraryRoot is the base directory for reading image files.
	LibraryRoot string

	// TrustedProxy is the IP of the reverse proxy. Passed through to
	// isSecureRequest for correct Secure cookie handling.
	TrustedProxy string

	// authLimiter rate-limits failed web UI login attempts per IP.
	authLimiter *htmlRateLimiter
}

// htmlRateLimiter is a simple per-IP failed-login counter for the web UI.
// It mirrors the logic in http/middleware.go without importing that package.
type htmlRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*htmlIPAttempts
	max      int
	window   time.Duration
}

type htmlIPAttempts struct {
	count     int
	windowEnd time.Time
}

func newHTMLRateLimiter() *htmlRateLimiter {
	rl := &htmlRateLimiter{
		attempts: make(map[string]*htmlIPAttempts),
		max:      5,
		window:   time.Minute,
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.mu.Lock()
			now := time.Now()
			for ip, a := range rl.attempts {
				if now.After(a.windowEnd) {
					delete(rl.attempts, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

func (rl *htmlRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	a, ok := rl.attempts[ip]
	if !ok || time.Now().After(a.windowEnd) {
		return true
	}
	return a.count < rl.max
}

func (rl *htmlRateLimiter) record(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	a, ok := rl.attempts[ip]
	if !ok || now.After(a.windowEnd) {
		rl.attempts[ip] = &htmlIPAttempts{count: 1, windowEnd: now.Add(rl.window)}
		return
	}
	a.count++
}

// New returns a configured Server ready to register routes.
func New() (*Server, error) {
	return &Server{authLimiter: newHTMLRateLimiter()}, nil
}

// isSecureRequest returns true when the original request used HTTPS.
// Only trusts X-Forwarded-Proto when the connection is from trustedProxy
// (or trustedProxy is empty, meaning trust all).
func isSecureRequest(r *http.Request, trustedProxy string) bool {
	if r.TLS != nil {
		return true
	}
	if trustedProxy != "" {
		directIP, _, _ := net.SplitHostPort(r.RemoteAddr)
		if directIP != trustedProxy {
			return false
		}
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// RegisterRoutes registers all HTML UI routes on mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Static assets.
	mux.Handle("GET /static/", http.FileServerFS(assets))

	// Public photo/thumb serving.
	mux.HandleFunc("GET /p/{id}", s.handlePublicPhoto)
	mux.HandleFunc("GET /p/{id}/thumb", s.handlePublicThumb)
	mux.HandleFunc("GET /photo/{id}", s.handlePhotoDetail)
	mux.HandleFunc("GET /photo/{id}/file", s.handlePrivatePhotoFile)
	mux.HandleFunc("GET /photo/{id}/thumb", s.handlePrivateThumb)
	mux.HandleFunc("GET /photo/{id}/preview", s.handlePrivatePreview)

	// Auth.
	mux.HandleFunc("GET /login", s.handleLoginForm)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.HandleFunc("GET /register", s.handleRegisterForm)
	mux.HandleFunc("POST /register", s.handleRegisterPost)
	mux.HandleFunc("GET /logout", s.handleLogout)

	// UI pages — unauthenticated routes show published-only content.
	mux.HandleFunc("GET /", s.handleGrid)
	mux.HandleFunc("GET /albums", s.handleAlbumList)
	mux.HandleFunc("GET /albums/{id}", s.handleAlbumDetail)
	mux.HandleFunc("GET /me", s.requireAuth(s.handleMe))
	mux.HandleFunc("GET /upload", s.requireAuth(s.handleUploadForm))
	mux.HandleFunc("POST /upload", s.requireAuth(s.handleUploadPost))
	mux.HandleFunc("GET /admin/status", s.requireAdmin(s.handleAdminStatus))
	mux.HandleFunc("GET /backup", s.requireAdmin(s.handleBackup))
}

// --- base template data ----------------------------------------------------

// baseData is embedded in every page's template data.
type baseData struct {
	Page          string
	Authenticated bool
	IsAdmin       bool
	DisplayName   string
}

func (s *Server) newBase(r *http.Request, page string) baseData {
	userID, authed := s.authenticatedUserID(r)
	isAdmin := false
	displayName := ""
	if authed {
		if u, err := s.UserService.FindUserByID(r.Context(), userID); err == nil {
			isAdmin = u.IsAdmin
			displayName = u.DisplayName()
		}
	}
	return baseData{Page: page, Authenticated: authed, IsAdmin: isAdmin, DisplayName: displayName}
}

// render executes the named template with data, writing to w.
// All page templates define "base" as their root block, so we execute
// render parses base.html + the named page template together and executes "base".
// Parsing per-request is required because each page template redefines "content"
// and only one definition can exist in a parsed template set at a time.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data interface{}) {
	funcMap := template.FuncMap{
		"formatBytes": formatBytes,
		"deref":       func(f *float64) float64 { return *f },
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(assets,
		"templates/base.html",
		"templates/"+name,
	)
	if err != nil {
		log.Printf("html: parse %s: %v", name, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("html: render %s: %v", name, err)
	}
}

// renderNotFound renders a styled 404 page with the correct status code.
func (s *Server) renderNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	s.render(w, r, "404.html", struct {
		baseData
	}{
		baseData: s.newBase(r, ""),
	})
}

// renderServerError renders a styled 500 page with the correct status code.
// The original error is logged but not shown to the client.
func (s *Server) renderServerError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("html: server error: %v", err)
	w.WriteHeader(http.StatusInternalServerError)
	s.render(w, r, "500.html", struct {
		baseData
	}{
		baseData: s.newBase(r, ""),
	})
}

// --- cookie auth -----------------------------------------------------------

const cookieName = "photo_session"

// authenticatedUserID reads and validates the session cookie.
// Returns (userID, true) if valid, (zero, false) otherwise.
func (s *Server) authenticatedUserID(r *http.Request) (kid.ID, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return kid.ID{}, false
	}
	token := cookie.Value
	claims, err := s.verifyJWT(token)
	if err != nil {
		return kid.ID{}, false
	}
	// Verify session exists in DB.
	hash := tokenHash(token)
	sess, err := s.SessionService.FindSessionByTokenHash(r.Context(), hash)
	if err != nil || sess.IsExpired() {
		return kid.ID{}, false
	}
	userID, err := kid.FromString(claims.UserID)
	if err != nil {
		return kid.ID{}, false
	}
	return userID, true
}

// requireAuth wraps a handler, redirecting to /login if not authenticated.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authenticatedUserID(r); !ok {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// requireAdmin wraps requireAuth and additionally checks the user is an admin.
// Renders a 404 (not 403) for non-admins to avoid revealing the route exists.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := s.authenticatedUserID(r)
		u, err := s.UserService.FindUserByID(r.Context(), userID)
		if err != nil || !u.IsAdmin {
			s.renderNotFound(w, r)
			return
		}
		next(w, r)
	})
}

// --- JWT (reuses logic from http/auth.go) ----------------------------------

type jwtClaims struct {
	UserID    string `json:"uid"`
	ExpiresAt int64  `json:"exp"`
}

func (s *Server) verifyJWT(token string) (*jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}
	unsigned := parts[0] + "." + parts[1]
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature")
	}
	mac := hmac.New(sha256.New, s.JWTSecret)
	mac.Write([]byte(unsigned)) //nolint:errcheck
	if !hmac.Equal(gotSig, mac.Sum(nil)) {
		return nil, fmt.Errorf("invalid signature")
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload")
	}
	var claims jwtClaims
	if err := json.Unmarshal(rawPayload, &claims); err != nil {
		return nil, fmt.Errorf("parse claims")
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}
	return &claims, nil
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// --- helpers ---------------------------------------------------------------

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// pageURL builds a URL preserving existing query params but replacing offset.
func pageURL(r *http.Request, offset int) string {
	q := r.URL.Query()
	if offset == 0 {
		q.Del("offset")
	} else {
		q.Set("offset", fmt.Sprintf("%d", offset))
	}
	if len(q) == 0 {
		return r.URL.Path
	}
	return r.URL.Path + "?" + q.Encode()
}

func pageInfo(offset, limit, total int) string {
	from := offset + 1
	to := offset + limit
	if to > total {
		to = total
	}
	return fmt.Sprintf("%d–%d of %d", from, to, total)
}
