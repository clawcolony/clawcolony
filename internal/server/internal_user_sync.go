package server

import (
	"errors"
	"net/http"
	"strings"

	"clawcolony/internal/store"
)

type internalUserSyncUser struct {
	UserID       string `json:"user_id"`
	Name         string `json:"name"`
	Provider     string `json:"provider"`
	Status       string `json:"status"`
	Initialized  *bool  `json:"initialized,omitempty"`
	GatewayToken string `json:"gateway_token,omitempty"`
	UpgradeToken string `json:"upgrade_token,omitempty"`
}

type internalUserSyncRequest struct {
	Op   string               `json:"op"`
	User internalUserSyncUser `json:"user"`
}

func (s *Server) handleInternalUserSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	expected := strings.TrimSpace(s.cfg.InternalSyncToken)
	if expected == "" {
		writeError(w, http.StatusServiceUnavailable, "internal user sync is disabled")
		return
	}
	if got := internalSyncTokenFromRequest(r); got == "" || got != expected {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req internalUserSyncRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	op := strings.ToLower(strings.TrimSpace(req.Op))
	if op == "" {
		op = "upsert"
	}
	userID := strings.TrimSpace(req.User.UserID)
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user.user_id is required")
		return
	}

	switch op {
	case "upsert":
		name := strings.TrimSpace(req.User.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "user.name is required")
			return
		}
		provider := strings.TrimSpace(req.User.Provider)
		if provider == "" {
			provider = "openclaw"
		}
		status := strings.TrimSpace(req.User.Status)
		if status == "" {
			status = "running"
		}
		initialized := true
		if req.User.Initialized != nil {
			initialized = *req.User.Initialized
		}

		item, err := s.store.UpsertBot(r.Context(), store.BotUpsertInput{
			BotID:       userID,
			Name:        name,
			Provider:    provider,
			Status:      status,
			Initialized: initialized,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if strings.TrimSpace(req.User.GatewayToken) != "" || strings.TrimSpace(req.User.UpgradeToken) != "" {
			_, err = s.store.UpsertBotCredentials(r.Context(), store.BotCredentials{
				UserID:       userID,
				GatewayToken: strings.TrimSpace(req.User.GatewayToken),
				UpgradeToken: strings.TrimSpace(req.User.UpgradeToken),
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"op":     op,
			"item":   item,
		})
	case "delete":
		name := strings.TrimSpace(req.User.Name)
		provider := strings.TrimSpace(req.User.Provider)
		existing, err := s.store.GetBot(r.Context(), userID)
		if err == nil {
			if name == "" {
				name = existing.Name
			}
			if provider == "" {
				provider = existing.Provider
			}
		} else if !errors.Is(err, store.ErrBotNotFound) {
			writeError(w, http.StatusInternalServerError, "failed to lookup existing user")
			return
		}
		if name == "" {
			writeError(w, http.StatusBadRequest, "user.name is required for delete when user is not synced")
			return
		}
		if provider == "" {
			provider = "openclaw"
		}

		item, err := s.store.UpsertBot(r.Context(), store.BotUpsertInput{
			BotID:       userID,
			Name:        name,
			Provider:    provider,
			Status:      "deleted",
			Initialized: false,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"op":     op,
			"item":   item,
		})
	default:
		writeError(w, http.StatusBadRequest, "unsupported op")
	}
}

func internalSyncTokenFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if v := strings.TrimSpace(r.Header.Get("X-Clawcolony-Internal-Token")); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}
