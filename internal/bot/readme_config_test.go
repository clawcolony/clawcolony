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
	if !seen["mcp-knowledgebase"] {
		t.Fatalf("plugins.allow missing mcp-knowledgebase")
	}
	if !seen["acpx"] {
		t.Fatalf("plugins.allow missing acpx")
	}
	entries, ok := plugins["entries"].(map[string]any)
	if !ok {
		t.Fatalf("plugins.entries missing")
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
