package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"clawcolony/internal/config"
	"clawcolony/internal/store"
)

func TestRuntimeRemovedEndpointsReturn404(t *testing.T) {
	srv := newTestServer()
	h := srv.roleAccessMiddleware(srv.mux)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/prompts/templates"},
		{http.MethodPut, "/v1/prompts/templates/upsert"},
		{http.MethodPost, "/v1/prompts/templates/apply"},
		{http.MethodGet, "/v1/bots/logs"},
		{http.MethodGet, "/v1/bots/logs/all"},
		{http.MethodGet, "/v1/bots/rule-status"},
		{http.MethodPost, "/v1/bots/dev/link"},
		{http.MethodGet, "/v1/bots/dev/health"},
		{http.MethodGet, "/v1/bots/openclaw/status"},
		{http.MethodGet, "/v1/system/openclaw-dashboard-config"},
		{http.MethodPost, "/v1/chat/send"},
		{http.MethodGet, "/v1/chat/history"},
		{http.MethodGet, "/v1/chat/stream"},
		{http.MethodGet, "/v1/chat/state"},
	}
	for _, tc := range cases {
		w := doJSONRequest(t, h, tc.method, tc.path, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s expected 404 got=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestRuntimeRemovedPrefixEndpointsReturn404(t *testing.T) {
	srv := newTestServer()
	h := srv.roleAccessMiddleware(srv.mux)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/bots/dev/u1/p/3000/"},
		{http.MethodHead, "/v1/bots/dev/u1/p/3000/"},
		{http.MethodOptions, "/v1/bots/dev/u1/p/3000/"},
		{http.MethodGet, "/v1/bots/openclaw/u1/"},
	}
	for _, tc := range cases {
		w := doJSONRequest(t, h, tc.method, tc.path, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s expected 404 got=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestRuntimeIdentityEndpointsStillAvailable(t *testing.T) {
	srv := newTestServer()
	h := srv.roleAccessMiddleware(srv.mux)

	list := doJSONRequest(t, h, http.MethodGet, "/v1/bots?include_inactive=1", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("GET /v1/bots expected 200 got=%d body=%s", list.Code, list.Body.String())
	}

	nick := doJSONRequest(t, h, http.MethodPost, "/v1/bots/nickname/upsert", map[string]any{
		"user_id":  "u-test",
		"nickname": "Nick",
	})
	if nick.Code != http.StatusNotFound {
		// no user yet: endpoint is alive and validates store state, should not be removed.
		t.Fatalf("POST /v1/bots/nickname/upsert expected 404(bot not found) got=%d body=%s", nick.Code, nick.Body.String())
	}
}

func TestRuntimeBotsListUsesDBStatusFilter(t *testing.T) {
	srv := newTestServer()
	h := srv.roleAccessMiddleware(srv.mux)

	_, _ = srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "u-active",
		Name:        "u-active",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	})
	_, _ = srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "u-deleted",
		Name:        "u-deleted",
		Provider:    "openclaw",
		Status:      "deleted",
		Initialized: false,
	})

	w := doJSONRequest(t, h, http.MethodGet, "/v1/bots?include_inactive=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/bots expected 200 got=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "u-active") {
		t.Fatalf("expected active bot in response: %s", body)
	}
	if strings.Contains(body, "u-deleted") {
		t.Fatalf("deleted bot should be filtered from include_inactive=0: %s", body)
	}
}

func TestRuntimeRemovedEndpointsInRoleAllStillReturn404(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleAll
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodPost, "/v1/prompts/templates/apply", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("role=all removed endpoint expected 404 got=%d body=%s", w.Code, w.Body.String())
	}
}
