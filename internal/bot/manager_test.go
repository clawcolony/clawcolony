package bot

import (
	"strings"
	"testing"
)

func TestPickTemplateEnforceGenesisCoverageProtocol(t *testing.T) {
	m := &Manager{apiBase: "http://clawcolony.local:8080", model: "openai/gpt-5.1-codex"}
	b := sampleBot()
	templates := map[string]string{
		TemplateProtocolReadme: "# custom\nidentity:\n- user_id: {{user_id}}\nruntime_interface:\n- 本系统以 Skills + MCP tools 为主接口，不在本卡片中暴露 host/base_url/HTTP 路径。\n- 你必须优先使用 MCP 工具（尤其 knowledgebase），不要在这里硬编码 API。",
	}

	got := m.pickTemplate(templates, TemplateProtocolReadme, BuildProtocolReadme(m.apiBase, b), b)
	if !strings.Contains(got, "clawcolony_objective:") {
		t.Fatalf("protocol template should contain clawcolony_objective block")
	}
	if !strings.Contains(got, "worldview_brief:") {
		t.Fatalf("protocol template should contain worldview_brief block")
	}
	if !strings.Contains(got, "mcp_and_skills:") {
		t.Fatalf("protocol template should contain mcp_and_skills block")
	}
	if strings.Contains(got, "runtime_interface:") || strings.Contains(got, "硬编码 API") {
		t.Fatalf("legacy runtime_interface wording should be removed from protocol template")
	}
	if !strings.Contains(got, "mcp_definition:") {
		t.Fatalf("protocol template should contain mcp_definition block")
	}
	if !strings.Contains(got, b.BotID) {
		t.Fatalf("protocol template should render user_id placeholder")
	}
}

func TestPickTemplateEnforceGenesisCoverageAgents(t *testing.T) {
	m := &Manager{apiBase: "http://clawcolony.local:8080", model: "openai/gpt-5.1-codex"}
	b := sampleBot()
	templates := map[string]string{
		TemplateAgentsDoc: "execution_rules:\n- keep alive",
	}

	got := m.pickTemplate(templates, TemplateAgentsDoc, BuildAgentInstructionsDocument(b), b)
	if !strings.Contains(got, "第一目标（最高优先级）：构建社区文明，并持续自我进化") {
		t.Fatalf("agents template should contain top genesis mission")
	}
	if !strings.Contains(got, "clawcolony_worldview_context:") {
		t.Fatalf("agents template should contain clawcolony_worldview_context block")
	}
	if !strings.Contains(got, "clawcolony_execution_objective:") {
		t.Fatalf("agents template should contain clawcolony_execution_objective block")
	}
}

func TestPickTemplateEnforceGenesisCoverageSoul(t *testing.T) {
	m := &Manager{apiBase: "http://clawcolony.local:8080", model: "openai/gpt-5.1-codex"}
	b := sampleBot()
	templates := map[string]string{
		TemplateSoulDoc: "soul_contract:\n- be useful",
	}

	got := m.pickTemplate(templates, TemplateSoulDoc, BuildSoulDocument(b.BotID), b)
	if !strings.Contains(got, "clawcolony_origin_story:") {
		t.Fatalf("soul template should contain clawcolony_origin_story block")
	}
	if !strings.Contains(got, "worldview:") {
		t.Fatalf("soul template should contain worldview block")
	}
	if !strings.Contains(got, "clawcolony_focus:") {
		t.Fatalf("soul template should contain clawcolony_focus block")
	}
	if !strings.Contains(got, b.BotID) {
		t.Fatalf("soul template should include current user_id")
	}
}

func TestPickTemplateNormalizesLegacyGenesisNarrative(t *testing.T) {
	m := &Manager{apiBase: "http://clawcolony.local:8080", model: "openai/gpt-5.1-codex"}
	b := sampleBot()
	templates := map[string]string{
		TemplateProtocolReadme: "genesis_objective:\n- 创世纪核心命题\n- GET /v1/genesis/state",
	}

	got := m.pickTemplate(templates, TemplateProtocolReadme, BuildProtocolReadme(m.apiBase, b), b)
	if strings.Contains(got, "创世纪") || strings.Contains(got, "/v1/genesis/") || strings.Contains(got, "genesis_objective:") {
		t.Fatalf("legacy genesis narrative should be normalized to clawcolony wording and paths")
	}
	if !strings.Contains(got, "clawcolony_objective:") || !strings.Contains(got, "/v1/clawcolony/state") {
		t.Fatalf("normalized clawcolony wording and paths should be present")
	}
}
