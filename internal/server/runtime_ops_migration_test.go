package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"clawcolony/internal/config"
)

func TestRuntimeMigratedOpsCompatProxyForwardsAndMarksDeprecated(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotQuery string
	var gotBody string
	var gotHeader string
	var gotCookie string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotHeader = strings.TrimSpace(r.Header.Get("X-Test-Trace"))
		gotCookie = strings.TrimSpace(r.Header.Get("Cookie"))
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("X-Upstream-Test", "ok")
		w.Header().Add("Set-Cookie", "rt=1; Path=/; HttpOnly")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"source":"deployer"}`))
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = upstream.URL
	h := srv.roleAccessMiddleware(srv.mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/prompts/templates/apply?source=dashboard", strings.NewReader(`{"user_id":"u1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Trace", "trace-1")
	req.Header.Set("Cookie", "sid=abc")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/prompts/templates/apply" || gotQuery != "source=dashboard" {
		t.Fatalf("unexpected upstream request method=%s path=%s query=%s", gotMethod, gotPath, gotQuery)
	}
	if gotHeader != "trace-1" {
		t.Fatalf("missing forwarded header, got=%q", gotHeader)
	}
	if gotCookie != "" {
		t.Fatalf("cookie header should not be forwarded, got=%q", gotCookie)
	}
	if gotBody != `{"user_id":"u1"}` {
		t.Fatalf("unexpected upstream body=%q", gotBody)
	}
	if strings.TrimSpace(w.Header().Get("X-Clawcolony-Deprecated")) == "" {
		t.Fatalf("missing deprecation header")
	}
	if strings.TrimSpace(w.Header().Get("X-Upstream-Test")) != "ok" {
		t.Fatalf("upstream response headers should be preserved")
	}
	if strings.TrimSpace(w.Header().Get("Set-Cookie")) != "" {
		t.Fatalf("set-cookie from deployer should not be forwarded")
	}
	if !strings.Contains(w.Body.String(), `"source":"deployer"`) {
		t.Fatalf("unexpected response body=%s", w.Body.String())
	}
}

func TestRuntimeMigratedOpsCompatProxyPreservesDeployerBasePath(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = upstream.URL + "/internal/deployer"
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodPost, "/v1/prompts/templates/apply", map[string]any{
		"user_id": "u1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if gotPath != "/internal/deployer/v1/prompts/templates/apply" {
		t.Fatalf("unexpected proxied path=%q", gotPath)
	}
}

func TestRuntimeMigratedOpsHardCutDisablesEndpoint(t *testing.T) {
	var upstreamHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamHits, 1)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeHardCut
	srv.cfg.DeployerAPIBaseURL = upstream.URL
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/bots/dev/health?user_id=user-1", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("hard cut should disable migrated endpoint, got=%d body=%s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(&upstreamHits) != 0 {
		t.Fatalf("hard cut should not proxy requests")
	}
}

func TestRuntimeLogsExceptionStaysLocalInCompatMode(t *testing.T) {
	var upstreamHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamHits, 1)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = upstream.URL
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/bots/logs/all?tail=20", nil)
	if atomic.LoadInt32(&upstreamHits) != 0 {
		t.Fatalf("logs exception endpoints should never be proxied")
	}
	if strings.TrimSpace(w.Header().Get("X-Clawcolony-Deprecated")) != "" {
		t.Fatalf("logs exception endpoint should not include deprecation header")
	}
}

func TestRuntimeDeployerOnlyPathSetIsExplicit(t *testing.T) {
	srv := newTestServer()
	if !srv.isDeployerOnlyPath("/v1/bots/dev/user-1/p/3000/preview") {
		t.Fatalf("expected /v1/bots/dev/* to be deployer-only prefix")
	}
	if !srv.isDeployerOnlyPath("/v1/system/openclaw-dashboard-config") {
		t.Fatalf("expected /v1/system/openclaw-dashboard-config to be deployer-only path")
	}
	if srv.isDeployerOnlyPath("/v1/bots/logs/all") {
		t.Fatalf("logs exception path must remain runtime-owned")
	}
}

func TestRuntimeMigratedOpsCompatRequiresDeployerBase(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = ""
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodPost, "/v1/prompts/templates/apply", map[string]any{
		"user_id": "u1",
	})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing deployer base should return 503, got=%d body=%s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Header().Get("X-Clawcolony-Deprecated")) == "" {
		t.Fatalf("missing deprecation header")
	}
}

func TestRuntimeMigratedOpsCompatRejectsUnsupportedDeployerScheme(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = "file:///tmp/deployer.sock"
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodPost, "/v1/prompts/templates/apply", map[string]any{
		"user_id": "u1",
	})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unsupported deployer scheme should return 503, got=%d body=%s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Header().Get("X-Clawcolony-Deprecated")) == "" {
		t.Fatalf("missing deprecation header")
	}
}

func TestRuntimeMigratedOpsCompatInterceptsInRoleAll(t *testing.T) {
	var upstreamHits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamHits, 1)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleAll
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = upstream.URL
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/bots/openclaw/status?user_id=u1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("role=all should proxy migrated endpoint in compat mode, got=%d body=%s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(&upstreamHits) != 1 {
		t.Fatalf("expected migrated endpoint to be proxied once")
	}
	if strings.TrimSpace(w.Header().Get("X-Clawcolony-Deprecated")) == "" {
		t.Fatalf("missing deprecation header")
	}
}

func TestRuntimeMigratedOpsRoleAllWithoutDeployerBaseFallsBackLocal(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleAll
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = ""
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodPost, "/v1/prompts/templates/apply", map[string]any{
		"user_id": "u1",
	})
	if strings.TrimSpace(w.Header().Get("X-Clawcolony-Deprecated")) != "" {
		t.Fatalf("role=all without deployer base should not be proxied")
	}
}

func TestRuntimeMigratedOpsClassificationNormalizesPath(t *testing.T) {
	srv := newTestServer()
	if srv.isMigratedOpsPath("/v1/bots/logs/all/") {
		t.Fatalf("logs trailing slash should still be treated as runtime logs exception")
	}
	if srv.isMigratedOpsPath("/v1/bots/dev/../logs/all") {
		t.Fatalf("cleaned logs path should not be treated as migrated ops")
	}
	if !srv.isMigratedOpsPath("/v1/bots/dev//user-1/p/3000/") {
		t.Fatalf("normalized dev proxy path should be treated as migrated ops")
	}
}

func TestRuntimeLocalModeRuntimeRoleAllowsMigratedPath(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeLocalLegacy
	if !srv.pathAllowedForRole(config.ServiceRoleRuntime, "/v1/bots/dev/link") {
		t.Fatalf("runtime local mode should allow local migrated ops path")
	}
}

func TestRuntimeMigratedOpsCompatRejectsOversizedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", int(proxyResponseBodyMaxBytes)+1)))
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.RuntimeOpsProxyMode = config.OpsProxyModeCompat
	srv.cfg.DeployerAPIBaseURL = upstream.URL
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodPost, "/v1/prompts/templates/apply", map[string]any{
		"user_id": "u1",
	})
	if w.Code != http.StatusBadGateway {
		t.Fatalf("oversized upstream response should return 502, got=%d body=%s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Header().Get("X-Clawcolony-Deprecated")) == "" {
		t.Fatalf("missing deprecation header")
	}
}
