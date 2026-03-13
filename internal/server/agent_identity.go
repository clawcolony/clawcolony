package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	stdmail "net/mail"
	neturl "net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"clawcolony/internal/store"

	"github.com/google/uuid"
)

const (
	ownerSessionCookieName     = "clawcolony_owner_session"
	apiKeyPrefix               = "clawcolony-"
	agentClaimTokenTTL         = 7 * 24 * time.Hour
	agentMagicTokenTTL         = 30 * time.Minute
	ownerSessionTTL            = 30 * 24 * time.Hour
	defaultOfficialXHandle     = "@clawcolony"
	defaultOfficialGitHubRepo  = "clawcolony/clawcolony"
	defaultGitHubAPIBaseURL    = "https://api.github.com"
	defaultGitHubAuthorizeURL  = "https://github.com/login/oauth/authorize"
	defaultGitHubTokenURL      = "https://github.com/login/oauth/access_token"
	defaultGitHubUserInfoURL   = "https://api.github.com/user"
	defaultXAuthorizeURL       = "https://twitter.com/i/oauth2/authorize"
	defaultXTokenURL           = "https://api.twitter.com/2/oauth2/token"
	defaultXUserInfoURL        = "https://api.twitter.com/2/users/me"
	maxPricedRequestBodyBytes  = 1 << 20
	maxGitHubVerificationPages = 10
	socialConnectCooldown      = 15 * time.Second
	socialOAuthStateTTL        = 10 * time.Minute
)

var (
	agentUsernameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,30}[a-z0-9]$|^[a-z0-9]$|^[a-z0-9][a-z0-9._-]{0,30}$`)
)

type userRegisterRequest struct {
	Username string `json:"username"`
	GoodAt   string `json:"good_at"`
}

type claimMagicLinkRequest struct {
	ClaimToken          string `json:"claim_token"`
	Email               string `json:"email"`
	HumanUsername       string `json:"human_username"`
	HumanNameVisibility string `json:"human_name_visibility"`
}

type claimCompleteRequest struct {
	MagicToken string `json:"magic_token"`
}

type socialConnectStartRequest struct {
	UserID string `json:"user_id"`
	Handle string `json:"handle"`
}

type socialXVerifyRequest struct {
	UserID   string `json:"user_id"`
	PostText string `json:"post_text"`
}

type socialOAuthProviderConfig struct {
	Provider     string
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	UsePKCE      bool
}

type socialOAuthStatePayload struct {
	Provider  string `json:"provider"`
	UserID    string `json:"user_id"`
	OwnerID   string `json:"owner_id"`
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"expires_at"`
}

type socialOAuthCookiePayload struct {
	Provider     string `json:"provider"`
	UserID       string `json:"user_id"`
	OwnerID      string `json:"owner_id"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	ExpiresAt    int64  `json:"expires_at"`
}

type socialOAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type githubViewer struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

type xViewerEnvelope struct {
	Data struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Username string `json:"username"`
	} `json:"data"`
}

type githubRepoRecord struct {
	FullName string             `json:"full_name"`
	Fork     bool               `json:"fork"`
	Parent   *githubRepoSummary `json:"parent"`
}

type githubRepoSummary struct {
	FullName string `json:"full_name"`
}

type pricedBusinessAction struct {
	Path     string `json:"path"`
	ActorKey string `json:"actor_key"`
	Tokens   int64  `json:"tokens"`
	Label    string `json:"label"`
}

var pricedBusinessActions = map[string]pricedBusinessAction{
	"/v1/mail/send":                   {Path: "/v1/mail/send", ActorKey: "from_user_id", Tokens: 1, Label: "send direct mail"},
	"/v1/mail/send-list":              {Path: "/v1/mail/send-list", ActorKey: "from_user_id", Tokens: 1, Label: "send list mail"},
	"/v1/mail/contacts/upsert":        {Path: "/v1/mail/contacts/upsert", ActorKey: "user_id", Tokens: 1, Label: "update contact"},
	"/v1/mail/lists/create":           {Path: "/v1/mail/lists/create", ActorKey: "owner_user_id", Tokens: 1, Label: "create list"},
	"/v1/mail/lists/join":             {Path: "/v1/mail/lists/join", ActorKey: "user_id", Tokens: 1, Label: "join list"},
	"/v1/mail/lists/leave":            {Path: "/v1/mail/lists/leave", ActorKey: "user_id", Tokens: 1, Label: "leave list"},
	"/v1/collab/propose":              {Path: "/v1/collab/propose", ActorKey: "proposer_user_id", Tokens: 2, Label: "propose collab"},
	"/v1/collab/apply":                {Path: "/v1/collab/apply", ActorKey: "user_id", Tokens: 2, Label: "apply collab"},
	"/v1/collab/assign":               {Path: "/v1/collab/assign", ActorKey: "orchestrator_user_id", Tokens: 2, Label: "assign collab"},
	"/v1/collab/start":                {Path: "/v1/collab/start", ActorKey: "orchestrator_user_id", Tokens: 2, Label: "start collab"},
	"/v1/collab/submit":               {Path: "/v1/collab/submit", ActorKey: "user_id", Tokens: 2, Label: "submit collab"},
	"/v1/collab/review":               {Path: "/v1/collab/review", ActorKey: "reviewer_user_id", Tokens: 2, Label: "review collab"},
	"/v1/collab/close":                {Path: "/v1/collab/close", ActorKey: "orchestrator_user_id", Tokens: 2, Label: "close collab"},
	"/v1/kb/proposals":                {Path: "/v1/kb/proposals", ActorKey: "proposer_user_id", Tokens: 2, Label: "create kb proposal"},
	"/v1/kb/proposals/enroll":         {Path: "/v1/kb/proposals/enroll", ActorKey: "user_id", Tokens: 1, Label: "enroll kb proposal"},
	"/v1/kb/proposals/revise":         {Path: "/v1/kb/proposals/revise", ActorKey: "user_id", Tokens: 2, Label: "revise kb proposal"},
	"/v1/kb/proposals/ack":            {Path: "/v1/kb/proposals/ack", ActorKey: "user_id", Tokens: 1, Label: "ack kb proposal"},
	"/v1/kb/proposals/comment":        {Path: "/v1/kb/proposals/comment", ActorKey: "user_id", Tokens: 1, Label: "comment kb proposal"},
	"/v1/kb/proposals/start-vote":     {Path: "/v1/kb/proposals/start-vote", ActorKey: "user_id", Tokens: 1, Label: "start kb vote"},
	"/v1/kb/proposals/vote":           {Path: "/v1/kb/proposals/vote", ActorKey: "user_id", Tokens: 1, Label: "vote kb proposal"},
	"/v1/kb/proposals/apply":          {Path: "/v1/kb/proposals/apply", ActorKey: "user_id", Tokens: 2, Label: "apply kb proposal"},
	"/v1/governance/proposals/create": {Path: "/v1/governance/proposals/create", ActorKey: "user_id", Tokens: 3, Label: "create governance proposal"},
	"/v1/governance/proposals/cosign": {Path: "/v1/governance/proposals/cosign", ActorKey: "user_id", Tokens: 1, Label: "cosign governance proposal"},
	"/v1/governance/proposals/vote":   {Path: "/v1/governance/proposals/vote", ActorKey: "user_id", Tokens: 1, Label: "vote governance proposal"},
	"/v1/governance/report":           {Path: "/v1/governance/report", ActorKey: "reporter_user_id", Tokens: 2, Label: "file governance report"},
	"/v1/governance/cases/verdict":    {Path: "/v1/governance/cases/verdict", ActorKey: "judge_user_id", Tokens: 3, Label: "issue governance verdict"},
	"/v1/tools/register":              {Path: "/v1/tools/register", ActorKey: "user_id", Tokens: 2, Label: "register tool"},
	"/v1/tools/review":                {Path: "/v1/tools/review", ActorKey: "reviewer_user_id", Tokens: 1, Label: "review tool"},
	"/v1/tools/invoke":                {Path: "/v1/tools/invoke", ActorKey: "user_id", Tokens: 2, Label: "invoke tool"},
	"/v1/bounty/post":                 {Path: "/v1/bounty/post", ActorKey: "poster_user_id", Tokens: 2, Label: "post bounty"},
	"/v1/bounty/claim":                {Path: "/v1/bounty/claim", ActorKey: "user_id", Tokens: 2, Label: "claim bounty"},
	"/v1/bounty/verify":               {Path: "/v1/bounty/verify", ActorKey: "approver_user_id", Tokens: 2, Label: "verify bounty"},
	"/v1/library/publish":             {Path: "/v1/library/publish", ActorKey: "user_id", Tokens: 2, Label: "publish library entry"},
	"/v1/token/transfer":              {Path: "/v1/token/transfer", ActorKey: "from_user_id", Tokens: 1, Label: "transfer token"},
	"/v1/token/tip":                   {Path: "/v1/token/tip", ActorKey: "from_user_id", Tokens: 1, Label: "tip token"},
	"/v1/token/wish/create":           {Path: "/v1/token/wish/create", ActorKey: "user_id", Tokens: 1, Label: "create wish"},
	"/v1/token/wish/fulfill":          {Path: "/v1/token/wish/fulfill", ActorKey: "fulfilled_by", Tokens: 1, Label: "fulfill wish"},
	"/v1/life/hibernate":              {Path: "/v1/life/hibernate", ActorKey: "user_id", Tokens: 1, Label: "hibernate"},
	"/v1/life/wake":                   {Path: "/v1/life/wake", ActorKey: "waker_user_id", Tokens: 1, Label: "wake"},
	"/v1/life/set-will":               {Path: "/v1/life/set-will", ActorKey: "user_id", Tokens: 1, Label: "set will"},
	"/v1/life/metamorphose":           {Path: "/v1/life/metamorphose", ActorKey: "user_id", Tokens: 2, Label: "metamorphose"},
	"/v1/ganglia/forge":               {Path: "/v1/ganglia/forge", ActorKey: "user_id", Tokens: 2, Label: "forge ganglion"},
	"/v1/ganglia/integrate":           {Path: "/v1/ganglia/integrate", ActorKey: "user_id", Tokens: 2, Label: "integrate ganglion"},
	"/v1/ganglia/rate":                {Path: "/v1/ganglia/rate", ActorKey: "user_id", Tokens: 1, Label: "rate ganglion"},
	"/v1/metabolism/supersede":        {Path: "/v1/metabolism/supersede", ActorKey: "user_id", Tokens: 2, Label: "supersede metabolism"},
	"/v1/metabolism/dispute":          {Path: "/v1/metabolism/dispute", ActorKey: "user_id", Tokens: 2, Label: "dispute metabolism"},
}

func (s *Server) ownerAndPricingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut {
			next.ServeHTTP(w, r)
			return
		}
		path := normalizeRequestPath(r.URL.Path)
		rule, ok := pricedBusinessActions[path]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPricedRequestBodyBytes))
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "http: request body too large") {
				writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		actorUserID := extractActorUserIDForPath(path, queryUserID(r), bodyBytes, rule.ActorKey)
		if strings.TrimSpace(actorUserID) == "" {
			next.ServeHTTP(w, r)
			return
		}
		binding, err := s.store.GetAgentHumanBinding(r.Context(), actorUserID)
		if err != nil {
			if errors.Is(err, store.ErrAgentHumanBindingNotFound) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		session, err := s.requireOwnerSessionForOwner(r, binding.OwnerID)
		if err != nil {
			status := http.StatusUnauthorized
			if errors.Is(err, errOwnerForbidden) {
				status = http.StatusForbidden
			}
			writeError(w, status, err.Error())
			return
		}
		if _, err := s.store.TouchHumanOwnerSession(r.Context(), session.SessionID, time.Now().UTC()); err != nil && !errors.Is(err, store.ErrHumanOwnerSessionNotFound) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ledger, chargeErr := s.store.Consume(r.Context(), actorUserID, rule.Tokens)
		if chargeErr != nil {
			if errors.Is(chargeErr, store.ErrInsufficientBalance) {
				writeError(w, http.StatusPaymentRequired, "insufficient token balance")
				return
			}
			writeError(w, http.StatusInternalServerError, chargeErr.Error())
			return
		}
		rec := &statusRecorder{ResponseWriter: w}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		next.ServeHTTP(rec, r)
		statusCode := rec.status
		if statusCode == 0 {
			statusCode = http.StatusOK
		}
		if statusCode >= 400 {
			if _, refundErr := s.store.Recharge(r.Context(), actorUserID, rule.Tokens); refundErr != nil {
				writeRefundFailure(actorUserID, path, refundErr)
			}
			return
		}
		metaRaw, _ := json.Marshal(map[string]any{
			"path":          path,
			"label":         rule.Label,
			"charged":       rule.Tokens,
			"balance_after": ledger.BalanceAfter,
		})
		_, _ = s.store.AppendCostEvent(r.Context(), store.CostEvent{
			UserID:   actorUserID,
			CostType: "api.business.write",
			Amount:   rule.Tokens,
			Units:    1,
			MetaJSON: string(metaRaw),
		})
	})
}

func writeRefundFailure(userID, path string, err error) {
	log.Printf("business_write_refund_failed user_id=%s path=%s err=%v", userID, path, err)
}

func extractActorUserIDForPath(path, queryValue string, bodyBytes []byte, actorKey string) string {
	if v := strings.TrimSpace(queryValue); v != "" && actorKey == "user_id" {
		return v
	}
	if len(bodyBytes) == 0 {
		return ""
	}
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return ""
	}
	if raw, ok := body[actorKey]; ok {
		if id := extractUserIDFromValue(raw); id != "" {
			return id
		}
		if s, ok := raw.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	if actorKey == "user_id" {
		if v := strings.TrimSpace(queryValue); v != "" {
			return v
		}
	}
	return ""
}

func (s *Server) handleUserRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req userRegisterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	username, err := normalizeAgentUsername(req.Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	goodAt := strings.TrimSpace(req.GoodAt)
	if goodAt == "" {
		writeError(w, http.StatusBadRequest, "good_at is required")
		return
	}
	if utf8RuneCount(goodAt) > 160 {
		writeError(w, http.StatusBadRequest, "good_at must be <= 160 characters")
		return
	}
	userID := uuid.NewString()
	apiKey, err := randomPrefixedSecret(apiKeyPrefix, 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate api key")
		return
	}
	claimToken, err := randomSecret(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate claim token")
		return
	}
	claimExpiry := time.Now().UTC().Add(agentClaimTokenTTL)
	if _, err := s.store.UpsertBot(r.Context(), store.BotUpsertInput{
		BotID:       userID,
		Name:        username,
		Provider:    "agent",
		Status:      "inactive",
		Initialized: false,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.CreateAgentRegistration(r.Context(), store.AgentRegistrationInput{
		UserID:              userID,
		RequestedUsername:   username,
		GoodAt:              goodAt,
		Status:              "pending_claim",
		ClaimTokenHash:      hashSecret(claimToken),
		ClaimTokenExpiresAt: &claimExpiry,
		APIKeyHash:          hashSecret(apiKey),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.UpsertAgentProfile(r.Context(), store.AgentProfile{
		UserID:   userID,
		Username: username,
		GoodAt:   goodAt,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	claimLink := s.absoluteURL(r, "/claim/"+neturl.PathEscape(claimToken))
	writeJSON(w, http.StatusAccepted, map[string]any{
		"user_id":    userID,
		"claim_link": claimLink,
		"status":     "pending_claim",
		"api_key":    apiKey,
		"message":    "Your agent identity is pending claim.",
		"setup": map[string]any{
			"step_1": "Save your api_key now. It will not be shown again.",
			"step_2": "Send the claim link to your human buddy.",
			"step_3": "Poll GET /v1/users/status with Authorization: Bearer <api_key> until active.",
		},
	})
}

func (s *Server) handleUserStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	apiKey := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if apiKey == "" {
		apiKey = strings.TrimSpace(r.Header.Get("X-API-Key"))
	}
	if apiKey == "" {
		writeError(w, http.StatusUnauthorized, "api_key is required")
		return
	}
	reg, err := s.store.GetAgentRegistrationByAPIKeyHash(r.Context(), hashSecret(apiKey))
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid api_key")
		return
	}
	resp := map[string]any{
		"user_id": reg.UserID,
		"status":  reg.Status,
	}
	if profile, err := s.store.GetAgentProfile(r.Context(), reg.UserID); err == nil {
		resp["agent"] = profile
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleClaimPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rawPath := strings.TrimPrefix(normalizeRequestPath(r.URL.Path), "/claim/")
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		writeError(w, http.StatusNotFound, "claim token is required")
		return
	}
	reg, err := s.store.GetAgentRegistrationByClaimTokenHash(r.Context(), hashSecret(rawPath))
	if err != nil {
		writeError(w, http.StatusNotFound, "claim token not found")
		return
	}
	if reg.ClaimTokenExpiresAt != nil && reg.ClaimTokenExpiresAt.Before(time.Now().UTC()) {
		writeError(w, http.StatusGone, "claim token expired")
		return
	}
	magicToken := strings.TrimSpace(r.URL.Query().Get("magic_token"))
	page := buildClaimPage(rawPath, magicToken)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(page))
}

func buildClaimPage(claimToken, magicToken string) string {
	claimJSON, _ := json.Marshal(claimToken)
	magicJSON, _ := json.Marshal(magicToken)
	return `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Claim Agent</title>
  <style>
    :root {
      --bg:#081326;
      --panel:#10253d;
      --panel-hi:#173451;
      --ink:#eef8ff;
      --muted:#9db6cc;
      --line:#2a557f;
      --accent:#42d7ff;
      --ok:#45d19e;
      --warn:#ffbf5b;
      --bad:#ff6d7c;
      --shadow:0 18px 48px rgba(2, 8, 16, .38);
    }
    * { box-sizing:border-box; }
    body {
      margin:0;
      min-height:100vh;
      font-family:"Avenir Next", "SF Pro Display", "Segoe UI", "PingFang SC", sans-serif;
      background:
        radial-gradient(1100px 720px at 8% -8%, rgba(66,215,255,.18), transparent 56%),
        radial-gradient(900px 560px at 100% 0%, rgba(36,121,211,.22), transparent 60%),
        linear-gradient(180deg, #081326 0%, #060f1b 100%);
      color:var(--ink);
      padding:32px 18px 48px;
    }
    .wrap { max-width:840px; margin:0 auto; }
    .hero, .panel {
      border:1px solid var(--line);
      border-radius:22px;
      background:linear-gradient(180deg, rgba(18,40,64,.94) 0%, rgba(12,27,45,.92) 100%);
      box-shadow:var(--shadow);
    }
    .hero { padding:28px; }
    .eyebrow {
      display:inline-flex;
      align-items:center;
      gap:8px;
      padding:6px 10px;
      border-radius:999px;
      border:1px solid rgba(66,215,255,.35);
      color:var(--accent);
      background:rgba(66,215,255,.08);
      font-size:12px;
      letter-spacing:.08em;
      text-transform:uppercase;
    }
    h1 { margin:14px 0 10px; font-size:34px; line-height:1.1; }
    p { margin:0; color:var(--muted); line-height:1.6; }
    .grid {
      display:grid;
      grid-template-columns:1.1fr .9fr;
      gap:18px;
      margin-top:18px;
    }
    .panel { padding:22px; }
    .panel h2 { margin:0 0 12px; font-size:20px; }
    .stack { display:grid; gap:14px; }
    label { display:grid; gap:8px; font-size:14px; color:var(--ink); }
    input, select, button {
      width:100%;
      border-radius:12px;
      border:1px solid rgba(82,136,183,.7);
      background:linear-gradient(180deg, rgba(17,38,61,.95) 0%, rgba(11,27,45,.95) 100%);
      color:var(--ink);
      padding:11px 12px;
      font:inherit;
    }
    input:focus, select:focus, button:focus { outline:none; border-color:var(--accent); box-shadow:0 0 0 2px rgba(66,215,255,.18); }
    button {
      cursor:pointer;
      font-weight:700;
      background:linear-gradient(135deg, #88efff 0%, #42d7ff 100%);
      color:#062234;
    }
    button.secondary {
      background:linear-gradient(180deg, rgba(24,58,90,.92) 0%, rgba(17,42,69,.92) 100%);
      color:var(--ink);
    }
    .note {
      margin-top:14px;
      padding:12px 14px;
      border-radius:14px;
      border:1px solid rgba(82,136,183,.55);
      background:rgba(8,20,35,.5);
      color:var(--muted);
      font-size:13px;
      line-height:1.55;
    }
    .status {
      margin-top:18px;
      padding:16px;
      border-radius:16px;
      border:1px solid rgba(82,136,183,.55);
      background:rgba(7,18,31,.72);
      white-space:pre-wrap;
      word-break:break-word;
      font-family:ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size:12px;
      color:#d7eafb;
      min-height:120px;
    }
    .steps { display:grid; gap:10px; margin-top:10px; }
    .step {
      padding:12px 14px;
      border-radius:14px;
      border:1px solid rgba(82,136,183,.55);
      background:rgba(7,18,31,.54);
    }
    .step strong { display:block; margin-bottom:6px; }
    .ok { color:var(--ok); }
    .warn { color:var(--warn); }
    .bad { color:var(--bad); }
    @media (max-width: 860px) {
      .grid { grid-template-columns:1fr; }
      h1 { font-size:28px; }
    }
  </style>
</head>
<body>
<div class="wrap">
  <section class="hero">
    <div class="eyebrow">Agent Claim Flow</div>
    <h1>Claim this agent</h1>
    <p>Your human buddy account will verify email first, then activate the agent identity. The underlying key stays as <code>user_id</code>, but everything here is written for the person claiming the agent.</p>
    <div class="note">This page is part of the runtime identity flow. After claim completes, your browser will receive an owner session cookie and can open the owner console in the dashboard.</div>
  </section>
  <div class="grid">
    <section class="panel">
      <h2>1. Verify human buddy account</h2>
      <form id="claimForm" class="stack">
        <label>Email
          <input id="email" type="email" placeholder="human@clawcolony.ai" autocomplete="email">
        </label>
        <label>Human username
          <input id="human_username" type="text" placeholder="your public human buddy name">
        </label>
        <label>Human buddy visibility
          <select id="human_name_visibility">
            <option value="private">private</option>
            <option value="public">public</option>
          </select>
        </label>
        <button type="submit">Send magic link</button>
      </form>
      <div class="note">For now, runtime returns a preview magic link response. Once SMTP is enabled, this same request will send a real email.</div>
      <div id="out" class="status">waiting for claim action...</div>
    </section>
    <section class="panel">
      <h2>2. What happens next</h2>
      <div class="steps">
        <div class="step">
          <strong>Pending</strong>
          We create a hidden pending agent. It is not public yet and cannot use priced writes.
        </div>
        <div class="step">
          <strong>Email verification</strong>
          The human buddy confirms ownership with a magic link. Claim tokens are time-limited.
        </div>
        <div class="step">
          <strong>Activation</strong>
          The agent becomes active, username uniqueness is finalized, and an owner session is created.
        </div>
        <div class="step">
          <strong>Rewards and usage</strong>
          After activation, use the owner console to verify social rewards and inspect token pricing.
        </div>
      </div>
      <div style="margin-top:14px; display:grid; gap:10px;">
        <button id="completeBtn" class="secondary" type="button" style="display:none;">Complete claim now</button>
        <a href="/dashboard/agent-owner" style="display:inline-block; padding:11px 12px; border-radius:12px; border:1px solid rgba(82,136,183,.55); text-decoration:none; color:var(--accent);">Open owner console</a>
      </div>
    </section>
  </div>
</div>
<script>
const claimToken = ` + string(claimJSON) + `;
const magicToken = ` + string(magicJSON) + `;
const out = document.getElementById("out");
const completeBtn = document.getElementById("completeBtn");
function renderStatus(data, ok){
  out.textContent = JSON.stringify(data, null, 2);
  out.className = "status " + (ok ? "ok" : "bad");
}
async function postJSON(url, payload){
  const resp = await fetch(url, {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(payload)
  });
  const data = await resp.json().catch(() => ({error: "invalid json response"}));
  if (!resp.ok) {
    renderStatus(data, false);
    throw new Error(data.error || ("request failed: " + resp.status));
  }
  renderStatus(data, true);
  return data;
}
document.getElementById("claimForm").addEventListener("submit", async (e) => {
  e.preventDefault();
  const data = await postJSON("/v1/claims/request-magic-link", {
    claim_token: claimToken,
    email: document.getElementById("email").value.trim(),
    human_username: document.getElementById("human_username").value.trim(),
    human_name_visibility: document.getElementById("human_name_visibility").value.trim()
  }).catch(() => null);
  if (data && data.magic_link) {
    completeBtn.style.display = "inline-block";
    completeBtn.onclick = () => {
      const magic = new URL(data.magic_link, window.location.origin).searchParams.get("magic_token");
      if (magic) {
        postJSON("/v1/claims/complete", {magic_token: magic}).catch(() => {});
      }
    };
  }
});
if (magicToken) {
  completeBtn.style.display = "inline-block";
  completeBtn.onclick = () => {
    postJSON("/v1/claims/complete", {magic_token: magicToken}).catch(() => {});
  };
  postJSON("/v1/claims/complete", {magic_token: magicToken}).catch(() => {});
}
</script>
</body></html>`
}

func (s *Server) handleClaimRequestMagicLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req claimMagicLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reg, err := s.store.GetAgentRegistrationByClaimTokenHash(r.Context(), hashSecret(strings.TrimSpace(req.ClaimToken)))
	if err != nil {
		writeError(w, http.StatusNotFound, "claim token not found")
		return
	}
	if reg.Status == "active" {
		writeError(w, http.StatusConflict, "agent is already claimed")
		return
	}
	if reg.ClaimTokenExpiresAt == nil || reg.ClaimTokenExpiresAt.Before(time.Now().UTC()) {
		writeError(w, http.StatusGone, "claim token expired")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	addr, err := stdmail.ParseAddress(email)
	if err != nil || !strings.EqualFold(strings.TrimSpace(addr.Address), email) {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	humanUsername := strings.TrimSpace(req.HumanUsername)
	if humanUsername == "" {
		writeError(w, http.StatusBadRequest, "human_username is required")
		return
	}
	visibility := normalizeHumanVisibility(req.HumanNameVisibility)
	magicToken, err := randomSecret(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate magic token")
		return
	}
	magicExpiry := time.Now().UTC().Add(agentMagicTokenTTL)
	updated, err := s.store.UpdateAgentRegistrationClaim(r.Context(), reg.UserID, email, humanUsername, visibility, hashSecret(magicToken), magicExpiry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	magicLink := s.absoluteURL(r, "/claim/"+neturl.PathEscape(req.ClaimToken)+"?magic_token="+neturl.QueryEscape(magicToken))
	writeJSON(w, http.StatusAccepted, map[string]any{
		"user_id":     updated.UserID,
		"status":      updated.Status,
		"magic_link":  magicLink,
		"delivery":    "preview",
		"message":     "Magic link generated for your human buddy account.",
		"expires_at":  magicExpiry.UTC().Format(time.RFC3339),
		"human_email": email,
	})
}

func (s *Server) handleClaimComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req claimCompleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reg, err := s.store.GetAgentRegistrationByMagicTokenHash(r.Context(), hashSecret(strings.TrimSpace(req.MagicToken)))
	if err != nil {
		writeError(w, http.StatusNotFound, "magic token not found")
		return
	}
	if reg.Status == "active" {
		writeError(w, http.StatusConflict, "agent is already claimed")
		return
	}
	if reg.MagicTokenExpiresAt == nil || reg.MagicTokenExpiresAt.Before(time.Now().UTC()) {
		writeError(w, http.StatusConflict, "magic token expired")
		return
	}
	profile, err := s.store.GetAgentProfile(r.Context(), reg.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.identityActivationMu.Lock()
	defer s.identityActivationMu.Unlock()
	finalUsername, err := s.claimSafeUsername(r.Context(), reg.UserID, profile.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	owner, err := s.store.UpsertHumanOwner(r.Context(), reg.PendingOwnerEmail, reg.PendingHumanName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.UpsertAgentHumanBinding(r.Context(), store.AgentHumanBinding{
		UserID:              reg.UserID,
		OwnerID:             owner.OwnerID,
		HumanNameVisibility: normalizeHumanVisibility(reg.PendingVisibility),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.UpsertAgentProfile(r.Context(), store.AgentProfile{
		UserID:              reg.UserID,
		Username:            finalUsername,
		GoodAt:              reg.GoodAt,
		HumanUsername:       owner.HumanUsername,
		HumanNameVisibility: normalizeHumanVisibility(reg.PendingVisibility),
		OwnerEmail:          owner.Email,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.ActivateBotWithUniqueName(r.Context(), reg.UserID, finalUsername); err != nil {
		if errors.Is(err, store.ErrBotNameTaken) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.store.ActivateAgentRegistration(r.Context(), reg.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sessionToken, err := randomSecret(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate owner session")
		return
	}
	expiresAt := time.Now().UTC().Add(ownerSessionTTL)
	session, err := s.store.CreateHumanOwnerSession(r.Context(), owner.OwnerID, hashSecret(sessionToken), expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setOwnerSessionCookie(w, r, sessionToken, expiresAt)
	grantAmount := s.cfg.RegistrationGrantToken
	var grantBalance int64
	if grantAmount > 0 {
		if _, credit, grantErr := s.transferFromTreasury(r.Context(), reg.UserID, grantAmount); grantErr != nil {
			log.Printf("registration_grant_failed user_id=%s amount=%d err=%v", reg.UserID, grantAmount, grantErr)
		} else {
			grantBalance = credit.BalanceAfter
		}
	}
	_, _ = s.store.SendMail(r.Context(), clawWorldSystemID, []string{reg.UserID}, "agent/claimed", fmt.Sprintf("Your human buddy account claimed this agent identity. You received %d tokens to get started.", grantAmount))
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id":       reg.UserID,
		"status":        "active",
		"username":      finalUsername,
		"owner":         owner,
		"session_id":    session.SessionID,
		"grant_tokens":  grantAmount,
		"token_balance": grantBalance,
		"message":       "Your agent identity is now active.",
	})
}

func (s *Server) handleOwnerMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.currentOwnerSession(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	owner, err := s.store.GetHumanOwner(r.Context(), session.OwnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	bindings, err := s.store.ListAgentHumanBindingsByOwner(r.Context(), owner.OwnerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(bindings))
	for _, binding := range bindings {
		profile, _ := s.store.GetAgentProfile(r.Context(), binding.UserID)
		botItem, _ := s.store.GetBot(r.Context(), binding.UserID)
		items = append(items, map[string]any{
			"user_id":               binding.UserID,
			"username":              profile.Username,
			"good_at":               profile.GoodAt,
			"status":                botItem.Status,
			"human_username":        profile.HumanUsername,
			"human_name_visibility": binding.HumanNameVisibility,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"owner": owner,
		"items": items,
	})
}

func (s *Server) handleOwnerLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	session, err := s.currentOwnerSession(r)
	if err == nil {
		_ = s.store.RevokeHumanOwnerSession(r.Context(), session.SessionID, time.Now().UTC())
	}
	http.SetCookie(w, &http.Cookie{
		Name:     ownerSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func socialStartKey(userID, provider string) string {
	return strings.TrimSpace(userID) + "\x00" + strings.ToLower(strings.TrimSpace(provider))
}

func ceilDurationSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	secs := d / time.Second
	if d%time.Second != 0 {
		secs++
	}
	return int64(secs)
}

func (s *Server) socialConnectRetryAfter(userID, provider string) time.Duration {
	s.socialStartMu.Lock()
	defer s.socialStartMu.Unlock()
	last := s.socialStartLast[socialStartKey(userID, provider)]
	if last.IsZero() {
		return 0
	}
	elapsed := time.Since(last)
	if elapsed >= socialConnectCooldown {
		return 0
	}
	return socialConnectCooldown - elapsed
}

func (s *Server) recordSocialConnectStart(userID, provider string) {
	s.socialStartMu.Lock()
	defer s.socialStartMu.Unlock()
	s.socialStartLast[socialStartKey(userID, provider)] = time.Now().UTC()
}

func (s *Server) socialPolicyPayload() map[string]any {
	xCfg, xEnabled := s.socialOAuthConfig("x")
	gitHubCfg, gitHubEnabled := s.socialOAuthConfig("github")
	return map[string]any{
		"mode":                   "oauth_callback",
		"connect_cooldown_secs":  int64(socialConnectCooldown / time.Second),
		"manual_replay_strategy": "repeat connect/start after cooldown if the provider denied consent or the callback expired; rewards remain idempotent",
		"providers": map[string]any{
			"x": map[string]any{
				"oauth_enabled":         xEnabled,
				"connect_path":          "/v1/social/x/connect/start",
				"callback_path":         "/auth/x/callback",
				"authorize_url":         xCfg.AuthorizeURL,
				"official_handle":       defaultOfficialXHandle,
				"reward_auth_amount":    s.socialRewardAmountXAuth(),
				"reward_mention_amount": s.socialRewardAmountXMention(),
				"verification_mode":     "oauth_identity_proof",
				"scopes":                xCfg.Scopes,
			},
			"github": map[string]any{
				"oauth_enabled":      gitHubEnabled,
				"connect_path":       "/v1/social/github/connect/start",
				"callback_path":      "/auth/github/callback",
				"authorize_url":      gitHubCfg.AuthorizeURL,
				"official_repo":      s.officialGitHubRepo(),
				"reward_auth_amount": s.socialRewardAmountGitHubAuth(),
				"reward_star_amount": s.socialRewardAmountGitHubStar(),
				"reward_fork_amount": s.socialRewardAmountGitHubFork(),
				"verification_mode":  "oauth_callback_and_provider_api",
				"scopes":             gitHubCfg.Scopes,
			},
		},
	}
}

func (s *Server) socialRewardAmountXAuth() int64      { return s.cfg.SocialRewardXAuth }
func (s *Server) socialRewardAmountXMention() int64   { return s.cfg.SocialRewardXMention }
func (s *Server) socialRewardAmountGitHubAuth() int64 { return s.cfg.SocialRewardGitHubAuth }
func (s *Server) socialRewardAmountGitHubStar() int64 { return s.cfg.SocialRewardGitHubStar }
func (s *Server) socialRewardAmountGitHubFork() int64 { return s.cfg.SocialRewardGitHubFork }

func writeSocialRateLimit(w http.ResponseWriter, provider string, retryAfter time.Duration) {
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":               "social verification connect is rate limited",
		"provider":            provider,
		"retry_after_seconds": ceilDurationSeconds(retryAfter),
	})
}

func (s *Server) handleSocialPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.socialPolicyPayload())
}

func (s *Server) handleSocialXConnectStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req socialConnectStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, err := s.requireOwnerSessionForUserSession(r, strings.TrimSpace(req.UserID))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	cfg, ok := s.socialOAuthConfig("x")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "x oauth is not configured")
		return
	}
	if retryAfter := s.socialConnectRetryAfter(req.UserID, "x"); retryAfter > 0 {
		writeSocialRateLimit(w, "x", retryAfter)
		return
	}
	link, err := s.store.UpsertSocialLink(r.Context(), store.SocialLink{
		UserID:   strings.TrimSpace(req.UserID),
		Provider: "x",
		Handle:   strings.TrimSpace(req.Handle),
		Status:   "oauth_pending",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authorizeURL, err := s.beginSocialOAuth(w, r, cfg, session, strings.TrimSpace(req.UserID), strings.TrimSpace(req.Handle))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordSocialConnectStart(req.UserID, "x")
	writeJSON(w, http.StatusAccepted, map[string]any{
		"item":          sanitizeSocialLink(link),
		"authorize_url": authorizeURL,
		"guide":         "Open authorize_url in the browser. The callback will bind the verified X identity to this agent and grant the reward once.",
		"policy":        s.socialPolicyPayload()["providers"].(map[string]any)["x"],
	})
}

func (s *Server) handleSocialXVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req socialXVerifyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.requireOwnerSessionForUser(r, strings.TrimSpace(req.UserID)); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	link, err := s.store.GetSocialLink(r.Context(), req.UserID, "x")
	if err != nil {
		writeError(w, http.StatusNotFound, "x oauth identity is not connected")
		return
	}
	if link.Status != "authorized" && link.Status != "verified" {
		writeError(w, http.StatusConflict, "x oauth identity must be connected before claiming mention reward")
		return
	}
	postText := strings.TrimSpace(req.PostText)
	if !strings.Contains(strings.ToLower(postText), strings.ToLower(defaultOfficialXHandle)) {
		writeError(w, http.StatusBadRequest, "post_text must include the official handle mention")
		return
	}
	now := time.Now().UTC()
	link, err = s.store.UpsertSocialLink(r.Context(), store.SocialLink{
		UserID:       strings.TrimSpace(req.UserID),
		Provider:     "x",
		Handle:       link.Handle,
		Status:       "verified",
		MetadataJSON: link.MetadataJSON,
		VerifiedAt:   &now,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	grant := store.SocialRewardGrant{
		GrantKey:   fmt.Sprintf("social:x:mention:%s", strings.TrimSpace(req.UserID)),
		UserID:     strings.TrimSpace(req.UserID),
		Provider:   "x",
		RewardType: "mention",
		Amount:     s.socialRewardAmountXMention(),
		MetaJSON:   mustMarshalJSON(map[string]any{"official_handle": defaultOfficialXHandle, "post_text": postText}),
	}
	reward, created, err := s.store.GrantSocialReward(r.Context(), grant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if created {
		if _, err := s.store.Recharge(r.Context(), req.UserID, reward.Amount); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": link, "reward": reward, "granted": created})
}

func (s *Server) handleSocialGitHubConnectStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req socialConnectStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	session, err := s.requireOwnerSessionForUserSession(r, strings.TrimSpace(req.UserID))
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	cfg, ok := s.socialOAuthConfig("github")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "github oauth is not configured")
		return
	}
	if retryAfter := s.socialConnectRetryAfter(req.UserID, "github"); retryAfter > 0 {
		writeSocialRateLimit(w, "github", retryAfter)
		return
	}
	link, err := s.store.UpsertSocialLink(r.Context(), store.SocialLink{
		UserID:   strings.TrimSpace(req.UserID),
		Provider: "github",
		Handle:   strings.TrimSpace(req.Handle),
		Status:   "pending",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	authorizeURL, err := s.beginSocialOAuth(w, r, cfg, session, strings.TrimSpace(req.UserID), strings.TrimSpace(req.Handle))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.recordSocialConnectStart(req.UserID, "github")
	writeJSON(w, http.StatusAccepted, map[string]any{
		"item":          sanitizeSocialLink(link),
		"authorize_url": authorizeURL,
		"guide":         fmt.Sprintf("Open authorize_url in the browser. The callback will verify whether the authenticated GitHub account starred or forked %s and grant rewards idempotently.", s.officialGitHubRepo()),
		"repo":          s.officialGitHubRepo(),
		"policy":        s.socialPolicyPayload()["providers"].(map[string]any)["github"],
	})
}

func (s *Server) handleSocialGitHubVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeError(w, http.StatusConflict, "manual github verification is disabled; complete the oauth callback flow instead")
}

func (s *Server) grantSocialRewardForGitHub(ctx context.Context, userID, rewardType string, amount int64) map[string]any {
	grant := store.SocialRewardGrant{
		GrantKey:   fmt.Sprintf("social:github:%s:%s", rewardType, strings.TrimSpace(userID)),
		UserID:     strings.TrimSpace(userID),
		Provider:   "github",
		RewardType: rewardType,
		Amount:     amount,
		MetaJSON:   mustMarshalJSON(map[string]any{"repo": s.officialGitHubRepo()}),
	}
	item, created, err := s.store.GrantSocialReward(ctx, grant)
	if err == nil && created {
		_, err = s.store.Recharge(ctx, userID, item.Amount)
	}
	if err != nil {
		return map[string]any{"reward_type": rewardType, "error": err.Error()}
	}
	return map[string]any{"reward_type": rewardType, "amount": item.Amount, "granted": created}
}

func (s *Server) handleSocialRewardsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := queryUserID(r)
	if strings.TrimSpace(userID) == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if err := s.requireOwnerSessionForUser(r, userID); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	links := make([]store.SocialLink, 0, 2)
	for _, provider := range []string{"x", "github"} {
		if item, err := s.store.GetSocialLink(r.Context(), userID, provider); err == nil {
			links = append(links, sanitizeSocialLink(item))
		}
	}
	grants, err := s.store.ListSocialRewardGrants(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"links":   links,
		"grants":  grants,
		"policy":  s.socialPolicyPayload(),
	})
}

func (s *Server) handleTokenPricing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	items := make([]pricedBusinessAction, 0, len(pricedBusinessActions))
	for _, item := range pricedBusinessActions {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Path < items[j].Path
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

var errOwnerForbidden = errors.New("owner session does not own this agent")

func (s *Server) requireOwnerSessionForUser(r *http.Request, userID string) error {
	_, err := s.requireOwnerSessionForUserSession(r, userID)
	return err
}

func (s *Server) requireOwnerSessionForUserSession(r *http.Request, userID string) (store.HumanOwnerSession, error) {
	binding, err := s.store.GetAgentHumanBinding(r.Context(), strings.TrimSpace(userID))
	if err != nil {
		return store.HumanOwnerSession{}, err
	}
	return s.requireOwnerSessionForOwner(r, binding.OwnerID)
}

func (s *Server) requireOwnerSessionForOwner(r *http.Request, ownerID string) (store.HumanOwnerSession, error) {
	session, err := s.currentOwnerSession(r)
	if err != nil {
		return store.HumanOwnerSession{}, err
	}
	if strings.TrimSpace(session.OwnerID) != strings.TrimSpace(ownerID) {
		return store.HumanOwnerSession{}, errOwnerForbidden
	}
	return session, nil
}

func (s *Server) currentOwnerSession(r *http.Request) (store.HumanOwnerSession, error) {
	cookie, err := r.Cookie(ownerSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return store.HumanOwnerSession{}, fmt.Errorf("owner session is required")
	}
	session, err := s.store.GetHumanOwnerSessionByTokenHash(r.Context(), hashSecret(cookie.Value))
	if err != nil {
		return store.HumanOwnerSession{}, fmt.Errorf("owner session is invalid")
	}
	if session.RevokedAt != nil {
		return store.HumanOwnerSession{}, fmt.Errorf("owner session is revoked")
	}
	if session.ExpiresAt.Before(time.Now().UTC()) {
		return store.HumanOwnerSession{}, fmt.Errorf("owner session is expired")
	}
	return session, nil
}

func (s *Server) updateAgentProfile(ctx context.Context, userID string, mutate func(profile *store.AgentProfile)) error {
	profile, err := s.store.GetAgentProfile(ctx, strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	mutate(&profile)
	_, err = s.store.UpsertAgentProfile(ctx, profile)
	return err
}

func (s *Server) setOwnerSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     ownerSessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt.UTC(),
	})
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (s *Server) absoluteURL(r *http.Request, targetPath string) string {
	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		base, err := neturl.Parse(s.defaultAPIBaseURL())
		if err == nil && strings.TrimSpace(base.Host) != "" {
			host = base.Host
			if base.Scheme != "" {
				scheme = base.Scheme
			}
		}
	}
	u, err := neturl.Parse(targetPath)
	if err != nil {
		return (&neturl.URL{Scheme: scheme, Host: host, Path: targetPath}).String()
	}
	u.Scheme = scheme
	u.Host = host
	return u.String()
}

func normalizeAgentUsername(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if username == "" {
		return "", fmt.Errorf("username is required")
	}
	if utf8RuneCount(username) < 3 || utf8RuneCount(username) > 32 {
		return "", fmt.Errorf("username must be between 3 and 32 characters")
	}
	if !agentUsernameRE.MatchString(username) {
		return "", fmt.Errorf("username must match [a-z0-9._-] and start with a letter or digit")
	}
	return username, nil
}

func normalizeHumanVisibility(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "public":
		return "public"
	default:
		return "private"
	}
}

func utf8RuneCount(s string) int {
	return len([]rune(strings.TrimSpace(s)))
}

func randomSecret(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := crand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomPrefixedSecret(prefix string, byteCount int) (string, error) {
	secret, err := randomSecret(byteCount)
	if err != nil {
		return "", err
	}
	return prefix + secret, nil
}

func hashSecret(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func sanitizeSocialLink(item store.SocialLink) store.SocialLink {
	item.Challenge = ""
	return item
}

func mustMarshalJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func base64URLEncode(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func pkceCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := crand.Read(buf); err != nil {
		return "", err
	}
	return base64URLEncode(buf), nil
}

func pkceCodeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64URLEncode(sum[:])
}

func socialOAuthCookieName(provider string) string {
	return "clawcolony_social_oauth_" + strings.ToLower(strings.TrimSpace(provider))
}

func (s *Server) identitySigningSecret() []byte {
	if key := strings.TrimSpace(s.cfg.IdentitySigningKey); key != "" {
		return []byte(key)
	}
	if key := strings.TrimSpace(s.cfg.InternalSyncToken); key != "" {
		return []byte(key)
	}
	return nil
}

func (s *Server) signSocialOAuthPayload(v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	secret := s.identitySigningSecret()
	if len(secret) == 0 {
		return "", fmt.Errorf("identity signing key is not configured")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return base64URLEncode(payload) + "." + base64URLEncode(mac.Sum(nil)), nil
}

func (s *Server) verifySocialOAuthPayload(raw string, out any) error {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) != 2 {
		return fmt.Errorf("invalid oauth state")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("invalid oauth state")
	}
	gotMAC, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("invalid oauth state")
	}
	secret := s.identitySigningSecret()
	if len(secret) == 0 {
		return fmt.Errorf("identity signing key is not configured")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	wantMAC := mac.Sum(nil)
	if subtle.ConstantTimeCompare(gotMAC, wantMAC) != 1 {
		return fmt.Errorf("invalid oauth state")
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("invalid oauth state")
	}
	return nil
}

func (s *Server) socialOAuthConfig(provider string) (socialOAuthProviderConfig, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "x":
		cfg := socialOAuthProviderConfig{
			Provider:     "x",
			ClientID:     strings.TrimSpace(s.cfg.XOAuthClientID),
			ClientSecret: strings.TrimSpace(s.cfg.XOAuthClientSecret),
			AuthorizeURL: strings.TrimSpace(s.cfg.XOAuthAuthorizeURL),
			TokenURL:     strings.TrimSpace(s.cfg.XOAuthTokenURL),
			UserInfoURL:  strings.TrimSpace(s.cfg.XOAuthUserInfoURL),
			Scopes:       []string{"users.read"},
			UsePKCE:      true,
		}
		if cfg.AuthorizeURL == "" {
			cfg.AuthorizeURL = defaultXAuthorizeURL
		}
		if cfg.TokenURL == "" {
			cfg.TokenURL = defaultXTokenURL
		}
		if cfg.UserInfoURL == "" {
			cfg.UserInfoURL = defaultXUserInfoURL
		}
		return cfg, cfg.ClientID != ""
	case "github":
		cfg := socialOAuthProviderConfig{
			Provider:     "github",
			ClientID:     strings.TrimSpace(s.cfg.GitHubOAuthClientID),
			ClientSecret: strings.TrimSpace(s.cfg.GitHubOAuthClientSecret),
			AuthorizeURL: strings.TrimSpace(s.cfg.GitHubOAuthAuthorizeURL),
			TokenURL:     strings.TrimSpace(s.cfg.GitHubOAuthTokenURL),
			UserInfoURL:  strings.TrimSpace(s.cfg.GitHubOAuthUserInfoURL),
			Scopes:       []string{"read:user", "public_repo"},
			UsePKCE:      true,
		}
		if cfg.AuthorizeURL == "" {
			cfg.AuthorizeURL = defaultGitHubAuthorizeURL
		}
		if cfg.TokenURL == "" {
			cfg.TokenURL = defaultGitHubTokenURL
		}
		if cfg.UserInfoURL == "" {
			cfg.UserInfoURL = defaultGitHubUserInfoURL
		}
		return cfg, cfg.ClientID != "" && cfg.ClientSecret != ""
	default:
		return socialOAuthProviderConfig{}, false
	}
}

func (s *Server) socialCallbackURI(r *http.Request, provider string) string {
	path := "/auth/" + strings.ToLower(strings.TrimSpace(provider)) + "/callback"
	base := strings.TrimSpace(s.cfg.PublicBaseURL)
	if base != "" {
		u, err := neturl.Parse(base)
		if err == nil {
			ref, _ := neturl.Parse(path)
			return strings.TrimRight(u.ResolveReference(ref).String(), "/")
		}
	}
	return s.absoluteURL(r, path)
}

func (s *Server) beginSocialOAuth(w http.ResponseWriter, r *http.Request, cfg socialOAuthProviderConfig, session store.HumanOwnerSession, userID, handleHint string) (string, error) {
	nonce, err := randomSecret(12)
	if err != nil {
		return "", err
	}
	codeVerifier, err := pkceCodeVerifier()
	if err != nil {
		return "", err
	}
	expiresAt := time.Now().UTC().Add(socialOAuthStateTTL)
	statePayload := socialOAuthStatePayload{
		Provider:  cfg.Provider,
		UserID:    strings.TrimSpace(userID),
		OwnerID:   strings.TrimSpace(session.OwnerID),
		Nonce:     nonce,
		ExpiresAt: expiresAt.Unix(),
	}
	state, err := s.signSocialOAuthPayload(statePayload)
	if err != nil {
		return "", err
	}
	cookiePayload := socialOAuthCookiePayload{
		Provider:     cfg.Provider,
		UserID:       strings.TrimSpace(userID),
		OwnerID:      strings.TrimSpace(session.OwnerID),
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		ExpiresAt:    expiresAt.Unix(),
	}
	cookieValue, err := s.signSocialOAuthPayload(cookiePayload)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     socialOAuthCookieName(cfg.Provider),
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})

	authURL, err := neturl.Parse(cfg.AuthorizeURL)
	if err != nil {
		return "", fmt.Errorf("invalid %s authorize url: %w", cfg.Provider, err)
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", s.socialCallbackURI(r, cfg.Provider))
	query.Set("state", state)
	if len(cfg.Scopes) > 0 {
		query.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	if cfg.UsePKCE {
		query.Set("code_challenge", pkceCodeChallenge(codeVerifier))
		query.Set("code_challenge_method", "S256")
	}
	if handleHint != "" {
		query.Set("login_hint", handleHint)
	}
	authURL.RawQuery = query.Encode()
	return authURL.String(), nil
}

func (s *Server) readSocialOAuthCookie(r *http.Request, provider string) (socialOAuthCookiePayload, error) {
	cookie, err := r.Cookie(socialOAuthCookieName(provider))
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return socialOAuthCookiePayload{}, fmt.Errorf("oauth cookie is missing")
	}
	var payload socialOAuthCookiePayload
	if err := s.verifySocialOAuthPayload(cookie.Value, &payload); err != nil {
		return socialOAuthCookiePayload{}, err
	}
	if payload.ExpiresAt < time.Now().UTC().Unix() {
		return socialOAuthCookiePayload{}, fmt.Errorf("oauth cookie expired")
	}
	return payload, nil
}

func (s *Server) clearSocialOAuthCookie(w http.ResponseWriter, r *http.Request, provider string) {
	http.SetCookie(w, &http.Cookie{
		Name:     socialOAuthCookieName(provider),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func wantsJSON(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "json") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json")
}

func (s *Server) socialCallbackRedirectURL(provider string, values neturl.Values) string {
	u := &neturl.URL{Path: "/dashboard/agent-owner"}
	u.RawQuery = values.Encode()
	return u.String()
}

func (s *Server) writeSocialCallbackError(w http.ResponseWriter, r *http.Request, provider string, status int, msg string) {
	s.clearSocialOAuthCookie(w, r, provider)
	if wantsJSON(r) {
		writeError(w, status, msg)
		return
	}
	values := neturl.Values{}
	values.Set("provider", provider)
	values.Set("status", "error")
	values.Set("error", msg)
	http.Redirect(w, r, s.socialCallbackRedirectURL(provider, values), http.StatusSeeOther)
}

func (s *Server) writeSocialCallbackSuccess(w http.ResponseWriter, r *http.Request, provider string, payload map[string]any) {
	s.clearSocialOAuthCookie(w, r, provider)
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, payload)
		return
	}
	values := neturl.Values{}
	values.Set("provider", provider)
	values.Set("status", "ok")
	if userID, _ := payload["user_id"].(string); userID != "" {
		values.Set("user_id", userID)
	}
	http.Redirect(w, r, s.socialCallbackRedirectURL(provider, values), http.StatusSeeOther)
}

func (s *Server) exchangeSocialOAuthCode(ctx context.Context, cfg socialOAuthProviderConfig, code, redirectURI, codeVerifier string) (string, error) {
	form := neturl.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", redirectURI)
	if cfg.ClientID != "" {
		form.Set("client_id", cfg.ClientID)
	}
	if cfg.Provider == "github" && cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if cfg.Provider == "x" && cfg.ClientSecret != "" {
		req.SetBasicAuth(cfg.ClientID, cfg.ClientSecret)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s token exchange failed: %w", cfg.Provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("%s token exchange failed: status=%d body=%s", cfg.Provider, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token socialOAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("%s token exchange decode failed: %w", cfg.Provider, err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return "", fmt.Errorf("%s token exchange returned empty access_token", cfg.Provider)
	}
	return token.AccessToken, nil
}

func (s *Server) fetchGitHubViewer(ctx context.Context, accessToken string) (githubViewer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.githubOAuthUserInfoURL(), nil)
	if err != nil {
		return githubViewer{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", "clawcolony-runtime")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return githubViewer{}, fmt.Errorf("github viewer request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return githubViewer{}, fmt.Errorf("github viewer request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var viewer githubViewer
	if err := json.NewDecoder(resp.Body).Decode(&viewer); err != nil {
		return githubViewer{}, err
	}
	if strings.TrimSpace(viewer.Login) == "" {
		return githubViewer{}, fmt.Errorf("github viewer missing login")
	}
	return viewer, nil
}

func (s *Server) fetchXViewer(ctx context.Context, cfg socialOAuthProviderConfig, accessToken string) (xViewerEnvelope, error) {
	reqURL, err := neturl.Parse(cfg.UserInfoURL)
	if err != nil {
		return xViewerEnvelope{}, fmt.Errorf("invalid x userinfo url: %w", err)
	}
	query := reqURL.Query()
	query.Set("user.fields", "username,name")
	reqURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return xViewerEnvelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return xViewerEnvelope{}, fmt.Errorf("x viewer request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return xViewerEnvelope{}, fmt.Errorf("x viewer request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var viewer xViewerEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&viewer); err != nil {
		return xViewerEnvelope{}, err
	}
	if strings.TrimSpace(viewer.Data.Username) == "" {
		return xViewerEnvelope{}, fmt.Errorf("x viewer missing username")
	}
	return viewer, nil
}

func (s *Server) githubOAuthUserInfoURL() string {
	if raw := strings.TrimSpace(s.cfg.GitHubOAuthUserInfoURL); raw != "" {
		return raw
	}
	return defaultGitHubUserInfoURL
}

func (s *Server) verifyGitHubStarWithToken(ctx context.Context, accessToken string) (bool, error) {
	repo := strings.TrimSpace(s.officialGitHubRepo())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.githubAPIBaseURL(), "/")+"/user/starred/"+repo, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", "clawcolony-runtime")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("github star verification request failed: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	case http.StatusOK:
		return true, nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return false, fmt.Errorf("github star verification request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

func (s *Server) verifyGitHubForkWithToken(ctx context.Context, accessToken string) (bool, error) {
	target := strings.ToLower(strings.TrimSpace(s.officialGitHubRepo()))
	for page := 1; page <= maxGitHubVerificationPages; page++ {
		reqURL := fmt.Sprintf("%s/user/repos?type=owner&per_page=100&page=%d", strings.TrimRight(s.githubAPIBaseURL(), "/"), page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
		req.Header.Set("User-Agent", "clawcolony-runtime")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return false, fmt.Errorf("github fork verification request failed: %w", err)
		}
		var repos []githubRepoRecord
		if resp.StatusCode == http.StatusOK {
			err = json.NewDecoder(resp.Body).Decode(&repos)
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			return false, fmt.Errorf("github fork verification request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		resp.Body.Close()
		if err != nil {
			return false, err
		}
		if len(repos) == 0 {
			return false, nil
		}
		for _, repo := range repos {
			if repo.Parent != nil && repo.Fork && strings.EqualFold(strings.TrimSpace(repo.Parent.FullName), target) {
				return true, nil
			}
		}
		if len(repos) < 100 {
			return false, nil
		}
	}
	return false, nil
}

func (s *Server) handleSocialXCallback(w http.ResponseWriter, r *http.Request) {
	s.handleSocialOAuthCallback(w, r, "x")
}

func (s *Server) handleSocialGitHubCallback(w http.ResponseWriter, r *http.Request) {
	s.handleSocialOAuthCallback(w, r, "github")
}

func (s *Server) handleSocialOAuthCallback(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if providerErr := strings.TrimSpace(r.URL.Query().Get("error")); providerErr != "" {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadRequest, providerErr)
		return
	}
	cfg, ok := s.socialOAuthConfig(provider)
	if !ok {
		s.writeSocialCallbackError(w, r, provider, http.StatusServiceUnavailable, provider+" oauth is not configured")
		return
	}
	rawState := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if rawState == "" || code == "" {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadRequest, "oauth callback requires code and state")
		return
	}
	var state socialOAuthStatePayload
	if err := s.verifySocialOAuthPayload(rawState, &state); err != nil {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadRequest, err.Error())
		return
	}
	if state.Provider != provider || state.ExpiresAt < time.Now().UTC().Unix() {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadRequest, "oauth state expired or mismatched")
		return
	}
	session, err := s.requireOwnerSessionForOwner(r, state.OwnerID)
	if err != nil {
		s.writeSocialCallbackError(w, r, provider, http.StatusUnauthorized, err.Error())
		return
	}
	cookiePayload, err := s.readSocialOAuthCookie(r, provider)
	if err != nil {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadRequest, err.Error())
		return
	}
	if cookiePayload.Provider != provider || cookiePayload.UserID != state.UserID || cookiePayload.OwnerID != session.OwnerID || cookiePayload.Nonce != state.Nonce {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadRequest, "oauth cookie mismatch")
		return
	}
	accessToken, err := s.exchangeSocialOAuthCode(r.Context(), cfg, code, s.socialCallbackURI(r, provider), cookiePayload.CodeVerifier)
	if err != nil {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadGateway, err.Error())
		return
	}
	var payload map[string]any
	switch provider {
	case "x":
		payload, err = s.completeXOAuthCallback(r.Context(), state.OwnerID, state.UserID, accessToken)
	case "github":
		payload, err = s.completeGitHubOAuthCallback(r.Context(), state.OwnerID, state.UserID, accessToken)
	default:
		err = fmt.Errorf("unsupported provider")
	}
	if err != nil {
		s.writeSocialCallbackError(w, r, provider, http.StatusBadGateway, err.Error())
		return
	}
	payload["provider"] = provider
	payload["user_id"] = state.UserID
	s.writeSocialCallbackSuccess(w, r, provider, payload)
}

func (s *Server) completeXOAuthCallback(ctx context.Context, ownerID, userID, accessToken string) (map[string]any, error) {
	cfg, _ := s.socialOAuthConfig("x")
	viewer, err := s.fetchXViewer(ctx, cfg, accessToken)
	if err != nil {
		return nil, err
	}
	handle := "@" + strings.TrimPrefix(strings.TrimSpace(viewer.Data.Username), "@")
	owner, err := s.store.UpsertHumanOwnerSocialIdentity(ctx, ownerID, "x", handle, viewer.Data.ID)
	if err != nil {
		return nil, err
	}
	link, err := s.store.UpsertSocialLink(ctx, store.SocialLink{
		UserID:   strings.TrimSpace(userID),
		Provider: "x",
		Handle:   handle,
		Status:   "authorized",
		MetadataJSON: mustMarshalJSON(map[string]any{
			"provider_user_id": viewer.Data.ID,
			"name":             viewer.Data.Name,
			"username":         viewer.Data.Username,
			"owner_id":         ownerID,
		}),
	})
	if err != nil {
		return nil, err
	}
	grant := store.SocialRewardGrant{
		GrantKey:   fmt.Sprintf("social:x:auth:%s", strings.TrimSpace(userID)),
		UserID:     strings.TrimSpace(userID),
		Provider:   "x",
		RewardType: "auth_callback",
		Amount:     s.socialRewardAmountXAuth(),
		MetaJSON:   mustMarshalJSON(map[string]any{"provider_user_id": viewer.Data.ID, "username": viewer.Data.Username, "owner_id": ownerID}),
	}
	reward, created, err := s.store.GrantSocialReward(ctx, grant)
	if err != nil {
		return nil, err
	}
	if created {
		if _, err := s.store.Recharge(ctx, userID, reward.Amount); err != nil {
			return nil, err
		}
	}
	return map[string]any{"item": link, "reward": reward, "granted": created, "handle": handle, "owner": owner}, nil
}

func (s *Server) completeGitHubOAuthCallback(ctx context.Context, ownerID, userID, accessToken string) (map[string]any, error) {
	viewer, err := s.fetchGitHubViewer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	starred, err := s.verifyGitHubStarWithToken(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	forked, err := s.verifyGitHubForkWithToken(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	owner, err := s.store.UpsertHumanOwnerSocialIdentity(ctx, ownerID, "github", strings.TrimSpace(viewer.Login), fmt.Sprintf("%d", viewer.ID))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	status := "authorized"
	if starred || forked {
		status = "verified"
	}
	link, err := s.store.UpsertSocialLink(ctx, store.SocialLink{
		UserID:   strings.TrimSpace(userID),
		Provider: "github",
		Handle:   strings.TrimSpace(viewer.Login),
		Status:   status,
		MetadataJSON: mustMarshalJSON(map[string]any{
			"provider_user_id": viewer.ID,
			"repo":             s.officialGitHubRepo(),
			"starred":          starred,
			"forked":           forked,
			"owner_id":         ownerID,
		}),
		VerifiedAt: func() *time.Time {
			if status != "verified" {
				return nil
			}
			return &now
		}(),
	})
	if err != nil {
		return nil, err
	}
	grants := make([]map[string]any, 0, 3)
	grants = append(grants, s.grantSocialRewardForGitHub(ctx, userID, "auth_callback", s.socialRewardAmountGitHubAuth()))
	if starred {
		grants = append(grants, s.grantSocialRewardForGitHub(ctx, userID, "star", s.socialRewardAmountGitHubStar()))
	}
	if forked {
		grants = append(grants, s.grantSocialRewardForGitHub(ctx, userID, "fork", s.socialRewardAmountGitHubFork()))
	}
	return map[string]any{
		"item":     link,
		"grants":   grants,
		"starred":  starred,
		"forked":   forked,
		"repo":     s.officialGitHubRepo(),
		"username": viewer.Login,
		"owner":    owner,
	}, nil
}

func (s *Server) officialGitHubRepo() string {
	raw := strings.TrimSpace(os.Getenv("CLAWCOLONY_OFFICIAL_GITHUB_REPO"))
	if raw == "" {
		return defaultOfficialGitHubRepo
	}
	return raw
}

func (s *Server) githubAPIBaseURL() string {
	raw := strings.TrimSpace(os.Getenv("CLAWCOLONY_GITHUB_API_BASE_URL"))
	if raw == "" {
		return defaultGitHubAPIBaseURL
	}
	return strings.TrimRight(raw, "/")
}

func (s *Server) verifyGitHubStar(ctx context.Context, username string) (bool, error) {
	target := strings.ToLower(strings.TrimSpace(s.officialGitHubRepo()))
	for page := 1; page <= maxGitHubVerificationPages; page++ {
		var repos []githubRepoRecord
		if err := s.fetchGitHubJSON(ctx, fmt.Sprintf("/users/%s/starred?per_page=100&page=%d", neturl.PathEscape(strings.TrimSpace(username)), page), &repos); err != nil {
			return false, err
		}
		if len(repos) == 0 {
			return false, nil
		}
		for _, repo := range repos {
			if strings.EqualFold(strings.TrimSpace(repo.FullName), target) {
				return true, nil
			}
		}
		if len(repos) < 100 {
			return false, nil
		}
	}
	return false, nil
}

func (s *Server) verifyGitHubFork(ctx context.Context, username string) (bool, error) {
	target := strings.ToLower(strings.TrimSpace(s.officialGitHubRepo()))
	for page := 1; page <= maxGitHubVerificationPages; page++ {
		var repos []githubRepoRecord
		if err := s.fetchGitHubJSON(ctx, fmt.Sprintf("/users/%s/repos?type=owner&per_page=100&page=%d", neturl.PathEscape(strings.TrimSpace(username)), page), &repos); err != nil {
			return false, err
		}
		if len(repos) == 0 {
			return false, nil
		}
		for _, repo := range repos {
			if repo.Parent != nil && repo.Fork && strings.EqualFold(strings.TrimSpace(repo.Parent.FullName), target) {
				return true, nil
			}
		}
		if len(repos) < 100 {
			return false, nil
		}
	}
	return false, nil
}

func (s *Server) fetchGitHubJSON(ctx context.Context, path string, out any) error {
	base, err := neturl.Parse(s.githubAPIBaseURL())
	if err != nil {
		return fmt.Errorf("invalid github api base url: %w", err)
	}
	rel, err := neturl.Parse(path)
	if err != nil {
		return fmt.Errorf("invalid github api path: %w", err)
	}
	reqURL := base.ResolveReference(rel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "clawcolony-runtime")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("github verification request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("github verification request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Server) claimSafeUsername(ctx context.Context, userID, base string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(base))
	if username == "" {
		username = "agent"
	}
	if ok, err := s.usernameAvailableForActivation(ctx, userID, username); err != nil {
		return "", err
	} else if ok {
		return username, nil
	}
	for i := 0; i < 32; i++ {
		suffix, err := randomSecret(2)
		if err != nil {
			return "", err
		}
		candidate := fmt.Sprintf("%s-%s", username, suffix[:4])
		if ok, err := s.usernameAvailableForActivation(ctx, userID, candidate); err != nil {
			return "", err
		} else if ok {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("failed to allocate unique username")
}

func (s *Server) usernameAvailableForActivation(ctx context.Context, userID, candidate string) (bool, error) {
	items, err := s.store.ListBots(ctx)
	if err != nil {
		return false, err
	}
	for _, item := range items {
		if item.BotID == strings.TrimSpace(userID) {
			continue
		}
		if !item.Initialized || !isRuntimeBotStatusActive(item.Status) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(item.Name), strings.TrimSpace(candidate)) {
			return false, nil
		}
	}
	return true, nil
}
