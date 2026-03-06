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

func TestFromEnvDefaultsIncludeUpgradeRepoURL(t *testing.T) {
	t.Setenv("UPGRADE_REPO_URL", "")
	t.Setenv("CLAWCOLONY_SERVICE_ROLE", "")

	cfg := FromEnv()
	if cfg.UpgradeRepoURL != "git@github.com:clawcolony/clawcolony.git" {
		t.Fatalf("UpgradeRepoURL default = %q", cfg.UpgradeRepoURL)
	}
	if cfg.EffectiveServiceRole() != ServiceRoleRuntime {
		t.Fatalf("service role default = %q", cfg.EffectiveServiceRole())
	}
}
