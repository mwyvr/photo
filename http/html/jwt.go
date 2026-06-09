package html

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// issueJWTHS256 creates a minimal HS256 JWT with uid and exp claims.
func issueJWTHS256(secret []byte, userID string, ttl time.Duration) (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	payload, err := json.Marshal(struct {
		UserID    string `json:"uid"`
		ExpiresAt int64  `json:"exp"`
		IssuedAt  int64  `json:"iat"`
	}{
		UserID:    userID,
		ExpiresAt: now.Add(ttl).Unix(),
		IssuedAt:  now.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal jwt claims: %w", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	unsigned := header + "." + encodedPayload

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(unsigned)) //nolint:errcheck
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return unsigned + "." + sig, nil
}
