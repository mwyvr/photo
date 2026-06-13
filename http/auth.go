package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
	"golang.org/x/crypto/bcrypt"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const (
	contextKeyUserID contextKey = iota
)

// --- JWT (hand-rolled HS256, no external dependency) -----------------------
// We use a minimal hand-rolled HS256 JWT to avoid pulling in a JWT library.
// Format: base64url(header).base64url(payload).base64url(signature)
// Header is always {"alg":"HS256","typ":"JWT"}.

type jwtClaims struct {
	UserID    string `json:"uid"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

// issueJWT creates a signed HS256 JWT for the given user ID.
func (s *Server) issueJWT(userID kid.ID) (string, error) {
	now := time.Now()
	claims := jwtClaims{
		UserID:    userID.String(),
		ExpiresAt: now.Add(s.TokenTTL).Unix(),
		IssuedAt:  now.Unix(),
	}

	header := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"alg":"HS256","typ":"JWT"}`),
	)
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal jwt claims: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)

	unsigned := header + "." + encodedPayload
	sig := s.sign(unsigned)

	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// verifyJWT validates a JWT string and returns the claims.
// Returns an error if the token is malformed, has a bad signature, or is expired.
func (s *Server) verifyJWT(token string) (*jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token")
	}

	unsigned := parts[0] + "." + parts[1]
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}
	expectedSig := s.sign(unsigned)
	if !hmac.Equal(gotSig, expectedSig) {
		return nil, fmt.Errorf("invalid signature")
	}

	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}
	var claims jwtClaims
	if err := json.Unmarshal(rawPayload, &claims); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}
	return &claims, nil
}

// sign computes HMAC-SHA256 of msg using the server's JWTSecret.
func (s *Server) sign(msg string) []byte {
	mac := hmac.New(sha256.New, s.JWTSecret)
	mac.Write([]byte(msg)) //nolint:errcheck
	return mac.Sum(nil)
}

// tokenHash returns the SHA-256 hex digest of a raw token string.
// This is what is stored in the sessions table.
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// --- context helpers --------------------------------------------------------

func setUserID(ctx context.Context, id kid.ID) context.Context {
	return context.WithValue(ctx, contextKeyUserID, id)
}

// userIDFromContext retrieves the authenticated user's ID from the context.
// Returns a nil ID if not set (should not happen on authenticated routes).
func userIDFromContext(ctx context.Context) kid.ID {
	id, _ := ctx.Value(contextKeyUserID).(kid.ID)
	return id
}

// --- middleware -------------------------------------------------------------

// requireAuth wraps a handler, validating the Bearer JWT and populating the
// user ID in the request context. Responds 401 if auth fails.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			respondError(w, photo.Errorf(photo.EUNAUTHORIZED, "authentication required"))
			return
		}

		claims, err := s.verifyJWT(token)
		if err != nil {
			respondError(w, photo.Errorf(photo.EUNAUTHORIZED, "invalid or expired token"))
			return
		}

		// Verify the session still exists in the DB (supports logout invalidation).
		hash := tokenHash(token)
		sess, err := s.SessionService.FindSessionByTokenHash(r.Context(), hash)
		if err != nil || sess.IsExpired() {
			respondError(w, photo.Errorf(photo.EUNAUTHORIZED, "session not found or expired"))
			return
		}

		userID, err := kid.FromString(claims.UserID)
		if err != nil {
			respondError(w, photo.Errorf(photo.EUNAUTHORIZED, "invalid user ID in token"))
			return
		}

		ctx := setUserID(r.Context(), userID)
		next(w, r.WithContext(ctx))
	}
}

// requireAdmin wraps requireAuth and additionally checks the user is an admin.
// Responds 403 if the authenticated user is not an admin.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		u, err := s.UserService.FindUserByID(r.Context(), userID)
		if err != nil || !u.IsAdmin {
			respondError(w, photo.Errorf(photo.EFORBIDDEN, "admin access required"))
			return
		}
		next(w, r)
	})
}

// --- auth handlers ----------------------------------------------------------

type registerRequest struct {
	Username    string `json:"username"` // email address
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Password    string `json:"password"`
	InviteToken string `json:"inviteToken"`
}

type authResponse struct {
	Token string      `json:"token"`
	User  *photo.User `json:"user"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid request body"))
		return
	}
	if req.Username == "" || req.Password == "" {
		respondError(w, photo.Errorf(photo.EINVALID, "username (email) and password are required"))
		return
	}
	if len(req.Password) < 8 {
		respondError(w, photo.Errorf(photo.EINVALID, "password must be at least 8 characters"))
		return
	}

	// Bootstrap case: the very first user does not need an invite and
	// becomes an admin automatically. All subsequent registrations require
	// a valid, unused invite token.
	count, err := s.UserService.CountUsers(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	isFirstUser := count == 0

	var inv *photo.Invite
	if !isFirstUser {
		if req.InviteToken == "" {
			respondError(w, photo.Errorf(photo.EFORBIDDEN, "an invite token is required to register"))
			return
		}
		inv, err = s.InviteService.FindInviteByToken(r.Context(), req.InviteToken)
		if err != nil {
			respondError(w, photo.Errorf(photo.EFORBIDDEN, "invalid invite token"))
			return
		}
		if !inv.IsValid() {
			respondError(w, photo.Errorf(photo.EFORBIDDEN, "invite token has expired or already been used"))
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(w, photo.Errorf(photo.EINTERNAL, "could not hash password"))
		return
	}

	u := &photo.User{
		Username:     req.Username,
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		PasswordHash: string(hash),
		IsAdmin:      isFirstUser,
	}
	if err := s.UserService.CreateUser(r.Context(), u); err != nil {
		respondError(w, err)
		return
	}

	if inv != nil {
		if err := s.InviteService.MarkInviteUsed(r.Context(), inv.Token, u.ID); err != nil {
			// User was created but invite couldn't be marked used — log and continue.
			// This is not fatal: the invite remains usable, which is the safer failure mode
			// (a stuck invite is better than a stuck registration).
			log.Printf("register: mark invite used: %v", err)
		}
	}

	token, sess, err := s.createSession(r.Context(), u.ID)
	if err != nil {
		respondError(w, err)
		return
	}
	_ = sess

	respond(w, http.StatusCreated, authResponse{Token: token, User: u})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid request body"))
		return
	}

	u, err := s.UserService.FindUserByUsername(r.Context(), req.Username)
	if err != nil {
		// Run a dummy bcrypt comparison to equalise response time regardless
		// of whether the username exists, preventing timing-based enumeration.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$dummyhashfortimingXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"), []byte(req.Password)) //nolint:errcheck
		respondError(w, photo.Errorf(photo.EUNAUTHORIZED, "invalid username or password"))
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		respondError(w, photo.Errorf(photo.EUNAUTHORIZED, "invalid username or password"))
		return
	}

	token, _, err := s.createSession(r.Context(), u.ID)
	if err != nil {
		respondError(w, err)
		return
	}

	respond(w, http.StatusOK, authResponse{Token: token, User: u})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	hash := tokenHash(token)

	sess, err := s.SessionService.FindSessionByTokenHash(r.Context(), hash)
	if err != nil {
		// Already gone — treat as success.
		respond(w, http.StatusNoContent, nil)
		return
	}
	if err := s.SessionService.DeleteSession(r.Context(), sess.ID); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

// createSession issues a JWT, hashes it, and persists the session record.
func (s *Server) createSession(ctx context.Context, userID kid.ID) (string, *photo.Session, error) {
	token, err := s.issueJWT(userID)
	if err != nil {
		return "", nil, photo.Errorf(photo.EINTERNAL, "could not issue token")
	}

	sess := &photo.Session{
		UserID:    userID,
		TokenHash: tokenHash(token),
		ExpiresAt: time.Now().Add(s.TokenTTL),
	}
	if err := s.SessionService.CreateSession(ctx, sess); err != nil {
		return "", nil, err
	}
	return token, sess, nil
}
