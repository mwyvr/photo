// Package http implements the photo HTTP API server.
// It depends only on the root photo package (domain interfaces) and stdlib.
// Route layout:
//
//	POST   /api/v1/register
//	POST   /api/v1/login
//	DELETE /api/v1/logout
//
//	GET    /api/v1/photos
//	POST   /api/v1/photos          (multipart upload)
//	GET    /api/v1/photos/{id}
//	PATCH  /api/v1/photos/{id}
//	DELETE /api/v1/photos/{id}
//
//	POST   /api/v1/photos/{id}/tags/{name}
//	DELETE /api/v1/photos/{id}/tags/{name}
package http

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/kid"
)

// Server is the HTTP API server. It holds references to all domain services
// and the JWT signing secret. Construct via New() and call ListenAndServe().
type Server struct {
	server *http.Server
	router *http.ServeMux

	// Domain services — all are interfaces so mocks work in tests.
	UserService    photo.UserService
	SessionService photo.SessionService
	PhotoService   photo.PhotoService
	TagService     photo.TagService
	Importer       photo.Importer
	Geocoder       photo.Geocoder
	StatusService  photo.StatusService
	AlbumService   photo.AlbumService
	BackupService  photo.BackupService
	InviteService  photo.InviteService

	// JWTSecret is the HMAC-SHA256 signing key for JWT tokens.
	// Generated once at server startup and stored in the server config file.
	JWTSecret []byte

	// TokenTTL is how long a JWT is valid. Default: 30 days.
	TokenTTL time.Duration

	// PublishDefault is the server-wide default for photo visibility on upload.
	// RAW files are always unpublished regardless of this setting.
	PublishDefault bool

	// TrustedProxy is the IP of the reverse proxy. When set, X-Forwarded-For
	// is only trusted when the direct connection comes from this address.
	TrustedProxy string

	// LibraryRoot is the base directory where photo files are stored.
	LibraryRoot string

	// authLimiter rate-limits failed authentication attempts per IP.
	authLimiter *rateLimiter
}

// New returns a configured Server. Call ListenAndServe() to start accepting connections.
func New(addr string) *Server {
	s := &Server{
		TokenTTL: 30 * 24 * time.Hour,
	}
	s.router = http.NewServeMux()
	s.server = &http.Server{
		Addr:         addr,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	// Initialise with empty trustedProxy for now; reinitialised in ListenAndServe
	// once TrustedProxy field is set by the caller.
	s.authLimiter = newRateLimiter(5, time.Minute, "")
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// Public routes — login endpoints are rate-limited against brute force.
	s.router.HandleFunc("POST /api/v1/register", s.handleRegister)
	s.router.HandleFunc("POST /api/v1/login", s.authLimiter.rateLimit(s.handleLogin))

	// Authenticated routes — wrapped in requireAuth middleware.
	s.router.HandleFunc("DELETE /api/v1/logout", s.requireAuth(s.handleLogout))
	s.router.HandleFunc("GET /api/v1/status", s.requireAuth(s.handleStatus))
	s.router.HandleFunc("GET /api/v1/admin/status", s.requireAdmin(s.handleAdminStatus))
	s.router.HandleFunc("GET /api/v1/backup", s.requireAdmin(s.handleBackup))
	s.router.HandleFunc("POST /api/v1/admin/invites", s.requireAdmin(s.handleCreateInvite))
	s.router.HandleFunc("GET /api/v1/admin/invites", s.requireAdmin(s.handleListInvites))
	s.router.HandleFunc("DELETE /api/v1/admin/invites/{token}", s.requireAdmin(s.handleRevokeInvite))

	s.router.HandleFunc("GET /api/v1/photos", s.requireAuth(s.handleListPhotos))
	s.router.HandleFunc("GET /api/v1/photos/exists", s.requireAuth(s.handlePhotoExists))
	s.router.HandleFunc("POST /api/v1/photos", s.requireAuth(s.handleUploadPhoto))
	s.router.HandleFunc("GET /api/v1/photos/{id}", s.requireAuth(s.handleGetPhoto))
	s.router.HandleFunc("GET /api/v1/photos/{id}/file", s.requireAuth(s.handleServePhotoFile))
	s.router.HandleFunc("PATCH /api/v1/photos/{id}", s.requireAuth(s.handleUpdatePhoto))
	s.router.HandleFunc("DELETE /api/v1/photos/{id}", s.requireAuth(s.handleDeletePhoto))
	s.router.HandleFunc("POST /api/v1/photos/{id}/regeocode", s.requireAuth(s.handleRegeocode))

	s.router.HandleFunc("POST /api/v1/photos/{id}/tags/{name}", s.requireAuth(s.handleAttachTag))
	s.router.HandleFunc("DELETE /api/v1/photos/{id}/tags/{name}", s.requireAuth(s.handleDetachTag))

	s.registerAlbumRoutes()
}

// Router returns the underlying ServeMux so additional routes can be registered.
func (s *Server) Router() *http.ServeMux {
	return s.router
}

// WrapForTest applies the same middleware chain as ListenAndServe so that
// tests exercise security headers and access logging. TrustedProxy is empty
// (trust all X-Forwarded-For) which is fine for tests.
func WrapForTest(h http.Handler) http.Handler {
	return securityHeaders(accessLog(h, ""))
}

// ListenAndServe starts the HTTP server. It blocks until the context is done.
// TLS termination is handled by a reverse proxy (Caddy, Mox, nginx, etc.)
// running in front of photod; photod itself always speaks plain HTTP.
func (s *Server) ListenAndServe(ctx context.Context) error {
	// Build middleware chain here so TrustedProxy is available.
	s.authLimiter = newRateLimiter(5, time.Minute, s.TrustedProxy)
	s.server.Handler = securityHeaders(accessLog(s.router, s.TrustedProxy))

	go s.cleanupSessions(ctx)
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.server.Shutdown(shutCtx) //nolint:errcheck
	}()
	log.Printf("photod listening on %s", s.server.Addr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) cleanupSessions(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SessionService.DeleteExpiredSessions(ctx); err != nil {
				log.Printf("cleanup sessions: %v", err)
			}
		}
	}
}

// --- response helpers -------------------------------------------------------

func respond(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("encode response: %v", err)
		}
	}
}

func respondError(w http.ResponseWriter, err error) {
	code := photo.ErrorCode(err)
	msg := photo.ErrorMessage(err)
	status := http.StatusInternalServerError
	switch code {
	case photo.ENOTFOUND:
		status = http.StatusNotFound
	case photo.EINVALID:
		status = http.StatusBadRequest
	case photo.ECONFLICT:
		status = http.StatusConflict
	case photo.EUNAUTHORIZED:
		status = http.StatusUnauthorized
	case photo.EFORBIDDEN:
		status = http.StatusForbidden
	default:
		// Log unexpected internal errors server-side; don't expose details to client.
		log.Printf("internal error: %v", err)
	}
	respond(w, status, map[string]string{"error": msg, "code": code})
}

// parsePathID parses a kid ID from an HTTP path value.
// Writes a 400 response and returns false on invalid input.
func parsePathID(w http.ResponseWriter, r *http.Request, key string) (kid.ID, bool) {
	raw := r.PathValue(key)
	id, err := kid.FromString(raw)
	if err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid ID: %q", raw))
		return kid.ID{}, false
	}
	return id, true
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}
