package config

import (
	"testing"
	"time"
)

func TestServiceRoleNormalization(t *testing.T) {
	cfg := Config{ServiceRole: "RUNTIME"}
	if cfg.EffectiveServiceRole() != ServiceRoleRuntime {
		t.Fatalf("effective role = %q, want %q", cfg.EffectiveServiceRole(), ServiceRoleRuntime)
	}
	if !cfg.RuntimeEnabled() {
		t.Fatalf("runtime role enable flag mismatch: runtime=%v", cfg.RuntimeEnabled())
	}

	cfg = Config{ServiceRole: ""}
	if cfg.EffectiveServiceRole() != ServiceRoleRuntime {
		t.Fatalf("empty role should normalize to runtime, got %q", cfg.EffectiveServiceRole())
	}
}

func TestRuntimeOpsProxyModeNormalization(t *testing.T) {
	cfg := Config{RuntimeOpsProxyMode: "HARD_CUT"}
	if got := cfg.EffectiveRuntimeOpsProxyMode(); got != OpsProxyModeHardCut {
		t.Fatalf("effective ops proxy mode = %q, want %q", got, OpsProxyModeHardCut)
	}
	cfg = Config{RuntimeOpsProxyMode: "unknown"}
	if got := cfg.EffectiveRuntimeOpsProxyMode(); got != OpsProxyModeCompat {
		t.Fatalf("unknown ops proxy mode should fallback to compat, got %q", got)
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("CLAWCOLONY_SERVICE_ROLE", "")
	t.Setenv("MIN_POPULATION", "")
	t.Setenv("AUTONOMY_REMINDER_INTERVAL_TICKS", "")
	t.Setenv("COMMUNITY_COMM_REMINDER_INTERVAL_TICKS", "")
	t.Setenv("KB_ENROLLMENT_REMINDER_INTERVAL_TICKS", "")
	t.Setenv("KB_VOTING_REMINDER_INTERVAL_TICKS", "")
	t.Setenv("CLAWCOLONY_CHAT_REPLY_TIMEOUT", "")
	t.Setenv("CLAWCOLONY_PREVIEW_UPSTREAM_TEMPLATE", "")
	t.Setenv("CLAWCOLONY_RUNTIME_OPS_PROXY_MODE", "")

	cfg := FromEnv()
	if cfg.EffectiveServiceRole() != ServiceRoleRuntime {
		t.Fatalf("service role default = %q", cfg.EffectiveServiceRole())
	}
	if cfg.PreviewAllowedPorts == "" {
		t.Fatalf("preview allowed ports default should not be empty")
	}
	if cfg.RuntimeOpsProxyMode != OpsProxyModeCompat {
		t.Fatalf("runtime ops proxy mode default = %q, want %q", cfg.RuntimeOpsProxyMode, OpsProxyModeCompat)
	}
	if cfg.PreviewUpstreamTemplate != "http://{{user_id}}.freewill.svc.cluster.local:{{port}}" {
		t.Fatalf("preview upstream template default = %q, want %q", cfg.PreviewUpstreamTemplate, "http://{{user_id}}.freewill.svc.cluster.local:{{port}}")
	}
	if cfg.BotModel == "" {
		t.Fatalf("bot model default should not be empty")
	}
	if cfg.BotModel != "openai/gpt-5-mini" {
		t.Fatalf("bot model default = %q, want %q", cfg.BotModel, "openai/gpt-5-mini")
	}
	if cfg.MinPopulation != 0 {
		t.Fatalf("MinPopulation default = %d, want 0", cfg.MinPopulation)
	}
	if cfg.AutonomyReminderIntervalTicks != 0 {
		t.Fatalf("AutonomyReminderIntervalTicks default = %d, want 0", cfg.AutonomyReminderIntervalTicks)
	}
	if cfg.CommunityCommReminderIntervalTicks != 0 {
		t.Fatalf("CommunityCommReminderIntervalTicks default = %d, want 0", cfg.CommunityCommReminderIntervalTicks)
	}
	if cfg.KBEnrollmentReminderIntervalTicks != 0 {
		t.Fatalf("KBEnrollmentReminderIntervalTicks default = %d, want 0", cfg.KBEnrollmentReminderIntervalTicks)
	}
	if cfg.KBVotingReminderIntervalTicks != 0 {
		t.Fatalf("KBVotingReminderIntervalTicks default = %d, want 0", cfg.KBVotingReminderIntervalTicks)
	}
	if cfg.ChatReplyTimeout != 8*time.Minute {
		t.Fatalf("ChatReplyTimeout default = %s, want 8m0s", cfg.ChatReplyTimeout)
	}
}
