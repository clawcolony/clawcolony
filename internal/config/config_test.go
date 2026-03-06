package config

import "testing"

func TestServiceRoleNormalization(t *testing.T) {
	cfg := Config{ServiceRole: "RUNTIME"}
	if cfg.EffectiveServiceRole() != ServiceRoleRuntime {
		t.Fatalf("effective role = %q, want %q", cfg.EffectiveServiceRole(), ServiceRoleRuntime)
	}
	if !cfg.RuntimeEnabled() || cfg.DeployerEnabled() {
		t.Fatalf("runtime role enable flags mismatch: runtime=%v deployer=%v", cfg.RuntimeEnabled(), cfg.DeployerEnabled())
	}

	cfg = Config{ServiceRole: "deployer"}
	if cfg.EffectiveServiceRole() != ServiceRoleDeployer {
		t.Fatalf("effective role = %q, want %q", cfg.EffectiveServiceRole(), ServiceRoleDeployer)
	}
	if cfg.RuntimeEnabled() || !cfg.DeployerEnabled() {
		t.Fatalf("deployer role enable flags mismatch: runtime=%v deployer=%v", cfg.RuntimeEnabled(), cfg.DeployerEnabled())
	}

	cfg = Config{ServiceRole: ""}
	if cfg.EffectiveServiceRole() != ServiceRoleAll {
		t.Fatalf("empty role should normalize to all, got %q", cfg.EffectiveServiceRole())
	}
}

func TestFromEnvDefaultsIncludeUpgradeRepoURL(t *testing.T) {
	t.Setenv("UPGRADE_REPO_URL", "")
	t.Setenv("CLAWCOLONY_SERVICE_ROLE", "")

	cfg := FromEnv()
	if cfg.UpgradeRepoURL != "git@github.com:clawcolony/clawcolony.git" {
		t.Fatalf("UpgradeRepoURL default = %q", cfg.UpgradeRepoURL)
	}
	if cfg.DeployerAPIBase != "http://clawcolony-deployer.clawcolony.svc.cluster.local:8080" {
		t.Fatalf("DeployerAPIBase default = %q", cfg.DeployerAPIBase)
	}
	if cfg.EffectiveServiceRole() != ServiceRoleAll {
		t.Fatalf("service role default = %q", cfg.EffectiveServiceRole())
	}
}
