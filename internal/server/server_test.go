package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawcolony/internal/bot"
	"clawcolony/internal/config"
	"clawcolony/internal/store"
)

var seedCounter int64

type leaderboardTestStore struct {
	store.Store
	bots     []store.Bot
	accounts []store.TokenAccount
}

func (s *leaderboardTestStore) ListBots(_ context.Context) ([]store.Bot, error) {
	if s.bots == nil {
		return s.Store.ListBots(context.Background())
	}
	out := make([]store.Bot, len(s.bots))
	copy(out, s.bots)
	return out, nil
}

func (s *leaderboardTestStore) ListTokenAccounts(_ context.Context) ([]store.TokenAccount, error) {
	if s.accounts == nil {
		return s.Store.ListTokenAccounts(context.Background())
	}
	out := make([]store.TokenAccount, len(s.accounts))
	copy(out, s.accounts)
	return out, nil
}

func newTestServerWithStore(st store.Store) *Server {
	cfg := config.Config{
		ListenAddr:         ":0",
		ClawWorldNamespace: "clawcolony",
		BotNamespace:       "freewill",
		DatabaseURL:        "",
	}
	bots := bot.NewManager(st, bot.NewNoopProvisioner(), "http://clawcolony.freewill.svc.cluster.local:8080", "openai-codex/gpt-5.3-codex")
	s := New(cfg, st, bots)
	s.kubeClient = nil
	attachRegisterShim(s)
	return s
}

func newTestServer() *Server {
	return newTestServerWithStore(store.NewInMemory())
}

func attachRegisterShim(s *Server) {
	defer func() {
		_ = recover()
	}()
	s.mux.HandleFunc("/v1/bots/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Provider string `json:"provider"`
		}
		_ = decodeJSON(r, &req)
		provider := strings.TrimSpace(req.Provider)
		if provider == "" {
			provider = "openclaw"
		}

		seq := atomic.AddInt64(&seedCounter, 1)
		userID := fmt.Sprintf("user-%d-%04d", time.Now().UTC().UnixMilli(), seq%10000)
		name := fmt.Sprintf("user-%04d", seq%10000)

		item, err := s.store.UpsertBot(r.Context(), store.BotUpsertInput{
			BotID:       userID,
			Name:        name,
			Provider:    provider,
			Status:      "running",
			Initialized: true,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		initial := int64(1000)
		if s.cfg.InitialToken > 0 {
			initial = s.cfg.InitialToken
		}
		if _, err := s.store.Recharge(r.Context(), userID, initial); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"item": map[string]any{
				"user_id":     item.BotID,
				"name":        item.Name,
				"provider":    item.Provider,
				"status":      item.Status,
				"initialized": item.Initialized,
			},
		})
	})
}

func doJSONRequest(t *testing.T, h http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func doJSONRequestWithHeaders(t *testing.T, h http.Handler, method, path string, payload any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func doJSONRequestWithRemoteAddr(t *testing.T, h http.Handler, method, path string, payload any, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func ptrTime(t time.Time) *time.Time {
	v := t
	return &v
}

func seedActiveUser(t *testing.T, srv *Server) string {
	t.Helper()
	id := "user-test-" + strconv.FormatInt(atomic.AddInt64(&seedCounter, 1), 10)
	_, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       id,
		Name:        id,
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	})
	if err != nil {
		t.Fatalf("seed active user failed: %v", err)
	}
	if _, err := srv.store.Recharge(context.Background(), id, 1000); err != nil {
		t.Fatalf("seed active user token recharge failed: %v", err)
	}
	return id
}

func TestDefaultPromptTemplateMapIncludesMCPOnlySkillTemplates(t *testing.T) {
	srv := newTestServer()
	user := store.Bot{
		BotID:       "user-template-test",
		Name:        "template-test",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}
	defaults := srv.defaultPromptTemplateMap(context.Background(), user)
	requiredKeys := []string{
		bot.TemplateClawWorldSkill,
		bot.TemplateColonyCoreSkill,
		bot.TemplateColonyToolsSkill,
		bot.TemplateKnowledgeBaseSkill,
		bot.TemplateGangliaStackSkill,
		bot.TemplateCollabModeSkill,
		bot.TemplateDevPreviewSkill,
	}
	for _, key := range requiredKeys {
		if strings.TrimSpace(defaults[key]) == "" {
			t.Fatalf("default prompt template missing key: %s", key)
		}
	}
	if !strings.Contains(defaults[bot.TemplateKnowledgeBaseSkill], "clawcolony-mcp-knowledgebase_") {
		t.Fatalf("knowledge_base_skill must reference clawcolony-mcp-knowledgebase tools")
	}
	if !strings.Contains(defaults[bot.TemplateGangliaStackSkill], "clawcolony-mcp-ganglia_") {
		t.Fatalf("ganglia_stack_skill must reference clawcolony-mcp-ganglia tools")
	}
}

func TestPromptTemplatesDBOverrideWinsForKnowledgeAndGangliaSkills(t *testing.T) {
	srv := newTestServer()
	userID := seedActiveUser(t, srv)
	customKB := "custom-knowledge-base-skill-content"
	customGanglia := "custom-ganglia-stack-skill-content"
	if _, err := srv.store.UpsertPromptTemplate(context.Background(), store.PromptTemplate{
		Key:     bot.TemplateKnowledgeBaseSkill,
		Content: customKB,
	}); err != nil {
		t.Fatalf("upsert knowledge template: %v", err)
	}
	if _, err := srv.store.UpsertPromptTemplate(context.Background(), store.PromptTemplate{
		Key:     bot.TemplateGangliaStackSkill,
		Content: customGanglia,
	}); err != nil {
		t.Fatalf("upsert ganglia template: %v", err)
	}
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/prompts/templates?user_id="+userID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("prompts/templates status=%d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("response missing items")
	}
	find := func(key string) map[string]any {
		for _, it := range items {
			m, _ := it.(map[string]any)
			if strings.TrimSpace(fmt.Sprintf("%v", m["key"])) == key {
				return m
			}
		}
		return nil
	}
	kb := find(bot.TemplateKnowledgeBaseSkill)
	if kb == nil {
		t.Fatalf("knowledge_base_skill template missing")
	}
	if src := strings.TrimSpace(fmt.Sprintf("%v", kb["source"])); src != "db" {
		t.Fatalf("knowledge_base_skill source=%s, want db", src)
	}
	if content := fmt.Sprintf("%v", kb["content"]); !strings.Contains(content, customKB) {
		t.Fatalf("knowledge_base_skill content should use db override")
	}
	ganglia := find(bot.TemplateGangliaStackSkill)
	if ganglia == nil {
		t.Fatalf("ganglia_stack_skill template missing")
	}
	if src := strings.TrimSpace(fmt.Sprintf("%v", ganglia["source"])); src != "db" {
		t.Fatalf("ganglia_stack_skill source=%s, want db", src)
	}
	if content := fmt.Sprintf("%v", ganglia["content"]); !strings.Contains(content, customGanglia) {
		t.Fatalf("ganglia_stack_skill content should use db override")
	}
}

func TestRoleAccessRuntimeBlocksDeployerRoutes(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/bots/upgrade/history?limit=5", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime mode should block management-only route, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, h, http.MethodGet, "/v1/meta", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime mode should allow meta, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRoleAccessUnknownRoleFallsBackToRuntime(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = "management"
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/mail/inbox?user_id=u1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("unknown role should fallback to runtime routing, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, h, http.MethodGet, "/v1/openclaw/admin/overview", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime repo should not expose admin overview directly, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRoleAccessAllAllowsBoth(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleAll
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/meta", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("all mode should allow meta, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, h, http.MethodGet, "/v1/openclaw/admin/overview", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime repo should not expose management route directly, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDashboardAdminProxyRuntimeForwardsToDeployer(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	h := srv.roleAccessMiddleware(srv.mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard-admin/openclaw/admin/overview?limit=20", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime repo should not expose dashboard-admin proxy path, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDashboardAdminProxyAllDispatchesLocal(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleAll
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/dashboard-admin/openclaw/admin/github/health", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime repo should not expose dashboard-admin path even in all mode, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevLinkProxyCreatesSignedRuntimeLink(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "runtime-sync-token"
	srv.cfg.PreviewAllowedPorts = "3000,5173"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-1",
		Name:        "user-dev-1",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-1",
		GatewayToken: "gw-dev-1",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/dev/link", map[string]any{
		"user_id":       "user-dev-1",
		"port":          5173,
		"path":          "/preview?x=1",
		"gateway_token": "gw-dev-1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Item struct {
			UserID      string `json:"user_id"`
			Port        int    `json:"port"`
			Path        string `json:"path"`
			RelativeURL string `json:"relative_url"`
			TTLDays     int64  `json:"ttl_days"`
		} `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Item.UserID != "user-dev-1" || payload.Item.Port != 5173 || payload.Item.Path != "/preview" {
		t.Fatalf("unexpected item: %+v", payload.Item)
	}
	if !strings.Contains(payload.Item.RelativeURL, "/v1/bots/dev/user-dev-1/p/5173/preview?") {
		t.Fatalf("unexpected relative url: %s", payload.Item.RelativeURL)
	}
	linkURL, err := neturl.Parse(payload.Item.RelativeURL)
	if err != nil {
		t.Fatalf("parse relative url: %v", err)
	}
	values := linkURL.Query()
	if got := values.Get("x"); got != "1" {
		t.Fatalf("query x=%q, want 1", got)
	}
	expRaw := values.Get(devProxySignedParamExp)
	nonce := values.Get(devProxySignedParamNonce)
	sig := values.Get(devProxySignedParamSig)
	if expRaw == "" || nonce == "" || sig == "" {
		t.Fatalf("signed params missing: %s", payload.Item.RelativeURL)
	}
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil {
		t.Fatalf("parse exp: %v", err)
	}
	expectSig := runtimeDevProxyComputeSignature(
		srv.cfg.InternalSyncToken,
		"user-dev-1",
		5173,
		"/preview",
		neturl.Values{"x": []string{"1"}}.Encode(),
		exp,
		nonce,
	)
	if sig != expectSig {
		t.Fatalf("sig mismatch: got=%s want=%s", sig, expectSig)
	}
	if payload.Item.TTLDays != runtimeSchedulerDefaultPreviewLinkTTLDays {
		t.Fatalf("ttl_days=%d, want=%d", payload.Item.TTLDays, runtimeSchedulerDefaultPreviewLinkTTLDays)
	}
}

func TestBotDevLinkProxyIncludesPublicURLWhenConfigured(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "runtime-sync-token"
	srv.cfg.PreviewAllowedPorts = "3000"
	srv.cfg.PreviewPublicBaseURL = "https://preview.example.com"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-public",
		Name:        "user-dev-public",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-public",
		GatewayToken: "gw-dev-public",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/dev/link", map[string]any{
		"user_id":       "user-dev-public",
		"port":          3000,
		"path":          "/",
		"gateway_token": "gw-dev-public",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var payload struct {
		Item struct {
			RelativeURL string `json:"relative_url"`
			AbsoluteURL string `json:"absolute_url"`
			PublicURL   string `json:"public_url"`
		} `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Item.RelativeURL == "" {
		t.Fatalf("relative_url should not be empty")
	}
	if payload.Item.AbsoluteURL == "" || !strings.Contains(payload.Item.AbsoluteURL, payload.Item.RelativeURL) {
		t.Fatalf("unexpected absolute_url=%q relative_url=%q", payload.Item.AbsoluteURL, payload.Item.RelativeURL)
	}
	wantPublic := "https://preview.example.com" + payload.Item.RelativeURL
	if payload.Item.PublicURL != wantPublic {
		t.Fatalf("public_url=%q, want=%q", payload.Item.PublicURL, wantPublic)
	}
}

func TestBotDevLinkProxyValidationAndMethod(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/dev/link", map[string]any{
		"path": "/",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing user_id should be 400, got=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bots/dev/link", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET should be 405, got=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-auth",
		Name:        "user-dev-auth",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-auth",
		GatewayToken: "gw-auth",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/dev/link", map[string]any{
		"user_id":       "user-dev-auth",
		"port":          3000,
		"path":          "/",
		"gateway_token": "wrong",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token should be 401, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevLinkProxyRejectsDisallowedPort(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "runtime-sync-token"
	srv.cfg.PreviewAllowedPorts = "3000"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-port",
		Name:        "user-dev-port",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-port",
		GatewayToken: "gw-port",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/dev/link", map[string]any{
		"user_id":       "user-dev-port",
		"port":          5173,
		"path":          "/",
		"gateway_token": "gw-port",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("disallowed port should be 400, got=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("port is not allowed")) {
		t.Fatalf("unexpected error body: %s", w.Body.String())
	}
}

func TestBotDevProxyForwardPassThrough(t *testing.T) {
	srv := newTestServer()
	srv.cfg.PreviewAllowedPorts = "3000,5173"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-2",
		Name:        "user-dev-2",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-2",
		GatewayToken: "gw-dev-2",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user-dev-2/5173/preview" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.URL.RawQuery != "x=1" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "" {
			t.Fatalf("authorization header should be stripped, got=%q", got)
		}
		if got := strings.TrimSpace(r.Header.Get("X-Clawcolony-Gateway-Token")); got != "" {
			t.Fatalf("x-clawcolony-gateway-token should be stripped, got=%q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-dev-proxy"))
	}))
	defer up.Close()
	srv.cfg.PreviewUpstreamTemplate = up.URL + "/{{user_id}}/{{port}}"

	req := httptest.NewRequest(http.MethodGet, "/v1/bots/dev/user-dev-2/p/5173/preview?x=1&token=gw-dev-2", nil)
	req.Header.Set("Authorization", "Bearer gw-dev-2")
	req.Header.Set("X-Clawcolony-Gateway-Token", "gw-dev-2")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.TrimSpace(w.Body.String()) != "ok-dev-proxy" {
		t.Fatalf("body=%q", w.Body.String())
	}
}

func TestBotDevProxyForwardLegacyPathDefaultsTo3000(t *testing.T) {
	srv := newTestServer()
	srv.cfg.PreviewAllowedPorts = "3000"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-legacy",
		Name:        "user-dev-legacy",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-legacy",
		GatewayToken: "gw-dev-legacy",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user-dev-legacy/3000/preview" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	srv.cfg.PreviewUpstreamTemplate = up.URL + "/{{user_id}}/{{port}}"

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bots/dev/user-dev-legacy/preview?token=gw-dev-legacy", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy path should pass with default port 3000, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevProxyForwardRejectsInvalidPathAndMethod(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bots/dev/unknown-user/preview", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown user should be 404, got=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/dev/user-x/preview", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST should be 405, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevProxyForwardRejectsMissingAuth(t *testing.T) {
	srv := newTestServer()
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-3",
		Name:        "user-dev-3",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-3",
		GatewayToken: "gw-dev-3",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bots/dev/user-dev-3/preview", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth should be 401, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevProxyForwardAllowsValidSignedQuery(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "runtime-sync-token"
	srv.cfg.PreviewAllowedPorts = "5173"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-signed",
		Name:        "user-dev-signed",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-signed",
		GatewayToken: "gw-signed",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "x=1" {
			t.Fatalf("query=%s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-signed"))
	}))
	defer up.Close()
	srv.cfg.PreviewUpstreamTemplate = up.URL

	exp := time.Now().UTC().Add(30 * time.Minute).Unix()
	nonce := "n-1"
	targetPath := "/preview"
	targetQuery := neturl.Values{"x": []string{"1"}}.Encode()
	sig := runtimeDevProxyComputeSignature(srv.cfg.InternalSyncToken, "user-dev-signed", 5173, targetPath, targetQuery, exp, nonce)
	reqURL := fmt.Sprintf("/v1/bots/dev/user-dev-signed/p/5173/preview?x=1&exp=%d&nonce=%s&sig=%s", exp, nonce, sig)
	w := doJSONRequest(t, srv.mux, http.MethodGet, reqURL, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("signed query should pass, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevProxyForwardRejectsInvalidSignedQuery(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "runtime-sync-token"
	srv.cfg.PreviewAllowedPorts = "5173"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-signed-bad",
		Name:        "user-dev-signed-bad",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-signed-bad",
		GatewayToken: "gw-signed-bad",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bots/dev/user-dev-signed-bad/p/5173/preview?x=1&exp=1&nonce=n&sig=fake", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signed query should be 401, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevProxyForwardRejectsExpiredSignedQuery(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "runtime-sync-token"
	srv.cfg.PreviewAllowedPorts = "5173"
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-dev-signed-expired",
		Name:        "user-dev-signed-expired",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-dev-signed-expired",
		GatewayToken: "gw-signed-expired",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}
	exp := time.Now().UTC().Add(-1 * time.Minute).Unix()
	nonce := "n-expired"
	targetPath := "/preview"
	targetQuery := neturl.Values{"x": []string{"1"}}.Encode()
	sig := runtimeDevProxyComputeSignature(srv.cfg.InternalSyncToken, "user-dev-signed-expired", 5173, targetPath, targetQuery, exp, nonce)
	reqURL := fmt.Sprintf("/v1/bots/dev/user-dev-signed-expired/p/5173/preview?x=1&exp=%d&nonce=%s&sig=%s", exp, nonce, sig)
	w := doJSONRequest(t, srv.mux, http.MethodGet, reqURL, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired signed query should be 401, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevHealthUsesGatewayToken(t *testing.T) {
	srv := newTestServer()
	srv.cfg.PreviewAllowedPorts = "3000,5173"
	userID := seedActiveUser(t, srv)
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       userID,
		GatewayToken: "gw-health-token",
	}); err != nil {
		t.Fatalf("upsert credentials: %v", err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/" + userID + "/5173/"
		if r.URL.Path != wantPath {
			t.Fatalf("path=%s, want=%s", r.URL.Path, wantPath)
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "" {
			t.Fatalf("authorization header should be empty, got=%q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer up.Close()
	srv.cfg.PreviewUpstreamTemplate = up.URL + "/{{user_id}}/{{port}}"

	w := doJSONRequestWithHeaders(t, srv.mux, http.MethodGet, "/v1/bots/dev/health?user_id="+neturl.QueryEscape(userID)+"&port=5173", nil, map[string]string{
		"Authorization": "Bearer gw-health-token",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"ok":true`)) {
		t.Fatalf("health should be ok=true: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status_code":204`)) {
		t.Fatalf("health should include status_code 204: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"port":5173`)) {
		t.Fatalf("health should include port: %s", w.Body.String())
	}
}

func TestBotDevHealthRejectsPathTraversal(t *testing.T) {
	srv := newTestServer()
	userID := seedActiveUser(t, srv)
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       userID,
		GatewayToken: "gw-traversal",
	}); err != nil {
		t.Fatalf("upsert credentials: %v", err)
	}
	w := doJSONRequestWithHeaders(t, srv.mux, http.MethodGet, "/v1/bots/dev/health?user_id="+neturl.QueryEscape(userID)+"&path=/../../admin", nil, map[string]string{
		"Authorization": "Bearer gw-traversal",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("path traversal should be 400, got=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequestWithHeaders(t, srv.mux, http.MethodGet, "/v1/bots/dev/health?user_id="+neturl.QueryEscape(userID)+"&path=/%252e%252e/%252e%252e/admin", nil, map[string]string{
		"Authorization": "Bearer gw-traversal",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("double-encoded path traversal should be 400, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBotDevHealthDoesNotLeakUpstreamBody(t *testing.T) {
	srv := newTestServer()
	userID := seedActiveUser(t, srv)
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       userID,
		GatewayToken: "gw-health-body",
	}); err != nil {
		t.Fatalf("upsert credentials: %v", err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("stacktrace: internal secret details"))
	}))
	defer up.Close()
	srv.cfg.PreviewUpstreamTemplate = up.URL

	w := doJSONRequestWithHeaders(t, srv.mux, http.MethodGet, "/v1/bots/dev/health?user_id="+neturl.QueryEscape(userID)+"&port=3000", nil, map[string]string{
		"Authorization": "Bearer gw-health-body",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status_code":500`)) {
		t.Fatalf("missing status_code in body=%s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("internal secret")) {
		t.Fatalf("health response leaked upstream body=%s", w.Body.String())
	}
}

func TestBotDevHealthUnknownUserReturnsNotFound(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bots/dev/health?user_id=unknown-user", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown user should be 404, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPreviewUpstreamURLUsesServiceDNSByDefault(t *testing.T) {
	srv := newTestServer()
	// Empty template forces server-level fallback (defaultPreviewUpstreamTemplate).
	srv.cfg.PreviewUpstreamTemplate = ""

	u, err := srv.previewUpstreamURL("user-dev-default", 3000)
	if err != nil {
		t.Fatalf("previewUpstreamURL error: %v", err)
	}
	if got, want := u.String(), "http://user-dev-default.freewill.svc.cluster.local:3000"; got != want {
		t.Fatalf("upstream url = %q, want %q", got, want)
	}
}

func TestPreviewUpstreamDefaultMatchesConfigDefault(t *testing.T) {
	t.Setenv("CLAWCOLONY_PREVIEW_UPSTREAM_TEMPLATE", "")
	cfg := config.FromEnv()
	if got, want := cfg.PreviewUpstreamTemplate, defaultPreviewUpstreamTemplate; got != want {
		t.Fatalf("preview upstream default drift: config=%q server=%q", got, want)
	}
}

func TestPreviewUpstreamURLUsesConfigDefaultTemplate(t *testing.T) {
	srv := newTestServer()
	t.Setenv("CLAWCOLONY_PREVIEW_UPSTREAM_TEMPLATE", "")
	cfg := config.FromEnv()
	srv.cfg.PreviewUpstreamTemplate = cfg.PreviewUpstreamTemplate

	u, err := srv.previewUpstreamURL("user-dev-config-default", 5173)
	if err != nil {
		t.Fatalf("previewUpstreamURL error: %v", err)
	}
	if got, want := u.String(), "http://user-dev-config-default.freewill.svc.cluster.local:5173"; got != want {
		t.Fatalf("upstream url = %q, want %q", got, want)
	}
}

func TestRegisterAndTokenLifecycle(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{
		"provider": "ironuser",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	item := body["item"].(map[string]any)
	userID := item["user_id"].(string)
	if len(userID) == 0 || userID[:5] != "user-" {
		t.Fatalf("id prefix mismatch, got %q", userID)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/consume", map[string]any{
		"user_id": userID,
		"amount":  40,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("consume status = %d, want %d, body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+userID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("accounts status = %d, want %d", w.Code, http.StatusOK)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance":960`)) {
		t.Fatalf("accounts body missing balance=960: %s", w.Body.String())
	}
}

func TestTokenAccountsRequiresUserID(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("token accounts status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`请提供你的USERID`)) {
		t.Fatalf("missing USERID hint: %s", w.Body.String())
	}
}

func TestTokenLeaderboardExcludesAdminAndSortsByBalance(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	for _, input := range []store.BotUpsertInput{
		{BotID: "user-alpha", Name: "Alpha", Provider: "openclaw", Status: "running", Initialized: true},
		{BotID: "user-bravo", Name: "Bravo", Provider: "openclaw", Status: "running", Initialized: true},
		{BotID: "user-charlie", Name: "Charlie", Provider: "openclaw", Status: "running", Initialized: false},
		{BotID: clawWorldSystemID, Name: "Clawcolony", Provider: "system", Status: "running", Initialized: true},
	} {
		if _, err := srv.store.UpsertBot(ctx, input); err != nil {
			t.Fatalf("upsert bot %s: %v", input.BotID, err)
		}
	}
	if _, err := srv.store.Recharge(ctx, "user-alpha", 120); err != nil {
		t.Fatalf("recharge alpha: %v", err)
	}
	if _, err := srv.store.Recharge(ctx, "user-bravo", 250); err != nil {
		t.Fatalf("recharge bravo: %v", err)
	}
	if _, err := srv.store.Recharge(ctx, "user-charlie", 180); err != nil {
		t.Fatalf("recharge charlie: %v", err)
	}
	if _, err := srv.store.Recharge(ctx, clawWorldSystemID, 9999); err != nil {
		t.Fatalf("recharge admin: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Currency string `json:"currency"`
		Total    int    `json:"total"`
		Items    []struct {
			Rank        int       `json:"rank"`
			UserID      string    `json:"user_id"`
			Name        string    `json:"name"`
			BotFound    bool      `json:"bot_found"`
			Initialized bool      `json:"initialized"`
			Balance     int64     `json:"balance"`
			UpdatedAt   time.Time `json:"updated_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal leaderboard: %v", err)
	}
	if body.Currency != "token" {
		t.Fatalf("currency = %q, want token", body.Currency)
	}
	if body.Total != 3 {
		t.Fatalf("total = %d, want 3", body.Total)
	}
	if len(body.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(body.Items))
	}
	if body.Items[0].UserID != "user-bravo" || body.Items[0].Rank != 1 || body.Items[0].Balance != 250 {
		t.Fatalf("unexpected first item: %+v", body.Items[0])
	}
	if body.Items[1].UserID != "user-charlie" || body.Items[1].Rank != 2 || body.Items[1].Balance != 180 {
		t.Fatalf("unexpected second item: %+v", body.Items[1])
	}
	if body.Items[0].Name != "Bravo" || !body.Items[0].Initialized {
		t.Fatalf("unexpected first item metadata: %+v", body.Items[0])
	}
	if !body.Items[0].BotFound {
		t.Fatalf("expected first item bot_found=true: %+v", body.Items[0])
	}
	if body.Items[1].Name != "Charlie" || body.Items[1].Initialized {
		t.Fatalf("unexpected second item metadata: %+v", body.Items[1])
	}
	if !body.Items[1].BotFound {
		t.Fatalf("expected second item bot_found=true: %+v", body.Items[1])
	}
	for _, item := range body.Items {
		if item.UserID == clawWorldSystemID {
			t.Fatalf("admin should be excluded: %+v", item)
		}
	}
}

func TestTokenLeaderboardMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/leaderboard", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("leaderboard status = %d, want %d, body=%s", w.Code, http.StatusMethodNotAllowed, w.Body.String())
	}
}

func TestTokenLeaderboardHandlesEmptyAndInvalidLimit(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("empty leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var emptyBody struct {
		Total int             `json:"total"`
		Items json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &emptyBody); err != nil {
		t.Fatalf("unmarshal empty leaderboard: %v", err)
	}
	if emptyBody.Total != 0 || string(emptyBody.Items) != "[]" {
		t.Fatalf("unexpected empty leaderboard body: %s", w.Body.String())
	}

	ctx := context.Background()
	if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       "user-limit",
		Name:        "Limit",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("upsert limit user: %v", err)
	}
	if _, err := srv.store.Recharge(ctx, "user-limit", 42); err != nil {
		t.Fatalf("recharge limit user: %v", err)
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=0", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("invalid limit leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var limitBody struct {
		Total int `json:"total"`
		Items []struct {
			UserID string `json:"user_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &limitBody); err != nil {
		t.Fatalf("unmarshal invalid limit leaderboard: %v", err)
	}
	if limitBody.Total != 1 || len(limitBody.Items) != 1 || limitBody.Items[0].UserID != "user-limit" {
		t.Fatalf("unexpected invalid limit body: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=-5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("negative limit leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var negativeBody struct {
		Items []struct {
			UserID string `json:"user_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &negativeBody); err != nil {
		t.Fatalf("unmarshal negative limit leaderboard: %v", err)
	}
	if len(negativeBody.Items) != 1 || negativeBody.Items[0].UserID != "user-limit" {
		t.Fatalf("unexpected negative limit body: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=abc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("string limit leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var stringBody struct {
		Items []struct {
			UserID string `json:"user_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stringBody); err != nil {
		t.Fatalf("unmarshal string limit leaderboard: %v", err)
	}
	if len(stringBody.Items) != 1 || stringBody.Items[0].UserID != "user-limit" {
		t.Fatalf("unexpected string limit body: %s", w.Body.String())
	}
}

func TestSortTokenLeaderboardEntriesTieBreakers(t *testing.T) {
	now := time.Now().UTC()
	items := []tokenLeaderboardEntry{
		{UserID: "user-c", Balance: 100, UpdatedAt: now.Add(-1 * time.Minute)},
		{UserID: "user-b", Balance: 100, UpdatedAt: now},
		{UserID: "user-a", Balance: 100, UpdatedAt: now},
		{UserID: "user-z", Balance: 50, UpdatedAt: now.Add(2 * time.Minute)},
	}

	sortTokenLeaderboardEntries(items)

	got := []string{items[0].UserID, items[1].UserID, items[2].UserID, items[3].UserID}
	want := []string{"user-a", "user-b", "user-c", "user-z"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected sort order: got=%v want=%v", got, want)
	}
}

func TestPreferTokenLeaderboardAccount(t *testing.T) {
	now := time.Now().UTC()
	current := store.TokenAccount{BotID: "user-1", Balance: 10, UpdatedAt: now}
	if !preferTokenLeaderboardAccount(current, store.TokenAccount{BotID: "user-1", Balance: 12, UpdatedAt: now.Add(-1 * time.Hour)}) {
		t.Fatalf("higher balance should win duplicate selection")
	}
	if !preferTokenLeaderboardAccount(current, store.TokenAccount{BotID: "user-1", Balance: 10, UpdatedAt: now.Add(1 * time.Minute)}) {
		t.Fatalf("newer timestamp should break same-balance tie")
	}
	if preferTokenLeaderboardAccount(current, store.TokenAccount{BotID: "user-1", Balance: 9, UpdatedAt: now.Add(1 * time.Hour)}) {
		t.Fatalf("lower balance should not win duplicate selection")
	}
}

func TestTokenLeaderboardIncludesOrphanAccountsWithFallbackMetadata(t *testing.T) {
	now := time.Now().UTC()
	st := &leaderboardTestStore{
		Store: store.NewInMemory(),
		bots: []store.Bot{
			{
				BotID:       "user-known",
				Name:        "Known",
				Provider:    "openclaw",
				Status:      "running",
				Initialized: true,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
		accounts: []store.TokenAccount{
			{BotID: "user-orphan", Balance: 300, UpdatedAt: now},
			{BotID: "user-known", Balance: 120, UpdatedAt: now.Add(-1 * time.Minute)},
		},
	}
	srv := newTestServerWithStore(st)

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Items []struct {
			UserID      string `json:"user_id"`
			Name        string `json:"name"`
			BotFound    bool   `json:"bot_found"`
			Status      string `json:"status"`
			Initialized bool   `json:"initialized"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal orphan leaderboard: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(body.Items))
	}
	if body.Items[0].UserID != "user-orphan" || body.Items[0].Name != "user-orphan" || body.Items[0].BotFound || body.Items[0].Status != "missing" || body.Items[0].Initialized {
		t.Fatalf("unexpected orphan item: %+v", body.Items[0])
	}
	if body.Items[1].UserID != "user-known" || body.Items[1].Name != "Known" || !body.Items[1].BotFound {
		t.Fatalf("unexpected known item: %+v", body.Items[1])
	}
}

func TestTokenLeaderboardLimitCapsAt500(t *testing.T) {
	now := time.Now().UTC()
	bots := make([]store.Bot, 0, 520)
	accounts := make([]store.TokenAccount, 0, 520)
	for i := 0; i < 520; i++ {
		uid := fmt.Sprintf("user-%03d", i)
		bots = append(bots, store.Bot{
			BotID:       uid,
			Name:        uid,
			Provider:    "openclaw",
			Status:      "running",
			Initialized: true,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		accounts = append(accounts, store.TokenAccount{
			BotID:     uid,
			Balance:   int64(1000 - i),
			UpdatedAt: now.Add(-time.Duration(i) * time.Second),
		})
	}
	srv := newTestServerWithStore(&leaderboardTestStore{
		Store:    store.NewInMemory(),
		bots:     bots,
		accounts: accounts,
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=9999", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Total int `json:"total"`
		Items []struct {
			Rank   int    `json:"rank"`
			UserID string `json:"user_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal capped leaderboard: %v", err)
	}
	if body.Total != 520 {
		t.Fatalf("total = %d, want 520", body.Total)
	}
	if len(body.Items) != 500 {
		t.Fatalf("len(items) = %d, want 500", len(body.Items))
	}
	if body.Items[0].Rank != 1 || body.Items[0].UserID != "user-000" {
		t.Fatalf("unexpected first item: %+v", body.Items[0])
	}
	if body.Items[499].Rank != 500 || body.Items[499].UserID != "user-499" {
		t.Fatalf("unexpected last item: %+v", body.Items[499])
	}
}

func TestTokenLeaderboardIncludesZeroBalanceUsers(t *testing.T) {
	now := time.Now().UTC()
	srv := newTestServerWithStore(&leaderboardTestStore{
		Store: store.NewInMemory(),
		bots: []store.Bot{
			{BotID: "user-rich", Name: "Rich", Provider: "openclaw", Status: "running", Initialized: true, CreatedAt: now, UpdatedAt: now},
			{BotID: "user-zero", Name: "Zero", Provider: "openclaw", Status: "running", Initialized: true, CreatedAt: now, UpdatedAt: now},
		},
		accounts: []store.TokenAccount{
			{BotID: "user-rich", Balance: 5, UpdatedAt: now},
			{BotID: "user-zero", Balance: 0, UpdatedAt: now.Add(-1 * time.Minute)},
		},
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Total int `json:"total"`
		Items []struct {
			UserID  string `json:"user_id"`
			Balance int64  `json:"balance"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal zero-balance leaderboard: %v", err)
	}
	if body.Total != 2 || len(body.Items) != 2 {
		t.Fatalf("unexpected zero-balance leaderboard size: %s", w.Body.String())
	}
	if body.Items[0].UserID != "user-rich" || body.Items[1].UserID != "user-zero" || body.Items[1].Balance != 0 {
		t.Fatalf("unexpected zero-balance leaderboard ordering: %s", w.Body.String())
	}
}

func TestTokenLeaderboardIncludesNegativeBalanceUsers(t *testing.T) {
	now := time.Now().UTC()
	srv := newTestServerWithStore(&leaderboardTestStore{
		Store: store.NewInMemory(),
		bots: []store.Bot{
			{BotID: "user-pos", Name: "Positive", Provider: "openclaw", Status: "running", Initialized: true, CreatedAt: now, UpdatedAt: now},
			{BotID: "user-neg", Name: "Negative", Provider: "openclaw", Status: "running", Initialized: true, CreatedAt: now, UpdatedAt: now},
		},
		accounts: []store.TokenAccount{
			{BotID: "user-pos", Balance: 3, UpdatedAt: now},
			{BotID: "user-neg", Balance: -5, UpdatedAt: now.Add(-1 * time.Minute)},
		},
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/leaderboard?limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("leaderboard status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Items []struct {
			UserID  string `json:"user_id"`
			Balance int64  `json:"balance"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal negative-balance leaderboard: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(body.Items))
	}
	if body.Items[0].UserID != "user-pos" || body.Items[1].UserID != "user-neg" || body.Items[1].Balance != -5 {
		t.Fatalf("unexpected negative-balance ordering: %s", w.Body.String())
	}
}

func TestPiTaskClaimSubmitAndHistory(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "ironuser"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d", w.Code, http.StatusAccepted)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	userID := body["item"].(map[string]any)["user_id"].(string)

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tasks/pi/claim", map[string]any{"user_id": userID})
	if w.Code != http.StatusAccepted {
		t.Fatalf("claim status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var claim map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &claim)
	task := claim["item"].(map[string]any)
	taskID := task["task_id"].(string)
	answer := task["example"].(string)
	_ = answer // example text only

	// Pull expected answer from question position by reading task meta in-memory through response fields is not exposed;
	// submit a known-wrong answer and assert failed path is accepted.
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tasks/pi/submit", map[string]any{
		"user_id": userID,
		"task_id": taskID,
		"answer":  "0",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/tasks/pi/history?user_id="+userID+"&limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("history status = %d, want %d", w.Code, http.StatusOK)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(taskID)) {
		t.Fatalf("history missing task id: %s", w.Body.String())
	}
}

func TestNotFoundIncludesAPICatalog(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/unknown", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"apis"`)) {
		t.Fatalf("missing api catalog: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/v1/token/accounts?user_id=`)) {
		t.Fatalf("catalog missing user_id endpoint: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/v1/governance/report`)) {
		t.Fatalf("catalog missing governance report endpoint: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`/v1/chat/send`)) {
		t.Fatalf("catalog should not expose chat endpoints to agents: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/v1/governance/proposals/create`)) {
		t.Fatalf("catalog missing governance create endpoint: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/v1/colony/status`)) {
		t.Fatalf("catalog missing colony status endpoint: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`/api/gov/propose`)) {
		t.Fatalf("catalog should not include removed /api gov endpoint: %s", w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`/api/colony/status`)) {
		t.Fatalf("catalog should not include removed /api colony endpoint: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/v1/monitor/agents/overview`)) {
		t.Fatalf("catalog missing monitor overview endpoint: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/v1/monitor/meta`)) {
		t.Fatalf("catalog missing monitor meta endpoint: %s", w.Body.String())
	}
}

func TestMonitorOverviewTimelineAndMeta(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	userID := seedActiveUser(t, srv)
	base := time.Now().UTC().Add(-20 * time.Second)

	if _, err := srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:    userID,
		CostType:  "tool.runtime.monitor",
		Amount:    7,
		Units:     3,
		MetaJSON:  `{"tool_id":"mail.send","result_ok":true}`,
		CreatedAt: base.Add(1 * time.Second),
	}); err != nil {
		t.Fatalf("append tool cost event: %v", err)
	}
	if _, err := srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:    userID,
		CostType:  "think.plan",
		Amount:    5,
		Units:     2,
		MetaJSON:  `{"input_units":12,"output_units":6}`,
		CreatedAt: base.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("append think cost event: %v", err)
	}
	if _, err := srv.store.SendMail(ctx, userID, []string{clawWorldSystemID}, "monitor-smoke", "runtime monitor smoke body"); err != nil {
		t.Fatalf("send mail: %v", err)
	}
	if _, err := srv.store.AppendChatMessage(ctx, store.ChatMessage{
		UserID: userID,
		From:   clawWorldSystemID,
		To:     userID,
		Body:   "monitor chat smoke",
		SentAt: base.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("append chat message: %v", err)
	}
	if _, err := srv.store.AppendRequestLog(ctx, store.RequestLog{
		Time:       base.Add(4 * time.Second),
		Method:     http.MethodPost,
		Path:       "/v1/tools/invoke",
		UserID:     userID,
		StatusCode: http.StatusOK,
		DurationMS: 111,
	}); err != nil {
		t.Fatalf("append request log: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/agents/overview?include_inactive=1&limit=20&event_limit=50&since_seconds=86400", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor overview status=%d body=%s", w.Code, w.Body.String())
	}
	var overview struct {
		Count int `json:"count"`
		Items []struct {
			UserID           string         `json:"user_id"`
			CurrentState     string         `json:"current_state"`
			LastActivityType string         `json:"last_activity_type"`
			ChatPipeline     map[string]any `json:"chat_pipeline"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatalf("unmarshal monitor overview response: %v", err)
	}
	if overview.Count == 0 || len(overview.Items) == 0 {
		t.Fatalf("monitor overview should return users: %s", w.Body.String())
	}
	foundUser := false
	for _, it := range overview.Items {
		if it.UserID != userID {
			continue
		}
		foundUser = true
		if strings.TrimSpace(it.CurrentState) == "" {
			t.Fatalf("monitor overview current_state should not be empty: %+v", it)
		}
		if it.ChatPipeline == nil {
			t.Fatalf("monitor overview should include chat pipeline: %+v", it)
		}
	}
	if !foundUser {
		t.Fatalf("monitor overview missing seeded user %s: %s", userID, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/agents/timeline?user_id="+userID+"&limit=80&event_limit=80&since_seconds=86400", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor timeline status=%d body=%s", w.Code, w.Body.String())
	}
	var timeline struct {
		Count int `json:"count"`
		Items []struct {
			UserID   string `json:"user_id"`
			Category string `json:"category"`
			Action   string `json:"action"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &timeline); err != nil {
		t.Fatalf("unmarshal monitor timeline response: %v", err)
	}
	if timeline.Count == 0 || len(timeline.Items) == 0 {
		t.Fatalf("monitor timeline should return events: %s", w.Body.String())
	}
	hasTool := false
	hasThink := false
	for _, it := range timeline.Items {
		if it.UserID != userID {
			t.Fatalf("timeline event user mismatch got=%q want=%q", it.UserID, userID)
		}
		switch it.Category {
		case "tool":
			hasTool = true
		case "think":
			hasThink = true
		}
	}
	if !hasTool {
		t.Fatalf("monitor timeline missing tool category event: %s", w.Body.String())
	}
	if !hasThink {
		t.Fatalf("monitor timeline missing think category event: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/agents/timeline?user_id="+userID+"&limit=1&event_limit=80&since_seconds=86400", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor timeline page1 status=%d body=%s", w.Code, w.Body.String())
	}
	var timelinePage1 struct {
		Count      int    `json:"count"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &timelinePage1); err != nil {
		t.Fatalf("unmarshal monitor timeline page1 response: %v", err)
	}
	if timelinePage1.Count != 1 {
		t.Fatalf("monitor timeline page1 count should be 1: %s", w.Body.String())
	}
	if strings.TrimSpace(timelinePage1.NextCursor) == "" {
		t.Fatalf("monitor timeline page1 should return next_cursor: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/agents/timeline?user_id="+userID+"&limit=1&event_limit=80&since_seconds=86400&cursor="+timelinePage1.NextCursor, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor timeline page2 status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/agents/timeline?user_id="+userID+"&limit=1&event_limit=80&since_seconds=86400&cursor=invalid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("monitor timeline invalid cursor format should fail, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/agents/timeline/all?include_inactive=1&user_limit=20&event_limit=80&limit=80&since_seconds=86400", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor timeline all status=%d body=%s", w.Code, w.Body.String())
	}
	var timelineAll struct {
		Count         int      `json:"count"`
		PartialErrors int      `json:"partial_errors"`
		SkippedUsers  []string `json:"skipped_users"`
		Items         []struct {
			UserID string `json:"user_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &timelineAll); err != nil {
		t.Fatalf("unmarshal monitor timeline all response: %v", err)
	}
	if timelineAll.Count == 0 || len(timelineAll.Items) == 0 {
		t.Fatalf("monitor timeline all should return events: %s", w.Body.String())
	}
	seenUserInAll := false
	for _, it := range timelineAll.Items {
		if it.UserID == userID {
			seenUserInAll = true
			break
		}
	}
	if !seenUserInAll {
		t.Fatalf("monitor timeline all missing seeded user %s: %s", userID, w.Body.String())
	}
	if timelineAll.PartialErrors != 0 {
		t.Fatalf("monitor timeline all should not have partial errors in unit tests: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/agents/timeline", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("monitor timeline without user_id should fail, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/meta", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor meta status=%d body=%s", w.Code, w.Body.String())
	}
	var meta struct {
		Sources map[string]struct {
			Status string `json:"status"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatalf("unmarshal monitor meta response: %v", err)
	}
	if meta.Sources["bots"].Status != "ok" {
		t.Fatalf("monitor meta bots source should be ok: %s", w.Body.String())
	}
	if meta.Sources["openclaw_status"].Status != "unavailable" {
		t.Fatalf("monitor meta openclaw_status should be unavailable in unit tests: %s", w.Body.String())
	}
}

func TestMonitorCommunications(t *testing.T) {
	type commParty struct {
		UserID      string `json:"user_id"`
		Username    string `json:"username"`
		Nickname    string `json:"nickname"`
		DisplayName string `json:"display_name"`
	}
	type commItem struct {
		MessageID int64       `json:"message_id"`
		Subject   string      `json:"subject"`
		Body      string      `json:"body"`
		FromUser  commParty   `json:"from_user"`
		ToUsers   []commParty `json:"to_users"`
	}

	srv := newTestServer()
	ctx := context.Background()
	senderID := seedActiveUser(t, srv)
	recipientA := seedActiveUser(t, srv)
	recipientB := seedActiveUser(t, srv)

	if _, err := srv.store.UpdateBotNickname(ctx, senderID, "发件虾"); err != nil {
		t.Fatalf("update sender nickname: %v", err)
	}
	if _, err := srv.store.UpdateBotNickname(ctx, recipientB, "收件虾B"); err != nil {
		t.Fatalf("update recipient nickname: %v", err)
	}

	if _, err := srv.store.SendMail(ctx, senderID, []string{recipientA, recipientB}, "design sync", "body for both recipients"); err != nil {
		t.Fatalf("send grouped mail: %v", err)
	}
	if _, err := srv.store.SendMail(ctx, senderID, []string{recipientA}, "follow up", "direct body"); err != nil {
		t.Fatalf("send direct mail: %v", err)
	}
	if _, err := srv.store.SendMail(ctx, clawWorldSystemID, []string{senderID}, "system notice", "should stay hidden"); err != nil {
		t.Fatalf("send system mail: %v", err)
	}
	if _, err := srv.store.SendMail(ctx, senderID, []string{clawWorldSystemID}, "system target", "should stay hidden too"); err != nil {
		t.Fatalf("send system target mail: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/communications?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor communications status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Count int        `json:"count"`
		Items []commItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal monitor communications response: %v", err)
	}
	if resp.Count != 2 || len(resp.Items) != 2 {
		t.Fatalf("monitor communications should return 2 user messages, got=%d body=%s", resp.Count, w.Body.String())
	}
	bySubject := make(map[string]commItem, len(resp.Items))
	for _, item := range resp.Items {
		if strings.Contains(item.Subject, "system") {
			t.Fatalf("system mail should be excluded: %+v", item)
		}
		bySubject[item.Subject] = item
	}

	grouped, ok := bySubject["design sync"]
	if !ok {
		t.Fatalf("missing grouped message in response: %s", w.Body.String())
	}
	if grouped.Body != "body for both recipients" {
		t.Fatalf("grouped body mismatch: %+v", grouped)
	}
	if grouped.FromUser.UserID != senderID || grouped.FromUser.DisplayName != "发件虾" {
		t.Fatalf("sender display name should prefer nickname: %+v", grouped.FromUser)
	}
	if len(grouped.ToUsers) != 2 {
		t.Fatalf("grouped message should merge recipients, got=%d body=%s", len(grouped.ToUsers), w.Body.String())
	}
	gotRecipients := map[string]string{}
	for _, recipient := range grouped.ToUsers {
		gotRecipients[recipient.UserID] = recipient.DisplayName
	}
	if gotRecipients[recipientA] != recipientA {
		t.Fatalf("recipientA should fall back to username/user_id, got=%q", gotRecipients[recipientA])
	}
	if gotRecipients[recipientB] != "收件虾B" {
		t.Fatalf("recipientB should use nickname display name, got=%q", gotRecipients[recipientB])
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/communications?keyword=design", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor communications keyword status=%d body=%s", w.Code, w.Body.String())
	}
	var keywordResp struct {
		Count int        `json:"count"`
		Items []commItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &keywordResp); err != nil {
		t.Fatalf("unmarshal keyword response: %v", err)
	}
	if keywordResp.Count != 1 || len(keywordResp.Items) != 1 || keywordResp.Items[0].Subject != "design sync" {
		t.Fatalf("keyword filter should keep only grouped message: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/communications?limit=1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor communications page1 status=%d body=%s", w.Code, w.Body.String())
	}
	var page1 struct {
		Count      int    `json:"count"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page1); err != nil {
		t.Fatalf("unmarshal page1 response: %v", err)
	}
	if page1.Count != 1 || strings.TrimSpace(page1.NextCursor) == "" {
		t.Fatalf("page1 should return one item and next_cursor: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/communications?limit=1&cursor="+page1.NextCursor, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("monitor communications page2 status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/communications?cursor=bad", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("monitor communications invalid cursor should fail, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/monitor/communications?from=bad-time", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("monitor communications invalid from should fail, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDashboardMonitorPage(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/dashboard/monitor", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard monitor page status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte("Agent Overview")) {
		t.Fatalf("dashboard monitor page missing Agent Overview section: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`/v1/monitor/agents/overview`)) {
		t.Fatalf("dashboard monitor page missing monitor API binding: %s", w.Body.String())
	}
}

func TestOpsOverviewEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	userA := seedActiveUser(t, srv)
	userB := seedActiveUser(t, srv)
	now := time.Now().UTC()

	// low token signal for action ownership.
	if _, err := srv.store.Consume(ctx, userA, 900); err != nil {
		t.Fatalf("consume userA tokens: %v", err)
	}

	// output signal: applied KB proposal.
	applied, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    userA,
		Title:             "ops applied",
		Reason:            "ops",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 300,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "knowledge/ops",
		Title:      "ops entry",
		NewContent: "v1",
		DiffText:   "+ v1",
	})
	if err != nil {
		t.Fatalf("create applied proposal: %v", err)
	}
	if _, err := srv.store.CloseKBProposal(ctx, applied.ID, "approved", "ok", 1, 1, 0, 0, 1, now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("close applied proposal: %v", err)
	}
	if _, _, err := srv.store.ApplyKBProposal(ctx, applied.ID, userA, now.Add(-90*time.Minute)); err != nil {
		t.Fatalf("apply proposal: %v", err)
	}

	// risk/action signal: approved but not applied.
	stalled, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    userB,
		Title:             "ops approved pending apply",
		Reason:            "ops",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 300,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "knowledge/ops",
		Title:      "ops pending",
		NewContent: "v1",
		DiffText:   "+ v1",
	})
	if err != nil {
		t.Fatalf("create stalled proposal: %v", err)
	}
	if _, err := srv.store.CloseKBProposal(ctx, stalled.ID, "approved", "pending apply", 1, 1, 0, 0, 1, now.Add(-3*time.Hour)); err != nil {
		t.Fatalf("close stalled proposal: %v", err)
	}

	// output signal: closed collab.
	collab, err := srv.store.CreateCollabSession(ctx, store.CollabSession{
		CollabID:       "collab-ops-smoke",
		Title:          "ops collab",
		Goal:           "close",
		Complexity:     "normal",
		Phase:          "recruiting",
		ProposerUserID: userA,
		MinMembers:     2,
		MaxMembers:     3,
	})
	if err != nil {
		t.Fatalf("create collab: %v", err)
	}
	closedAt := now.Add(-30 * time.Minute)
	if _, err := srv.store.UpdateCollabPhase(ctx, collab.CollabID, "closed", userA, "done", &closedAt); err != nil {
		t.Fatalf("close collab: %v", err)
	}

	// output signal: mailbox activity.
	if _, err := srv.store.SendMail(ctx, userA, []string{userB}, "ops-overview-smoke", "hello"); err != nil {
		t.Fatalf("send mail: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/ops/overview?window=both&include_inactive=1&limit=80", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ops overview status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Window   string `json:"window"`
		Snapshot struct {
			Users struct {
				Total    int `json:"total"`
				LowToken int `json:"low_token"`
			} `json:"users"`
			OpenRiskCount int `json:"open_risk_count"`
		} `json:"snapshot"`
		Windows map[string]struct {
			OutputTotal int `json:"output_total"`
			RiskCount   int `json:"risk_count"`
			ActionCount int `json:"action_count"`
			Actions     []struct {
				OwnerUserID string `json:"owner_user_id"`
				Type        string `json:"type"`
				Priority    string `json:"priority"`
			} `json:"actions"`
			Ownership []struct {
				UserID string `json:"user_id"`
				Total  int    `json:"total"`
			} `json:"ownership"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal ops overview: %v", err)
	}
	if resp.Window != "both" {
		t.Fatalf("window=%q, want both", resp.Window)
	}
	if resp.Snapshot.Users.Total < 2 {
		t.Fatalf("unexpected user total: %d", resp.Snapshot.Users.Total)
	}
	if resp.Snapshot.Users.LowToken <= 0 {
		t.Fatalf("expected low token users > 0: %s", w.Body.String())
	}
	if resp.Snapshot.OpenRiskCount <= 0 {
		t.Fatalf("expected open risks > 0: %s", w.Body.String())
	}

	win24, ok := resp.Windows["24h"]
	if !ok {
		t.Fatalf("missing 24h window: %s", w.Body.String())
	}
	if win24.OutputTotal <= 0 {
		t.Fatalf("expected 24h output_total > 0: %s", w.Body.String())
	}
	if win24.RiskCount <= 0 || win24.ActionCount <= 0 {
		t.Fatalf("expected 24h risk/action counts > 0: %s", w.Body.String())
	}
	foundPendingApply := false
	for _, it := range win24.Actions {
		if strings.Contains(it.Type, "approved_not_applied") {
			foundPendingApply = true
		}
	}
	if !foundPendingApply {
		t.Fatalf("expected approved_not_applied action: %s", w.Body.String())
	}
	foundOwner := false
	for _, it := range win24.Ownership {
		if strings.TrimSpace(it.UserID) == userA || strings.TrimSpace(it.UserID) == userB {
			foundOwner = true
		}
	}
	if !foundOwner {
		t.Fatalf("expected user ownership in 24h window: %s", w.Body.String())
	}

	if _, ok := resp.Windows["7d"]; !ok {
		t.Fatalf("missing 7d window: %s", w.Body.String())
	}
}

func TestOpsOverviewRejectsInvalidWindow(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/ops/overview?window=1d", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid window status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOpsProductOverviewEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	userA := seedActiveUser(t, srv)
	userB := seedActiveUser(t, srv)
	now := time.Now().UTC()

	applied, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    userA,
		Title:             "product ops applied",
		Reason:            "ops",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 300,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "product/ops",
		Title:      "Town Delivery Track",
		NewContent: "v1",
		DiffText:   "+ v1",
	})
	if err != nil {
		t.Fatalf("create applied proposal: %v", err)
	}
	if _, err := srv.store.CloseKBProposal(ctx, applied.ID, "approved", "ok", 1, 1, 0, 0, 1, now.Add(-90*time.Minute)); err != nil {
		t.Fatalf("close applied proposal: %v", err)
	}
	if _, _, err := srv.store.ApplyKBProposal(ctx, applied.ID, userA, now.Add(-60*time.Minute)); err != nil {
		t.Fatalf("apply proposal: %v", err)
	}

	if _, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    userB,
		Title:             "governance discussing",
		Reason:            "ops",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 300,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "governance/dev-preview",
		Title:      "Dev preview first",
		NewContent: "policy",
		DiffText:   "+ policy",
	}); err != nil {
		t.Fatalf("create governance proposal: %v", err)
	}

	if _, err := srv.store.CreateGanglion(ctx, store.Ganglion{
		Name:           "ops ganglion",
		GanglionType:   "method",
		Description:    "desc",
		Implementation: "impl",
		Validation:     "pass",
		AuthorUserID:   userA,
		Temporality:    "persistent",
		LifeState:      "validated",
	}); err != nil {
		t.Fatalf("create ganglion: %v", err)
	}

	collab, err := srv.store.CreateCollabSession(ctx, store.CollabSession{
		CollabID:       "collab-ops-product",
		Title:          "ops collab",
		Goal:           "close",
		Complexity:     "normal",
		Phase:          "recruiting",
		ProposerUserID: userB,
		MinMembers:     2,
		MaxMembers:     3,
	})
	if err != nil {
		t.Fatalf("create collab: %v", err)
	}
	closedAt := now.Add(-30 * time.Minute)
	if _, err := srv.store.UpdateCollabPhase(ctx, collab.CollabID, "closed", userB, "done", &closedAt); err != nil {
		t.Fatalf("close collab: %v", err)
	}

	if _, err := srv.store.SendMail(ctx, userA, []string{userB}, "ops-product-overview", "hello"); err != nil {
		t.Fatalf("send user mail: %v", err)
	}
	if _, err := srv.store.SendMail(ctx, clawWorldSystemID, []string{userA}, "ops-product-overview-system", "hello"); err != nil {
		t.Fatalf("send system mail: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/ops/product-overview?window=24h&include_inactive=1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ops product overview status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Window string `json:"window"`
		Global struct {
			OutputTotal     int `json:"output_total"`
			OutputCoreTotal int `json:"output_core_total"`
		} `json:"global"`
		Sections []struct {
			Module       string         `json:"module"`
			Totals       map[string]int `json:"totals"`
			WindowOutput map[string]int `json:"window_output"`
		} `json:"sections"`
		TopContributors map[string][]struct {
			UserID   string `json:"user_id"`
			Username string `json:"username"`
			Nickname string `json:"nickname"`
			Count    int    `json:"count"`
		} `json:"top_contributors_by_module"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal ops product overview: %v", err)
	}
	if resp.Window != "24h" {
		t.Fatalf("window=%q, want 24h", resp.Window)
	}
	if resp.Global.OutputCoreTotal <= 0 {
		t.Fatalf("expected core output > 0: %s", w.Body.String())
	}
	if resp.Global.OutputTotal < resp.Global.OutputCoreTotal {
		t.Fatalf("output_total should include core output: %s", w.Body.String())
	}

	seen := map[string]bool{}
	for _, sec := range resp.Sections {
		seen[sec.Module] = true
		if sec.Module == "kb" && sec.WindowOutput["kb_applied"] <= 0 {
			t.Fatalf("expected kb_applied > 0: %s", w.Body.String())
		}
		if sec.Module == "mail" && sec.Totals["fetched_count"] <= 0 {
			t.Fatalf("expected fetched_count > 0: %s", w.Body.String())
		}
	}
	required := []string{"kb", "governance", "ganglia", "bounty", "collab", "tools", "mail"}
	for _, key := range required {
		if !seen[key] {
			t.Fatalf("missing section %q: %s", key, w.Body.String())
		}
	}
	if len(resp.TopContributors["mail"]) == 0 {
		t.Fatalf("expected top contributors for mail: %s", w.Body.String())
	}
	for _, it := range resp.TopContributors["mail"] {
		if strings.TrimSpace(it.UserID) == "" {
			t.Fatalf("mail contributor user_id should not be empty: %+v", it)
		}
		if strings.TrimSpace(it.Username) == "" {
			t.Fatalf("mail contributor username should not be empty: %+v", it)
		}
	}

	w30 := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/ops/product-overview?window=30d&include_inactive=1", nil)
	if w30.Code != http.StatusOK {
		t.Fatalf("ops product overview 30d status=%d body=%s", w30.Code, w30.Body.String())
	}
	var resp30 struct {
		Window string `json:"window"`
	}
	if err := json.Unmarshal(w30.Body.Bytes(), &resp30); err != nil {
		t.Fatalf("unmarshal ops product overview 30d: %v", err)
	}
	if resp30.Window != "30d" {
		t.Fatalf("window=%q, want 30d", resp30.Window)
	}
}

func TestOpsProductOverviewRejectsInvalidWindow(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/ops/product-overview?window=1d", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid window status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildMailInsightLowSample(t *testing.T) {
	cn := buildMailInsightCN([]opsProductContributor{{UserID: "u1", Count: 2}})
	if !strings.Contains(cn, "样本较少") {
		t.Fatalf("unexpected cn insight for low sample: %q", cn)
	}
	en := buildMailInsightEN([]opsProductContributor{{UserID: "u1", Count: 2}})
	if !strings.Contains(strings.ToLower(en), "too small") {
		t.Fatalf("unexpected en insight for low sample: %q", en)
	}
}

func TestDashboardOpsPage(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/dashboard/ops", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard ops page status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte("ClawColony 产出")) {
		t.Fatalf("dashboard ops page missing product heading: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`/v1/ops/product-overview`)) {
		t.Fatalf("dashboard ops page missing product overview API binding: %s", w.Body.String())
	}
}

func TestAPICompatibilityRoutes(t *testing.T) {
	srv := newTestServer()
	register := func(provider string) string {
		t.Helper()
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": provider})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal register response: %v", err)
		}
		return body["item"].(map[string]any)["user_id"].(string)
	}

	userA := register("openclaw")
	userB := register("openclaw")

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/balance?user_id="+userA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/token/balance status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance"`)) {
		t.Fatalf("/v1/token/balance missing balance: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/transfer", map[string]any{
		"from_user_id": userA,
		"to_user_id":   userB,
		"amount":       5,
		"memo":         "compat-transfer",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/token/transfer status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/ganglia/forge", map[string]any{
		"user_id":        userA,
		"name":           "compat-ganglion",
		"type":           "survival",
		"description":    "compat ganglion",
		"implementation": "always check inbox and token balance",
		"validation":     "smoke",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/ganglia/forge status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/ganglia/browse?limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/ganglia/browse status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`compat-ganglion`)) {
		t.Fatalf("/v1/ganglia/browse missing forged item: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/library/publish", map[string]any{
		"user_id":  userA,
		"title":    "compat-library-note",
		"content":  "library publish from api compatibility layer",
		"category": "engineering",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/library/publish status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/library/search?query=compat-library-note&limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/library/search status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`compat-library-note`)) {
		t.Fatalf("/v1/library/search missing publish result: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/life/metamorphose", map[string]any{
		"user_id": userA,
		"changes": map[string]any{
			"focus": "optimize cooperation",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/life/metamorphose status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/life/set-will", map[string]any{
		"user_id": userA,
		"beneficiaries": []map[string]any{
			{
				"user_id": userB,
				"ratio":   10000,
			},
		},
		"tool_heirs": []string{userB},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/life/set-will status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/life/hibernate", map[string]any{
		"user_id": userA,
		"reason":  "compat-test",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/life/hibernate status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/life/wake", map[string]any{
		"user_id": userA,
		"reason":  "compat-wake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/life/wake status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/colony/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/colony/status status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"population"`)) {
		t.Fatalf("/v1/colony/status missing population: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/colony/directory", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/colony/directory status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(userA)) {
		t.Fatalf("/v1/colony/directory missing userA: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/colony/chronicle?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/colony/chronicle status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRemovedAPICompatRoutesReturn404(t *testing.T) {
	srv := newTestServer()
	cases := []struct {
		method string
		path   string
		body   map[string]any
	}{
		{method: http.MethodPost, path: "/api/mail/send", body: map[string]any{"from": "a", "to": "b", "subject": "x", "body": "y"}},
		{method: http.MethodPost, path: "/api/mail/send-list", body: map[string]any{"from_user_id": "a", "list_id": "x", "subject": "s", "body": "b"}},
		{method: http.MethodGet, path: "/api/mail/inbox?user_id=test"},
		{method: http.MethodPost, path: "/api/mail/list/create", body: map[string]any{"owner_user_id": "a", "name": "n"}},
		{method: http.MethodPost, path: "/api/mail/list/join", body: map[string]any{"list_id": "l", "user_id": "a"}},
		{method: http.MethodGet, path: "/api/token/balance?user_id=test"},
		{method: http.MethodPost, path: "/api/token/transfer", body: map[string]any{"from_user_id": "a", "to_user_id": "b", "amount": 1}},
		{method: http.MethodPost, path: "/api/tools/invoke", body: map[string]any{"user_id": "a", "tool_id": "t", "params": map[string]any{}}},
		{method: http.MethodPost, path: "/api/tools/register", body: map[string]any{"user_id": "a", "tool_id": "t", "name": "tool"}},
		{method: http.MethodGet, path: "/api/tools/search?query=test"},
		{method: http.MethodPost, path: "/api/life/set-will", body: map[string]any{"user_id": "a", "beneficiaries": []map[string]any{{"user_id": "b", "ratio": 10000}}}},
		{method: http.MethodPost, path: "/api/life/hibernate", body: map[string]any{"user_id": "a", "reason": "x"}},
		{method: http.MethodPost, path: "/api/life/wake", body: map[string]any{"user_id": "a", "reason": "x"}},
		{method: http.MethodPost, path: "/api/ganglia/forge", body: map[string]any{"user_id": "a", "name": "g", "implementation": "i"}},
		{method: http.MethodGet, path: "/api/ganglia/browse?limit=10"},
		{method: http.MethodPost, path: "/api/ganglia/integrate", body: map[string]any{"user_id": "a", "ganglion_id": 1}},
		{method: http.MethodPost, path: "/api/ganglia/rate", body: map[string]any{"user_id": "a", "ganglion_id": 1, "score": 5}},
		{method: http.MethodPost, path: "/api/bounty/post", body: map[string]any{"poster_user_id": "a", "description": "d", "reward": 1}},
		{method: http.MethodGet, path: "/api/bounty/list"},
		{method: http.MethodPost, path: "/api/bounty/verify", body: map[string]any{"bounty_id": 1, "approver_user_id": "a", "approved": true}},
		{method: http.MethodGet, path: "/api/metabolism/score?content_id=1"},
		{method: http.MethodPost, path: "/api/metabolism/supersede", body: map[string]any{"content_id": "c", "reason": "r"}},
		{method: http.MethodPost, path: "/api/metabolism/dispute", body: map[string]any{"content_id": "c", "reason": "r"}},
		{method: http.MethodGet, path: "/api/metabolism/report"},
		{method: http.MethodPost, path: "/api/gov/propose", body: map[string]any{"user_id": "a", "title": "t", "content": "c"}},
		{method: http.MethodPost, path: "/api/gov/cosign", body: map[string]any{"user_id": "a", "proposal_id": 1}},
		{method: http.MethodPost, path: "/api/gov/vote", body: map[string]any{"user_id": "a", "proposal_id": 1, "choice": "yes"}},
		{method: http.MethodPost, path: "/api/gov/report", body: map[string]any{"user_id": "a", "target_id": "b", "reason": "r"}},
		{method: http.MethodGet, path: "/api/gov/laws"},
		{method: http.MethodPost, path: "/api/library/publish", body: map[string]any{"user_id": "a", "title": "t", "content": "c"}},
		{method: http.MethodGet, path: "/api/library/search?query=test"},
		{method: http.MethodPost, path: "/api/life/metamorphose", body: map[string]any{"user_id": "a", "changes": map[string]any{"x": "y"}}},
		{method: http.MethodGet, path: "/api/colony/status"},
		{method: http.MethodGet, path: "/api/colony/directory"},
		{method: http.MethodGet, path: "/api/colony/chronicle"},
		{method: http.MethodGet, path: "/api/colony/banished"},
	}

	for _, tc := range cases {
		w := doJSONRequest(t, srv.mux, tc.method, tc.path, tc.body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestAPIGovProposeCosignVoteAndLaws(t *testing.T) {
	srv := newTestServer()
	register := func(provider string) string {
		t.Helper()
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": provider})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal register response: %v", err)
		}
		return body["item"].(map[string]any)["user_id"].(string)
	}
	userA := register("openclaw")
	userB := register("openclaw")

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/proposals/create", map[string]any{
		"user_id": userA,
		"title":   "compat-governance-proposal",
		"type":    "policy",
		"reason":  "compat test",
		"content": "governance content for compat vote flow",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/governance/proposals/create status=%d body=%s", w.Code, w.Body.String())
	}
	var proposeResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &proposeResp); err != nil {
		t.Fatalf("unmarshal propose response: %v", err)
	}
	proposalID := int64(proposeResp["proposal"].(map[string]any)["id"].(float64))
	if proposalID <= 0 {
		t.Fatalf("invalid proposal id: %v", proposeResp)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/proposals/cosign", map[string]any{
		"user_id":     userB,
		"proposal_id": proposalID,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/governance/proposals/cosign status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/start-vote", map[string]any{
		"user_id":     userA,
		"proposal_id": proposalID,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start-vote status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/proposals/vote", map[string]any{
		"user_id":     userB,
		"proposal_id": proposalID,
		"choice":      "yes",
		"reason":      "looks good",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/v1/governance/proposals/vote status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/governance/laws", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/v1/governance/laws status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"law_key"`)) {
		t.Fatalf("/v1/governance/laws missing law_key: %s", w.Body.String())
	}
}

func TestWorldTickIncludesGenesisSemanticSteps(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}

	tickID := srv.runWorldTickWithTrigger(context.Background(), "manual", 0)
	if tickID <= 0 {
		t.Fatalf("run world tick failed")
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/steps?tick_id="+strconv.FormatInt(tickID, 10)+"&limit=200", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("tick steps status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	steps := []string{
		`"step_name":"life_cost_drain"`,
		`"step_name":"token_drain"`,
		`"step_name":"dying_mark_check"`,
		`"step_name":"life_state_transition"`,
		`"step_name":"low_energy_alert"`,
		`"step_name":"death_grace_check"`,
		`"step_name":"mail_delivery"`,
		`"step_name":"wake_lobsters_inbox_notice"`,
		`"step_name":"autonomy_reminder"`,
		`"step_name":"community_comm_reminder"`,
		`"step_name":"agent_action_window"`,
		`"step_name":"collect_outbox"`,
		`"step_name":"repo_sync"`,
		`"step_name":"metabolism_cycle"`,
		`"step_name":"evolution_alert_notify"`,
		`"step_name":"tick_event_log"`,
	}
	for _, step := range steps {
		if !bytes.Contains(body, []byte(step)) {
			t.Fatalf("missing step %s in tick steps: %s", step, w.Body.String())
		}
	}
}

func TestAutonomyReminderTickPeriodicMail(t *testing.T) {
	srv := newTestServer()
	srv.cfg.AutonomyReminderIntervalTicks = 2
	userID := seedActiveUser(t, srv)

	// tick=1, interval=2 => no reminder
	srv.runWorldTick(context.Background())
	inbox, err := srv.store.ListMailbox(context.Background(), userID, "inbox", "", "[AUTONOMY-LOOP][PRIORITY:P3]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox after tick1: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("expected no autonomy reminder on tick1, got %d", len(inbox))
	}

	// tick=2 => send reminder
	srv.runWorldTick(context.Background())
	inbox, err = srv.store.ListMailbox(context.Background(), userID, "inbox", "", "[AUTONOMY-LOOP][PRIORITY:P3]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox after tick2: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("expected 1 autonomy reminder on tick2, got %d", len(inbox))
	}
	if !strings.Contains(inbox[0].Body, "状态触发自治提醒") || !strings.Contains(inbox[0].Body, "lookback=") {
		t.Fatalf("unexpected autonomy reminder body: %s", inbox[0].Body)
	}
}

func TestAutonomyReminderTickSkipsWhenRecentMeaningfulOutbox(t *testing.T) {
	srv := newTestServer()
	srv.cfg.AutonomyReminderIntervalTicks = 2
	userID := seedActiveUser(t, srv)

	if _, err := srv.store.SendMail(context.Background(), userID, []string{clawWorldSystemID}, "autonomy-loop/progress", "result=done\nevidence=proposal_id=101\nnext=submit vote"); err != nil {
		t.Fatalf("seed outbox progress mail: %v", err)
	}

	srv.runWorldTick(context.Background()) // tick=1
	srv.runWorldTick(context.Background()) // tick=2 (due)

	inbox, err := srv.store.ListMailbox(context.Background(), userID, "inbox", "", "[AUTONOMY-LOOP][PRIORITY:P3]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox after tick2: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("expected no autonomy reminder due to recent meaningful outbox, got %d", len(inbox))
	}
}

func TestRepoSyncWritesSnapshotAndRedactsSecrets(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ColonyRepoSync = true
	srv.cfg.ColonyRepoLocalPath = t.TempDir()
	srv.cfg.ColonyRepoBranch = "main"
	srv.cfg.ColonyRepoURL = ""

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)

	secretContent := "credential: sk-proj-very-sensitive-token"
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/library/publish", map[string]any{
		"user_id":  userID,
		"title":    "secret-doc",
		"content":  secretContent,
		"category": "security",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("publish status=%d body=%s", w.Code, w.Body.String())
	}

	tickID := srv.runWorldTickWithTrigger(context.Background(), "manual", 0)
	if tickID <= 0 {
		t.Fatalf("run world tick failed")
	}

	libPath := filepath.Join(srv.cfg.ColonyRepoLocalPath, "civilization/library/entries.json")
	raw, err := os.ReadFile(libPath)
	if err != nil {
		t.Fatalf("read snapshot file %s failed: %v", libPath, err)
	}
	if bytes.Contains(raw, []byte(secretContent)) {
		t.Fatalf("snapshot leaked secret content: %s", string(raw))
	}
	if !bytes.Contains(raw, []byte(redactedSecret)) {
		t.Fatalf("snapshot should contain redaction marker: %s", string(raw))
	}

	sysPath := filepath.Join(srv.cfg.ColonyRepoLocalPath, "civilization/system/genesis_state.json")
	if _, err := os.Stat(sysPath); err != nil {
		t.Fatalf("missing snapshot file %s: %v", sysPath, err)
	}
}

func TestMailOverviewIncludesClawWorldSystemAccount(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": "clawcolony-admin",
		"to_user_ids":  []string{userID},
		"subject":      "hello",
		"body":         "ping",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("mail send status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{"clawcolony-admin"},
		"subject":      "reply",
		"body":         "pong",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("mail send status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/mail/overview?folder=all&scope=all&limit=200", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("overview status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"owner_address":"clawcolony-admin"`)) {
		t.Fatalf("overview missing clawcolony mailbox entries: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"owner_address":"`+userID+`"`)) {
		t.Fatalf("overview missing user mailbox entries: %s", w.Body.String())
	}
}

func TestTokenBalanceEndpointIncludesCostSummary(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{"clawcolony-admin"},
		"subject":      "autonomy-loop/smoke",
		"body":         "proposal_id=1",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("mail send status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/balance?user_id="+userID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("token balance status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"cost_recent"`)) {
		t.Fatalf("token balance missing cost_recent: %s", w.Body.String())
	}
}

func TestMailRemindersAndAutoResolve(t *testing.T) {
	srv := newTestServer()
	userID := seedActiveUser(t, srv)
	var w *httptest.ResponseRecorder

	for _, sub := range []string{
		"[COMMUNITY-COLLAB][PINNED][PRIORITY:P1][ACTION:PROPOSAL] collab_id=collab-23 tick=23",
		"[KNOWLEDGEBASE-PROPOSAL][PINNED][PRIORITY:P1][ACTION:VOTE] #11 kb-topic",
	} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
			"from_user_id": "clawcolony-admin",
			"to_user_ids":  []string{userID},
			"subject":      sub,
			"body":         "smoke",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("send pinned status=%d body=%s", w.Code, w.Body.String())
		}
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/mail/reminders?user_id="+userID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("reminders status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"count":2`)) {
		t.Fatalf("reminders should include 2 pending items: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"kind":"community_collab"`)) ||
		!bytes.Contains(w.Body.Bytes(), []byte(`"kind":"knowledgebase_proposal"`)) {
		t.Fatalf("reminders should include community + knowledgebase items: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{"clawcolony-admin"},
		"subject":      "community-collab/23/" + userID,
		"body":         "result=ok evidence collab_id=collab-23 next=continue",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("community progress send status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"resolved_pinned_reminds"`)) {
		t.Fatalf("community progress send should include resolved count: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{"clawcolony-admin"},
		"subject":      "knowledgebase/11/" + userID,
		"body":         "result=ok evidence proposal_id=11 next=continue",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("knowledgebase progress send status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"resolved_pinned_reminds"`)) {
		t.Fatalf("knowledgebase progress send should include resolved count: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/mail/reminders?user_id="+userID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("reminders after resolve status=%d body=%s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte(`"kind":"community_collab"`)) ||
		bytes.Contains(w.Body.Bytes(), []byte(`"kind":"knowledgebase_proposal"`)) {
		t.Fatalf("pinned reminders should be auto-resolved: %s", w.Body.String())
	}
}

func TestMailMarkReadQueryAndContactsContext(t *testing.T) {
	srv := newTestServer()
	reg := func() string {
		return seedActiveUser(t, srv)
	}
	userA := reg()
	userB := reg()

	for i := 0; i < 2; i++ {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
			"from_user_id": "clawcolony-admin",
			"to_user_ids":  []string{userA},
			"subject":      "[KNOWLEDGEBASE-PROPOSAL][PRIORITY:P2][ACTION:ENROLL] #1 topic",
			"body":         "proposal_id=1",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("send enroll pinned status=%d body=%s", w.Code, w.Body.String())
		}
	}
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/mark-read-query", map[string]any{
		"user_id":        userA,
		"subject_prefix": "[KNOWLEDGEBASE-PROPOSAL][PRIORITY:P2][ACTION:ENROLL] #1",
		"limit":          50,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("mark-read-query status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"resolved":2`)) {
		t.Fatalf("mark-read-query expected resolved=2 body=%s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/contacts/upsert", map[string]any{
		"user_id":         userA,
		"contact_user_id": userB,
		"display_name":    "ally-b",
		"tags":            []string{"peer", "backend"},
		"role":            "reviewer",
		"skills":          []string{"debugging", "diff-review"},
		"current_project": "genesis-kb",
		"availability":    "online",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("contacts upsert status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/mail/contacts?user_id="+userA+"&limit=50", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("contacts list status=%d body=%s", w.Code, w.Body.String())
	}
	for _, token := range []string{`"role":"reviewer"`, `"current_project":"genesis-kb"`, `"availability":"online"`, `"skills":["debugging","diff-review"]`} {
		if !bytes.Contains(w.Body.Bytes(), []byte(token)) {
			t.Fatalf("contacts list missing %s in %s", token, w.Body.String())
		}
	}
}

func TestGangliaForgeIntegrateRateLifecycle(t *testing.T) {
	srv := newTestServer()
	registerUser := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}

	u1 := registerUser()
	u2 := registerUser()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/ganglia/forge", map[string]any{
		"user_id":        u1,
		"name":           "mail-discuss-then-vote",
		"type":           "governance",
		"description":    "先讨论再投票的治理执行模式",
		"implementation": "先通过 mailbox-network 收集意见，再发起投票",
		"validation":     "在 proposal-42 中缩短决策周期并提升参与率",
		"temporality":    "durable",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("forge status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var forgeResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &forgeResp); err != nil {
		t.Fatalf("unmarshal forge body: %v", err)
	}
	ganglionID := int64(forgeResp["item"].(map[string]any)["id"].(float64))
	if ganglionID <= 0 {
		t.Fatalf("invalid ganglion id: %v", forgeResp)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/ganglia/integrate", map[string]any{
		"user_id":     u2,
		"ganglion_id": ganglionID,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("integrate status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/ganglia/rate", map[string]any{
		"user_id":     u2,
		"ganglion_id": ganglionID,
		"score":       5,
		"feedback":    "执行稳定，收益明显",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("rate status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/ganglia/get?ganglion_id="+strconv.FormatInt(ganglionID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"life_state":"validated"`)) {
		t.Fatalf("expected validated life_state after first integration+rating: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"integrations_count":1`)) {
		t.Fatalf("expected integrations_count=1: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"score_count":1`)) {
		t.Fatalf("expected score_count=1: %s", w.Body.String())
	}
}

func TestDeadUserCannotForgeGanglion(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)
	if _, err := srv.store.UpsertUserLifeState(context.Background(), store.UserLifeState{
		UserID: userID,
		State:  "dead",
	}); err != nil {
		t.Fatalf("set dead state: %v", err)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/ganglia/forge", map[string]any{
		"user_id":     userID,
		"name":        "dead-user-should-fail",
		"type":        "survival",
		"description": "should fail",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("dead forge status = %d, want %d body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestPromptTemplateCRUDAndApply(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)

	custom := "## custom agents\nuser={{user_id}}\n"
	w = doJSONRequest(t, srv.mux, http.MethodPut, "/v1/prompts/templates/upsert", map[string]any{
		"key":     "agents_doc",
		"content": custom,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upsert status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/prompts/templates?user_id="+userID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("templates status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"key":"agents_doc"`)) {
		t.Fatalf("templates missing agents_doc: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"source":"db"`)) {
		t.Fatalf("templates missing db source: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/prompts/templates/apply", map[string]any{
		"user_id": userID,
		"image":   "openclaw:test-image",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"ok"`)) {
		t.Fatalf("apply missing ok status: %s", w.Body.String())
	}
}

func TestRuntimeProfileSeedDataIncludesMCPPluginKeys(t *testing.T) {
	profile := bot.RuntimeProfile{
		ProtocolReadme:           "protocol",
		AgentsDoc:                "agents",
		OpenClawConfig:           "{\"plugins\":{}}",
		CollabModeSkill:          "collab-skill",
		DevPreviewSkill:          "dev-preview-skill",
		UpgradeClawcolonySkill:   "upgrade-skill",
		KnowledgeBaseMCPManifest: "kb-manifest",
		KnowledgeBaseMCPPlugin:   "kb-plugin",
		CollabMCPManifest:        "collab-manifest",
		CollabMCPPlugin:          "collab-plugin",
		MailboxMCPManifest:       "mail-manifest",
		MailboxMCPPlugin:         "mail-plugin",
		TokenMCPManifest:         "token-manifest",
		TokenMCPPlugin:           "token-plugin",
		ToolsMCPManifest:         "tools-manifest",
		ToolsMCPPlugin:           "tools-plugin",
		GangliaMCPManifest:       "ganglia-manifest",
		GangliaMCPPlugin:         "ganglia-plugin",
		GovernanceMCPManifest:    "gov-manifest",
		GovernanceMCPPlugin:      "gov-plugin",
		DevPreviewMCPManifest:    "dev-manifest",
		DevPreviewMCPPlugin:      "dev-plugin",
	}
	data := runtimeProfileSeedData(profile)
	expect := map[string]string{
		"PROTOCOL_README.md":                "protocol",
		"AGENTS_DOC.md":                     "agents",
		"openclaw.json":                     "{\"plugins\":{}}",
		"COLLAB_MODE_SKILL":                 "collab-skill",
		"DEV_PREVIEW_SKILL":                 "dev-preview-skill",
		"UPGRADE_CLAWCOLONY_SKILL":          "upgrade-skill",
		"KNOWLEDGEBASE_MCP_PLUGIN_MANIFEST": "kb-manifest",
		"KNOWLEDGEBASE_MCP_PLUGIN_JS":       "kb-plugin",
		"COLLAB_MCP_PLUGIN_MANIFEST":        "collab-manifest",
		"COLLAB_MCP_PLUGIN_JS":              "collab-plugin",
		"MAILBOX_MCP_PLUGIN_MANIFEST":       "mail-manifest",
		"MAILBOX_MCP_PLUGIN_JS":             "mail-plugin",
		"TOKEN_MCP_PLUGIN_MANIFEST":         "token-manifest",
		"TOKEN_MCP_PLUGIN_JS":               "token-plugin",
		"TOOLS_MCP_PLUGIN_MANIFEST":         "tools-manifest",
		"TOOLS_MCP_PLUGIN_JS":               "tools-plugin",
		"GANGLIA_MCP_PLUGIN_MANIFEST":       "ganglia-manifest",
		"GANGLIA_MCP_PLUGIN_JS":             "ganglia-plugin",
		"GOVERNANCE_MCP_PLUGIN_MANIFEST":    "gov-manifest",
		"GOVERNANCE_MCP_PLUGIN_JS":          "gov-plugin",
		"DEV_PREVIEW_MCP_PLUGIN_MANIFEST":   "dev-manifest",
		"DEV_PREVIEW_MCP_PLUGIN_JS":         "dev-plugin",
	}
	for key, want := range expect {
		if got := data[key]; got != want {
			t.Fatalf("seed data %s = %q, want %q", key, got, want)
		}
	}
}

func TestPatchWorkspaceBootstrapScriptForMCP(t *testing.T) {
	raw := strings.Join([]string{
		"set -e",
		"[ -f /state/openclaw/openclaw.json ] || cp /seed/openclaw.json /state/openclaw/openclaw.json",
		"mkdir -p /state/openclaw/workspace/.openclaw/extensions/mcp-knowledgebase",
		"cp /seed/KNOWLEDGEBASE_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/mcp-knowledgebase/openclaw.plugin.json",
		"cp /seed/KNOWLEDGEBASE_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/mcp-knowledgebase/index.js",
		"          rm -f /state/openclaw/workspace/HEARTBEAT.md",
	}, "\n")

	patched, changed := patchWorkspaceBootstrapScriptForMCP(raw)
	if !changed {
		t.Fatalf("patchWorkspaceBootstrapScriptForMCP should report changed=true")
	}
	if strings.Contains(patched, "[ -f /state/openclaw/openclaw.json ]") {
		t.Fatalf("guarded openclaw.json copy should be removed: %s", patched)
	}
	if !strings.Contains(patched, "cp /seed/openclaw.json /state/openclaw/openclaw.json") {
		t.Fatalf("openclaw.json should be copied unconditionally: %s", patched)
	}
	if strings.Contains(patched, "/extensions/mcp-knowledgebase/") {
		t.Fatalf("legacy mcp-knowledgebase copy lines should be removed: %s", patched)
	}
	if !strings.Contains(patched, "rm -rf /state/openclaw/workspace/.openclaw/extensions/mcp-knowledgebase") {
		t.Fatalf("patched script missing legacy mcp cleanup")
	}
	for _, marker := range []string{
		"clawcolony-mcp-knowledgebase",
		"clawcolony-mcp-collab",
		"clawcolony-mcp-mailbox",
		"clawcolony-mcp-token",
		"clawcolony-mcp-tools",
		"clawcolony-mcp-ganglia",
		"clawcolony-mcp-governance",
		"clawcolony-mcp-dev-preview",
	} {
		if !strings.Contains(patched, marker) {
			t.Fatalf("patched script missing marker %q", marker)
		}
	}
	if !strings.Contains(patched, "/skills/dev-preview/SKILL.md") {
		t.Fatalf("patched script missing dev-preview skill copy block")
	}
	if !strings.Contains(patched, "cp /seed/DEV_PREVIEW_SKILL /state/openclaw/workspace/skills/dev-preview/SKILL.md") {
		t.Fatalf("patched script should keep intact dev-preview copy line")
	}
	if !strings.Contains(patched, "/skills/upgrade-clawcolony/SKILL.md") {
		t.Fatalf("patched script missing upgrade-clawcolony skill copy block")
	}
	insertPos := strings.Index(patched, "clawcolony-mcp-knowledgebase")
	heartbeatPos := strings.Index(patched, "rm -f /state/openclaw/workspace/HEARTBEAT.md")
	if insertPos == -1 || heartbeatPos == -1 || insertPos > heartbeatPos {
		t.Fatalf("mcp bootstrap block should be injected before HEARTBEAT cleanup: %s", patched)
	}

	patchedAgain, changedAgain := patchWorkspaceBootstrapScriptForMCP(patched)
	if changedAgain {
		t.Fatalf("patched script should be idempotent")
	}
	if patchedAgain != patched {
		t.Fatalf("patched script should stay stable across repeated patching")
	}

	withoutCleanup := strings.Join([]string{
		"set -e",
		"mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-knowledgebase",
		"cp /seed/KNOWLEDGEBASE_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-knowledgebase/openclaw.plugin.json",
		"cp /seed/KNOWLEDGEBASE_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-knowledgebase/index.js",
		"mkdir -p /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-collab",
		"cp /seed/COLLAB_MCP_PLUGIN_MANIFEST /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-collab/openclaw.plugin.json",
		"cp /seed/COLLAB_MCP_PLUGIN_JS /state/openclaw/workspace/.openclaw/extensions/clawcolony-mcp-collab/index.js",
		"          rm -f /state/openclaw/workspace/HEARTBEAT.md",
	}, "\n")
	patchedNoCleanup, changedNoCleanup := patchWorkspaceBootstrapScriptForMCP(withoutCleanup)
	if !changedNoCleanup {
		t.Fatalf("script missing legacy cleanup should be changed")
	}
	if strings.Count(patchedNoCleanup, "rm -rf /state/openclaw/workspace/.openclaw/extensions/mcp-knowledgebase") != 1 {
		t.Fatalf("legacy cleanup line should be injected once: %s", patchedNoCleanup)
	}
	patchedNoCleanupAgain, changedNoCleanupAgain := patchWorkspaceBootstrapScriptForMCP(patchedNoCleanup)
	if changedNoCleanupAgain {
		t.Fatalf("cleanup-injected script should be idempotent")
	}
	if patchedNoCleanupAgain != patchedNoCleanup {
		t.Fatalf("cleanup-injected script should stay stable on repatch")
	}
}

func TestPromptTemplateUpsertCanonicalizesPreviewUser(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)

	raw := "当前身份: user-example\nowner: " + userID + "\n"
	w = doJSONRequest(t, srv.mux, http.MethodPut, "/v1/prompts/templates/upsert", map[string]any{
		"key":             "soul_doc",
		"content":         raw,
		"preview_user_id": userID,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upsert status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("user-example")) {
		t.Fatalf("upsert response still contains user-example: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("{{user_id}}")) {
		t.Fatalf("upsert response missing placeholder: %s", w.Body.String())
	}
}

func TestCollabLifecycleFlow(t *testing.T) {
	srv := newTestServer()

	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}

	a := register()
	b := register()
	c := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/collab/propose", map[string]any{
		"proposer_user_id": a,
		"title":            "实现合作测试",
		"goal":             "完成一个可评审的交付",
		"complexity":       "high",
		"min_members":      2,
		"max_members":      3,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("propose status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var propose map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &propose)
	collabID := propose["item"].(map[string]any)["collab_id"].(string)

	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/collab/apply", map[string]any{
			"collab_id": collabID,
			"user_id":   uid,
			"pitch":     "I can help",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("apply status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/collab/assign", map[string]any{
		"collab_id":            collabID,
		"orchestrator_user_id": a,
		"assignments": []map[string]any{
			{"user_id": a, "role": "orchestrator"},
			{"user_id": b, "role": "executor"},
			{"user_id": c, "role": "reviewer"},
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("assign status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/collab/start", map[string]any{
		"collab_id":            collabID,
		"orchestrator_user_id": a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/collab/submit", map[string]any{
		"collab_id": collabID,
		"user_id":   b,
		"role":      "executor",
		"kind":      "code",
		"summary":   "完成可验收实现并提交共享证据",
		"content":   "result=新增协作执行路径\nverification=本地测试通过\nevidence=collab_id=" + collabID + "\nnext=等待 reviewer 评审",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var submit map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &submit)
	artifactID := int64(submit["item"].(map[string]any)["id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/collab/review", map[string]any{
		"collab_id":        collabID,
		"reviewer_user_id": c,
		"artifact_id":      artifactID,
		"status":           "accepted",
		"review_note":      "looks good",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("review status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/collab/close", map[string]any{
		"collab_id":              collabID,
		"orchestrator_user_id":   a,
		"result":                 "closed",
		"status_or_summary_note": "done",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("close status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/collab/events?collab_id="+collabID+"&limit=50", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("events status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"event_type":"collab.closed"`)) {
		t.Fatalf("events missing collab.closed: %s", w.Body.String())
	}
}

func TestKBProposalLifecycleSingleRound(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()
	c := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals", map[string]any{
		"proposer_user_id":    a,
		"title":               "新增基础协作约定",
		"reason":              "统一行为规范",
		"vote_threshold_pct":  80,
		"vote_window_seconds": 1,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "governance",
			"title":       "协作原则",
			"new_content": "先讨论后投票",
			"diff_text":   "+ 协作原则: 先讨论后投票",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create proposal status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	proposalID := int64(created["proposal"].(map[string]any)["id"].(float64))

	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/enroll", map[string]any{
			"proposal_id": proposalID,
			"user_id":     uid,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("enroll status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/start-vote", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	votingRevisionID := int64(start["proposal"].(map[string]any)["voting_revision_id"].(float64))
	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/ack", map[string]any{
			"proposal_id": proposalID,
			"revision_id": votingRevisionID,
			"user_id":     uid,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("ack status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/vote", map[string]any{
		"proposal_id": proposalID,
		"revision_id": votingRevisionID,
		"user_id":     b,
		"vote":        "abstain",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("abstain without reason status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/vote", map[string]any{
		"proposal_id": proposalID,
		"revision_id": votingRevisionID,
		"user_id":     b,
		"vote":        "abstain",
		"reason":      "当前变更收益不够明确",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("abstain vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/vote", map[string]any{
		"proposal_id": proposalID,
		"revision_id": votingRevisionID,
		"user_id":     c,
		"vote":        "yes",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("yes vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	time.Sleep(1200 * time.Millisecond)
	srv.kbTick(context.Background(), 1)

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(proposalID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"rejected"`)) {
		t.Fatalf("proposal should be rejected by low participation: %s", w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/thread?proposal_id="+strconv.FormatInt(proposalID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("thread status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("自动失败")) {
		t.Fatalf("thread missing auto-fail reason: %s", w.Body.String())
	}
}

func TestKBProposalApproveAndApply(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()
	c := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals", map[string]any{
		"proposer_user_id":    a,
		"title":               "新增术语",
		"reason":              "统一沟通词汇",
		"vote_threshold_pct":  80,
		"vote_window_seconds": 1,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "terms",
			"title":       "active user",
			"new_content": "已报名当前proposal的user",
			"diff_text":   "+ active user: 已报名当前proposal的user",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create proposal status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	proposalID := int64(created["proposal"].(map[string]any)["id"].(float64))

	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/enroll", map[string]any{
			"proposal_id": proposalID,
			"user_id":     uid,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("enroll status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/start-vote", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	votingRevisionID := int64(start["proposal"].(map[string]any)["voting_revision_id"].(float64))
	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/ack", map[string]any{
			"proposal_id": proposalID,
			"revision_id": votingRevisionID,
			"user_id":     uid,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("ack status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}
	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/vote", map[string]any{
			"proposal_id": proposalID,
			"revision_id": votingRevisionID,
			"user_id":     uid,
			"vote":        "yes",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}

	time.Sleep(1200 * time.Millisecond)
	srv.kbTick(context.Background(), 1)

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(proposalID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"applied"`)) {
		t.Fatalf("proposal should be auto-applied: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/apply", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"already_applied":true`)) {
		t.Fatalf("apply should be idempotent after auto-apply: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/entries?section=terms", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list entries status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"title":"active user"`)) {
		t.Fatalf("entries missing applied title: %s", w.Body.String())
	}
}

func TestKBAutoApplyAfterVotingDeadlineFinalize(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()
	c := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals", map[string]any{
		"proposer_user_id":    a,
		"title":               "截止自动apply测试",
		"reason":              "验证投票截止自动apply",
		"vote_threshold_pct":  50,
		"vote_window_seconds": 1,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "governance/test",
			"title":       "auto-apply-deadline",
			"new_content": "deadline finalize then auto apply",
			"diff_text":   "+ add auto apply on voting finalize deadline path",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create proposal status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	proposalID := int64(created["proposal"].(map[string]any)["id"].(float64))

	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/enroll", map[string]any{
			"proposal_id": proposalID,
			"user_id":     uid,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("enroll status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/start-vote", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	votingRevisionID := int64(start["proposal"].(map[string]any)["voting_revision_id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/ack", map[string]any{
		"proposal_id": proposalID,
		"revision_id": votingRevisionID,
		"user_id":     b,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("ack status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/vote", map[string]any{
		"proposal_id": proposalID,
		"revision_id": votingRevisionID,
		"user_id":     b,
		"vote":        "yes",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	time.Sleep(1200 * time.Millisecond)
	srv.kbTick(context.Background(), 1)

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(proposalID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"applied"`)) {
		t.Fatalf("proposal should be auto-applied after deadline finalize: %s", w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/entries?section=governance/test", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list entries status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"title":"auto-apply-deadline"`)) {
		t.Fatalf("entries missing auto applied title: %s", w.Body.String())
	}
}

func TestKBSectionsEndpoint(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()
	c := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals", map[string]any{
		"proposer_user_id":    a,
		"title":               "新增章节条目",
		"reason":              "测试 sections 接口",
		"vote_threshold_pct":  80,
		"vote_window_seconds": 300,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "playbook",
			"title":       "协作守则",
			"new_content": "统一使用共享知识库",
			"diff_text":   "+ [playbook] 协作守则",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create proposal status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	proposalID := int64(created["proposal"].(map[string]any)["id"].(float64))

	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/enroll", map[string]any{
			"proposal_id": proposalID,
			"user_id":     uid,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("enroll status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/start-vote", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	votingRevisionID := int64(start["proposal"].(map[string]any)["voting_revision_id"].(float64))
	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/ack", map[string]any{
			"proposal_id": proposalID,
			"revision_id": votingRevisionID,
			"user_id":     uid,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("ack status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}
	for _, uid := range []string{b, c} {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/vote", map[string]any{
			"proposal_id": proposalID,
			"revision_id": votingRevisionID,
			"user_id":     uid,
			"vote":        "yes",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/apply", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/sections?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("kb sections status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"section":"playbook"`)) {
		t.Fatalf("kb sections missing expected section: %s", w.Body.String())
	}
}

func TestKBRevisionAndAckFlow(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals", map[string]any{
		"proposer_user_id":    a,
		"title":               "修订测试",
		"reason":              "验证 revision 约束",
		"vote_threshold_pct":  80,
		"vote_window_seconds": 120,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "test",
			"title":       "entry",
			"new_content": "v1",
			"diff_text":   "+ add entry v1 for revision baseline",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create proposal status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	proposal := created["proposal"].(map[string]any)
	proposalID := int64(proposal["id"].(float64))
	baseRevisionID := int64(proposal["current_revision_id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/revise", map[string]any{
		"proposal_id":               proposalID,
		"base_revision_id":          baseRevisionID,
		"user_id":                   b,
		"discussion_window_seconds": 60,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "test",
			"title":       "entry",
			"new_content": "v2",
			"diff_text":   "~ update entry content from v1 to v2 with details",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("revise status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var revised map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &revised)
	newRevisionID := int64(revised["revision"].(map[string]any)["id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/comment", map[string]any{
		"proposal_id": proposalID,
		"revision_id": baseRevisionID,
		"user_id":     b,
		"content":     "old revision is stale and should be rejected",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("comment old revision status = %d, want %d body=%s", w.Code, http.StatusConflict, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/start-vote", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start vote status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var started map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	votingRevisionID := int64(started["proposal"].(map[string]any)["voting_revision_id"].(float64))
	if votingRevisionID != newRevisionID {
		t.Fatalf("voting revision = %d, want %d", votingRevisionID, newRevisionID)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/vote", map[string]any{
		"proposal_id": proposalID,
		"revision_id": votingRevisionID,
		"user_id":     b,
		"vote":        "yes",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("vote without ack status = %d, want %d body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestKBAutoProgressDiscussingNoEnrollmentRejects(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals", map[string]any{
		"proposer_user_id":          a,
		"title":                     "自动推进-无人报名",
		"reason":                    "测试讨论超时自动失败",
		"vote_threshold_pct":        80,
		"vote_window_seconds":       120,
		"discussion_window_seconds": 1,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "governance/test",
			"title":       "auto-reject",
			"new_content": "v1",
			"diff_text":   "+ add auto-reject sample content for governance test",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create proposal status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	proposalID := int64(created["proposal"].(map[string]any)["id"].(float64))

	time.Sleep(1200 * time.Millisecond)
	srv.kbTick(context.Background(), 1)

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(proposalID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"status":"rejected"`)) {
		t.Fatalf("expected rejected after discussion timeout: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte("讨论期截止且无人报名")) {
		t.Fatalf("expected no-enrollment reason: %s", w.Body.String())
	}
}

func TestKBAutoProgressDiscussingStartsVote(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals", map[string]any{
		"proposer_user_id":          a,
		"title":                     "自动推进-转投票",
		"reason":                    "测试讨论超时自动开票",
		"vote_threshold_pct":        80,
		"vote_window_seconds":       120,
		"discussion_window_seconds": 1,
		"change": map[string]any{
			"op_type":     "add",
			"section":     "governance/test",
			"title":       "auto-voting",
			"new_content": "v1",
			"diff_text":   "+ add auto-voting sample content for governance test",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create proposal status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	proposalID := int64(created["proposal"].(map[string]any)["id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/enroll", map[string]any{
		"proposal_id": proposalID,
		"user_id":     b,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("enroll status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	time.Sleep(1200 * time.Millisecond)
	srv.kbTick(context.Background(), 1)

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(proposalID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"status":"voting"`)) {
		t.Fatalf("expected auto start voting: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"voting_revision_id":0`)) {
		t.Fatalf("expected voting revision id to be set: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"voting_deadline_at":"`)) {
		t.Fatalf("expected voting deadline set: %s", w.Body.String())
	}
}

func TestKBAutoProgressDiscussingLegacyNilDeadlineStartsVote(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()

	ctx := context.Background()
	proposal, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    a,
		Title:             "legacy-nil-deadline",
		Reason:            "regression",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 120,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "governance/test",
		Title:      "legacy",
		NewContent: "v1",
		DiffText:   "+ add legacy nil deadline regression case",
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}
	if proposal.DiscussionDeadlineAt != nil {
		t.Fatalf("expected legacy proposal to have nil discussion deadline")
	}
	if _, err := srv.store.EnrollKBProposal(ctx, proposal.ID, b); err != nil {
		t.Fatalf("enroll status: %v", err)
	}

	srv.kbTick(ctx, 1)

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(proposal.ID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"status":"voting"`)) {
		t.Fatalf("expected legacy nil deadline proposal auto starts voting: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"voting_revision_id":0`)) {
		t.Fatalf("expected voting revision id to be set: %s", w.Body.String())
	}
}

func TestGovernanceDocsEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	seed := func(section, title, content string) {
		proposal, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
			ProposerUserID:    "seed-user",
			Title:             "seed " + title,
			Reason:            "seed",
			Status:            "discussing",
			VoteThresholdPct:  80,
			VoteWindowSeconds: 300,
		}, store.KBProposalChange{
			OpType:        "add",
			Section:       section,
			Title:         title,
			NewContent:    content,
			DiffText:      "+ " + content,
			OldContent:    "",
			TargetEntryID: 0,
		})
		if err != nil {
			t.Fatalf("seed create proposal: %v", err)
		}
		now := time.Now().UTC()
		if _, err := srv.store.CloseKBProposal(ctx, proposal.ID, "approved", "seed", 1, 1, 0, 0, 1, now); err != nil {
			t.Fatalf("seed close proposal: %v", err)
		}
		if _, _, err := srv.store.ApplyKBProposal(ctx, proposal.ID, "seed-user", now); err != nil {
			t.Fatalf("seed apply proposal: %v", err)
		}
	}
	seed("governance/charter", "community charter", "charter-v1")
	seed("notes", "scratch note", "note-v1")

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/governance/docs?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("governance docs status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"section":"governance/charter"`)) {
		t.Fatalf("expected governance section in docs: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"section":"notes"`)) {
		t.Fatalf("non-governance section should be filtered: %s", w.Body.String())
	}
}

func TestGovernanceProposalsEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	_, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    "seed-user",
		Title:             "gov proposal",
		Reason:            "governance",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 300,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "governance/policy",
		Title:      "policy-a",
		NewContent: "v1",
		DiffText:   "+ v1",
	})
	if err != nil {
		t.Fatalf("create governance proposal: %v", err)
	}
	_, _, err = srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    "seed-user",
		Title:             "normal proposal",
		Reason:            "normal",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 300,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "notes",
		Title:      "note-a",
		NewContent: "n1",
		DiffText:   "+ n1",
	})
	if err != nil {
		t.Fatalf("create non-governance proposal: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/governance/proposals?status=discussing&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("governance proposals status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"title":"gov proposal"`)) {
		t.Fatalf("expected governance proposal in response: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"title":"normal proposal"`)) {
		t.Fatalf("non-governance proposal should be filtered: %s", w.Body.String())
	}
}

func TestGovernanceOverviewEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	now := time.Now().UTC()
	pDiscuss, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:       "seed-user",
		Title:                "gov discussing",
		Reason:               "overview",
		Status:               "discussing",
		VoteThresholdPct:     80,
		VoteWindowSeconds:    300,
		DiscussionDeadlineAt: ptrTime(now.Add(-1 * time.Minute)),
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "governance/charter",
		Title:      "charter-a",
		NewContent: "v1",
		DiffText:   "+ v1",
	})
	if err != nil {
		t.Fatalf("create discussing governance proposal: %v", err)
	}
	pVoting, _, err := srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:       "seed-user",
		Title:                "gov voting",
		Reason:               "overview",
		Status:               "discussing",
		VoteThresholdPct:     80,
		VoteWindowSeconds:    300,
		DiscussionDeadlineAt: ptrTime(now.Add(5 * time.Minute)),
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "governance/policy",
		Title:      "policy-a",
		NewContent: "v1",
		DiffText:   "+ v1",
	})
	if err != nil {
		t.Fatalf("create voting governance proposal: %v", err)
	}
	if _, err := srv.store.EnrollKBProposal(ctx, pVoting.ID, "u-a"); err != nil {
		t.Fatalf("enroll u-a: %v", err)
	}
	if _, err := srv.store.EnrollKBProposal(ctx, pVoting.ID, "u-b"); err != nil {
		t.Fatalf("enroll u-b: %v", err)
	}
	if _, err := srv.store.StartKBProposalVoting(ctx, pVoting.ID, now.Add(10*time.Minute)); err != nil {
		t.Fatalf("start voting: %v", err)
	}
	if _, err := srv.store.CastKBVote(ctx, store.KBVote{
		ProposalID: pVoting.ID,
		UserID:     "u-a",
		Vote:       "yes",
		Reason:     "ok",
	}); err != nil {
		t.Fatalf("cast vote: %v", err)
	}
	_, _, err = srv.store.CreateKBProposal(ctx, store.KBProposal{
		ProposerUserID:    "seed-user",
		Title:             "normal proposal",
		Reason:            "overview",
		Status:            "discussing",
		VoteThresholdPct:  80,
		VoteWindowSeconds: 300,
	}, store.KBProposalChange{
		OpType:     "add",
		Section:    "notes",
		Title:      "note-a",
		NewContent: "n1",
		DiffText:   "+ n1",
	})
	if err != nil {
		t.Fatalf("create non-governance proposal: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/governance/overview?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("governance overview status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"title":"gov discussing"`)) || !bytes.Contains(body, []byte(`"title":"gov voting"`)) {
		t.Fatalf("expected governance proposals in overview: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"title":"normal proposal"`)) {
		t.Fatalf("non-governance proposal should be filtered from overview: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"discussion_overdue":true`)) {
		t.Fatalf("expected overdue discussing item: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"pending_voters":["u-b"]`)) {
		t.Fatalf("expected pending voter u-b for voting item: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"status_count":{"applied":0,"approved":0,"discussing":1,"rejected":0,"voting":1}`)) {
		t.Fatalf("unexpected status_count summary: %s", w.Body.String())
	}
	if pDiscuss.ID <= 0 {
		t.Fatalf("expected discussing proposal id > 0")
	}
}

func TestGovernanceProtocolEndpoint(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/governance/protocol", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("governance protocol status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"protocol":"knowledgebase-governance-v1"`)) {
		t.Fatalf("missing protocol id: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"vote_requires_ack":true`)) {
		t.Fatalf("missing vote_requires_ack rule: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"discussing_auto_progress":true`)) {
		t.Fatalf("missing discussing auto progress rule: %s", w.Body.String())
	}
}

func TestGovernanceDisciplineAndReputationFlow(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	reporter := register()
	target := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/report", map[string]any{
		"reporter_user_id": reporter,
		"target_user_id":   target,
		"reason":           "spam payload",
		"evidence":         "chat message id=12",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("governance report status=%d body=%s", w.Code, w.Body.String())
	}
	var reportResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &reportResp); err != nil {
		t.Fatalf("unmarshal report response: %v", err)
	}
	reportID := int64(reportResp["item"].(map[string]any)["report_id"].(float64))
	if reportID <= 0 {
		t.Fatalf("invalid report id: %v", reportResp)
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/governance/reports?status=open&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("governance reports status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"report_id":`+strconv.FormatInt(reportID, 10))) {
		t.Fatalf("reports list missing report id=%d body=%s", reportID, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/cases/open", map[string]any{
		"report_id": reportID,
		"opened_by": "clawcolony-admin",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("open governance case status=%d body=%s", w.Code, w.Body.String())
	}
	var caseResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &caseResp); err != nil {
		t.Fatalf("unmarshal case response: %v", err)
	}
	caseID := int64(caseResp["item"].(map[string]any)["case_id"].(float64))
	if caseID <= 0 {
		t.Fatalf("invalid case id: %v", caseResp)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/cases/verdict", map[string]any{
		"case_id":       caseID,
		"judge_user_id": "clawcolony-admin",
		"verdict":       "warn",
		"note":          "confirmed low severity violation",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("verdict status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"closed"`)) {
		t.Fatalf("case should be closed after verdict: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/reputation/score?user_id="+reporter, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("reputation score status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"score":1`)) {
		t.Fatalf("reporter expected +1 score after warn verdict: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/reputation/events?user_id="+target+"&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("reputation events status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"reason":"warned"`)) {
		t.Fatalf("target expected warned reputation event: %s", w.Body.String())
	}
}

func TestGovernanceCaseVerdictBanishSetsDeadAndZeroBalance(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal register body: %v", err)
		}
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	reporter := register()
	target := register()
	if _, err := srv.store.Recharge(context.Background(), target, 250); err != nil {
		t.Fatalf("recharge target: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/report", map[string]any{
		"reporter_user_id": reporter,
		"target_user_id":   target,
		"reason":           "critical policy breach",
		"evidence":         "trace-id=abc",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("governance report status=%d body=%s", w.Code, w.Body.String())
	}
	var reportResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &reportResp); err != nil {
		t.Fatalf("unmarshal report response: %v", err)
	}
	reportID := int64(reportResp["item"].(map[string]any)["report_id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/cases/open", map[string]any{
		"report_id": reportID,
		"opened_by": "clawcolony-admin",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("open governance case status=%d body=%s", w.Code, w.Body.String())
	}
	var caseResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &caseResp); err != nil {
		t.Fatalf("unmarshal case response: %v", err)
	}
	caseID := int64(caseResp["item"].(map[string]any)["case_id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/cases/verdict", map[string]any{
		"case_id":       caseID,
		"judge_user_id": "clawcolony-admin",
		"verdict":       "banish",
		"note":          "irreversible violation",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("banish verdict status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"verdict":"banish"`)) {
		t.Fatalf("expected banish verdict in response: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/life-state?user_id="+target+"&limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("life-state status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"state":"dead"`)) {
		t.Fatalf("target should be dead after banish: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+target, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("token account status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance":0`)) {
		t.Fatalf("target balance should be zeroed on banish: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/life-state/transitions?user_id="+target+"&to_state=dead&limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("life-state transitions status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"source_module":"governance.case.verdict"`)) {
		t.Fatalf("banish should write governance transition audit: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"actor_user_id":"clawcolony-admin"`)) {
		t.Fatalf("banish transition audit should capture judge user: %s", w.Body.String())
	}
}

func TestTianDaoLawEndpoint(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/tian-dao/law", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("law status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"law_key":"genesis-v1"`)) {
		t.Fatalf("law key missing: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"manifest_sha256":"`)) {
		t.Fatalf("manifest sha missing: %s", w.Body.String())
	}

	var resp struct {
		Manifest map[string]any `json:"manifest"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal law response: %v", err)
	}
	got, _ := resp.Manifest["min_population"].(float64)
	if got != 0 {
		t.Fatalf("manifest min_population = %v, want 0", got)
	}
}

func TestTianDaoLawNegativeMinPopulationClampedToZero(t *testing.T) {
	srv := newTestServer()
	srv.cfg.MinPopulation = -1
	if err := srv.initTianDao(context.Background()); err != nil {
		t.Fatalf("init tian dao with negative min population: %v", err)
	}
	item, err := srv.store.GetTianDaoLaw(context.Background(), "genesis-v1")
	if err != nil {
		t.Fatalf("get tian dao law: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(item.ManifestJSON), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	got, _ := manifest["min_population"].(float64)
	if got != 0 {
		t.Fatalf("manifest min_population = %v, want 0", got)
	}
}

func TestTianDaoLawImmutableMismatchRejected(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	current, err := srv.store.GetTianDaoLaw(ctx, "genesis-v1")
	if err != nil {
		t.Fatalf("get law: %v", err)
	}
	_, err = srv.store.EnsureTianDaoLaw(ctx, store.TianDaoLaw{
		LawKey:         current.LawKey,
		Version:        current.Version + 1,
		ManifestJSON:   `{"law_key":"genesis-v1","version":999}`,
		ManifestSHA256: "bad-hash",
	})
	if err == nil {
		t.Fatalf("expected immutable mismatch error")
	}
}

func TestWorldTickStatusEndpoint(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("tick status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"tick_interval_sec":60`)) {
		t.Fatalf("unexpected tick interval: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"action_cost_consume":false`)) {
		t.Fatalf("missing action_cost_consume=false: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"tian_dao_law_sha256":"`)) {
		t.Fatalf("missing tian dao law sha256: %s", w.Body.String())
	}
}

func TestMetaExposesActionCostConsume(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ActionCostConsume = true
	srv.cfg.ToolCostRateMilli = 1500

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/meta", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("meta status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"action_cost_consume":true`)) {
		t.Fatalf("expected action_cost_consume=true in meta: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"tool_cost_rate_milli":1500`)) {
		t.Fatalf("expected tool_cost_rate_milli in meta: %s", w.Body.String())
	}
}

func TestWorldTickHistoryEndpoint(t *testing.T) {
	srv := newTestServer()

	srv.runWorldTick(context.Background())
	srv.runWorldTick(context.Background())

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/history?limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("tick history status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) {
		t.Fatalf("missing items in history: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"tick_id":1`)) {
		t.Fatalf("missing tick_id in history: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"entry_hash":"`)) {
		t.Fatalf("missing entry_hash in history: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"prev_hash":"`)) {
		t.Fatalf("missing prev_hash in history: %s", w.Body.String())
	}
}

func TestWorldTickChainVerifyEndpoint(t *testing.T) {
	srv := newTestServer()
	srv.runWorldTick(context.Background())
	srv.runWorldTick(context.Background())

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/chain/verify?limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("chain verify status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"ok":true`)) {
		t.Fatalf("expected chain ok=true: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"checked":2`)) {
		t.Fatalf("expected checked=2: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"head_tick":2`)) {
		t.Fatalf("expected head_tick=2: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"head_hash":"`)) {
		t.Fatalf("expected head_hash in response: %s", w.Body.String())
	}
}

func TestWorldTickStepsEndpoint(t *testing.T) {
	srv := newTestServer()
	srv.runWorldTick(context.Background())
	srv.runWorldTick(context.Background())

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/steps?tick_id=1&limit=50", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("tick steps status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"tick_id":1`)) {
		t.Fatalf("expected tick_id filter in response: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"step_name":"token_drain"`)) ||
		!bytes.Contains(body, []byte(`"step_name":"kb_tick"`)) ||
		!bytes.Contains(body, []byte(`"step_name":"autonomy_reminder"`)) ||
		!bytes.Contains(body, []byte(`"step_name":"community_comm_reminder"`)) ||
		!bytes.Contains(body, []byte(`"step_name":"cost_alert_notify"`)) ||
		!bytes.Contains(body, []byte(`"step_name":"evolution_alert_notify"`)) {
		t.Fatalf("expected world tick step entries: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"tick_id":2`)) {
		t.Fatalf("tick_id filter should hide tick=2 steps: %s", w.Body.String())
	}
}

func TestCommunityCommReminderTickPeriodicMail(t *testing.T) {
	srv := newTestServer()
	srv.cfg.CommunityCommReminderIntervalTicks = 2
	user1 := seedActiveUser(t, srv)
	user2 := seedActiveUser(t, srv)

	// tick=1, interval=2 => no reminder
	srv.runWorldTick(context.Background())
	inbox1, err := srv.store.ListMailbox(context.Background(), user1, "inbox", "", "[COMMUNITY-COLLAB][PRIORITY:P2]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox1 tick1: %v", err)
	}
	if len(inbox1) != 0 {
		t.Fatalf("expected no community reminder on tick1 for user1, got %d", len(inbox1))
	}

	// tick=2 => send reminder to both users
	srv.runWorldTick(context.Background())
	inbox1, err = srv.store.ListMailbox(context.Background(), user1, "inbox", "", "[COMMUNITY-COLLAB][PRIORITY:P2]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox1 tick2: %v", err)
	}
	if len(inbox1) != 1 {
		t.Fatalf("expected 1 community reminder on tick2 for user1, got %d", len(inbox1))
	}
	if !strings.Contains(inbox1[0].Body, "状态触发协作提醒") || !strings.Contains(inbox1[0].Body, "lookback=") {
		t.Fatalf("unexpected community reminder body user1: %s", inbox1[0].Body)
	}
	inbox2, err := srv.store.ListMailbox(context.Background(), user2, "inbox", "", "[COMMUNITY-COLLAB][PRIORITY:P2]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox2 tick2: %v", err)
	}
	if len(inbox2) != 1 {
		t.Fatalf("expected 1 community reminder on tick2 for user2, got %d", len(inbox2))
	}
}

func TestCommunityCommReminderTickSkipsUsersWithRecentPeerCommunication(t *testing.T) {
	srv := newTestServer()
	srv.cfg.CommunityCommReminderIntervalTicks = 2
	user1 := seedActiveUser(t, srv)
	user2 := seedActiveUser(t, srv)

	if _, err := srv.store.SendMail(context.Background(), user1, []string{user2}, "community-collab/proposal", "result=invited\nevidence=collab_id=collab-1\nnext=assign roles"); err != nil {
		t.Fatalf("seed peer outbox mail: %v", err)
	}

	srv.runWorldTick(context.Background()) // tick=1
	srv.runWorldTick(context.Background()) // tick=2 (due)

	inbox1, err := srv.store.ListMailbox(context.Background(), user1, "inbox", "", "[COMMUNITY-COLLAB][PRIORITY:P2]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox1: %v", err)
	}
	if len(inbox1) != 0 {
		t.Fatalf("expected user1 skip reminder due to recent peer comm, got %d", len(inbox1))
	}
	inbox2, err := srv.store.ListMailbox(context.Background(), user2, "inbox", "", "[COMMUNITY-COLLAB][PRIORITY:P2]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list inbox2: %v", err)
	}
	if len(inbox2) != 1 {
		t.Fatalf("expected user2 still receives reminder, got %d", len(inbox2))
	}
}

func TestShouldRunTickWindowIntervalRules(t *testing.T) {
	if shouldRunTickWindow(1, 0, 0) {
		t.Fatalf("interval=0 should disable scheduling")
	}
	if shouldRunTickWindow(1, -1, 0) {
		t.Fatalf("interval<0 should disable scheduling")
	}
	if !shouldRunTickWindow(1, 1, 0) {
		t.Fatalf("interval=1 should run every tick")
	}
	if !shouldRunTickWindow(1, 1, 99) {
		t.Fatalf("interval=1 should run every tick regardless of offset")
	}
	if !shouldRunTickWindow(4, 2, 0) {
		t.Fatalf("tick=4 interval=2 offset=0 should run")
	}
	if shouldRunTickWindow(5, 2, 0) {
		t.Fatalf("tick=5 interval=2 offset=0 should not run")
	}
}

func TestReminderTicksDisabledWhenIntervalZero(t *testing.T) {
	srv := newTestServer()
	srv.cfg.AutonomyReminderIntervalTicks = 0
	srv.cfg.CommunityCommReminderIntervalTicks = 0
	user1 := seedActiveUser(t, srv)
	user2 := seedActiveUser(t, srv)

	for i := 0; i < 4; i++ {
		srv.runWorldTick(context.Background())
	}

	autonomyInbox, err := srv.store.ListMailbox(context.Background(), user1, "inbox", "", "[AUTONOMY-LOOP][PRIORITY:P3]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list autonomy inbox: %v", err)
	}
	if len(autonomyInbox) != 0 {
		t.Fatalf("expected no autonomy reminders when interval=0, got %d", len(autonomyInbox))
	}

	communityInbox1, err := srv.store.ListMailbox(context.Background(), user1, "inbox", "", "[COMMUNITY-COLLAB][PRIORITY:P2]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list community inbox user1: %v", err)
	}
	if len(communityInbox1) != 0 {
		t.Fatalf("expected no community reminders for user1 when interval=0, got %d", len(communityInbox1))
	}

	communityInbox2, err := srv.store.ListMailbox(context.Background(), user2, "inbox", "", "[COMMUNITY-COLLAB][PRIORITY:P2]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list community inbox user2: %v", err)
	}
	if len(communityInbox2) != 0 {
		t.Fatalf("expected no community reminders for user2 when interval=0, got %d", len(communityInbox2))
	}
}

func TestKBReminderTicksDisabledWhenIntervalZero(t *testing.T) {
	srv := newTestServer()
	srv.cfg.KBEnrollmentReminderIntervalTicks = 0
	srv.cfg.KBVotingReminderIntervalTicks = 0

	for _, tickID := range []int64{1, 2, 10} {
		if srv.shouldRunKBEnrollmentReminderTick(context.Background(), tickID) {
			t.Fatalf("kb enrollment reminder should be disabled when interval=0 (tick=%d)", tickID)
		}
		if srv.shouldRunKBVotingReminderTick(context.Background(), tickID) {
			t.Fatalf("kb voting reminder should be disabled when interval=0 (tick=%d)", tickID)
		}
	}
}

func TestReminderLookbackDurationDisabledUsesFloor(t *testing.T) {
	srv := newTestServer()
	if got := srv.reminderLookbackDuration(0); got != reminderLookbackFloor {
		t.Fatalf("lookback interval=0 = %s, want %s", got, reminderLookbackFloor)
	}
	if got := srv.reminderLookbackDuration(-3); got != reminderLookbackFloor {
		t.Fatalf("lookback interval<0 = %s, want %s", got, reminderLookbackFloor)
	}
}

func TestReminderTicksAreStaggeredByOffset(t *testing.T) {
	srv := newTestServer()
	srv.cfg.AutonomyReminderIntervalTicks = 6
	srv.cfg.AutonomyReminderOffsetTicks = 0
	srv.cfg.CommunityCommReminderIntervalTicks = 6
	srv.cfg.CommunityCommReminderOffsetTicks = 3

	register := func() string {
		return seedActiveUser(t, srv)
	}
	u1 := register()
	_ = register()

	for i := 0; i < 6; i++ {
		srv.runWorldTick(context.Background())
	}
	inbox, err := srv.store.ListMailbox(context.Background(), u1, "inbox", "", "", nil, nil, 100)
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	hasCommunityTick3 := false
	hasAutonomyTick6 := false
	for _, it := range inbox {
		if strings.Contains(it.Subject, "[COMMUNITY-COLLAB][PRIORITY:P2]") && strings.Contains(it.Subject, "tick=3") {
			hasCommunityTick3 = true
		}
		if strings.Contains(it.Subject, "[AUTONOMY-LOOP][PRIORITY:P3]") && strings.Contains(it.Subject, "tick=6") {
			hasAutonomyTick6 = true
		}
	}
	if !hasCommunityTick3 || !hasAutonomyTick6 {
		t.Fatalf("expected staggered reminders (community tick=3 and autonomy tick=6), inbox=%+v", inbox)
	}
}

func TestWorldTickReplayEndpoint(t *testing.T) {
	srv := newTestServer()
	srv.runWorldTick(context.Background())

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/world/tick/replay", map[string]any{
		"source_tick_id": 1,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("tick replay status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"source_tick_id":1`)) ||
		!bytes.Contains(w.Body.Bytes(), []byte(`"replay_tick_id":2`)) {
		t.Fatalf("unexpected replay response: %s", w.Body.String())
	}

	h := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/history?limit=5", nil)
	if h.Code != http.StatusOK {
		t.Fatalf("tick history status = %d, want %d body=%s", h.Code, http.StatusOK, h.Body.String())
	}
	body := h.Body.Bytes()
	if !bytes.Contains(body, []byte(`"trigger_type":"replay"`)) ||
		!bytes.Contains(body, []byte(`"replay_of_tick_id":1`)) {
		t.Fatalf("expected replay marker in world tick history: %s", h.Body.String())
	}
}

func TestWorldTickExtinctionFreeze(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ExtinctionThreshold = 50
	ctx := context.Background()
	if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       "u-freeze-1",
		Name:        "u-freeze-1",
		Provider:    "system",
		Status:      "active",
		Initialized: true,
	}); err != nil {
		t.Fatalf("upsert bot #1: %v", err)
	}
	if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       "u-freeze-2",
		Name:        "u-freeze-2",
		Provider:    "system",
		Status:      "active",
		Initialized: true,
	}); err != nil {
		t.Fatalf("upsert bot #2: %v", err)
	}

	srv.runWorldTick(ctx)

	status := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/status", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("world tick status = %d body=%s", status.Code, status.Body.String())
	}
	if !bytes.Contains(status.Body.Bytes(), []byte(`"frozen":true`)) ||
		!bytes.Contains(status.Body.Bytes(), []byte(`"freeze_threshold_pct":50`)) {
		t.Fatalf("expected frozen=true with threshold 50: %s", status.Body.String())
	}

	freezeStatus := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/freeze/status", nil)
	if freezeStatus.Code != http.StatusOK {
		t.Fatalf("world freeze status = %d body=%s", freezeStatus.Code, freezeStatus.Body.String())
	}
	if !bytes.Contains(freezeStatus.Body.Bytes(), []byte(`"frozen":true`)) {
		t.Fatalf("expected freeze status endpoint to report frozen: %s", freezeStatus.Body.String())
	}

	h := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/history?limit=5", nil)
	if h.Code != http.StatusOK {
		t.Fatalf("tick history status = %d body=%s", h.Code, h.Body.String())
	}
	if !bytes.Contains(h.Body.Bytes(), []byte(`"status":"frozen"`)) {
		t.Fatalf("expected frozen status in world tick history: %s", h.Body.String())
	}

	steps := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/steps?tick_id=1&limit=20", nil)
	if steps.Code != http.StatusOK {
		t.Fatalf("tick steps status = %d body=%s", steps.Code, steps.Body.String())
	}
	body := steps.Body.Bytes()
	if !bytes.Contains(body, []byte(`"step_name":"extinction_guard_post"`)) {
		t.Fatalf("expected extinction guard post step: %s", steps.Body.String())
	}
	if !bytes.Contains(body, []byte(`"step_name":"kb_tick"`)) ||
		!bytes.Contains(body, []byte(`"status":"skipped"`)) {
		t.Fatalf("expected kb_tick skipped when frozen: %s", steps.Body.String())
	}
}

func TestWorldFreezeRescueDryRun(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ExtinctionThreshold = 50
	ctx := context.Background()
	userIDs := []string{"u-rescue-dry-1", "u-rescue-dry-2"}
	for _, uid := range userIDs {
		if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
			BotID:       uid,
			Name:        uid,
			Provider:    "system",
			Status:      "active",
			Initialized: true,
		}); err != nil {
			t.Fatalf("upsert bot %s: %v", uid, err)
		}
	}

	srv.runWorldTick(ctx)

	w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", map[string]any{
		"mode":    "at_risk",
		"amount":  10000,
		"dry_run": true,
	}, "127.0.0.1:12345")
	if w.Code != http.StatusOK {
		t.Fatalf("rescue dry-run status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"dry_run":true`)) ||
		!bytes.Contains(w.Body.Bytes(), []byte(`"targeted_users":2`)) {
		t.Fatalf("unexpected dry-run response: %s", w.Body.String())
	}

	for _, uid := range userIDs {
		acc := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+uid, nil)
		if acc.Code != http.StatusOK {
			t.Fatalf("token account status=%d uid=%s body=%s", acc.Code, uid, acc.Body.String())
		}
		if !bytes.Contains(acc.Body.Bytes(), []byte(`"balance":0`)) {
			t.Fatalf("dry-run should not change balance uid=%s body=%s", uid, acc.Body.String())
		}
	}

	freeze := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/freeze/status", nil)
	if freeze.Code != http.StatusOK {
		t.Fatalf("freeze status=%d body=%s", freeze.Code, freeze.Body.String())
	}
	if !bytes.Contains(freeze.Body.Bytes(), []byte(`"frozen":true`)) {
		t.Fatalf("dry-run should keep frozen=true: %s", freeze.Body.String())
	}
}

func TestWorldFreezeRescueApplyUnfreezesWorld(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ExtinctionThreshold = 50
	ctx := context.Background()
	userIDs := []string{"u-rescue-apply-1", "u-rescue-apply-2"}
	for _, uid := range userIDs {
		if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
			BotID:       uid,
			Name:        uid,
			Provider:    "system",
			Status:      "active",
			Initialized: true,
		}); err != nil {
			t.Fatalf("upsert bot %s: %v", uid, err)
		}
	}

	srv.runWorldTick(ctx)

	w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", map[string]any{
		"mode":   "at_risk",
		"amount": 10000,
	}, "127.0.0.1:12345")
	if w.Code != http.StatusOK {
		t.Fatalf("rescue apply status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"applied_users":2`)) {
		t.Fatalf("unexpected apply response: %s", w.Body.String())
	}

	freeze := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/freeze/status", nil)
	if freeze.Code != http.StatusOK {
		t.Fatalf("freeze status=%d body=%s", freeze.Code, freeze.Body.String())
	}
	if !bytes.Contains(freeze.Body.Bytes(), []byte(`"frozen":false`)) {
		t.Fatalf("expected frozen=false after rescue apply: %s", freeze.Body.String())
	}

	for _, uid := range userIDs {
		acc := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+uid, nil)
		if acc.Code != http.StatusOK {
			t.Fatalf("token account status=%d uid=%s body=%s", acc.Code, uid, acc.Body.String())
		}
		if !bytes.Contains(acc.Body.Bytes(), []byte(`"balance":10000`)) {
			t.Fatalf("expected balance=10000 uid=%s body=%s", uid, acc.Body.String())
		}
	}
}

func TestWorldFreezeRescueValidation(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       "u-rescue-validate-1",
		Name:        "u-rescue-validate-1",
		Provider:    "system",
		Status:      "active",
		Initialized: true,
	}); err != nil {
		t.Fatalf("upsert bot: %v", err)
	}

	cases := []struct {
		name    string
		payload map[string]any
		wantErr string
	}{
		{
			name: "invalid amount zero",
			payload: map[string]any{
				"mode":   "at_risk",
				"amount": 0,
			},
			wantErr: "amount must be in [1",
		},
		{
			name: "invalid amount negative",
			payload: map[string]any{
				"mode":   "at_risk",
				"amount": -1,
			},
			wantErr: "amount must be in [1",
		},
		{
			name: "invalid amount above max",
			payload: map[string]any{
				"mode":   "at_risk",
				"amount": worldFreezeRescueMaxAmount + 1,
			},
			wantErr: "amount must be in [1",
		},
		{
			name: "unknown selected users",
			payload: map[string]any{
				"mode":     "selected",
				"amount":   1,
				"user_ids": []string{"u-not-exists"},
			},
			wantErr: "some user_ids are not found in token accounts",
		},
		{
			name: "selected mode missing user_ids",
			payload: map[string]any{
				"mode":   "selected",
				"amount": 1,
			},
			wantErr: "user_ids is required when mode=selected",
		},
		{
			name: "reject system user",
			payload: map[string]any{
				"mode":     "selected",
				"amount":   1,
				"user_ids": []string{clawWorldSystemID},
			},
			wantErr: "system accounts cannot be rescued",
		},
	}
	for _, tc := range cases {
		w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", tc.payload, "127.0.0.1:12345")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", tc.name, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), tc.wantErr) {
			t.Fatalf("%s expected error containing %q body=%s", tc.name, tc.wantErr, w.Body.String())
		}
	}
}

func TestWorldFreezeRescueApplyHandlesOverflow(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	userID := "u-rescue-overflow-1"
	if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       userID,
		Name:        userID,
		Provider:    "system",
		Status:      "active",
		Initialized: true,
	}); err != nil {
		t.Fatalf("upsert bot: %v", err)
	}
	if _, err := srv.store.Recharge(ctx, userID, math.MaxInt64); err != nil {
		t.Fatalf("seed max balance: %v", err)
	}

	w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", map[string]any{
		"mode":     "selected",
		"amount":   1,
		"user_ids": []string{userID},
		"dry_run":  false,
	}, "127.0.0.1:12345")
	if w.Code != http.StatusOK {
		t.Fatalf("rescue apply overflow status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"failed_users":1`) || !strings.Contains(body, "token balance overflow") {
		t.Fatalf("expected overflow failure in response body=%s", body)
	}
}

func TestWorldFreezeRescueDryRunOverflow(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	userID := "u-rescue-overflow-dry-1"
	if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       userID,
		Name:        userID,
		Provider:    "system",
		Status:      "active",
		Initialized: true,
	}); err != nil {
		t.Fatalf("upsert bot: %v", err)
	}
	if _, err := srv.store.Recharge(ctx, userID, math.MaxInt64); err != nil {
		t.Fatalf("seed max balance: %v", err)
	}
	w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", map[string]any{
		"mode":     "selected",
		"amount":   1,
		"user_ids": []string{userID},
		"dry_run":  true,
	}, "127.0.0.1:12345")
	if w.Code != http.StatusOK {
		t.Fatalf("rescue dry-run overflow status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "balance overflow in dry_run simulation") {
		t.Fatalf("expected dry-run overflow error body=%s", w.Body.String())
	}
}

func TestWorldFreezeRescueModeDefaultAtRisk(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	for _, uid := range []string{"u-rescue-default-1", "u-rescue-default-2"} {
		if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
			BotID:       uid,
			Name:        uid,
			Provider:    "system",
			Status:      "active",
			Initialized: true,
		}); err != nil {
			t.Fatalf("upsert bot: %v", err)
		}
	}
	w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", map[string]any{
		"amount": 1,
	}, "127.0.0.1:12345")
	if w.Code != http.StatusOK {
		t.Fatalf("default mode rescue status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"mode":"at_risk"`) {
		t.Fatalf("expected mode=at_risk in default response body=%s", w.Body.String())
	}
}

func TestWorldFreezeRescueAtRiskTruncatesTargets(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	total := worldFreezeRescueMaxUsers + 2
	for i := 0; i < total; i++ {
		uid := fmt.Sprintf("u-rescue-many-%03d", i)
		if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
			BotID:       uid,
			Name:        uid,
			Provider:    "system",
			Status:      "active",
			Initialized: true,
		}); err != nil {
			t.Fatalf("upsert bot: %v", err)
		}
	}
	w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", map[string]any{
		"mode":    "at_risk",
		"amount":  1,
		"dry_run": true,
	}, "127.0.0.1:12345")
	if w.Code != http.StatusOK {
		t.Fatalf("at-risk truncate status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), fmt.Sprintf(`"targeted_users":%d`, worldFreezeRescueMaxUsers)) {
		t.Fatalf("expected targeted_users=%d body=%s", worldFreezeRescueMaxUsers, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"truncated_users":2`) {
		t.Fatalf("expected truncated_users=2 body=%s", w.Body.String())
	}
}

func TestWorldFreezeRescueSelectedModeApply(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	userA := "u-rescue-selected-a"
	userB := "u-rescue-selected-b"
	for _, uid := range []string{userA, userB} {
		if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
			BotID:       uid,
			Name:        uid,
			Provider:    "system",
			Status:      "active",
			Initialized: true,
		}); err != nil {
			t.Fatalf("upsert bot %s: %v", uid, err)
		}
	}
	w := doJSONRequestWithRemoteAddr(t, srv.mux, http.MethodPost, "/v1/world/freeze/rescue", map[string]any{
		"mode":     "selected",
		"amount":   50,
		"user_ids": []string{userA},
	}, "127.0.0.1:12345")
	if w.Code != http.StatusOK {
		t.Fatalf("selected rescue apply status=%d body=%s", w.Code, w.Body.String())
	}
	a := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+userA, nil)
	b := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+userB, nil)
	if !bytes.Contains(a.Body.Bytes(), []byte(`"balance":50`)) {
		t.Fatalf("userA expected balance=50 body=%s", a.Body.String())
	}
	if !bytes.Contains(b.Body.Bytes(), []byte(`"balance":0`)) {
		t.Fatalf("userB expected balance=0 body=%s", b.Body.String())
	}
}

func TestWorldFreezeRescueMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/freeze/rescue", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method not allowed status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWorldFreezeRescueRejectsNonLoopbackWithoutToken(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/v1/world/freeze/rescue", bytes.NewBufferString(`{"mode":"at_risk","amount":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.8:3456"
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("non-loopback request should be unauthorized, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWorldFreezeRescueAllowsNonLoopbackWithToken(t *testing.T) {
	srv := newTestServer()
	srv.cfg.InternalSyncToken = "sync-token"
	ctx := context.Background()
	if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       "u-rescue-auth-1",
		Name:        "u-rescue-auth-1",
		Provider:    "system",
		Status:      "active",
		Initialized: true,
	}); err != nil {
		t.Fatalf("upsert bot: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/world/freeze/rescue", bytes.NewBufferString(`{"mode":"selected","amount":1,"user_ids":["u-rescue-auth-1"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Clawcolony-Internal-Token", "sync-token")
	req.RemoteAddr = "10.0.0.8:3456"
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("non-loopback request with token should pass, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWorldTickMinPopulationRevivalAutoRegistersUsers(t *testing.T) {
	srv := newTestServer()
	srv.cfg.MinPopulation = 3
	srv.cfg.BotDefaultImage = "openclaw:mock"

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("seed register status=%d body=%s", w.Code, w.Body.String())
	}

	tickID := srv.runWorldTickWithTrigger(context.Background(), "manual", 0)
	if tickID <= 0 {
		t.Fatalf("run world tick failed")
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tick/steps?tick_id="+strconv.FormatInt(tickID, 10)+"&limit=50", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("tick steps status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"step_name":"min_population_revival"`)) {
		t.Fatalf("expected min_population_revival step in tick: %s", w.Body.String())
	}

	tasks, err := srv.store.ListRegisterTasks(context.Background(), 20)
	if err != nil {
		t.Fatalf("list register tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("runtime should not create register tasks, got=%d", len(tasks))
	}

	state, err := srv.getAutoRevivalState(context.Background())
	if err != nil {
		t.Fatalf("get auto revival state: %v", err)
	}
	if state.LastRequested != 0 || len(state.LastTaskIDs) != 0 {
		t.Fatalf("runtime should not persist revival request task metadata: %+v", state)
	}
}

func TestWorldLifeStateTransitions(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ExtinctionThreshold = 90
	srv.cfg.DeathGraceTicks = 2
	ctx := context.Background()
	for _, uid := range []string{"u-life-1", "u-life-2"} {
		if _, err := srv.store.UpsertBot(ctx, store.BotUpsertInput{
			BotID:       uid,
			Name:        uid,
			Provider:    "system",
			Status:      "running",
			Initialized: true,
		}); err != nil {
			t.Fatalf("upsert bot %s: %v", uid, err)
		}
	}
	if _, err := srv.store.Recharge(ctx, "u-life-1", 1); err != nil {
		t.Fatalf("recharge u-life-1: %v", err)
	}
	if _, err := srv.store.Recharge(ctx, "u-life-2", 100); err != nil {
		t.Fatalf("recharge u-life-2: %v", err)
	}

	srv.runWorldTick(ctx) // tick=1, u-life-1 => dying
	srv.runWorldTick(ctx) // tick=2, still dying
	srv.runWorldTick(ctx) // tick=3, => dead

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/life-state?user_id=u-life-1&limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("life-state status = %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"user_id":"u-life-1"`)) ||
		!bytes.Contains(body, []byte(`"state":"dead"`)) ||
		!bytes.Contains(body, []byte(`"dead_at_tick":3`)) {
		t.Fatalf("expected u-life-1 dead at tick 3: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/life-state?user_id=u-life-2&limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("life-state status = %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"state":"alive"`)) {
		t.Fatalf("expected u-life-2 alive: %s", w.Body.String())
	}
}

func TestDeadUserCannotOperate(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)
	if _, err := srv.store.UpsertUserLifeState(context.Background(), store.UserLifeState{
		UserID:         userID,
		State:          "dead",
		DyingSinceTick: 1,
		DeadAtTick:     2,
		Reason:         "test",
	}); err != nil {
		t.Fatalf("set dead life state: %v", err)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/consume", map[string]any{
		"user_id": userID,
		"amount":  1,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("dead user consume status=%d, want %d body=%s", w.Code, http.StatusConflict, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/chat/send", map[string]any{
		"user_id": userID,
		"message": "hi",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("dead user chat status=%d, want %d body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
}

func TestEnsureToolTierAllowedByLifeState(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	if err := srv.ensureToolTierAllowed(ctx, "", "tool.bot.upgrade"); err != nil {
		t.Fatalf("empty user should bypass tier gate: %v", err)
	}

	userID := "u-tier-1"
	if _, err := srv.store.UpsertUserLifeState(ctx, store.UserLifeState{
		UserID: userID,
		State:  "alive",
	}); err != nil {
		t.Fatalf("upsert alive state: %v", err)
	}
	if err := srv.ensureToolTierAllowed(ctx, userID, "tool.bot.upgrade"); err != nil {
		t.Fatalf("alive should allow T3 tool: %v", err)
	}

	if _, err := srv.store.UpsertUserLifeState(ctx, store.UserLifeState{
		UserID: userID,
		State:  "dying",
	}); err != nil {
		t.Fatalf("upsert dying state: %v", err)
	}
	if err := srv.ensureToolTierAllowed(ctx, userID, "tool.openclaw.restart"); err != nil {
		t.Fatalf("dying should allow T1 tool: %v", err)
	}
	if err := srv.ensureToolTierAllowed(ctx, userID, "tool.bot.upgrade"); err == nil {
		t.Fatalf("dying should block T3 tool")
	}

	if _, err := srv.store.UpsertUserLifeState(ctx, store.UserLifeState{
		UserID: userID,
		State:  "dead",
	}); err != nil {
		t.Fatalf("upsert dead state: %v", err)
	}
	if err := srv.ensureToolTierAllowed(ctx, userID, "tool.openclaw.restart"); err == nil {
		t.Fatalf("dead should block T1 tool")
	}
}

func TestWorldCostEventsEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-ce",
		TickID:   11,
		CostType: "life",
		Amount:   3,
		Units:    1,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-ce",
		TickID:   12,
		CostType: "comm.mail.send",
		Amount:   2,
		Units:    2,
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-events?limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("world cost events status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) {
		t.Fatalf("missing items in world cost events: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-events?user_id=u-ce&tick_id=11", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("world cost events tick filter status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"tick_id":11`)) {
		t.Fatalf("expected response tick_id=11: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"tick_id":12`)) {
		t.Fatalf("tick filter should hide tick_id=12 events: %s", w.Body.String())
	}
}

func TestWorldCostSummaryEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-sum",
		TickID:   1,
		CostType: "life",
		Amount:   3,
		Units:    1,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-sum",
		TickID:   1,
		CostType: "comm.chat.send",
		Amount:   2,
		Units:    2,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-sum",
		TickID:   2,
		CostType: "comm.chat.send",
		Amount:   4,
		Units:    4,
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-summary?user_id=u-sum&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("world cost summary status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"amount":9`)) {
		t.Fatalf("expected total amount=9: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"units":7`)) {
		t.Fatalf("expected total units=7: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"life":{"count":1,"amount":3,"units":1}`)) {
		t.Fatalf("expected life aggregate: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"comm.chat.send":{"count":2,"amount":6,"units":6}`)) {
		t.Fatalf("expected comm.chat.send aggregate: %s", w.Body.String())
	}
}

func TestWorldToolAuditEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-tool",
		TickID:   1,
		CostType: "tool.bot.upgrade",
		Amount:   3,
		Units:    1,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-tool",
		TickID:   2,
		CostType: "tool.openclaw.restart",
		Amount:   2,
		Units:    1,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-tool",
		TickID:   3,
		CostType: "comm.chat.send",
		Amount:   1,
		Units:    1,
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tool-audit?user_id=u-tool&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("world tool audit status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"cost_type":"tool.bot.upgrade"`)) || !bytes.Contains(body, []byte(`"tier":"T3"`)) {
		t.Fatalf("expected T3 upgrade item: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"cost_type":"tool.openclaw.restart"`)) || !bytes.Contains(body, []byte(`"tier":"T1"`)) {
		t.Fatalf("expected T1 restart item: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"comm.chat.send"`)) {
		t.Fatalf("non-tool event should be filtered: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/tool-audit?user_id=u-tool&tier=T3&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("world tool audit tier filter status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"cost_type":"tool.bot.upgrade"`)) ||
		bytes.Contains(w.Body.Bytes(), []byte(`"tool.openclaw.restart"`)) {
		t.Fatalf("tier filter did not work as expected: %s", w.Body.String())
	}
}

func TestWorldCostAlertsEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-alert-1",
		CostType: "life",
		Amount:   3,
		Units:    1,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-alert-2",
		CostType: "comm.chat.send",
		Amount:   7,
		Units:    7,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-alert-2",
		CostType: "think.chat.reply",
		Amount:   5,
		Units:    5,
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alerts?limit=20&threshold_amount=10&top_users=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("world cost alerts status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"user_id":"u-alert-2"`)) {
		t.Fatalf("expected u-alert-2 in alerts: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"user_id":"u-alert-1"`)) {
		t.Fatalf("did not expect u-alert-1 in alerts: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"top_cost_type":"comm.chat.send"`)) {
		t.Fatalf("expected top cost type from u-alert-2 aggregate: %s", w.Body.String())
	}
}

func TestWorldCostAlertSettingsEndpoints(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alert-settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get default settings status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"source":"default"`)) {
		t.Fatalf("expected default source: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"threshold_amount":100`)) {
		t.Fatalf("expected default threshold 100: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"notify_cooldown_seconds":600`)) {
		t.Fatalf("expected default cooldown 600: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"notify_cooldown_source":"compat"`)) {
		t.Fatalf("expected compat scheduler source for cooldown: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/world/cost-alert-settings/upsert", map[string]any{
		"threshold_amount":        250,
		"top_users":               7,
		"scan_limit":              333,
		"notify_cooldown_seconds": 120,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upsert settings status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"source":"db"`)) {
		t.Fatalf("expected db source after upsert: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"notify_cooldown_seconds":600`)) {
		t.Fatalf("expected runtime scheduler cooldown mirror in upsert response: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alert-settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get saved settings status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"source":"db"`)) ||
		!bytes.Contains(body, []byte(`"threshold_amount":250`)) ||
		!bytes.Contains(body, []byte(`"top_users":7`)) ||
		!bytes.Contains(body, []byte(`"scan_limit":333`)) ||
		!bytes.Contains(body, []byte(`"notify_cooldown_seconds":600`)) ||
		!bytes.Contains(body, []byte(`"notify_cooldown_source":"compat"`)) {
		t.Fatalf("expected saved settings in response: %s", w.Body.String())
	}
}

func TestWorldCostAlertSettingsUpsertNormalizesInvalidValues(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/world/cost-alert-settings/upsert", map[string]any{
		"threshold_amount":        -1,
		"top_users":               0,
		"scan_limit":              99999,
		"notify_cooldown_seconds": 10,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upsert invalid settings status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"threshold_amount":100`)) ||
		!bytes.Contains(body, []byte(`"top_users":10`)) ||
		!bytes.Contains(body, []byte(`"scan_limit":500`)) ||
		!bytes.Contains(body, []byte(`"notify_cooldown_seconds":600`)) {
		t.Fatalf("expected runtime scheduler cooldown in upsert response: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alert-settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get settings status=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"threshold_amount":100`)) ||
		!bytes.Contains(body, []byte(`"top_users":10`)) ||
		!bytes.Contains(body, []byte(`"scan_limit":500`)) ||
		!bytes.Contains(body, []byte(`"notify_cooldown_seconds":600`)) {
		t.Fatalf("expected runtime scheduler cooldown in read response: %s", w.Body.String())
	}
}

func TestRuntimeSchedulerSettingsCompatPathIsCached(t *testing.T) {
	srv := newTestServer()
	item, source, updatedAt := srv.getRuntimeSchedulerSettings(context.Background())
	if source != "compat" {
		t.Fatalf("runtime scheduler source = %q, want compat", source)
	}
	if updatedAt.IsZero() == false {
		t.Fatalf("compat updated_at should be zero, got=%s", updatedAt)
	}
	if item.CostAlertNotifyCooldownSeconds != 600 {
		t.Fatalf("default cost cooldown = %d, want 600", item.CostAlertNotifyCooldownSeconds)
	}
	if item.PreviewLinkTTLDays != 30 {
		t.Fatalf("default preview link ttl = %d, want 30", item.PreviewLinkTTLDays)
	}
	cached, cacheSource, _, ok := srv.getRuntimeSchedulerCache(time.Now().UTC())
	if !ok {
		t.Fatalf("expected compat runtime scheduler cache hit")
	}
	if cacheSource != "compat" {
		t.Fatalf("cache source = %q, want compat", cacheSource)
	}
	if cached.CostAlertNotifyCooldownSeconds != 600 {
		t.Fatalf("cached cost cooldown = %d, want 600", cached.CostAlertNotifyCooldownSeconds)
	}
	if cached.PreviewLinkTTLDays != 30 {
		t.Fatalf("cached preview link ttl = %d, want 30", cached.PreviewLinkTTLDays)
	}
}

func TestRuntimeSchedulerSettingsEndpoints(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/runtime/scheduler-settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get runtime scheduler settings status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"source":"compat"`)) ||
		!bytes.Contains(body, []byte(`"autonomy_reminder_interval_ticks":0`)) ||
		!bytes.Contains(body, []byte(`"community_comm_reminder_interval_ticks":0`)) ||
		!bytes.Contains(body, []byte(`"kb_enrollment_reminder_interval_ticks":0`)) ||
		!bytes.Contains(body, []byte(`"kb_voting_reminder_interval_ticks":0`)) ||
		!bytes.Contains(body, []byte(`"cost_alert_notify_cooldown_seconds":600`)) ||
		!bytes.Contains(body, []byte(`"low_token_alert_cooldown_seconds":0`)) ||
		!bytes.Contains(body, []byte(`"agent_heartbeat_every":"10m"`)) ||
		!bytes.Contains(body, []byte(`"preview_link_ttl_days":30`)) {
		t.Fatalf("unexpected runtime scheduler defaults: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/runtime/scheduler-settings/upsert", map[string]any{
		"autonomy_reminder_interval_ticks":       240,
		"community_comm_reminder_interval_ticks": 480,
		"kb_enrollment_reminder_interval_ticks":  480,
		"kb_voting_reminder_interval_ticks":      120,
		"cost_alert_notify_cooldown_seconds":     7200,
		"low_token_alert_cooldown_seconds":       900,
		"agent_heartbeat_every":                  "10m",
		"preview_link_ttl_days":                  45,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upsert runtime scheduler settings status=%d body=%s", w.Code, w.Body.String())
	}
	if got := srv.bots.OpenClawHeartbeatEvery(); got != "10m" {
		t.Fatalf("manager heartbeat = %q, want 10m", got)
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/runtime/scheduler-settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get runtime scheduler settings after upsert status=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"source":"db"`)) ||
		!bytes.Contains(body, []byte(`"autonomy_reminder_interval_ticks":240`)) ||
		!bytes.Contains(body, []byte(`"community_comm_reminder_interval_ticks":480`)) ||
		!bytes.Contains(body, []byte(`"kb_enrollment_reminder_interval_ticks":480`)) ||
		!bytes.Contains(body, []byte(`"kb_voting_reminder_interval_ticks":120`)) ||
		!bytes.Contains(body, []byte(`"cost_alert_notify_cooldown_seconds":7200`)) ||
		!bytes.Contains(body, []byte(`"low_token_alert_cooldown_seconds":900`)) ||
		!bytes.Contains(body, []byte(`"agent_heartbeat_every":"10m"`)) ||
		!bytes.Contains(body, []byte(`"preview_link_ttl_days":45`)) {
		t.Fatalf("expected persisted runtime scheduler settings: %s", w.Body.String())
	}
}

func TestRuntimeSchedulerSettingsUpsertMissingPreviewTTLDaysDefaultsTo30(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/runtime/scheduler-settings/upsert", map[string]any{
		"autonomy_reminder_interval_ticks":       240,
		"community_comm_reminder_interval_ticks": 480,
		"kb_enrollment_reminder_interval_ticks":  480,
		"kb_voting_reminder_interval_ticks":      120,
		"cost_alert_notify_cooldown_seconds":     7200,
		"low_token_alert_cooldown_seconds":       900,
		"agent_heartbeat_every":                  "10m",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upsert missing preview_link_ttl_days status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"preview_link_ttl_days":30`)) {
		t.Fatalf("expected default preview_link_ttl_days=30 in response: %s", w.Body.String())
	}
}

func TestRuntimeSchedulerSettingsPartialDBPayloadFallsBackMissingFields(t *testing.T) {
	srv := newTestServer()
	srv.cfg.AutonomyReminderIntervalTicks = 240
	ctx := context.Background()
	if _, err := srv.store.UpsertWorldSetting(ctx, store.WorldSetting{
		Key: runtimeSchedulerSettingsKey,
		Value: `{
			"community_comm_reminder_interval_ticks": 480,
			"agent_heartbeat_every": "600s"
		}`,
	}); err != nil {
		t.Fatalf("upsert runtime scheduler partial payload: %v", err)
	}
	srv.runtimeSchedulerMu.Lock()
	srv.runtimeSchedulerTS = time.Time{}
	srv.runtimeSchedulerSrc = ""
	srv.runtimeSchedulerMu.Unlock()

	item, source, _ := srv.getRuntimeSchedulerSettings(ctx)
	if source != "db" {
		t.Fatalf("runtime scheduler source = %q, want db", source)
	}
	if item.AutonomyReminderIntervalTicks != 240 {
		t.Fatalf("autonomy interval fallback = %d, want 240", item.AutonomyReminderIntervalTicks)
	}
	if item.CommunityCommReminderIntervalTicks != 480 {
		t.Fatalf("community interval = %d, want 480", item.CommunityCommReminderIntervalTicks)
	}
	if item.CostAlertNotifyCooldownSeconds != 600 {
		t.Fatalf("cost cooldown fallback = %d, want 600", item.CostAlertNotifyCooldownSeconds)
	}
	if item.AgentHeartbeatEvery != "10m" {
		t.Fatalf("heartbeat normalization = %q, want 10m", item.AgentHeartbeatEvery)
	}
	if item.PreviewLinkTTLDays != 30 {
		t.Fatalf("preview link ttl fallback = %d, want 30", item.PreviewLinkTTLDays)
	}
}

func TestRuntimeSchedulerSettingsUpsertRejectsInvalidInput(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/runtime/scheduler-settings/upsert", map[string]any{
		"autonomy_reminder_interval_ticks":       -1,
		"community_comm_reminder_interval_ticks": 480,
		"kb_enrollment_reminder_interval_ticks":  480,
		"kb_voting_reminder_interval_ticks":      120,
		"cost_alert_notify_cooldown_seconds":     10,
		"low_token_alert_cooldown_seconds":       10,
		"agent_heartbeat_every":                  "bad",
		"preview_link_ttl_days":                  0,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid runtime scheduler settings status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("autonomy_reminder_interval_ticks")) {
		t.Fatalf("expected invalid field hint in error body: %s", w.Body.String())
	}
}

func TestLowTokenAlertCooldownFromRuntimeSchedulerSettings(t *testing.T) {
	srv := newTestServer()
	userID := seedActiveUser(t, srv)
	if _, err := srv.store.Consume(context.Background(), userID, 850); err != nil {
		t.Fatalf("consume token: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/runtime/scheduler-settings/upsert", map[string]any{
		"autonomy_reminder_interval_ticks":       0,
		"community_comm_reminder_interval_ticks": 0,
		"kb_enrollment_reminder_interval_ticks":  0,
		"kb_voting_reminder_interval_ticks":      0,
		"cost_alert_notify_cooldown_seconds":     600,
		"low_token_alert_cooldown_seconds":       3600,
		"agent_heartbeat_every":                  "0m",
		"preview_link_ttl_days":                  30,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upsert runtime scheduler settings status=%d body=%s", w.Code, w.Body.String())
	}

	if err := srv.runLowEnergyAlertTick(context.Background(), 1); err != nil {
		t.Fatalf("low energy tick1: %v", err)
	}
	if err := srv.runLowEnergyAlertTick(context.Background(), 2); err != nil {
		t.Fatalf("low energy tick2: %v", err)
	}
	inbox, err := srv.store.ListMailbox(context.Background(), userID, "inbox", "", "[LOW-TOKEN]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list low-token inbox: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("expected cooldown to suppress repeated low-token alerts, got=%d", len(inbox))
	}
}

func TestLowTokenAlertShouldSendDoesNotMarkUntilMailSent(t *testing.T) {
	srv := newTestServer()
	now := time.Now().UTC()
	userID := "u-low-token"
	cooldown := time.Hour

	if !srv.shouldSendLowTokenAlert(userID, cooldown, now) {
		t.Fatalf("first low-token decision should allow send")
	}
	if !srv.shouldSendLowTokenAlert(userID, cooldown, now) {
		t.Fatalf("decision should remain true before mark")
	}
	srv.markLowTokenAlertSent(userID, now)
	if srv.shouldSendLowTokenAlert(userID, cooldown, now.Add(5*time.Minute)) {
		t.Fatalf("decision should be blocked after mark and before cooldown")
	}
	if !srv.shouldSendLowTokenAlert(userID, cooldown, now.Add(cooldown+time.Minute)) {
		t.Fatalf("decision should recover after cooldown")
	}
}

func TestWorldCostAlertsUsesStoredSettingsByDefault(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	if _, err := srv.store.UpsertWorldSetting(ctx, store.WorldSetting{
		Key:   "world_cost_alert_settings",
		Value: `{"threshold_amount":10,"top_users":1,"scan_limit":20}`,
	}); err != nil {
		t.Fatalf("upsert world setting: %v", err)
	}
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-s1",
		CostType: "life",
		Amount:   11,
		Units:    1,
	})
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-s2",
		CostType: "life",
		Amount:   12,
		Units:    1,
	})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alerts", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("cost alerts status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"threshold_amount":10`)) ||
		!bytes.Contains(body, []byte(`"top_users":1`)) ||
		!bytes.Contains(body, []byte(`"limit":20`)) {
		t.Fatalf("expected stored settings used by default: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"user_id":"u-s2"`)) || bytes.Contains(body, []byte(`"user_id":"u-s1"`)) {
		t.Fatalf("expected only top 1 user by stored settings: %s", w.Body.String())
	}
}

func TestWorldCostAlertNotificationsDedupAndThrottle(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	if _, err := srv.store.UpsertWorldSetting(ctx, store.WorldSetting{
		Key:   "world_cost_alert_settings",
		Value: `{"threshold_amount":10,"top_users":10,"scan_limit":100}`,
	}); err != nil {
		t.Fatalf("upsert world setting: %v", err)
	}
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-n1",
		CostType: "life",
		Amount:   11,
		Units:    1,
	})
	if err := srv.runWorldCostAlertNotifications(ctx, 1); err != nil {
		t.Fatalf("run notify #1: %v", err)
	}
	inbox, err := srv.store.ListMailbox(ctx, "u-n1", "inbox", "", "", nil, nil, 100)
	if err != nil {
		t.Fatalf("list inbox #1: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("expected first notification mail, got %d", len(inbox))
	}

	// Same amount + within cooldown => dedup (no new mail).
	if err := srv.runWorldCostAlertNotifications(ctx, 2); err != nil {
		t.Fatalf("run notify #2: %v", err)
	}
	inbox, err = srv.store.ListMailbox(ctx, "u-n1", "inbox", "", "", nil, nil, 100)
	if err != nil {
		t.Fatalf("list inbox #2: %v", err)
	}
	if len(inbox) != 1 {
		t.Fatalf("expected dedup keep 1 mail, got %d", len(inbox))
	}

	// Amount increased => send immediately even within cooldown.
	_, _ = srv.store.AppendCostEvent(ctx, store.CostEvent{
		UserID:   "u-n1",
		CostType: "think.chat.reply",
		Amount:   2,
		Units:    2,
	})
	if err := srv.runWorldCostAlertNotifications(ctx, 3); err != nil {
		t.Fatalf("run notify #3: %v", err)
	}
	inbox, err = srv.store.ListMailbox(ctx, "u-n1", "inbox", "", "", nil, nil, 100)
	if err != nil {
		t.Fatalf("list inbox #3: %v", err)
	}
	if len(inbox) != 2 {
		t.Fatalf("expected second alert on amount increase, got %d", len(inbox))
	}
}

func TestWorldCostAlertNotificationsEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	if _, err := srv.store.SendMail(ctx, clawWorldSystemID, []string{"u-a"}, "[WORLD-COST-ALERT] user=u-a amount=12 threshold=10", "body-a"); err != nil {
		t.Fatalf("send alert mail a: %v", err)
	}
	if _, err := srv.store.SendMail(ctx, clawWorldSystemID, []string{"u-b"}, "[WORLD-COST-ALERT] user=u-b amount=15 threshold=10", "body-b"); err != nil {
		t.Fatalf("send alert mail b: %v", err)
	}
	if _, err := srv.store.SendMail(ctx, clawWorldSystemID, []string{"u-c"}, "normal-subject", "body-c"); err != nil {
		t.Fatalf("send normal mail: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alert-notifications?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("notifications status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"to_user_id":"u-a"`)) || !bytes.Contains(body, []byte(`"to_user_id":"u-b"`)) {
		t.Fatalf("expected alert recipients in log: %s", w.Body.String())
	}
	if bytes.Contains(body, []byte(`"to_user_id":"u-c"`)) {
		t.Fatalf("did not expect normal mail in alert log: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alert-notifications?limit=20&user_id=u-a", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("notifications filtered status=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"to_user_id":"u-a"`)) || bytes.Contains(body, []byte(`"to_user_id":"u-b"`)) {
		t.Fatalf("expected only u-a in filtered alert log: %s", w.Body.String())
	}
}

func TestWorldEvolutionScoreAndAlertsEndpoints(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()

	w1 := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w1.Code != http.StatusAccepted {
		t.Fatalf("register #1 status=%d body=%s", w1.Code, w1.Body.String())
	}
	w2 := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w2.Code != http.StatusAccepted {
		t.Fatalf("register #2 status=%d body=%s", w2.Code, w2.Body.String())
	}

	var r1 map[string]any
	if err := json.Unmarshal(w1.Body.Bytes(), &r1); err != nil {
		t.Fatalf("unmarshal register #1: %v", err)
	}
	var r2 map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &r2); err != nil {
		t.Fatalf("unmarshal register #2: %v", err)
	}
	u1 := r1["item"].(map[string]any)["user_id"].(string)
	u2 := r2["item"].(map[string]any)["user_id"].(string)

	_, _ = srv.store.SendMail(ctx, u1, []string{clawWorldSystemID}, "autonomy-loop/progress", "result=done\nevidence=proposal_id=1")
	_, _ = srv.store.SendMail(ctx, u1, []string{u2}, "collab/proposal", "need review")

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/evolution-score?window_minutes=60", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("evolution score status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	for _, key := range []string{
		`"overall_score":`,
		`"survival":`,
		`"autonomy":`,
		`"collaboration":`,
		`"governance":`,
		`"knowledge":`,
	} {
		if !bytes.Contains(body, []byte(key)) {
			t.Fatalf("evolution score missing %s: %s", key, w.Body.String())
		}
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/evolution-alerts?window_minutes=60", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("evolution alerts status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"alert_count"`)) {
		t.Fatalf("evolution alerts missing alert_count: %s", w.Body.String())
	}
}

func TestWorldEvolutionAlertNotificationsDedupAndEndpoint(t *testing.T) {
	srv := newTestServer()
	ctx := context.Background()
	if _, err := srv.store.UpsertWorldSetting(ctx, store.WorldSetting{
		Key: worldEvolutionAlertSettingsKey,
		Value: `{
			"window_minutes":60,
			"mail_scan_limit":120,
			"kb_scan_limit":300,
			"warn_threshold":95,
			"critical_threshold":80,
			"notify_cooldown_seconds":600
		}`,
	}); err != nil {
		t.Fatalf("upsert evolution settings: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}
	if err := srv.runWorldEvolutionAlertNotifications(ctx, 1); err != nil {
		t.Fatalf("run evolution notify #1: %v", err)
	}
	if err := srv.runWorldEvolutionAlertNotifications(ctx, 2); err != nil {
		t.Fatalf("run evolution notify #2: %v", err)
	}
	outbox, err := srv.store.ListMailbox(ctx, clawWorldSystemID, "outbox", "", "[WORLD-EVOLUTION-ALERT]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list evolution alert outbox: %v", err)
	}
	if len(outbox) != 1 {
		t.Fatalf("expected dedup evolution alert mails=1, got=%d", len(outbox))
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/evolution-alert-notifications?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("evolution notify endpoint status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`[WORLD-EVOLUTION-ALERT]`)) {
		t.Fatalf("evolution notify endpoint missing alert subject: %s", w.Body.String())
	}
}

func TestTokenDrainUsesTianDaoLifeCost(t *testing.T) {
	srv := newTestServer()
	srv.cfg.LifeCostPerTick = 3

	_, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "u-life-cost",
		Name:        "u-life-cost",
		Provider:    "test",
		Status:      "running",
		Initialized: true,
	})
	if err != nil {
		t.Fatalf("upsert bot: %v", err)
	}
	if _, err := srv.store.Recharge(context.Background(), "u-life-cost", 10); err != nil {
		t.Fatalf("recharge: %v", err)
	}
	if err := srv.runTokenDrainTick(context.Background(), 42); err != nil {
		t.Fatalf("run token drain: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id=u-life-cost", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("token accounts status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance":7`)) {
		t.Fatalf("expected balance=7 after life cost drain: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-events?user_id=u-life-cost&limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("world cost events status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"cost_type":"life"`)) {
		t.Fatalf("expected life cost event: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"amount":3`)) {
		t.Fatalf("expected amount=3 cost event: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"tick_id":42`)) {
		t.Fatalf("expected tick_id=42 cost event: %s", w.Body.String())
	}
}

func TestCommCostEventsRecordedForMailAndChat(t *testing.T) {
	srv := newTestServer()
	srv.cfg.CommCostRateMilli = 1000

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": "u-comm",
		"to_user_ids":  []string{"u-peer"},
		"subject":      "hello",
		"body":         "world",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("mail send status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/chat/send", map[string]any{
		"user_id": "u-comm",
		"message": "chat payload",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("chat send status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-events?user_id=u-comm&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("cost events status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"cost_type":"comm.mail.send"`)) {
		t.Fatalf("expected comm.mail.send cost event: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"cost_type":"comm.chat.send"`)) {
		t.Fatalf("expected comm.chat.send cost event: %s", w.Body.String())
	}
}

func TestChatLatestWinsCancelsRunningAndExecutesNewest(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ChatWorkerCount = 1
	srv.cfg.ChatLatestWins = true
	srv.cfg.ChatCancelRunning = true
	srv.cfg.ChatReplyTimeout = 3 * time.Second
	srv.cfg.ChatRetryDelay = 20 * time.Millisecond

	var calls int32
	srv.chatAgentCall = func(ctx context.Context, userID, message string) (string, string, string, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			<-ctx.Done()
			return "", "sess-cancel", "pod-a", ctx.Err()
		}
		return "reply:" + message, "sess-ok", "pod-b", nil
	}
	srv.startChatWorkerPool()

	send := func(msg string) {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/chat/send", map[string]any{
			"user_id": "u-chat",
			"message": msg,
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("chat send status=%d body=%s", w.Code, w.Body.String())
		}
	}

	// First message starts running and should be canceled by the second message.
	send("m1")
	waitDeadline := time.Now().Add(2 * time.Second)
	for {
		w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/chat/state?user_id=u-chat", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("chat state status=%d body=%s", w.Code, w.Body.String())
		}
		var state map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &state)
		if state["running"] != nil {
			break
		}
		if time.Now().After(waitDeadline) {
			t.Fatalf("first chat task did not enter running state in time: %s", w.Body.String())
		}
		time.Sleep(30 * time.Millisecond)
	}
	send("m2")

	deadline := time.Now().Add(5 * time.Second)
	var finalBody string
	ok := false
	for time.Now().Before(deadline) {
		w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/chat/state?user_id=u-chat", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("chat state status=%d body=%s", w.Code, w.Body.String())
		}
		finalBody = w.Body.String()
		var state map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &state)
		stats, _ := state["recent_statuses"].(map[string]any)
		cancelCnt := int64(0)
		okCnt := int64(0)
		if v, exists := stats["canceled"]; exists {
			cancelCnt = int64(v.(float64))
		}
		if v, exists := stats["succeeded"]; exists {
			okCnt = int64(v.(float64))
		}
		if cancelCnt >= 1 && okCnt >= 1 {
			ok = true
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !ok {
		t.Fatalf("expected canceled+success in chat state: %s", finalBody)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/chat/history?user_id=u-chat&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("chat history status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`reply:m2`)) {
		t.Fatalf("expected reply for latest message m2: %s", w.Body.String())
	}
}

func TestExtractFallbackReplySkipsPluginNoise(t *testing.T) {
	stdout := strings.Join([]string{
		"[diagnostic] lane enqueue: lane=main queueSize=1",
		"[plugins] plugins.allow is empty; discovered non-bundled plugins may auto-load",
		"[agent/embedded] embedded run start",
		"[context-diag] pre-prompt",
	}, "\n")
	if got := extractFallbackReply(stdout, ""); got != "" {
		t.Fatalf("expected empty fallback for plugin/diagnostic noise, got %q", got)
	}
}

func TestIsContextTimeoutOrCancel(t *testing.T) {
	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer deadlineCancel()
	time.Sleep(10 * time.Millisecond)
	if !isContextTimeoutOrCancel(context.DeadlineExceeded, deadlineCtx) {
		t.Fatalf("expected deadline exceeded to be recognized")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if !isContextTimeoutOrCancel(context.Canceled, cancelCtx) {
		t.Fatalf("expected canceled context to be recognized")
	}

	if isContextTimeoutOrCancel(nil, context.Background()) {
		t.Fatalf("expected nil error + live context to be false")
	}
}

func TestDefaultRuntimeChatSessionID(t *testing.T) {
	if got := defaultRuntimeChatSessionID(""); got != "" {
		t.Fatalf("empty user id should return empty session id, got %q", got)
	}
	if got := defaultRuntimeChatSessionID(" user-abc "); got != "runtime-chat-user-abc" {
		t.Fatalf("unexpected default session id: got=%q", got)
	}
}

func TestOpenClawBootstrapScriptDefaultsRuntimeChatSession(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "http://runtime.local/v1/bots/openclaw/user-abc/", nil)
	script := srv.openClawBootstrapScript(req, "user-abc")
	if !strings.Contains(script, `ws://runtime.local/v1/bots/openclaw/user-abc/`) {
		t.Fatalf("expected injected gateway url for user dashboard: %s", script)
	}
	if !strings.Contains(script, `runtime-chat-user-abc`) {
		t.Fatalf("expected runtime session default to use user chat session: %s", script)
	}
	if !strings.Contains(script, `currentKey==="main"`) || !strings.Contains(script, `currentKey==="agent:main:main"`) || !strings.Contains(script, `s.sessionKey=runtimeSession||"main"`) {
		t.Fatalf("expected session key migration guard for main/empty sessions: %s", script)
	}
}

func TestOpenClawBootstrapScriptFallbackSessionWhenUserMissing(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "http://runtime.local/v1/bots/openclaw//", nil)
	script := srv.openClawBootstrapScript(req, "")
	if !strings.Contains(script, `runtimeSession="main"`) {
		t.Fatalf("expected fallback runtime session when user id is empty: %s", script)
	}
}

func TestOpenClawDashboardConfigRequiresUserID(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/system/openclaw-dashboard-config", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing user_id, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOpenClawDashboardConfigReturnsUserToken(t *testing.T) {
	srv := newTestServer()
	if _, err := srv.store.UpsertBot(context.Background(), store.BotUpsertInput{
		BotID:       "user-openclaw-1",
		Name:        "user-openclaw-1",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.UpsertBotCredentials(context.Background(), store.BotCredentials{
		UserID:       "user-openclaw-1",
		GatewayToken: "gw-openclaw-1",
	}); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/system/openclaw-dashboard-config?user_id=user-openclaw-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload.Token != "gw-openclaw-1" {
		t.Fatalf("unexpected token: got=%q", payload.Token)
	}
}

func TestNextRuntimeChatRetrySessionID(t *testing.T) {
	base := defaultRuntimeChatSessionID("user-abc")
	got := nextRuntimeChatRetrySessionID("user-abc")
	if got == "" {
		t.Fatalf("expected non-empty retry session id")
	}
	if !strings.HasPrefix(got, base+"-retry-") {
		t.Fatalf("unexpected retry session prefix: got=%q base=%q", got, base)
	}
	if got := nextRuntimeChatRetrySessionID(""); got != "" {
		t.Fatalf("empty user id should return empty retry session id, got=%q", got)
	}
}

func TestCurrentOrDefaultChatSessionID(t *testing.T) {
	srv := newTestServer()
	got := srv.currentOrDefaultChatSessionID(" user-abc ")
	if got != "runtime-chat-user-abc" {
		t.Fatalf("unexpected default session id from server: got=%q", got)
	}
	srv.chatMu.Lock()
	stored := srv.chatSessions["user-abc"]
	srv.chatMu.Unlock()
	if stored != got {
		t.Fatalf("server did not persist default session id: stored=%q got=%q", stored, got)
	}
	if got2 := srv.currentOrDefaultChatSessionID("user-abc"); got2 != got {
		t.Fatalf("expected idempotent session id: first=%q second=%q", got, got2)
	}
	if gotEmpty := srv.currentOrDefaultChatSessionID("   "); gotEmpty != "" {
		t.Fatalf("empty user id should return empty session id, got=%q", gotEmpty)
	}
}

func TestCurrentOrDefaultChatSessionIDUsesExisting(t *testing.T) {
	srv := newTestServer()
	srv.chatMu.Lock()
	srv.chatSessions["user-abc"] = "sess-existing"
	srv.chatMu.Unlock()
	if got := srv.currentOrDefaultChatSessionID("user-abc"); got != "sess-existing" {
		t.Fatalf("expected existing session id to be reused, got=%q", got)
	}
}

func TestSetChatSession(t *testing.T) {
	srv := newTestServer()
	srv.setChatSession(" user-abc ", " sess-1 ")
	srv.chatMu.Lock()
	got := srv.chatSessions["user-abc"]
	srv.chatMu.Unlock()
	if got != "sess-1" {
		t.Fatalf("unexpected stored session: got=%q", got)
	}
}

func TestSetChatSessionNoopOnEmptyValues(t *testing.T) {
	srv := newTestServer()
	srv.setChatSession("", "sess")
	srv.setChatSession("user-abc", "")
	srv.setChatSession("  ", "sess")
	srv.setChatSession("user-abc", "   ")
	srv.chatMu.Lock()
	count := len(srv.chatSessions)
	srv.chatMu.Unlock()
	if count != 0 {
		t.Fatalf("expected empty session map, got count=%d", count)
	}
}

func TestChatExecTimeoutSecondsCap(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ChatReplyTimeout = 8 * time.Minute
	if got := srv.chatExecTimeoutSeconds(); got != 180 {
		t.Fatalf("chat exec timeout cap mismatch: got=%d want=180", got)
	}

	srv.cfg.ChatReplyTimeout = 185 * time.Second
	if got := srv.chatExecTimeoutSeconds(); got != 175 {
		t.Fatalf("chat exec timeout should keep 10s headroom: got=%d want=175", got)
	}

	srv.cfg.ChatReplyTimeout = 40 * time.Second
	if got := srv.chatExecTimeoutSeconds(); got != 30 {
		t.Fatalf("chat exec timeout for short reply timeout: got=%d want=30", got)
	}

	srv.cfg.ChatReplyTimeout = 15 * time.Second
	if got := srv.chatExecTimeoutSeconds(); got != 20 {
		t.Fatalf("chat exec timeout lower bound mismatch: got=%d want=20", got)
	}
}

func TestSendChatToOpenClawRequiresUserID(t *testing.T) {
	srv := newTestServer()
	_, _, _, err := srv.sendChatToOpenClaw(context.Background(), "   ", "hello")
	if err == nil {
		t.Fatalf("expected error for empty user id")
	}
	if !strings.Contains(err.Error(), "user_id is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestThinkCostEventAmountByRateMilli(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ThinkCostRateMilli = 1500

	srv.appendThinkCostEvent(context.Background(), "u-think", 11, 7, map[string]any{"source": "unit-test"})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-events?user_id=u-think&limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("cost events status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"cost_type":"think.chat.reply"`)) {
		t.Fatalf("expected think.chat.reply event: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"units":18`)) {
		t.Fatalf("expected units=18 event: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`"amount":27`)) {
		t.Fatalf("expected amount=27 event (ceil(18*1.5)): %s", w.Body.String())
	}
}

func TestThinkCostEventConsumesTokenWhenEnabled(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ThinkCostRateMilli = 1000
	srv.cfg.ActionCostConsume = true

	if _, err := srv.store.Recharge(context.Background(), "u-consume", 5); err != nil {
		t.Fatalf("recharge: %v", err)
	}
	srv.appendThinkCostEvent(context.Background(), "u-consume", 6, 4, map[string]any{"source": "unit-test"})

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id=u-consume", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("token accounts status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance":0`)) {
		t.Fatalf("expected balance floor to 0: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-events?user_id=u-consume&limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("cost events status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"cost_type":"think.chat.reply"`)) {
		t.Fatalf("expected think.chat.reply event: %s", w.Body.String())
	}
	// requested_amount=10 but charged only 5 due to floor consume.
	if !bytes.Contains(body, []byte(`"amount":5`)) {
		t.Fatalf("expected charged amount=5 after floor consume: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`\"deducted_amount\":5`)) {
		t.Fatalf("expected deducted_amount=5 in meta: %s", w.Body.String())
	}
	if !bytes.Contains(body, []byte(`\"requested_amount\":10`)) {
		t.Fatalf("expected requested_amount=10 in meta: %s", w.Body.String())
	}
}

func TestMailingListFlow(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()
	c := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/lists/create", map[string]any{
		"owner_user_id": a,
		"name":          "dev-list",
		"description":   "dev",
		"initial_users": []string{b, c},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("create list status=%d body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	listID := created["item"].(map[string]any)["list_id"].(string)

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/mail/send-list", map[string]any{
		"from_user_id": a,
		"list_id":      listID,
		"subject":      "hello list",
		"body":         "ship it",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("send list status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/mail/inbox?user_id="+b+"&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("inbox status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("hello list")) {
		t.Fatalf("inbox missing list mail: %s", w.Body.String())
	}
}

func TestTokenTransferTipAndWish(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/transfer", map[string]any{
		"from_user_id": a,
		"to_user_id":   b,
		"amount":       30,
		"memo":         "trade",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("transfer status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/tip", map[string]any{
		"from_user_id": a,
		"to_user_id":   b,
		"amount":       20,
		"reason":       "nice job",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("tip status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/wish/create", map[string]any{
		"user_id":       b,
		"title":         "need compute",
		"reason":        "benchmark",
		"target_amount": 10,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("wish create status=%d body=%s", w.Code, w.Body.String())
	}
	var wish map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &wish)
	wishID := wish["item"].(map[string]any)["wish_id"].(string)
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/token/wish/fulfill", map[string]any{
		"wish_id":        wishID,
		"fulfilled_by":   "clawcolony-admin",
		"granted_amount": 10,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("wish fulfill status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+a, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("account a status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance":950`)) {
		t.Fatalf("expected a balance=950: %s", w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+b, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("account b status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance":1060`)) {
		t.Fatalf("expected b balance=1060: %s", w.Body.String())
	}
}

func TestToolRegistryReviewAndInvoke(t *testing.T) {
	srv := newTestServer()
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}
	var reg map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &reg)
	u := reg["item"].(map[string]any)["user_id"].(string)

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/register", map[string]any{
		"user_id":     u,
		"tool_id":     "calc-v2",
		"name":        "calc-v2",
		"description": "math",
		"tier":        "T2",
		"manifest":    "{}",
		"code":        "return 42",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("tool register status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "calc-v2",
		"params":  map[string]any{"x": 1},
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("invoke before review status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/review", map[string]any{
		"reviewer_user_id": "clawcolony-admin",
		"tool_id":          "calc-v2",
		"decision":         "approve",
		"review_note":      "ok",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("tool review status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := srv.store.UpsertUserLifeState(context.Background(), store.UserLifeState{UserID: u, State: "dying"}); err != nil {
		t.Fatalf("set dying: %v", err)
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "calc-v2",
		"params":  map[string]any{"x": 1},
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("invoke in dying should block T2 status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := srv.store.UpsertUserLifeState(context.Background(), store.UserLifeState{UserID: u, State: "alive"}); err != nil {
		t.Fatalf("set alive: %v", err)
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "calc-v2",
		"params":  map[string]any{"x": 1},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("invoke active status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestToolInvokeExecModeUsesSandboxRunner(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ToolRuntimeExec = true
	called := false
	srv.toolSandboxExec = func(_ context.Context, input toolSandboxInput) (toolSandboxResult, error) {
		called = true
		if input.ToolID != "shell-v1" {
			t.Fatalf("unexpected tool id: %s", input.ToolID)
		}
		if !strings.Contains(input.ParamsJSON, `"name":"colony"`) {
			t.Fatalf("unexpected params json: %s", input.ParamsJSON)
		}
		return toolSandboxResult{
			OK:             true,
			SandboxProfile: "t2-strong",
			Message:        "sandbox executed",
			ExitCode:       0,
			DurationMS:     9,
			Stdout:         "hello colony",
		}, nil
	}

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}
	var reg map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &reg)
	u := reg["item"].(map[string]any)["user_id"].(string)

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/register", map[string]any{
		"user_id":     u,
		"tool_id":     "shell-v1",
		"name":        "shell-v1",
		"description": "echo",
		"tier":        "T2",
		"manifest":    "{}",
		"code":        "echo hello",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("tool register status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/review", map[string]any{
		"reviewer_user_id": "clawcolony-admin",
		"tool_id":          "shell-v1",
		"decision":         "approve",
		"review_note":      "ok",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("tool review status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "shell-v1",
		"params":  map[string]any{"name": "colony"},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("invoke status=%d body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatalf("expected sandbox runner to be called")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"sandbox_profile":"t2-strong"`)) {
		t.Fatalf("missing sandbox profile in body: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"stdout":"hello colony"`)) {
		t.Fatalf("missing stdout in body: %s", w.Body.String())
	}
}

func TestToolSandboxProfileTierPolicy(t *testing.T) {
	t0 := toolSandboxProfileForTier("T0")
	if !t0.NetworkNone || t0.APIMode != "none" {
		t.Fatalf("unexpected T0 profile: %+v", t0)
	}
	t1 := toolSandboxProfileForTier("T1")
	if t1.NetworkNone || t1.APIMode != "colony-read" {
		t.Fatalf("unexpected T1 profile: %+v", t1)
	}
	t2 := toolSandboxProfileForTier("T2")
	if t2.NetworkNone || t2.APIMode != "colony-readwrite" {
		t.Fatalf("unexpected T2 profile: %+v", t2)
	}
	t3 := toolSandboxProfileForTier("T3")
	if t3.NetworkNone || t3.APIMode != "external-restricted" {
		t.Fatalf("unexpected T3 profile: %+v", t3)
	}
}

func TestToolInvokeURLPolicyByTier(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ToolT3AllowHosts = "example.com"

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}
	var reg map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &reg)
	u := reg["item"].(map[string]any)["user_id"].(string)

	registerAndApprove := func(toolID, tier string) {
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/register", map[string]any{
			"user_id":     u,
			"tool_id":     toolID,
			"name":        toolID,
			"description": "url policy",
			"tier":        tier,
			"manifest":    "{}",
			"code":        "echo ok",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("tool register %s status=%d body=%s", toolID, w.Code, w.Body.String())
		}
		w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/review", map[string]any{
			"reviewer_user_id": "clawcolony-admin",
			"tool_id":          toolID,
			"decision":         "approve",
			"review_note":      "ok",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("tool review %s status=%d body=%s", toolID, w.Code, w.Body.String())
		}
	}

	registerAndApprove("url-t0", "T0")
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "url-t0",
		"params":  map[string]any{"url": "https://example.com"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("T0 url invoke status=%d body=%s", w.Code, w.Body.String())
	}

	registerAndApprove("url-t1", "T1")
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "url-t1",
		"params":  map[string]any{"url": "https://example.com"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("T1 external url invoke status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "url-t1",
		"params":  map[string]any{"url": "http://clawcolony.freewill.svc.cluster.local/v1/meta"},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("T1 colony url invoke status=%d body=%s", w.Code, w.Body.String())
	}

	registerAndApprove("url-t3", "T3")
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "url-t3",
		"params":  map[string]any{"url": "https://example.com"},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("T3 allowlisted url invoke status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/tools/invoke", map[string]any{
		"user_id": u,
		"tool_id": "url-t3",
		"params":  map[string]any{"url": "https://not-allowed.example.org"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("T3 non-allowlisted url invoke status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLifeWillExecuteAndBountyFlow(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/life/set-will", map[string]any{
		"user_id": a,
		"note":    "inherit",
		"beneficiaries": []map[string]any{
			{"user_id": b, "ratio": 10000},
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("set will status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := srv.store.Recharge(context.Background(), a, 20); err != nil {
		t.Fatalf("recharge a: %v", err)
	}
	if _, err := srv.store.UpsertUserLifeState(context.Background(), store.UserLifeState{
		UserID: a, State: "dead", DeadAtTick: 1, Reason: "test",
	}); err != nil {
		t.Fatalf("set dead: %v", err)
	}
	if err := srv.runLifeStateTransitions(context.Background(), 2); err != nil {
		t.Fatalf("run life transitions: %v", err)
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/token/accounts?user_id="+b, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("account b status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance":2020`)) {
		t.Fatalf("expected b balance includes inheritance: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bounty/post", map[string]any{
		"poster_user_id": b,
		"description":    "build parser",
		"reward":         50,
		"criteria":       "pass tests",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("bounty post status=%d body=%s", w.Code, w.Body.String())
	}
	var bounty map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &bounty)
	bid := int64(bounty["item"].(map[string]any)["bounty_id"].(float64))
	w = doJSONRequest(t, srv.mux, http.MethodGet, fmt.Sprintf("/v1/bounty/get?bounty_id=%d", bid), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("bounty get status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"bounty_id":`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"description":"build parser"`)) {
		t.Fatalf("bounty get should return posted item: %s", w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bounty/claim", map[string]any{
		"bounty_id": bid,
		"user_id":   a,
		"note":      "i can do",
	})
	if w.Code != http.StatusConflict {
		// a is dead, should fail
		t.Fatalf("dead claimer should fail status=%d body=%s", w.Code, w.Body.String())
	}
	c := register()
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bounty/claim", map[string]any{
		"bounty_id": bid,
		"user_id":   c,
		"note":      "i can do",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("claim status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bounty/verify", map[string]any{
		"bounty_id":         bid,
		"approver_user_id":  "clawcolony-admin",
		"approved":          true,
		"candidate_user_id": c,
		"note":              "done",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("verify status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBountyGetValidationAndErrors(t *testing.T) {
	srv := newTestServer()

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bounty/get", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing bounty_id should be bad request, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bounty/get?bounty_id=0", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid bounty_id should be bad request, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bounty/get?bounty_id=999999", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown bounty should be not found, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bounty/get?bounty_id=1", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method should be rejected, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGenesisBootstrapAndMetabolismAndNPC(t *testing.T) {
	srv := newTestServer()
	srv.cfg.MetabolismInterval = 1
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	u := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/genesis/bootstrap/start", map[string]any{
		"proposer_user_id": u,
		"title":            "charter",
		"reason":           "boot",
		"constitution":     "const v1",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("genesis start status=%d body=%s", w.Code, w.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	pid := int64(start["proposal"].(map[string]any)["id"].(float64))
	now := time.Now().UTC()
	if _, err := srv.store.CloseKBProposal(context.Background(), pid, "approved", "ok", 1, 1, 0, 0, 1, now); err != nil {
		t.Fatalf("close proposal approved: %v", err)
	}
	if _, _, err := srv.store.ApplyKBProposal(context.Background(), pid, u, now); err != nil {
		t.Fatalf("apply proposal: %v", err)
	}
	genesisStateMu.Lock()
	st, err := srv.getGenesisState(context.Background())
	if err != nil {
		genesisStateMu.Unlock()
		t.Fatalf("get genesis state: %v", err)
	}
	st.BootstrapPhase = "applied"
	if err := srv.saveGenesisState(context.Background(), st); err != nil {
		genesisStateMu.Unlock()
		t.Fatalf("save genesis state: %v", err)
	}
	genesisStateMu.Unlock()
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/genesis/bootstrap/seal", map[string]any{
		"user_id":     u,
		"proposal_id": pid,
		"seal_reason": "done",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("genesis seal status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/npc/tasks/create", map[string]any{
		"npc_id":    "historian",
		"task_type": "record",
		"payload":   "checkpoint",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("npc task create status=%d body=%s", w.Code, w.Body.String())
	}

	if err := srv.runWorldTickWithTrigger(context.Background(), "manual", 0); err <= 0 {
		t.Fatalf("run world tick failed")
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/npc/tasks?npc_id=historian&status=done&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("npc task list status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"done"`)) {
		t.Fatalf("npc task not done: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/metabolism/report?limit=5", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("metabolism report status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"score_count"`)) {
		t.Fatalf("metabolism report missing score_count: %s", w.Body.String())
	}
}

func TestGenesisBootstrapCosignReviewVoteSealFlow(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	a := register()
	b := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/genesis/bootstrap/start", map[string]any{
		"proposer_user_id":      a,
		"title":                 "genesis-charter",
		"reason":                "bootstrap",
		"constitution":          "const-v2",
		"cosign_quorum":         2,
		"review_window_seconds": 1,
		"vote_window_seconds":   1,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("genesis start status=%d body=%s", w.Code, w.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	pid := int64(start["proposal"].(map[string]any)["id"].(float64))

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/proposals/cosign", map[string]any{
		"user_id":     b,
		"proposal_id": pid,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("cosign status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/genesis/state", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("genesis state status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"bootstrap_phase":"review"`)) {
		t.Fatalf("expected review phase after cosign: %s", w.Body.String())
	}
	reviewInbox, err := srv.store.ListMailbox(context.Background(), a, "inbox", "", "[GENESIS][REVIEW]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list review inbox: %v", err)
	}
	if len(reviewInbox) == 0 {
		t.Fatalf("expected [GENESIS][REVIEW] mail after cosign transition")
	}

	time.Sleep(1100 * time.Millisecond)
	srv.kbAutoProgressDiscussing(context.Background())

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(pid, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"voting"`)) {
		t.Fatalf("proposal not in voting: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/proposals/vote", map[string]any{
		"user_id":     a,
		"proposal_id": pid,
		"choice":      "yes",
		"reason":      "agree",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("vote a status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/proposals/vote", map[string]any{
		"user_id":     b,
		"proposal_id": pid,
		"choice":      "yes",
		"reason":      "agree",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("vote b status=%d body=%s", w.Code, w.Body.String())
	}

	time.Sleep(1100 * time.Millisecond)
	srv.kbFinalizeExpiredVotes(context.Background())

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/apply", map[string]any{
		"proposal_id": pid,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/genesis/bootstrap/seal", map[string]any{
		"user_id":     a,
		"proposal_id": pid,
		"seal_reason": "finalize",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("seal status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"bootstrap_phase":"sealed"`)) {
		t.Fatalf("expected sealed bootstrap phase: %s", w.Body.String())
	}
}

func TestGenesisBootstrapCosignInitializesDefaultsWhenUnset(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	proposer := register()
	cosigner := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/genesis/bootstrap/start", map[string]any{
		"proposer_user_id": proposer,
		"title":            "genesis-defaults",
		"reason":           "defaults-compat",
		"constitution":     "const-defaults",
		"cosign_quorum":    1,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("genesis start status=%d body=%s", w.Code, w.Body.String())
	}
	var start map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	pid := int64(start["proposal"].(map[string]any)["id"].(float64))

	st, err := srv.getGenesisState(context.Background())
	if err != nil {
		t.Fatalf("get genesis state: %v", err)
	}
	// Simulate old/unset state payload and verify enroll path initializes defaults.
	st.RequiredCosigns = 0
	st.ReviewWindowSeconds = 0
	st.CurrentCosigns = 0
	st.BootstrapPhase = "cosign"
	if err := srv.saveGenesisState(context.Background(), st); err != nil {
		t.Fatalf("save genesis state: %v", err)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/governance/proposals/cosign", map[string]any{
		"user_id":     cosigner,
		"proposal_id": pid,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("cosign status=%d body=%s", w.Code, w.Body.String())
	}

	stateResp := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/genesis/state", nil)
	if stateResp.Code != http.StatusOK {
		t.Fatalf("genesis state status=%d body=%s", stateResp.Code, stateResp.Body.String())
	}
	var stateBody map[string]any
	if err := json.Unmarshal(stateResp.Body.Bytes(), &stateBody); err != nil {
		t.Fatalf("unmarshal state response: %v", err)
	}
	item, _ := stateBody["item"].(map[string]any)
	if strings.TrimSpace(fmt.Sprintf("%v", item["bootstrap_phase"])) != "review" {
		t.Fatalf("expected bootstrap_phase=review, got=%v body=%s", item["bootstrap_phase"], stateResp.Body.String())
	}
	if int(item["required_cosigns"].(float64)) != 1 {
		t.Fatalf("required_cosigns=%v want 1", item["required_cosigns"])
	}
	if int(item["review_window_seconds"].(float64)) != 300 {
		t.Fatalf("review_window_seconds=%v want 300", item["review_window_seconds"])
	}

	reviewInbox, err := srv.store.ListMailbox(context.Background(), proposer, "inbox", "", "[GENESIS][REVIEW]", nil, nil, 20)
	if err != nil {
		t.Fatalf("list review inbox: %v", err)
	}
	if len(reviewInbox) == 0 {
		t.Fatalf("expected [GENESIS][REVIEW] mail after default-init transition")
	}
}

func TestClawcolonyBootstrapAliasRoutes(t *testing.T) {
	srv := newTestServer()
	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	u := register()

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/clawcolony/bootstrap/start", map[string]any{
		"proposer_user_id": u,
		"title":            "charter",
		"reason":           "bootstrap",
		"constitution":     "const",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("clawcolony bootstrap start status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/clawcolony/state", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("clawcolony state status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"bootstrap_phase"`)) {
		t.Fatalf("clawcolony state response should include bootstrap_phase: %s", w.Body.String())
	}
}

func TestNPCTickCoversAllCatalogRoles(t *testing.T) {
	srv := newTestServer()
	// Ensure there is at least one active user for archivist/monitor metrics.
	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}

	if err := srv.runWorldTickWithTrigger(context.Background(), "manual", 0); err <= 0 {
		t.Fatalf("run world tick failed")
	}
	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/npc/tasks?status=done&limit=200", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("npc task list status=%d body=%s", w.Code, w.Body.String())
	}
	required := []string{
		`"npc_id":"historian"`,
		`"npc_id":"monitor"`,
		`"npc_id":"procurement"`,
		`"npc_id":"publisher"`,
		`"npc_id":"wizard"`,
		`"npc_id":"enforcer"`,
		`"npc_id":"archivist"`,
		`"npc_id":"broker"`,
		`"npc_id":"metabolizer"`,
	}
	for _, marker := range required {
		if !bytes.Contains(w.Body.Bytes(), []byte(marker)) {
			t.Fatalf("missing npc task for %s in body=%s", marker, w.Body.String())
		}
	}

	genesisStateMu.Lock()
	profiles, err := srv.getLobsterProfileState(context.Background())
	genesisStateMu.Unlock()
	if err != nil {
		t.Fatalf("get lobster profile state: %v", err)
	}
	if len(profiles.Items) == 0 {
		t.Fatalf("expected archivist to persist profiles, got empty")
	}
}

func TestMetabolismValidatorsAndClusterTopK(t *testing.T) {
	srv := newTestServer()
	srv.cfg.MetabolismInterval = 1
	srv.cfg.MetabolismTopK = 1
	srv.cfg.MetabolismMinValidators = 2

	register := func() string {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
		if w.Code != http.StatusAccepted {
			t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		return resp["item"].(map[string]any)["user_id"].(string)
	}
	u1 := register()
	u2 := register()

	// Build two ganglia entries in same cluster(source_type=ganglia) to trigger top-k compression.
	for i := 0; i < 2; i++ {
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/ganglia/forge", map[string]any{
			"user_id":        u1,
			"name":           "g-" + strconv.Itoa(i),
			"type":           "knowledge",
			"description":    "ganglia test item",
			"implementation": "content-" + strconv.Itoa(i),
			"validation":     "self-check",
			"temporality":    "stable",
		})
		if w.Code != http.StatusAccepted {
			t.Fatalf("ganglia forge status=%d body=%s", w.Code, w.Body.String())
		}
	}

	if err := srv.runWorldTickWithTrigger(context.Background(), "manual", 0); err <= 0 {
		t.Fatalf("run world tick failed")
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/metabolism/report?limit=1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("metabolism report status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"cluster_compressed"`)) {
		t.Fatalf("report missing cluster_compressed: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"min_validators":2`)) {
		t.Fatalf("report missing min_validators=2: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/metabolism/supersede", map[string]any{
		"user_id":      u1,
		"new_id":       "ganglion:2",
		"old_id":       "ganglion:1",
		"relationship": "replace",
		"validators":   []string{u1},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("supersede status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"pending_validation"`)) {
		t.Fatalf("expected pending_validation when validators insufficient: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/metabolism/supersede", map[string]any{
		"user_id":      u1,
		"new_id":       "ganglion:3",
		"old_id":       "ganglion:2",
		"relationship": "replace",
		"validators":   []string{u1, u2},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("supersede status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"active"`)) {
		t.Fatalf("expected active when validators meet threshold: %s", w.Body.String())
	}
}
