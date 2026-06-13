package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/mwyvr/photo"
)

// handleCreateInvite handles POST /api/v1/admin/invites.
// Admin only. Generates a new single-use invite token.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TTLHours int `json:"ttlHours"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	ttl := time.Duration(body.TTLHours) * time.Hour
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour // default 7 days
	}

	userID := userIDFromContext(r.Context())
	inv, err := s.InviteService.CreateInvite(r.Context(), userID, ttl)
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusCreated, inviteJSON(inv))
}

// handleListInvites handles GET /api/v1/admin/invites. Admin only.
func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.InviteService.FindInvites(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	out := make([]map[string]interface{}, len(invites))
	for i, inv := range invites {
		out[i] = inviteJSON(inv)
	}
	respond(w, http.StatusOK, map[string]interface{}{"invites": out})
}

// handleRevokeInvite handles DELETE /api/v1/admin/invites/{token}. Admin only.
func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if err := s.InviteService.DeleteInvite(r.Context(), token); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

// inviteJSON converts a photo.Invite to a JSON-friendly map.
func inviteJSON(inv *photo.Invite) map[string]interface{} {
	out := map[string]interface{}{
		"id":        inv.ID.String(),
		"token":     inv.Token,
		"createdBy": inv.CreatedBy.String(),
		"createdAt": inv.CreatedAt.UTC().Format(time.RFC3339),
		"expiresAt": inv.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if inv.UsedAt != nil {
		out["usedAt"] = inv.UsedAt.UTC().Format(time.RFC3339)
	}
	if inv.UsedBy != nil {
		out["usedBy"] = inv.UsedBy.String()
	}
	return out
}
