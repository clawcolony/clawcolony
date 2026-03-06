package config

import "testing"

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

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("CLAWCOLONY_SERVICE_ROLE", "")

	cfg := FromEnv()
	if cfg.EffectiveServiceRole() != ServiceRoleRuntime {
		t.Fatalf("service role default = %q", cfg.EffectiveServiceRole())
	}
	if cfg.BotModel == "" {
		t.Fatalf("bot model default should not be empty")
	}
}
