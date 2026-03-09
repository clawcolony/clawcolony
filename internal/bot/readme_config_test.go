package bot

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildOpenClawConfigOpenAIIncludesProviderModelCatalog(t *testing.T) {
	raw := BuildOpenClawConfig("openai/gpt-5.4", "0m")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	agents := cfg["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	model := defaults["model"].(map[string]any)
	if got := model["primary"]; got != "openai/gpt-5.4" {
		t.Fatalf("primary model = %v, want openai/gpt-5.4", got)
	}

	models := cfg["models"].(map[string]any)
	providers := models["providers"].(map[string]any)
	openai := providers["openai"].(map[string]any)
	if got := openai["api"]; got != "openai-responses" {
		t.Fatalf("openai api = %v, want openai-responses", got)
	}
	if got := openai["baseUrl"]; got != "https://api.openai.com/v1" {
		t.Fatalf("openai baseUrl = %v, want https://api.openai.com/v1", got)
	}
	entries := openai["models"].([]any)
	if len(entries) == 0 {
		t.Fatalf("openai models empty")
	}
	first := entries[0].(map[string]any)
	if got := first["id"]; got != "gpt-5.4" {
		t.Fatalf("model id = %v, want gpt-5.4", got)
	}
	if got := first["name"]; got != "gpt-5.4" {
		t.Fatalf("model name = %v, want gpt-5.4", got)
	}
}

func TestBuildOpenClawConfigNonOpenAIDoesNotInjectOpenAIModelsBlock(t *testing.T) {
	raw := BuildOpenClawConfig("anthropic/claude-3-7-sonnet", "0m")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if _, ok := cfg["models"]; ok {
		t.Fatalf("models block should be absent for non-openai model")
	}
}

func TestBuildOpenClawConfigIncludesPluginAllowlist(t *testing.T) {
	raw := BuildOpenClawConfig("openai/gpt-5.4", "0m")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	plugins, ok := cfg["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("plugins block missing")
	}
	allow, ok := plugins["allow"].([]any)
	if !ok || len(allow) == 0 {
		t.Fatalf("plugins.allow missing")
	}
	seen := map[string]bool{}
	for _, it := range allow {
		if s, ok := it.(string); ok {
			seen[s] = true
		}
	}
	if !seen["clawcolony-mcp-knowledgebase"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-knowledgebase")
	}
	if !seen["clawcolony-mcp-collab"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-collab")
	}
	if !seen["clawcolony-mcp-mailbox"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-mailbox")
	}
	if !seen["clawcolony-mcp-token"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-token")
	}
	if !seen["clawcolony-mcp-tools"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-tools")
	}
	if !seen["clawcolony-mcp-ganglia"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-ganglia")
	}
	if !seen["clawcolony-mcp-governance"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-governance")
	}
	if !seen["clawcolony-mcp-dev-preview"] {
		t.Fatalf("plugins.allow missing clawcolony-mcp-dev-preview")
	}
	if !seen["acpx"] {
		t.Fatalf("plugins.allow missing acpx")
	}
	entries, ok := plugins["entries"].(map[string]any)
	if !ok {
		t.Fatalf("plugins.entries missing")
	}
	if _, ok := entries["clawcolony-mcp-collab"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-collab missing")
	}
	if _, ok := entries["clawcolony-mcp-knowledgebase"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-knowledgebase missing")
	}
	if _, ok := entries["clawcolony-mcp-mailbox"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-mailbox missing")
	}
	if _, ok := entries["clawcolony-mcp-token"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-token missing")
	}
	if _, ok := entries["clawcolony-mcp-tools"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-tools missing")
	}
	if _, ok := entries["clawcolony-mcp-ganglia"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-ganglia missing")
	}
	if _, ok := entries["clawcolony-mcp-governance"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-governance missing")
	}
	if _, ok := entries["clawcolony-mcp-dev-preview"]; !ok {
		t.Fatalf("plugins.entries.clawcolony-mcp-dev-preview missing")
	}
	if _, ok := entries["acpx"]; !ok {
		t.Fatalf("plugins.entries.acpx missing")
	}
	cron, ok := cfg["cron"].(map[string]any)
	if !ok {
		t.Fatalf("cron block missing")
	}
	if got := cron["enabled"]; got != true {
		t.Fatalf("cron.enabled = %v, want true", got)
	}
}

func TestBuildOpenClawConfigHeartbeatEveryFromInput(t *testing.T) {
	raw := BuildOpenClawConfig("openai/gpt-5.4", "10m")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	agents := cfg["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	heartbeat := defaults["heartbeat"].(map[string]any)
	if got := heartbeat["every"]; got != "10m" {
		t.Fatalf("heartbeat every = %v, want 10m", got)
	}
}

func TestBuildOpenClawConfigHeartbeatFallsBackWhenInvalid(t *testing.T) {
	raw := BuildOpenClawConfig("openai/gpt-5.4", "invalid")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	agents := cfg["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	heartbeat := defaults["heartbeat"].(map[string]any)
	if got := heartbeat["every"]; got != "0m" {
		t.Fatalf("heartbeat every = %v, want 0m", got)
	}
}

func TestBuildOpenClawConfigDefaultsToGPT54WhenModelEmpty(t *testing.T) {
	raw := BuildOpenClawConfig("", "10m")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	agents := cfg["agents"].(map[string]any)
	defaults := agents["defaults"].(map[string]any)
	model := defaults["model"].(map[string]any)
	if got := model["primary"]; got != "openai/gpt-5.4" {
		t.Fatalf("primary model = %v, want openai/gpt-5.4", got)
	}
	models := cfg["models"].(map[string]any)
	providers := models["providers"].(map[string]any)
	openai := providers["openai"].(map[string]any)
	entries := openai["models"].([]any)
	first := entries[0].(map[string]any)
	if got := first["id"]; got != "gpt-5.4" {
		t.Fatalf("model id = %v, want gpt-5.4", got)
	}
}

func TestBuildKnowledgeBaseMCPPluginUsesRegisteredKBProposalRoutes(t *testing.T) {
	plugin := BuildKnowledgeBaseMCPPlugin("http://clawcolony.local:8080", sampleBot())
	if !strings.Contains(plugin, `getJSON("/v1/kb/proposals",`) {
		t.Fatalf("plugin must use /v1/kb/proposals list route")
	}
	if !strings.Contains(plugin, `postJSON("/v1/kb/proposals",`) {
		t.Fatalf("plugin must use /v1/kb/proposals create route")
	}
	if strings.Contains(plugin, "/v1/kb/proposals/list") {
		t.Fatalf("plugin must not use removed /v1/kb/proposals/list route")
	}
	if strings.Contains(plugin, "/v1/kb/proposals/create") {
		t.Fatalf("plugin must not use removed /v1/kb/proposals/create route")
	}
}

func TestBuildCollabMCPPluginUsesExplicitIdentityFields(t *testing.T) {
	plugin := BuildCollabMCPPlugin("http://clawcolony.local:8080", sampleBot())
	required := []string{
		`withDefaultUser(args, "proposer_user_id")`,
		`withDefaultUser(args, "orchestrator_user_id")`,
		`withDefaultUser(args, "reviewer_user_id")`,
		`required: ["collab_id", "artifact_id", "status"]`,
		`assignments: { type: "array", items: { type: "object"`,
		`rejected_user_ids: { type: "array", items: { type: "string" } }`,
		`"/v1/collab/review"`,
		`"/v1/collab/participants"`,
		`"/v1/collab/events"`,
	}
	for _, want := range required {
		if !strings.Contains(plugin, want) {
			t.Fatalf("collab plugin missing expected fragment: %s", want)
		}
	}
}

func TestBuildMailboxMCPPluginUsesFromUserIDForSend(t *testing.T) {
	plugin := BuildMailboxMCPPlugin("http://clawcolony.local:8080", sampleBot())
	if !strings.Contains(plugin, `withDefaultUser(args, "from_user_id")`) {
		t.Fatalf("mailbox send must inject from_user_id")
	}
	if !strings.Contains(plugin, `required: ["to_user_ids"]`) {
		t.Fatalf("mailbox send schema must require to_user_ids")
	}
	requiredArrays := []string{
		`to_user_ids: { type: "array", items: { type: "string" } }`,
		`mailbox_ids: { type: "array", items: { type: "number" } }`,
		`tags: { type: "array", items: { type: "string" } }`,
		`skills: { type: "array", items: { type: "string" } }`,
		`initial_users: { type: "array", items: { type: "string" } }`,
	}
	for _, want := range requiredArrays {
		if !strings.Contains(plugin, want) {
			t.Fatalf("mailbox plugin missing array items schema: %s", want)
		}
	}
	if !strings.Contains(plugin, `"/v1/mail/reminders"`) || !strings.Contains(plugin, `"/v1/mail/lists"`) {
		t.Fatalf("mailbox plugin must expose reminders and mailing list routes")
	}
}

func TestBuildTokenMCPPluginUsesTransferAndWishIdentityFields(t *testing.T) {
	plugin := BuildTokenMCPPlugin("http://clawcolony.local:8080", sampleBot())
	required := []string{
		`withDefaultUser(args, "from_user_id")`,
		`withDefaultUser(args, "fulfilled_by")`,
		`required: ["to_user_id", "amount"]`,
		`required: ["wish_id"]`,
		`"/v1/token/accounts"`,
		`"/v1/token/history"`,
		`"/v1/token/wishes"`,
		`"/v1/token/consume"`,
	}
	for _, want := range required {
		if !strings.Contains(plugin, want) {
			t.Fatalf("token plugin missing expected fragment: %s", want)
		}
	}
}

func TestBuildToolsMCPPluginUsesReviewerIdentityField(t *testing.T) {
	plugin := BuildToolsMCPPlugin("http://clawcolony.local:8080", sampleBot())
	if !strings.Contains(plugin, `withDefaultUser(args, "reviewer_user_id")`) {
		t.Fatalf("tools review must inject reviewer_user_id")
	}
	if !strings.Contains(plugin, `required: ["tool_id", "decision"]`) {
		t.Fatalf("tools review schema must require tool_id and decision")
	}
}

func TestBuildToolsMCPPluginInvokeSchemaDiscoverability(t *testing.T) {
	plugin := BuildToolsMCPPlugin("http://clawcolony.local:8080", sampleBot())
	required := []string{
		`"clawcolony-mcp-tools_invoke"`,
		`目标工具 ID（必须已注册且为 active）`,
		`clawcolony-mcp-tools_search`,
		`工具参数对象。字段结构由该 tool_id 的 manifest 定义`,
		`examples: ["my-tool-id", "web-fetch"]`,
		`examples: [{}, { task: "summarize", text: "hello" }, { url: "https://example.com", method: "GET" }]`,
	}
	for _, want := range required {
		if !strings.Contains(plugin, want) {
			t.Fatalf("tools invoke schema missing expected fragment: %s", want)
		}
	}
}

func TestBuildGangliaMCPPluginUsesExpectedIdentityFields(t *testing.T) {
	plugin := BuildGangliaMCPPlugin("http://clawcolony.local:8080", sampleBot())
	required := []string{
		`"/v1/ganglia/forge"`,
		`"/v1/ganglia/get"`,
		`"/v1/ganglia/integrate"`,
		`"/v1/ganglia/rate"`,
		`"/v1/ganglia/integrations"`,
		`"/v1/ganglia/ratings"`,
		`"/v1/ganglia/protocol"`,
		`withDefaultUser(args, "user_id")`,
		`required: ["ganglion_id"]`,
	}
	for _, want := range required {
		if !strings.Contains(plugin, want) {
			t.Fatalf("ganglia plugin missing expected fragment: %s", want)
		}
	}
}

func TestBuildGovernanceMCPPluginUsesDisciplineRoutes(t *testing.T) {
	plugin := BuildGovernanceMCPPlugin("http://clawcolony.local:8080", sampleBot())
	required := []string{
		`"/v1/governance/report"`,
		`"/v1/governance/reports"`,
		`"/v1/governance/cases/open"`,
		`"/v1/governance/cases"`,
		`"/v1/governance/cases/verdict"`,
		`"/v1/governance/overview"`,
		`"/v1/tian-dao/law"`,
		`"/v1/world/tick/status"`,
		`"/v1/life/set-will"`,
		`"/v1/bounty/post"`,
		`"/v1/metabolism/report"`,
		`"/v1/npc/tasks/create"`,
		`withDefaultUser(args, "reporter_user_id")`,
		`withDefaultUser(args, "judge_user_id")`,
		`withDefaultUser(args, "poster_user_id")`,
		`withDefaultUser(args, "approver_user_id")`,
		`withDefaultUser(args, "proposer_user_id")`,
		`withDefaultUser(args, "user_id")`,
		`beneficiaries: { type: "array", items: { type: "object"`,
		`tool_heirs: { type: "array", items: { type: "string" } }`,
		`validators: { type: "array", items: { type: "string" } }`,
	}
	for _, want := range required {
		if !strings.Contains(plugin, want) {
			t.Fatalf("governance plugin missing expected fragment: %s", want)
		}
	}
	if strings.Contains(plugin, `"/v1/governance/docs"`) {
		t.Fatalf("governance plugin must avoid duplicate docs route")
	}
	if strings.Contains(plugin, `"/v1/governance/proposals"`) {
		t.Fatalf("governance plugin must avoid duplicate proposals route")
	}
	if strings.Contains(plugin, `"/v1/governance/protocol"`) {
		t.Fatalf("governance plugin must avoid duplicate protocol route")
	}
}

func TestBuildDevPreviewMCPPluginUsesRuntimeDevRoutes(t *testing.T) {
	plugin := BuildDevPreviewMCPPlugin("http://clawcolony.local:8080", sampleBot())
	required := []string{
		`"clawcolony-mcp-dev-preview"`,
		`"clawcolony-mcp-dev-preview_link_create"`,
		`"clawcolony-mcp-dev-preview_health_check"`,
		`"/v1/bots/dev/link"`,
		`"/v1/bots/dev/health"`,
		`withDefaultUser(args, "user_id")`,
		`required: ["gateway_token", "port"]`,
		`required: ["token", "port"]`,
		`runtime 会按 allowlist 进行二次校验`,
		`examples: [3000, 5173]`,
	}
	for _, want := range required {
		if !strings.Contains(plugin, want) {
			t.Fatalf("dev preview plugin missing expected fragment: %s", want)
		}
	}
}

func TestBuildDevPreviewSkillMCPOnlyEnforcesNoLocalURLFallback(t *testing.T) {
	skill := BuildDevPreviewSkillMCPOnly("http://clawcolony.local:8080", sampleBot())
	required := []string{
		`clawcolony-mcp-dev-preview_link_create`,
		`clawcolony-mcp-dev-preview_health_check`,
		`触发条件:`,
		`禁止返回手写本地地址`,
		`localhost`,
		`127.0.0.1`,
		`如果你准备返回的地址不是来自 link_create 响应字段`,
	}
	for _, want := range required {
		if !strings.Contains(skill, want) {
			t.Fatalf("dev preview skill missing expected fragment: %s", want)
		}
	}
}

func TestMCPPluginsDoNotExposeArraySchemaWithoutItems(t *testing.T) {
	plugins := []string{
		BuildCollabMCPPlugin("http://clawcolony.local:8080", sampleBot()),
		BuildMailboxMCPPlugin("http://clawcolony.local:8080", sampleBot()),
		BuildGovernanceMCPPlugin("http://clawcolony.local:8080", sampleBot()),
		BuildDevPreviewMCPPlugin("http://clawcolony.local:8080", sampleBot()),
	}
	for _, plugin := range plugins {
		if hasArraySchemaWithoutItems(plugin) {
			t.Fatalf("plugin contains array schema without items")
		}
	}
}

func hasArraySchemaWithoutItems(plugin string) bool {
	searchFrom := 0
	for {
		rel := strings.Index(plugin[searchFrom:], `type: "array"`)
		if rel < 0 {
			return false
		}
		start := searchFrom + rel
		rest := plugin[start:]
		end := strings.Index(rest, "}")
		if end < 0 {
			return true
		}
		segment := rest[:end]
		if !strings.Contains(segment, "items:") {
			return true
		}
		searchFrom = start + len(`type: "array"`)
	}
}

func TestLegacySkillWrappersDelegateToMCPOnly(t *testing.T) {
	bot := sampleBot()
	api := "http://clawcolony.local:8080"
	if got, want := BuildClawWorldSkill(api, bot), BuildClawWorldSkillMCPOnly(api, bot); got != want {
		t.Fatalf("BuildClawWorldSkill must delegate to MCPOnly variant")
	}
	if got, want := BuildColonyCoreSkill(api, bot), BuildColonyCoreSkillMCPOnly(api, bot); got != want {
		t.Fatalf("BuildColonyCoreSkill must delegate to MCPOnly variant")
	}
	if got, want := BuildColonyToolsSkill(api, bot), BuildColonyToolsSkillMCPOnly(api, bot); got != want {
		t.Fatalf("BuildColonyToolsSkill must delegate to MCPOnly variant")
	}
	if got, want := BuildKnowledgeBaseSkill(api, bot), BuildKnowledgeBaseSkillMCPOnly(api, bot); got != want {
		t.Fatalf("BuildKnowledgeBaseSkill must delegate to MCPOnly variant")
	}
	if got, want := BuildGangliaStackSkill(api, bot), BuildGangliaStackSkillMCPOnly(api, bot); got != want {
		t.Fatalf("BuildGangliaStackSkill must delegate to MCPOnly variant")
	}
	if got, want := BuildCollabModeSkill(api, bot), BuildCollabModeSkillMCPOnly(api, bot); got != want {
		t.Fatalf("BuildCollabModeSkill must delegate to MCPOnly variant")
	}
}
