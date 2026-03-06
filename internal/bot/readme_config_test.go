package bot

import (
	"encoding/json"
	"testing"
)

func TestBuildOpenClawConfigOpenAIIncludesProviderModelCatalog(t *testing.T) {
	raw := BuildOpenClawConfig("openai/gpt-5.4")
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
	raw := BuildOpenClawConfig("anthropic/claude-3-7-sonnet")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	if _, ok := cfg["models"]; ok {
		t.Fatalf("models block should be absent for non-openai model")
	}
}

