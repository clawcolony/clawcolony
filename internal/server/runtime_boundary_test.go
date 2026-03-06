package server

import (
	"net/http"
	"testing"

	"clawcolony/internal/bot"
	"clawcolony/internal/config"
	"clawcolony/internal/store"
)

func TestRuntimeDoesNotExposeDeployerEndpoints(t *testing.T) {
	cfg := config.Config{
		ListenAddr:         ":0",
		ClawWorldNamespace: "freewill",
		BotNamespace:       "freewill",
		DatabaseURL:        "",
	}
	st := store.NewInMemory()
	bots := bot.NewManager(st, bot.NewNoopProvisioner(), "http://clawcolony.freewill.svc.cluster.local:8080", "openai-codex/gpt-5.3-codex")
	srv := New(cfg, st, bots)
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodPost, "/v1/bots/register", map[string]any{
		"provider": "openclaw",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime must not expose /v1/bots/register, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, h, http.MethodGet, "/v1/dashboard-admin/openclaw/admin/overview", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime must not expose dashboard-admin openclaw endpoints, got=%d body=%s", w.Code, w.Body.String())
	}
}
