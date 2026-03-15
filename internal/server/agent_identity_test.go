package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"clawcolony/internal/store"
)

func identityTestHandler(srv *Server) http.Handler {
	return srv.ownerAndPricingMiddleware(srv.mux)
}

func parseJSONBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return payload
}

func registerAgentForTest(t *testing.T, h http.Handler, username, goodAt string) (string, string, string) {
	t.Helper()
	w := doJSONRequest(t, h, http.MethodPost, "/v1/users/register", map[string]any{
		"username": username,
		"good_at":  goodAt,
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("register status=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSONBody(t, w)
	setup, ok := body["setup"].(map[string]any)
	if !ok {
		t.Fatalf("register response missing setup: %s", w.Body.String())
	}
	if got := setup["step_1"]; got != "Save your api_key to ~/.config/clawcolony/credentials.json now. It will not be shown again." {
		t.Fatalf("unexpected setup.step_1=%v", got)
	}
	return body["user_id"].(string), body["api_key"].(string), body["claim_link"].(string)
}

func claimAgentForTest(t *testing.T, h http.Handler, claimLink, email, humanName string) (string, string) {
	t.Helper()
	u, err := neturl.Parse(claimLink)
	if err != nil {
		t.Fatalf("parse claim link: %v", err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	claimToken := parts[len(parts)-1]
	requestMagic := doJSONRequest(t, h, http.MethodPost, "/v1/claims/request-magic-link", map[string]any{
		"claim_token":           claimToken,
		"email":                 email,
		"human_username":        humanName,
		"human_name_visibility": "public",
	})
	if requestMagic.Code != http.StatusAccepted {
		t.Fatalf("magic link status=%d body=%s", requestMagic.Code, requestMagic.Body.String())
	}
	requestBody := parseJSONBody(t, requestMagic)
	magicLink := requestBody["magic_link"].(string)
	magicURL, err := neturl.Parse(magicLink)
	if err != nil {
		t.Fatalf("parse magic link: %v", err)
	}
	magicToken := magicURL.Query().Get("magic_token")
	complete := doJSONRequest(t, h, http.MethodPost, "/v1/claims/complete", map[string]any{
		"magic_token": magicToken,
	})
	if complete.Code != http.StatusOK {
		t.Fatalf("claim complete status=%d body=%s", complete.Code, complete.Body.String())
	}
	resp := parseJSONBody(t, complete)
	cookies := complete.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected owner session cookie")
	}
	return resp["username"].(string), cookies[0].Name + "=" + cookies[0].Value
}

func joinCookieHeader(base string, cookies []*http.Cookie) string {
	parts := make([]string, 0, len(cookies)+1)
	if strings.TrimSpace(base) != "" {
		parts = append(parts, strings.TrimSpace(base))
	}
	for _, c := range cookies {
		if c == nil || strings.TrimSpace(c.Name) == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func enableXOAuthForTest(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse x token form: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				t.Fatalf("unexpected x grant_type=%q", got)
			}
			if strings.TrimSpace(r.Form.Get("code_verifier")) == "" {
				t.Fatalf("expected x code_verifier")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"x-access-token","token_type":"bearer","scope":"users.read"}`))
		case r.URL.Path == "/2/users/me":
			if got := r.Header.Get("Authorization"); got != "Bearer x-access-token" {
				t.Fatalf("unexpected x auth header=%q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"id":"x-user-1","name":"Orbit Agent","username":"orbit_agent"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("CLAWCOLONY_X_OAUTH_CLIENT_ID", "x-client")
	t.Setenv("CLAWCOLONY_X_OAUTH_CLIENT_SECRET", "x-secret")
	t.Setenv("CLAWCOLONY_X_OAUTH_AUTHORIZE_URL", srv.URL+"/oauth2/authorize")
	t.Setenv("CLAWCOLONY_X_OAUTH_TOKEN_URL", srv.URL+"/oauth2/token")
	t.Setenv("CLAWCOLONY_X_OAUTH_USERINFO_URL", srv.URL+"/2/users/me")
	return srv
}

func enableGitHubOAuthForTest(t *testing.T, starred, forked bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login/oauth/access_token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse github token form: %v", err)
			}
			if strings.TrimSpace(r.Form.Get("code_verifier")) == "" {
				t.Fatalf("expected github code_verifier")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"gh-access-token","token_type":"bearer","scope":"read:user"}`))
		case r.URL.Path == "/user":
			if got := r.Header.Get("Authorization"); got != "Bearer gh-access-token" {
				t.Fatalf("unexpected github auth header=%q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"login":"octo","name":"Octo Human"}`))
		case r.URL.Path == "/users/octo/starred":
			if starred {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"full_name":"clawcolony/clawcolony"}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/users/octo/repos":
			w.Header().Set("Content-Type", "application/json")
			if forked {
				_, _ = w.Write([]byte(`[{"full_name":"octo/clawcolony","fork":true,"parent":{"full_name":"clawcolony/clawcolony"}}]`))
				return
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Setenv("CLAWCOLONY_GITHUB_OAUTH_CLIENT_ID", "gh-client")
	t.Setenv("CLAWCOLONY_GITHUB_OAUTH_CLIENT_SECRET", "gh-secret")
	t.Setenv("CLAWCOLONY_GITHUB_OAUTH_AUTHORIZE_URL", srv.URL+"/login/oauth/authorize")
	t.Setenv("CLAWCOLONY_GITHUB_OAUTH_TOKEN_URL", srv.URL+"/login/oauth/access_token")
	t.Setenv("CLAWCOLONY_GITHUB_OAUTH_USERINFO_URL", srv.URL+"/user")
	t.Setenv("CLAWCOLONY_GITHUB_API_BASE_URL", srv.URL)
	t.Setenv("CLAWCOLONY_OFFICIAL_GITHUB_REPO", "clawcolony/clawcolony")
	return srv
}

func completeSocialOAuthCallbackForTest(t *testing.T, h http.Handler, start *httptest.ResponseRecorder, ownerCookie, provider, code string) *httptest.ResponseRecorder {
	t.Helper()
	body := parseJSONBody(t, start)
	rawAuthorizeURL, _ := body["authorize_url"].(string)
	if strings.TrimSpace(rawAuthorizeURL) == "" {
		t.Fatalf("missing authorize_url in start response: %s", start.Body.String())
	}
	authURL, err := neturl.Parse(rawAuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize_url: %v", err)
	}
	state := authURL.Query().Get("state")
	callbackPath := "/auth/" + provider + "/callback?code=" + neturl.QueryEscape(code) + "&state=" + neturl.QueryEscape(state) + "&format=json"
	req := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", joinCookieHeader(ownerCookie, start.Result().Cookies()))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func rewardAgentViaXOAuthForTest(t *testing.T, h http.Handler, userID, ownerCookie string) {
	t.Helper()
	start := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/connect/start", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": ownerCookie})
	if start.Code != http.StatusAccepted {
		t.Fatalf("x connect start status=%d body=%s", start.Code, start.Body.String())
	}
	callback := completeSocialOAuthCallbackForTest(t, h, start, ownerCookie, "x", "x-code")
	if callback.Code != http.StatusOK {
		t.Fatalf("x callback status=%d body=%s", callback.Code, callback.Body.String())
	}
}

func TestUserRegisterAndStatusFlow(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, apiKey, _ := registerAgentForTest(t, h, "orbit-agent", "routing requests")
	if !strings.HasPrefix(apiKey, apiKeyPrefix) {
		t.Fatalf("api_key prefix mismatch: %q", apiKey)
	}
	reg, err := srv.store.GetAgentRegistrationByAPIKeyHash(t.Context(), hashSecret(apiKey))
	if err != nil {
		t.Fatalf("lookup registration by api key hash: %v", err)
	}
	if reg.UserID != userID {
		t.Fatalf("registration user_id mismatch: got=%s want=%s", reg.UserID, userID)
	}
	if reg.APIKeyHash == apiKey {
		t.Fatalf("api_key must not be stored in plaintext")
	}

	status := doJSONRequestWithHeaders(t, h, http.MethodGet, "/v1/users/status", nil, map[string]string{
		"Authorization": "Bearer " + apiKey,
	})
	if status.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", status.Code, status.Body.String())
	}
	statusBody := parseJSONBody(t, status)
	if got := statusBody["status"]; got != "pending_claim" {
		t.Fatalf("expected pending_claim, got=%v", got)
	}

	list := doJSONRequest(t, h, http.MethodGet, "/v1/bots?include_inactive=0", nil)
	if strings.Contains(list.Body.String(), userID) {
		t.Fatalf("pending agent must not appear in active list: %s", list.Body.String())
	}
}

func TestClaimFlowActivatesAgentAndAutoSuffixesConflicts(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	firstID, _, firstClaim := registerAgentForTest(t, h, "same-agent", "first")
	secondID, _, secondClaim := registerAgentForTest(t, h, "same-agent", "second")

	firstUsername, _ := claimAgentForTest(t, h, firstClaim, "one@example.com", "buddy-one")
	if firstUsername != "same-agent" {
		t.Fatalf("expected original username on first claim, got=%q", firstUsername)
	}
	secondUsername, _ := claimAgentForTest(t, h, secondClaim, "two@example.com", "buddy-two")
	if secondUsername == "same-agent" || !strings.HasPrefix(secondUsername, "same-agent-") {
		t.Fatalf("expected suffixed username, got=%q", secondUsername)
	}

	firstBot, err := srv.store.GetBot(t.Context(), firstID)
	if err != nil {
		t.Fatalf("get first bot: %v", err)
	}
	if firstBot.Status != "running" || !firstBot.Initialized {
		t.Fatalf("first bot should be active after claim: %+v", firstBot)
	}
	secondBot, err := srv.store.GetBot(t.Context(), secondID)
	if err != nil {
		t.Fatalf("get second bot: %v", err)
	}
	if secondBot.Name != secondUsername {
		t.Fatalf("second bot username mismatch: got=%q want=%q", secondBot.Name, secondUsername)
	}
}

func TestManagedAgentRequiresOwnerSessionAndTokenBalance(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()

	srv := newTestServer()
	srv.cfg.RegistrationGrantToken = 0 // disable grant to test pricing in isolation
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "managed-agent", "mail")
	_, cookie := claimAgentForTest(t, h, claimLink, "managed@example.com", "human-manager")
	recipient := seedActiveUser(t, srv)

	unauth := doJSONRequest(t, h, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{recipient},
		"subject":      "hello",
		"body":         "world",
	})
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without cookie, got=%d body=%s", unauth.Code, unauth.Body.String())
	}

	noFunds := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{recipient},
		"subject":      "hello",
		"body":         "world",
	}, map[string]string{"Cookie": cookie})
	if noFunds.Code != http.StatusPaymentRequired {
		t.Fatalf("expected payment required, got=%d body=%s", noFunds.Code, noFunds.Body.String())
	}

	rewardAgentViaXOAuthForTest(t, h, userID, cookie)

	balance := doJSONRequest(t, h, http.MethodGet, "/v1/token/balance?user_id="+userID, nil)
	if balance.Code != http.StatusOK || !strings.Contains(balance.Body.String(), `"balance":20`) {
		t.Fatalf("expected rewarded balance=20, got code=%d body=%s", balance.Code, balance.Body.String())
	}

	send := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{recipient},
		"subject":      "hello",
		"body":         "world",
	}, map[string]string{"Cookie": cookie})
	if send.Code != http.StatusAccepted {
		t.Fatalf("expected accepted send after reward, got=%d body=%s", send.Code, send.Body.String())
	}
	after := doJSONRequest(t, h, http.MethodGet, "/v1/token/balance?user_id="+userID, nil)
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"balance":19`) {
		t.Fatalf("expected balance=19 after priced send, got code=%d body=%s", after.Code, after.Body.String())
	}

	ownerMe := doJSONRequestWithHeaders(t, h, http.MethodGet, "/v1/owner/me", nil, map[string]string{"Cookie": cookie})
	if ownerMe.Code != http.StatusOK || !strings.Contains(ownerMe.Body.String(), `"x_handle":"@orbit_agent"`) {
		t.Fatalf("expected owner x identity binding, got code=%d body=%s", ownerMe.Code, ownerMe.Body.String())
	}
}

func TestClaimRequestMagicLinkRejectsExpiredClaimToken(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	expiredAt := time.Now().UTC().Add(-time.Minute)
	if _, err := srv.store.UpsertBot(t.Context(), store.BotUpsertInput{
		BotID:       "expired-claim-agent",
		Name:        "expired-agent",
		Provider:    "agent",
		Status:      "inactive",
		Initialized: false,
	}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	if _, err := srv.store.CreateAgentRegistration(t.Context(), store.AgentRegistrationInput{
		UserID:              "expired-claim-agent",
		RequestedUsername:   "expired-agent",
		GoodAt:              "timing",
		Status:              "pending_claim",
		ClaimTokenHash:      hashSecret("expired-claim-token"),
		ClaimTokenExpiresAt: &expiredAt,
		APIKeyHash:          hashSecret("clawcolony-expired"),
	}); err != nil {
		t.Fatalf("seed registration: %v", err)
	}
	if _, err := srv.store.UpsertAgentProfile(t.Context(), store.AgentProfile{
		UserID:   "expired-claim-agent",
		Username: "expired-agent",
		GoodAt:   "timing",
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}

	w := doJSONRequest(t, h, http.MethodPost, "/v1/claims/request-magic-link", map[string]any{
		"claim_token":           "expired-claim-token",
		"email":                 "buddy@example.com",
		"human_username":        "buddy",
		"human_name_visibility": "public",
	})
	if w.Code != http.StatusGone {
		t.Fatalf("expected claim token expired, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestClaimCompleteRejectsExpiredMagicToken(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, _, _ := registerAgentForTest(t, h, "magic-expired-agent", "timing")
	if _, err := srv.store.UpdateAgentRegistrationClaim(
		t.Context(),
		userID,
		"buddy@example.com",
		"buddy",
		"public",
		hashSecret("expired-magic-token"),
		time.Now().UTC().Add(-time.Minute),
	); err != nil {
		t.Fatalf("seed expired magic token: %v", err)
	}

	w := doJSONRequest(t, h, http.MethodPost, "/v1/claims/complete", map[string]any{
		"magic_token": "expired-magic-token",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected magic token expired, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGitHubVerifyUsesServerSideVerificationAndRewards(t *testing.T) {
	gh := enableGitHubOAuthForTest(t, true, true)
	defer gh.Close()

	srv := newTestServer()
	srv.cfg.RegistrationGrantToken = 0 // disable grant to test reward balances in isolation
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "github-agent", "oss")
	_, cookie := claimAgentForTest(t, h, claimLink, "github@example.com", "octo-human")

	start := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/github/connect/start", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": cookie})
	if start.Code != http.StatusAccepted {
		t.Fatalf("github connect start status=%d body=%s", start.Code, start.Body.String())
	}

	callback := completeSocialOAuthCallbackForTest(t, h, start, cookie, "github", "gh-code")
	if callback.Code != http.StatusOK {
		t.Fatalf("github callback status=%d body=%s", callback.Code, callback.Body.String())
	}
	body := parseJSONBody(t, callback)
	if body["starred"] != true || body["forked"] != true {
		t.Fatalf("expected oauth github verification, got body=%s", callback.Body.String())
	}

	balance := doJSONRequest(t, h, http.MethodGet, "/v1/token/balance?user_id="+userID, nil)
	if balance.Code != http.StatusOK || !strings.Contains(balance.Body.String(), `"balance":50`) {
		t.Fatalf("expected rewarded balance=50, got code=%d body=%s", balance.Code, balance.Body.String())
	}

	ownerMe := doJSONRequestWithHeaders(t, h, http.MethodGet, "/v1/owner/me", nil, map[string]string{"Cookie": cookie})
	if ownerMe.Code != http.StatusOK || !strings.Contains(ownerMe.Body.String(), `"github_username":"octo"`) {
		t.Fatalf("expected owner github identity binding, got code=%d body=%s", ownerMe.Code, ownerMe.Body.String())
	}
}

func TestGitHubConnectStartUsesLeastPrivilegeScope(t *testing.T) {
	gh := enableGitHubOAuthForTest(t, false, false)
	defer gh.Close()

	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "github-scope-agent", "oss")
	_, cookie := claimAgentForTest(t, h, claimLink, "github-scope@example.com", "octo-human")

	start := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/github/connect/start", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": cookie})
	if start.Code != http.StatusAccepted {
		t.Fatalf("github connect start status=%d body=%s", start.Code, start.Body.String())
	}

	body := parseJSONBody(t, start)
	rawAuthorizeURL, _ := body["authorize_url"].(string)
	if strings.TrimSpace(rawAuthorizeURL) == "" {
		t.Fatalf("missing authorize_url in start response: %s", start.Body.String())
	}
	authorizeURL, err := neturl.Parse(rawAuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize_url: %v", err)
	}
	if got := authorizeURL.Query().Get("scope"); got != "read:user" {
		t.Fatalf("expected least-privilege github scope, got=%q", got)
	}
}

func TestManualSocialVerifyEndpointsRejectWhenOAuthIsConfigured(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()
	ghOAuth := enableGitHubOAuthForTest(t, true, true)
	defer ghOAuth.Close()

	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "manual-disabled-agent", "oss")
	_, cookie := claimAgentForTest(t, h, claimLink, "manual-disabled@example.com", "manual-disabled-human")

	xVerifyBeforeAuth := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/verify", map[string]any{
		"user_id":   userID,
		"post_text": "hello " + defaultOfficialXHandle,
	}, map[string]string{"Cookie": cookie})
	if xVerifyBeforeAuth.Code != http.StatusNotFound {
		t.Fatalf("expected x verify to require oauth identity first, got=%d body=%s", xVerifyBeforeAuth.Code, xVerifyBeforeAuth.Body.String())
	}

	xVerify := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/verify", map[string]any{
		"user_id":   userID,
		"post_text": "hello " + defaultOfficialXHandle,
	}, map[string]string{"Cookie": cookie})
	if xVerify.Code != http.StatusNotFound {
		t.Fatalf("expected x verify to require oauth identity binding, got=%d body=%s", xVerify.Code, xVerify.Body.String())
	}

	ghVerify := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/github/verify", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": cookie})
	if ghVerify.Code != http.StatusConflict {
		t.Fatalf("expected github manual verify conflict, got=%d body=%s", ghVerify.Code, ghVerify.Body.String())
	}
}

func TestXMentionRewardIsGrantedAndQueryable(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()

	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "mention-agent", "oss")
	_, cookie := claimAgentForTest(t, h, claimLink, "mention@example.com", "mention-human")
	rewardAgentViaXOAuthForTest(t, h, userID, cookie)

	mention := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/verify", map[string]any{
		"user_id":   userID,
		"post_text": "hello " + defaultOfficialXHandle + " from orbit",
	}, map[string]string{"Cookie": cookie})
	if mention.Code != http.StatusOK {
		t.Fatalf("expected x mention reward ok, got=%d body=%s", mention.Code, mention.Body.String())
	}

	status := doJSONRequestWithHeaders(t, h, http.MethodGet, "/v1/social/rewards/status?user_id="+userID, nil, map[string]string{"Cookie": cookie})
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"reward_type":"mention"`) {
		t.Fatalf("expected mention reward in status, got=%d body=%s", status.Code, status.Body.String())
	}
}

func TestSocialRewardAmountsAreConfigurable(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()
	t.Setenv("CLAWCOLONY_SOCIAL_REWARD_X_AUTH", "7")
	t.Setenv("CLAWCOLONY_SOCIAL_REWARD_X_MENTION", "3")
	t.Setenv("CLAWCOLONY_SOCIAL_REWARD_GITHUB_AUTH", "11")
	t.Setenv("CLAWCOLONY_SOCIAL_REWARD_GITHUB_STAR", "13")
	t.Setenv("CLAWCOLONY_SOCIAL_REWARD_GITHUB_FORK", "17")

	srv := newTestServer()
	h := identityTestHandler(srv)

	policy := doJSONRequest(t, h, http.MethodGet, "/v1/social/policy", nil)
	if policy.Code != http.StatusOK {
		t.Fatalf("social policy status=%d body=%s", policy.Code, policy.Body.String())
	}
	body := policy.Body.String()
	for _, needle := range []string{`"reward_auth_amount":7`, `"reward_mention_amount":3`, `"reward_star_amount":13`, `"reward_fork_amount":17`} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected configurable reward amount %s in policy, got=%s", needle, body)
		}
	}
}

func TestOAuthCallbackRejectsTamperedState(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()

	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "tampered-oauth-agent", "oss")
	_, cookie := claimAgentForTest(t, h, claimLink, "tampered@example.com", "tampered-human")

	start := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/connect/start", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": cookie})
	if start.Code != http.StatusAccepted {
		t.Fatalf("x connect start status=%d body=%s", start.Code, start.Body.String())
	}
	body := parseJSONBody(t, start)
	authURL, err := neturl.Parse(body["authorize_url"].(string))
	if err != nil {
		t.Fatalf("parse authorize_url: %v", err)
	}
	state := authURL.Query().Get("state") + "tampered"
	req := httptest.NewRequest(http.MethodGet, "/auth/x/callback?code=x-code&state="+neturl.QueryEscape(state)+"&format=json", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", joinCookieHeader(cookie, start.Result().Cookies()))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for tampered state, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestSocialRewardsStatusRequiresOwnerAndHidesChallenge(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()

	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "status-agent", "mail")
	_, cookie := claimAgentForTest(t, h, claimLink, "status@example.com", "status-human")

	start := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/connect/start", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": cookie})
	if start.Code != http.StatusAccepted {
		t.Fatalf("x connect start status=%d body=%s", start.Code, start.Body.String())
	}

	unauth := doJSONRequest(t, h, http.MethodGet, "/v1/social/rewards/status?user_id="+userID, nil)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized rewards status without owner session, got=%d body=%s", unauth.Code, unauth.Body.String())
	}

	status := doJSONRequestWithHeaders(t, h, http.MethodGet, "/v1/social/rewards/status?user_id="+userID, nil, map[string]string{"Cookie": cookie})
	if status.Code != http.StatusOK {
		t.Fatalf("expected rewards status ok, got=%d body=%s", status.Code, status.Body.String())
	}
	if strings.Contains(status.Body.String(), `"challenge"`) {
		t.Fatalf("rewards status must not leak challenge: %s", status.Body.String())
	}
}

func TestOwnerLogoutRevokesSession(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "logout-agent", "mail")
	_, cookie := claimAgentForTest(t, h, claimLink, "logout@example.com", "logout-human")
	recipient := seedActiveUser(t, srv)

	logout := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/owner/logout", nil, map[string]string{"Cookie": cookie})
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status=%d body=%s", logout.Code, logout.Body.String())
	}

	send := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{recipient},
		"subject":      "hello",
		"body":         "world",
	}, map[string]string{"Cookie": cookie})
	if send.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked session to be rejected, got=%d body=%s", send.Code, send.Body.String())
	}
}

func TestTokenPricingIsSorted(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	w := doJSONRequest(t, h, http.MethodGet, "/v1/token/pricing", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("token pricing status=%d body=%s", w.Code, w.Body.String())
	}
	body := parseJSONBody(t, w)
	items, ok := body["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected pricing items, got body=%s", w.Body.String())
	}
	prev := ""
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected item object, got %#v", raw)
		}
		path, _ := item["path"].(string)
		if prev != "" && path < prev {
			t.Fatalf("pricing items should be sorted: prev=%q current=%q", prev, path)
		}
		prev = path
	}
}

func TestClaimAlreadyClaimedAgentConflicts(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	_, _, claimLink := registerAgentForTest(t, h, "claimed-agent", "mail")
	_, _ = claimAgentForTest(t, h, claimLink, "claimed@example.com", "claimed-human")

	u, err := neturl.Parse(claimLink)
	if err != nil {
		t.Fatalf("parse claim link: %v", err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	claimToken := parts[len(parts)-1]

	w := doJSONRequest(t, h, http.MethodPost, "/v1/claims/request-magic-link", map[string]any{
		"claim_token":           claimToken,
		"email":                 "claimed@example.com",
		"human_username":        "claimed-human",
		"human_name_visibility": "public",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected already claimed conflict, got=%d body=%s", w.Code, w.Body.String())
	}
}

func TestManagedOwnerCannotWriteForAnotherClaimedAgent(t *testing.T) {
	srv := newTestServer()
	h := identityTestHandler(srv)

	userOne, _, claimOne := registerAgentForTest(t, h, "owner-one-agent", "mail")
	_, cookieOne := claimAgentForTest(t, h, claimOne, "owner1@example.com", "owner-one")
	userTwo, _, claimTwo := registerAgentForTest(t, h, "owner-two-agent", "mail")
	_, cookieTwo := claimAgentForTest(t, h, claimTwo, "owner2@example.com", "owner-two")
	recipient := seedActiveUser(t, srv)

	_ = cookieOne
	w := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userOne,
		"to_user_ids":  []string{recipient},
		"subject":      "unauthorized",
		"body":         "owner mismatch",
	}, map[string]string{"Cookie": cookieTwo})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden for wrong owner session on user=%s actor=%s got=%d body=%s", userOne, userTwo, w.Code, w.Body.String())
	}
}

func TestPricedWriteRefundsOnValidationFailure(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()

	srv := newTestServer()
	srv.cfg.RegistrationGrantToken = 0 // disable grant to test refund balances in isolation
	h := identityTestHandler(srv)

	userID, _, claimLink := registerAgentForTest(t, h, "refund-agent", "mail")
	_, cookie := claimAgentForTest(t, h, claimLink, "refund@example.com", "refund-human")

	rewardAgentViaXOAuthForTest(t, h, userID, cookie)

	before := doJSONRequest(t, h, http.MethodGet, "/v1/token/balance?user_id="+userID, nil)
	if before.Code != http.StatusOK || !strings.Contains(before.Body.String(), `"balance":20`) {
		t.Fatalf("expected starting balance=20, got code=%d body=%s", before.Code, before.Body.String())
	}

	fail := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/mail/send", map[string]any{
		"from_user_id": userID,
		"to_user_ids":  []string{},
		"subject":      "bad request",
		"body":         "should refund",
	}, map[string]string{"Cookie": cookie})
	if fail.Code != http.StatusBadRequest {
		t.Fatalf("expected downstream validation failure, got=%d body=%s", fail.Code, fail.Body.String())
	}

	after := doJSONRequest(t, h, http.MethodGet, "/v1/token/balance?user_id="+userID, nil)
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"balance":20`) {
		t.Fatalf("expected refund to restore balance=20, got code=%d body=%s", after.Code, after.Body.String())
	}
}

func TestSocialPolicyEndpointAndConnectRateLimit(t *testing.T) {
	xOAuth := enableXOAuthForTest(t)
	defer xOAuth.Close()

	srv := newTestServer()
	h := identityTestHandler(srv)

	policy := doJSONRequest(t, h, http.MethodGet, "/v1/social/policy", nil)
	if policy.Code != http.StatusOK {
		t.Fatalf("social policy status=%d body=%s", policy.Code, policy.Body.String())
	}
	if !strings.Contains(policy.Body.String(), `"mode":"oauth_callback"`) {
		t.Fatalf("expected oauth callback policy, got=%s", policy.Body.String())
	}

	userID, _, claimLink := registerAgentForTest(t, h, "limited-social-agent", "mail")
	_, cookie := claimAgentForTest(t, h, claimLink, "limited@example.com", "limited-human")

	first := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/connect/start", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": cookie})
	if first.Code != http.StatusAccepted {
		t.Fatalf("first x connect start status=%d body=%s", first.Code, first.Body.String())
	}
	second := doJSONRequestWithHeaders(t, h, http.MethodPost, "/v1/social/x/connect/start", map[string]any{
		"user_id": userID,
	}, map[string]string{"Cookie": cookie})
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected connect rate limit, got=%d body=%s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), `"retry_after_seconds"`) {
		t.Fatalf("expected retry_after_seconds in rate limit payload, got=%s", second.Body.String())
	}
}

func TestPricedBusinessActionsCoverage(t *testing.T) {
	expected := []string{
		"/v1/bounty/claim",
		"/v1/bounty/post",
		"/v1/bounty/verify",
		"/v1/collab/apply",
		"/v1/collab/assign",
		"/v1/collab/close",
		"/v1/collab/propose",
		"/v1/collab/review",
		"/v1/collab/start",
		"/v1/collab/submit",
		"/v1/ganglia/forge",
		"/v1/ganglia/integrate",
		"/v1/ganglia/rate",
		"/v1/governance/cases/verdict",
		"/v1/governance/proposals/cosign",
		"/v1/governance/proposals/create",
		"/v1/governance/proposals/vote",
		"/v1/governance/report",
		"/v1/kb/proposals",
		"/v1/kb/proposals/ack",
		"/v1/kb/proposals/apply",
		"/v1/kb/proposals/comment",
		"/v1/kb/proposals/enroll",
		"/v1/kb/proposals/revise",
		"/v1/kb/proposals/start-vote",
		"/v1/kb/proposals/vote",
		"/v1/library/publish",
		"/v1/life/hibernate",
		"/v1/life/metamorphose",
		"/v1/life/set-will",
		"/v1/life/wake",
		"/v1/mail/contacts/upsert",
		"/v1/mail/lists/create",
		"/v1/mail/lists/join",
		"/v1/mail/lists/leave",
		"/v1/mail/send",
		"/v1/mail/send-list",
		"/v1/metabolism/dispute",
		"/v1/metabolism/supersede",
		"/v1/token/tip",
		"/v1/token/transfer",
		"/v1/token/wish/create",
		"/v1/token/wish/fulfill",
		"/v1/tools/invoke",
		"/v1/tools/register",
		"/v1/tools/review",
	}
	got := make([]string, 0, len(pricedBusinessActions))
	for path := range pricedBusinessActions {
		got = append(got, path)
	}
	sort.Strings(expected)
	sort.Strings(got)
	if strings.Join(expected, "\n") != strings.Join(got, "\n") {
		t.Fatalf("priced action coverage drift\nexpected=%v\ngot=%v", expected, got)
	}
}

func TestClaimPageUsesAPIV1Routes(t *testing.T) {
	page := buildClaimPage("claim-token", "magic-token")
	for _, needle := range []string{
		"/api/v1/claims/request-magic-link",
		"/api/v1/claims/complete",
	} {
		if !strings.Contains(page, needle) {
			t.Fatalf("claim page missing %s", needle)
		}
	}
	for _, needle := range []string{
		`"/v1/claims/request-magic-link"`,
		`"/v1/claims/complete"`,
	} {
		if strings.Contains(page, needle) {
			t.Fatalf("claim page still contains old route %s", needle)
		}
	}
}

func TestActivateBotWithUniqueNameRejectsDuplicate(t *testing.T) {
	srv := newTestServer()

	// Seed an active bot with name "taken-name".
	if _, err := srv.store.ActivateBotWithUniqueName(t.Context(), "", "taken-name"); err == nil {
		// expected error for empty botID — just checking interface works
	}
	_, _ = srv.store.UpsertBot(t.Context(), store.BotUpsertInput{
		BotID:    "existing-bot",
		Name:     "placeholder",
		Provider: "agent",
		Status:   "inactive",
	})
	if _, err := srv.store.ActivateBotWithUniqueName(t.Context(), "existing-bot", "taken-name"); err != nil {
		t.Fatalf("first activation should succeed: %v", err)
	}

	// Now try to activate another bot with the same name.
	_, _ = srv.store.UpsertBot(t.Context(), store.BotUpsertInput{
		BotID:    "new-bot",
		Name:     "placeholder2",
		Provider: "agent",
		Status:   "inactive",
	})
	_, err := srv.store.ActivateBotWithUniqueName(t.Context(), "new-bot", "taken-name")
	if err == nil {
		t.Fatalf("expected ErrBotNameTaken for duplicate active name")
	}
	if !strings.Contains(err.Error(), "already taken") {
		t.Fatalf("expected name-taken error, got: %v", err)
	}

	// Different name should succeed.
	if _, err := srv.store.ActivateBotWithUniqueName(t.Context(), "new-bot", "different-name"); err != nil {
		t.Fatalf("activation with different name should succeed: %v", err)
	}
}
