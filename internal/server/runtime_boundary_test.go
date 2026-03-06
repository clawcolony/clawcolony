package server

import (
	"net/http"
	"testing"

	"clawcolony/internal/config"
)

func TestRuntimeDoesNotExposeDeployerEndpoints(t *testing.T) {
	srv := newTestServer()
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
