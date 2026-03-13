package server

import (
	"context"
	"net/http"
	"testing"
)

func TestInternalUserSyncUpsertAndDelete(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "sync-token"

	req := map[string]any{
		"op": "upsert",
		"user": map[string]any{
			"user_id":     "user-sync-1",
			"name":        "roy",
			"provider":    "runtime",
			"status":      "running",
			"initialized": true,
		},
	}

	unauth := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/internal/users/sync", req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("missing sync token should be unauthorized, got=%d body=%s", unauth.Code, unauth.Body.String())
	}

	upsert := doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/internal/users/sync", req, map[string]string{
		"X-Clawcolony-Internal-Token": "sync-token",
	})
	if upsert.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", upsert.Code, upsert.Body.String())
	}

	botItem, err := srv.store.GetBot(context.Background(), "user-sync-1")
	if err != nil {
		t.Fatalf("get synced bot: %v", err)
	}
	if botItem.Name != "roy" || botItem.Provider != "runtime" || botItem.Status != "running" || !botItem.Initialized {
		t.Fatalf("unexpected bot after sync: %+v", botItem)
	}

	delReq := map[string]any{
		"op": "delete",
		"user": map[string]any{
			"user_id": "user-sync-1",
		},
	}
	del := doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/internal/users/sync", delReq, map[string]string{
		"X-Clawcolony-Internal-Token": "sync-token",
	})
	if del.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", del.Code, del.Body.String())
	}

	after, err := srv.store.GetBot(context.Background(), "user-sync-1")
	if err != nil {
		t.Fatalf("get bot after delete: %v", err)
	}
	if after.Status != "deleted" || after.Initialized {
		t.Fatalf("unexpected bot after delete sync: %+v", after)
	}
}

func TestInternalUserSyncDisabledWithoutToken(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = ""
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/internal/users/sync", map[string]any{
		"op": "upsert",
		"user": map[string]any{
			"user_id": "user-sync-disabled",
			"name":    "disabled",
		},
	})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when sync token is unset, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestInternalUserSyncUpsertRequiresName(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "sync-token"

	req := map[string]any{
		"op": "upsert",
		"user": map[string]any{
			"user_id": "user-sync-noname",
			"status":  "running",
		},
	}
	w := doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/internal/users/sync", req, map[string]string{
		"X-Clawcolony-Internal-Token": "sync-token",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when user.name is empty, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestInternalUserSyncDeleteRequiresNameForUnsyncedUser(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "sync-token"

	w := doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/internal/users/sync", map[string]any{
		"op": "delete",
		"user": map[string]any{
			"user_id": "missing-user",
		},
	}, map[string]string{
		"Authorization": "Bearer sync-token",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when delete omits name for unknown user, got=%d body=%s", w.Code, w.Body.String())
	}
}
