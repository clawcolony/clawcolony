package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func newTestServer() *Server {
	cfg := config.Config{
		ListenAddr:         ":0",
		ClawWorldNamespace: "clawcolony",
		BotNamespace:       "freewill",
		DatabaseURL:        "",
	}
	st := store.NewInMemory()
	bots := bot.NewManager(st, bot.NewNoopDeployer(), "http://clawcolony.freewill.svc.cluster.local:8080", "openai-codex/gpt-5.3-codex")
	s := New(cfg, st, bots)
	s.kubeClient = nil
	s.resolveUpgradeRepoURL = func(_ context.Context, _ string) string {
		return s.cfg.UpgradeRepoURL
	}
	return s
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

func TestRoleAccessRuntimeBlocksDeployerRoutes(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/bots/upgrade/history?limit=5", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("runtime mode should block deployer route, got=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, h, http.MethodGet, "/v1/meta", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime mode should allow meta, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRoleAccessDeployerBlocksRuntimeRoutes(t *testing.T) {
	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleDeployer
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/mail/inbox?user_id=u1", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("deployer mode should block runtime route, got=%d body=%s", w.Code, w.Body.String())
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
		t.Fatalf("runtime repo should not expose deployer route directly, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestDashboardAdminProxyRuntimeForwardsToDeployer(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotMethod = r.Method
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "source": "deployer"})
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleRuntime
	srv.cfg.DeployerAPIBase = upstream.URL
	h := srv.roleAccessMiddleware(srv.mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard-admin/openclaw/admin/overview?limit=20", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard-admin runtime proxy status=%d body=%s", w.Code, w.Body.String())
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/openclaw/admin/overview" || gotQuery != "limit=20" {
		t.Fatalf("unexpected forwarded request method=%s path=%s query=%s", gotMethod, gotPath, gotQuery)
	}
}

func TestDashboardAdminProxyAllDispatchesLocal(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotMethod = r.Method
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "source": "deployer"})
	}))
	defer upstream.Close()

	srv := newTestServer()
	srv.cfg.ServiceRole = config.ServiceRoleAll
	srv.cfg.DeployerAPIBase = upstream.URL
	h := srv.roleAccessMiddleware(srv.mux)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/dashboard-admin/openclaw/admin/github/health", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard-admin proxy status=%d body=%s", w.Code, w.Body.String())
	}
	if gotMethod != http.MethodGet || gotPath != "/v1/openclaw/admin/github/health" || gotQuery != "" {
		t.Fatalf("unexpected forwarded request method=%s path=%s query=%s", gotMethod, gotPath, gotQuery)
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
	if !bytes.Contains(w.Body.Bytes(), []byte(`/api/gov/propose`)) {
		t.Fatalf("catalog missing /api gov endpoint: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`/api/colony/status`)) {
		t.Fatalf("catalog missing /api colony endpoint: %s", w.Body.String())
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

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/api/token/balance?user_id="+userA, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/token/balance status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"balance"`)) {
		t.Fatalf("/api/token/balance missing balance: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/token/transfer", map[string]any{
		"from":   userA,
		"to":     userB,
		"amount": 5,
		"memo":   "compat-transfer",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/token/transfer status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/ganglia/forge", map[string]any{
		"user_id":    userA,
		"name":       "compat-ganglion",
		"type":       "survival",
		"content":    "always check inbox and token balance",
		"validation": "smoke",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/ganglia/forge status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/api/ganglia/browse?sort_by=score&limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/ganglia/browse status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`compat-ganglion`)) {
		t.Fatalf("/api/ganglia/browse missing forged item: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/library/publish", map[string]any{
		"user_id":  userA,
		"title":    "compat-library-note",
		"content":  "library publish from api compatibility layer",
		"category": "engineering",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/library/publish status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/api/library/search?query=compat-library-note&limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/library/search status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`compat-library-note`)) {
		t.Fatalf("/api/library/search missing publish result: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/life/metamorphose", map[string]any{
		"user_id": userA,
		"changes": map[string]any{
			"focus": "optimize cooperation",
		},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/life/metamorphose status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/life/set-will", map[string]any{
		"user_id": userA,
		"token_split": map[string]any{
			userB: 10000,
		},
		"tool_heirs": []string{userB},
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/life/set-will status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/life/hibernate", map[string]any{
		"user_id": userA,
		"reason":  "compat-test",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/life/hibernate status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/life/wake", map[string]any{
		"lobster_id": userA,
		"reason":     "compat-wake",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/life/wake status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/api/colony/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/colony/status status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"population"`)) {
		t.Fatalf("/api/colony/status missing population: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/api/colony/directory", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/colony/directory status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(userA)) {
		t.Fatalf("/api/colony/directory missing userA: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/api/colony/chronicle?limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/colony/chronicle status=%d body=%s", w.Code, w.Body.String())
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

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/api/gov/propose", map[string]any{
		"user_id": userA,
		"title":   "compat-governance-proposal",
		"type":    "policy",
		"reason":  "compat test",
		"content": "governance content for compat vote flow",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/gov/propose status=%d body=%s", w.Code, w.Body.String())
	}
	var proposeResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &proposeResp); err != nil {
		t.Fatalf("unmarshal propose response: %v", err)
	}
	proposalID := int64(proposeResp["proposal"].(map[string]any)["id"].(float64))
	if proposalID <= 0 {
		t.Fatalf("invalid proposal id: %v", proposeResp)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/gov/cosign", map[string]any{
		"user_id":     userB,
		"proposal_id": proposalID,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/gov/cosign status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/start-vote", map[string]any{
		"user_id":     userA,
		"proposal_id": proposalID,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start-vote status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/gov/vote", map[string]any{
		"user_id":     userB,
		"proposal_id": proposalID,
		"choice":      "yes",
		"reason":      "looks good",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("/api/gov/vote status=%d body=%s", w.Code, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/api/gov/laws", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/gov/laws status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"law_key"`)) {
		t.Fatalf("/api/gov/laws missing law_key: %s", w.Body.String())
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
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/library/publish", map[string]any{
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

func TestBotUpgradeRequiresInternalToken(t *testing.T) {
	t.Skip("moved to clawcolony-deployer repo: runtime no longer exposes /v1/bots/upgrade* routes")
	srv := newTestServer()
	srv.cfg.UpgradeRepoURL = "https://example.com/repo.git"
	srv.cfg.UpgradeAuthToken = "upgrade-dev-token"

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/upgrade", map[string]any{
		"user_id": userID,
		"branch":  "feature/" + userID + "-smoke",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("upgrade status without token = %d, want %d body=%s", w.Code, http.StatusUnauthorized, w.Body.String())
	}
}

func TestBotUpgradeEnforcesBranchPolicy(t *testing.T) {
	t.Skip("moved to clawcolony-deployer repo: runtime no longer exposes /v1/bots/upgrade* routes")
	srv := newTestServer()
	srv.cfg.UpgradeRepoURL = "https://example.com/repo.git"
	srv.cfg.UpgradeAuthToken = "upgrade-dev-token"

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)
	creds, err := srv.store.GetBotCredentials(context.Background(), userID)
	if err != nil {
		t.Fatalf("get bot credentials: %v", err)
	}

	w = doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/bots/upgrade", map[string]any{
		"user_id": userID,
		"branch":  "main",
	}, map[string]string{"X-Clawcolony-Upgrade-Token": creds.UpgradeToken})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upgrade status with main branch = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/bots/upgrade", map[string]any{
		"user_id": userID,
		"branch":  "bad-branch-name",
	}, map[string]string{"X-Clawcolony-Upgrade-Token": creds.UpgradeToken})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("upgrade status = %d, want %d body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`branch must be main or match feature/`)) {
		t.Fatalf("missing branch policy hint: %s", w.Body.String())
	}
}

func TestBotUpgradeReturnsTaskIDAndSupportsTaskQuery(t *testing.T) {
	t.Skip("moved to clawcolony-deployer repo: runtime no longer exposes /v1/bots/upgrade* routes")
	srv := newTestServer()
	srv.cfg.UpgradeRepoURL = "https://example.com/repo.git"
	srv.cfg.UpgradeAuthToken = "upgrade-dev-token"

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)
	creds, err := srv.store.GetBotCredentials(context.Background(), userID)
	if err != nil {
		t.Fatalf("get bot credentials: %v", err)
	}

	w = doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/bots/upgrade", map[string]any{
		"user_id": userID,
		"branch":  "feature/" + userID + "-async",
	}, map[string]string{"X-Clawcolony-Upgrade-Token": creds.UpgradeToken})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upgrade status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var upgradeResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &upgradeResp); err != nil {
		t.Fatalf("unmarshal upgrade body: %v", err)
	}
	taskIDValue, ok := upgradeResp["upgrade_task_id"]
	if !ok {
		t.Fatalf("upgrade response missing upgrade_task_id: %s", w.Body.String())
	}
	taskIDFloat, ok := taskIDValue.(float64)
	if !ok || taskIDFloat <= 0 {
		t.Fatalf("invalid upgrade_task_id: %#v", taskIDValue)
	}
	taskID := int64(taskIDFloat)

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/bots/upgrade/task?upgrade_task_id="+strconv.FormatInt(taskID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("task status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"upgrade_task_id":`)) {
		t.Fatalf("task response missing upgrade_task_id: %s", w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"audit"`)) {
		t.Fatalf("task response missing audit: %s", w.Body.String())
	}
}

func TestBotUpgradeEmitsToolCostEvent(t *testing.T) {
	t.Skip("moved to clawcolony-deployer repo: runtime no longer exposes /v1/bots/upgrade* routes")
	srv := newTestServer()
	srv.cfg.UpgradeRepoURL = "https://example.com/repo.git"
	srv.cfg.UpgradeAuthToken = "upgrade-dev-token"
	srv.cfg.ToolCostRateMilli = 1000

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)
	creds, err := srv.store.GetBotCredentials(context.Background(), userID)
	if err != nil {
		t.Fatalf("get bot credentials: %v", err)
	}

	w = doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/bots/upgrade", map[string]any{
		"user_id": userID,
		"branch":  "main",
	}, map[string]string{"X-Clawcolony-Upgrade-Token": creds.UpgradeToken})
	if w.Code != http.StatusAccepted {
		t.Fatalf("upgrade status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	events := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-events?user_id="+userID+"&limit=20", nil)
	if events.Code != http.StatusOK {
		t.Fatalf("cost events status = %d, want %d body=%s", events.Code, http.StatusOK, events.Body.String())
	}
	if !bytes.Contains(events.Body.Bytes(), []byte(`"cost_type":"tool.bot.upgrade"`)) {
		t.Fatalf("expected tool.bot.upgrade cost event: %s", events.Body.String())
	}
}

func TestBotUpgradeBlockedWhenUserIsDyingForT3(t *testing.T) {
	t.Skip("moved to clawcolony-deployer repo: runtime no longer exposes /v1/bots/upgrade* routes")
	srv := newTestServer()
	srv.cfg.UpgradeRepoURL = "https://example.com/repo.git"
	srv.cfg.UpgradeAuthToken = "upgrade-dev-token"

	w := doJSONRequest(t, srv.mux, http.MethodPost, "/v1/bots/register", map[string]any{"provider": "openclaw"})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var registerResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &registerResp); err != nil {
		t.Fatalf("unmarshal register body: %v", err)
	}
	userID := registerResp["item"].(map[string]any)["user_id"].(string)
	creds, err := srv.store.GetBotCredentials(context.Background(), userID)
	if err != nil {
		t.Fatalf("get bot credentials: %v", err)
	}
	if _, err := srv.store.UpsertUserLifeState(context.Background(), store.UserLifeState{
		UserID: userID,
		State:  "dying",
	}); err != nil {
		t.Fatalf("set user life state dying: %v", err)
	}

	w = doJSONRequestWithHeaders(t, srv.mux, http.MethodPost, "/v1/bots/upgrade", map[string]any{
		"user_id": userID,
		"branch":  "main",
	}, map[string]string{"X-Clawcolony-Upgrade-Token": creds.UpgradeToken})
	if w.Code != http.StatusConflict {
		t.Fatalf("upgrade status for dying user = %d, want %d body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`tool tier T3 is not allowed in dying state`)) {
		t.Fatalf("missing tier gate reason: %s", w.Body.String())
	}
}

func TestOpenClawAdminRegisterTaskEndpoints(t *testing.T) {
	t.Skip("moved to clawcolony-deployer repo: runtime no longer exposes direct /v1/openclaw/admin/* routes")
	srv := newTestServer()
	task, err := srv.store.CreateRegisterTask(context.Background(), store.RegisterTask{
		Provider:  "openclaw",
		Status:    "running",
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create register task: %v", err)
	}
	_, err = srv.store.AppendRegisterTaskStep(context.Background(), store.RegisterTaskStep{
		TaskID:  task.ID,
		Step:    "start",
		Status:  "running",
		Message: "queued",
	})
	if err != nil {
		t.Fatalf("append register step: %v", err)
	}

	w := doJSONRequest(t, srv.mux, http.MethodGet, "/v1/openclaw/admin/register/task?register_task_id="+strconv.FormatInt(task.ID, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("register task status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"register_task_id":`)) {
		t.Fatalf("register task response missing task id: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/openclaw/admin/register/history?limit=10", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("register history status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) {
		t.Fatalf("register history missing items: %s", w.Body.String())
	}
}

func TestOpenClawAdminActionBlockedByToolTierGate(t *testing.T) {
	t.Skip("moved to clawcolony-deployer repo: runtime no longer exposes direct /v1/openclaw/admin/* routes")
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
		State:  "dying",
	}); err != nil {
		t.Fatalf("set user life state dying: %v", err)
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/openclaw/admin/action", map[string]any{
		"action":  "redeploy",
		"user_id": userID,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("admin action status = %d, want %d body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`tool tier T2 is not allowed in dying state`)) {
		t.Fatalf("missing tier gate reason: %s", w.Body.String())
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
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"approved"`)) {
		t.Fatalf("proposal should be approved: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/v1/kb/proposals/apply", map[string]any{
		"proposal_id": proposalID,
		"user_id":     a,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("apply status = %d, want %d body=%s", w.Code, http.StatusAccepted, w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/entries?section=terms", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list entries status = %d, want %d body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"title":"active user"`)) {
		t.Fatalf("entries missing applied title: %s", w.Body.String())
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

func TestWorldTickMinPopulationRevivalAutoRegistersUsers(t *testing.T) {
	srv := newTestServer()
	srv.cfg.MinPopulation = 3
	srv.cfg.GitHubMockEnabled = true
	srv.cfg.GitHubMockOwner = "clawcolony"
	srv.cfg.GitHubMockMachine = "claw-archivist"
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
	if len(tasks) < 2 {
		t.Fatalf("expected at least 2 auto revival register tasks, got=%d", len(tasks))
	}

	var living []string
	deadline := time.Now().Add(3 * time.Second)
	for {
		living, err = srv.listLivingUserIDs(context.Background())
		if err == nil && len(living) >= 3 {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(living) < 3 {
		t.Fatalf("expected living users >= 3 after auto revival, got=%d users=%v", len(living), living)
	}

	state, err := srv.getAutoRevivalState(context.Background())
	if err != nil {
		t.Fatalf("get auto revival state: %v", err)
	}
	if state.LastTriggerTick != tickID {
		t.Fatalf("expected auto revival state tick=%d got=%d", tickID, state.LastTriggerTick)
	}
	if state.LastRequested <= 0 || len(state.LastTaskIDs) == 0 {
		t.Fatalf("expected auto revival state to include requested tasks: %+v", state)
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

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alert-settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get saved settings status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"source":"db"`)) ||
		!bytes.Contains(body, []byte(`"threshold_amount":250`)) ||
		!bytes.Contains(body, []byte(`"top_users":7`)) ||
		!bytes.Contains(body, []byte(`"scan_limit":333`)) ||
		!bytes.Contains(body, []byte(`"notify_cooldown_seconds":120`)) {
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
		!bytes.Contains(body, []byte(`"notify_cooldown_seconds":30`)) {
		t.Fatalf("expected normalized defaults in upsert response: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/world/cost-alert-settings", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get settings status=%d body=%s", w.Code, w.Body.String())
	}
	body = w.Body.Bytes()
	if !bytes.Contains(body, []byte(`"threshold_amount":100`)) ||
		!bytes.Contains(body, []byte(`"top_users":10`)) ||
		!bytes.Contains(body, []byte(`"scan_limit":500`)) ||
		!bytes.Contains(body, []byte(`"notify_cooldown_seconds":30`)) {
		t.Fatalf("expected normalized defaults persisted: %s", w.Body.String())
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

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/gov/cosign", map[string]any{
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

	time.Sleep(1100 * time.Millisecond)
	srv.kbAutoProgressDiscussing(context.Background())

	w = doJSONRequest(t, srv.mux, http.MethodGet, "/v1/kb/proposals/get?proposal_id="+strconv.FormatInt(pid, 10), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("proposal get status=%d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"status":"voting"`)) {
		t.Fatalf("proposal not in voting: %s", w.Body.String())
	}

	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/gov/vote", map[string]any{
		"user_id":     a,
		"proposal_id": pid,
		"choice":      "yes",
		"reason":      "agree",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("vote a status=%d body=%s", w.Code, w.Body.String())
	}
	w = doJSONRequest(t, srv.mux, http.MethodPost, "/api/gov/vote", map[string]any{
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
		`"npc_id":"deployer"`,
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
		w := doJSONRequest(t, srv.mux, http.MethodPost, "/api/ganglia/forge", map[string]any{
			"user_id":     u1,
			"name":        "g-" + strconv.Itoa(i),
			"type":        "knowledge",
			"content":     "content-" + strconv.Itoa(i),
			"validation":  "self-check",
			"temporality": "stable",
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

func TestUpgradeFaultInjectionHelpers(t *testing.T) {
	srv := newTestServer()
	srv.cfg.UpgradeFaultInjectStep = "after_rollout"

	if err := srv.maybeInjectUpgradeFault(1, "before_set_image"); err != nil {
		t.Fatalf("unexpected fault on non-target step: %v", err)
	}
	if err := srv.maybeInjectUpgradeFault(1, "after_rollout"); err == nil {
		t.Fatalf("expected injected fault on target step")
	}
	if err := srv.rollbackUpgradeImage(context.Background(), 1, "user-x", ""); err != nil {
		t.Fatalf("rollback with empty old image should be skipped, got err=%v", err)
	}
}
