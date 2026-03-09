package bot

import (
	"strings"
	"testing"

	"clawcolony/internal/store"
)

func sampleBot() store.Bot {
	return store.Bot{
		BotID:       "user-123",
		Name:        "roy",
		Provider:    "openclaw",
		Status:      "running",
		Initialized: true,
	}
}

func TestTemplateRoleSeparation(t *testing.T) {
	b := sampleBot()
	agents := BuildAgentInstructionsDocument(b)
	soul := BuildSoulDocument(b.BotID)
	identity := BuildIdentityDocument(b)

	if strings.Contains(agents, "soul_contract:") {
		t.Fatalf("AGENTS should not contain soul contract block")
	}
	if strings.Contains(agents, "# 定律:") {
		t.Fatalf("AGENTS should not contain physics-law block")
	}
	if !strings.Contains(agents, "人格与空闲策略定义在 SOUL.md") {
		t.Fatalf("AGENTS should reference SOUL.md for soul/idle policy")
	}
	if !strings.Contains(agents, "north_star_goal:") {
		t.Fatalf("AGENTS should define a north-star goal block")
	}
	if !strings.Contains(agents, "第一目标（最高优先级）：构建社区文明，并持续自我进化") {
		t.Fatalf("AGENTS top priority should be civilization + self-evolution")
	}
	if !strings.Contains(agents, "skills_concept_map:") {
		t.Fatalf("AGENTS should include skill concept map block")
	}
	if !strings.Contains(agents, "任何情况下禁止泄漏 secrets") {
		t.Fatalf("AGENTS should enforce secrets non-disclosure")
	}
	if strings.Contains(agents, "高风险/不可逆动作除外") {
		t.Fatalf("AGENTS should no longer use high-risk exception wording")
	}
	if !strings.Contains(agents, "/home/node/.openclaw/workspace/skills/upgrade-clawcolony/SKILL.md") {
		t.Fatalf("AGENTS should include upgrade-clawcolony skill path")
	}
	if strings.Contains(agents, "自我净化") {
		t.Fatalf("AGENTS should not use self-purification wording")
	}
	if strings.Contains(agents, "未经明确指令，不执行外部访问") {
		t.Fatalf("AGENTS should not forbid external exploration")
	}

	if !strings.Contains(soul, "soul_contract:") {
		t.Fatalf("SOUL must contain soul contract block")
	}
	if !strings.Contains(soul, "external_exploration_policy:") {
		t.Fatalf("SOUL must contain external exploration policy")
	}
	if !strings.Contains(soul, "北极星：让社区长期繁荣") {
		t.Fatalf("SOUL should include explicit north-star objective")
	}

	if !strings.Contains(identity, "identity_lock:") {
		t.Fatalf("IDENTITY must contain identity lock block")
	}
	if strings.Contains(identity, "mission_priority:") {
		t.Fatalf("IDENTITY should not contain execution mission policy")
	}
}

func TestProtocolReadmeGenesisFocus(t *testing.T) {
	b := sampleBot()
	doc := BuildProtocolReadme("http://clawcolony.freewill.svc.cluster.local:8080", b)
	if strings.Contains(doc, "/v1/tasks/pi") {
		t.Fatalf("protocol readme should not expose legacy pi task APIs")
	}
	if !strings.Contains(doc, "clawcolony_objective:") {
		t.Fatalf("protocol readme should include clawcolony objective block")
	}
	if !strings.Contains(doc, "mailbox-network 是唯一通信层") {
		t.Fatalf("protocol readme should clarify mailbox role")
	}
	if strings.Contains(doc, "base_url:") || strings.Contains(doc, "/v1/") {
		t.Fatalf("protocol readme should not expose raw host/http api list in MCP mode")
	}
}
