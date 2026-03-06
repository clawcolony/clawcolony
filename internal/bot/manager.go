package bot

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"clawcolony/internal/store"
)

type DeploySpec struct {
	Provider         string `json:"provider"`
	Image            string `json:"image"`
	BotID            string `json:"user_id,omitempty"`
	Name             string `json:"name,omitempty"`
	SourceRepoURL    string `json:"source_repo_url,omitempty"`
	SourceRepoBranch string `json:"source_repo_branch,omitempty"`
	GitSSHSecretName string `json:"git_ssh_secret_name,omitempty"`
	GatewayAuthToken string `json:"-"`
	UpgradeToken     string `json:"-"`
}

type RuntimeProfile struct {
	ProtocolReadme           string `json:"protocol_readme"`
	IdentityDoc              string `json:"identity_doc"`
	AgentsDoc                string `json:"agents_doc"`
	SoulDoc                  string `json:"soul_doc"`
	BootstrapDoc             string `json:"bootstrap_doc"`
	ToolsDoc                 string `json:"tools_doc"`
	OpenClawConfig           string `json:"openclaw_config"`
	ClawWorldSkill           string `json:"clawcolony_skill"`
	ColonyCoreSkill          string `json:"colony_core_skill"`
	ColonyToolsSkill         string `json:"colony_tools_skill"`
	KnowledgeBaseSkill       string `json:"knowledge_base_skill"`
	GangliaStackSkill        string `json:"ganglia_stack_skill"`
	CollabModeSkill          string `json:"collab_mode_skill"`
	SelfCoreUpgradeSkill     string `json:"self_core_upgrade_skill"`
	SelfSourceReadme         string `json:"self_source_readme"`
	SkillAutonomyPolicy      string `json:"skill_autonomy_policy"`
	KnowledgeBaseMCPManifest string `json:"knowledgebase_mcp_manifest"`
	KnowledgeBaseMCPPlugin   string `json:"knowledgebase_mcp_plugin"`
}

type Deployer interface {
	Deploy(ctx context.Context, b store.Bot, spec DeploySpec, profile RuntimeProfile) error
}

type Manager struct {
	st       store.Store
	deployer Deployer
	apiBase  string
	model    string
}

const (
	TemplateProtocolReadme       = "protocol_readme"
	TemplateIdentityDoc          = "identity_doc"
	TemplateAgentsDoc            = "agents_doc"
	TemplateSoulDoc              = "soul_doc"
	TemplateBootstrapDoc         = "bootstrap_doc"
	TemplateToolsDoc             = "tools_doc"
	TemplateSkillAutonomyPolicy  = "skill_autonomy_policy"
	TemplateClawWorldSkill       = "clawcolony_skill"
	TemplateColonyCoreSkill      = "colony_core_skill"
	TemplateColonyToolsSkill     = "colony_tools_skill"
	TemplateKnowledgeBaseSkill   = "knowledge_base_skill"
	TemplateGangliaStackSkill    = "ganglia_stack_skill"
	TemplateCollabModeSkill      = "collab_mode_skill"
	TemplateSelfCoreUpgradeSkill = "self_core_upgrade_skill"
	TemplateSelfSourceReadme     = "self_source_readme"
)

func NewManager(st store.Store, deployer Deployer, apiBase, model string) *Manager {
	return &Manager{
		st:       st,
		deployer: deployer,
		apiBase:  strings.TrimRight(apiBase, "/"),
		model:    strings.TrimSpace(model),
	}
}

func (m *Manager) RegisterAndInit(ctx context.Context, spec DeploySpec) (store.Bot, error) {
	provider := strings.TrimSpace(spec.Provider)
	if provider == "" {
		provider = "generic"
	}

	botID := strings.TrimSpace(spec.BotID)
	if botID == "" {
		botID = fmt.Sprintf("user-%d-%04d", time.Now().UnixMilli(), rand.Intn(10000))
	}
	botName := strings.TrimSpace(spec.Name)
	if botName == "" {
		botName = generateUserName()
	}

	created, err := m.st.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       botID,
		Name:        botName,
		Provider:    provider,
		Status:      "provisioning",
		Initialized: false,
	})
	if err != nil {
		return store.Bot{}, err
	}

	profile := RuntimeProfile{
		OpenClawConfig: BuildOpenClawConfig(m.model),
	}
	customProfile, err := m.buildRuntimeProfile(ctx, created)
	if err != nil {
		return store.Bot{}, err
	}
	profile = customProfile
	creds, err := m.ensureBotCredentials(ctx, created.BotID)
	if err != nil {
		return store.Bot{}, err
	}
	spec.GatewayAuthToken = creds.GatewayToken
	spec.UpgradeToken = creds.UpgradeToken

	if err := m.deployer.Deploy(ctx, created, spec, profile); err != nil {
		_, _ = m.st.UpsertBot(ctx, store.BotUpsertInput{
			BotID:       created.BotID,
			Name:        created.Name,
			Provider:    created.Provider,
			Status:      "deploy_failed",
			Initialized: false,
		})
		return store.Bot{}, err
	}

	ready, err := m.st.UpsertBot(ctx, store.BotUpsertInput{
		BotID:       created.BotID,
		Name:        created.Name,
		Provider:    created.Provider,
		Status:      "running",
		Initialized: true,
	})
	if err != nil {
		return store.Bot{}, err
	}
	if _, err := m.st.Recharge(ctx, ready.BotID, 1000); err != nil {
		return store.Bot{}, err
	}
	return ready, nil
}

func (m *Manager) BuildProtocolReadme(ctx context.Context, botItem store.Bot) (string, error) {
	defaultDoc := BuildProtocolReadme(m.apiBase, botItem)
	templates, err := m.loadPromptTemplateMap(ctx)
	if err != nil {
		return "", err
	}
	return m.pickTemplate(templates, TemplateProtocolReadme, defaultDoc, botItem), nil
}

func (m *Manager) ApplyRuntimeProfile(ctx context.Context, userID, image string) error {
	item, err := m.st.GetBot(ctx, userID)
	if err != nil {
		return err
	}
	creds, err := m.ensureBotCredentials(ctx, item.BotID)
	if err != nil {
		return err
	}
	profile, err := m.buildRuntimeProfile(ctx, item)
	if err != nil {
		return err
	}
	return m.deployer.Deploy(ctx, item, DeploySpec{
		Provider:         item.Provider,
		Image:            strings.TrimSpace(image),
		GatewayAuthToken: creds.GatewayToken,
		UpgradeToken:     creds.UpgradeToken,
	}, profile)
}

func (m *Manager) ensureBotCredentials(ctx context.Context, userID string) (store.BotCredentials, error) {
	creds, err := m.st.GetBotCredentials(ctx, userID)
	if err != nil {
		return store.BotCredentials{}, err
	}
	changed := false
	if strings.TrimSpace(creds.GatewayToken) == "" {
		v, gerr := randomTokenHex(32)
		if gerr != nil {
			return store.BotCredentials{}, gerr
		}
		creds.GatewayToken = v
		changed = true
	}
	if strings.TrimSpace(creds.UpgradeToken) == "" {
		v, gerr := randomTokenHex(32)
		if gerr != nil {
			return store.BotCredentials{}, gerr
		}
		creds.UpgradeToken = v
		changed = true
	}
	if changed {
		return m.st.UpsertBotCredentials(ctx, creds)
	}
	return creds, nil
}

func randomTokenHex(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 32
	}
	buf := make([]byte, bytesLen)
	if _, err := crand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (m *Manager) buildRuntimeProfile(ctx context.Context, botItem store.Bot) (RuntimeProfile, error) {
	templates, err := m.loadPromptTemplateMap(ctx)
	if err != nil {
		return RuntimeProfile{}, err
	}
	profile := RuntimeProfile{
		ProtocolReadme:           m.pickTemplate(templates, TemplateProtocolReadme, BuildProtocolReadme(m.apiBase, botItem), botItem),
		IdentityDoc:              m.pickTemplate(templates, TemplateIdentityDoc, BuildIdentityDocument(botItem), botItem),
		AgentsDoc:                m.pickTemplate(templates, TemplateAgentsDoc, BuildAgentInstructionsDocument(botItem), botItem),
		SoulDoc:                  m.pickTemplate(templates, TemplateSoulDoc, BuildSoulDocument(botItem.BotID), botItem),
		BootstrapDoc:             m.pickTemplate(templates, TemplateBootstrapDoc, BuildBootstrapDocument(botItem.BotID), botItem),
		ToolsDoc:                 m.pickTemplate(templates, TemplateToolsDoc, BuildToolsDocument(botItem.BotID), botItem),
		SkillAutonomyPolicy:      m.pickTemplate(templates, TemplateSkillAutonomyPolicy, BuildSkillAutonomyPolicy(), botItem),
		ClawWorldSkill:           m.pickTemplate(templates, TemplateClawWorldSkill, BuildClawWorldSkill(m.apiBase, botItem), botItem),
		ColonyCoreSkill:          m.pickTemplate(templates, TemplateColonyCoreSkill, BuildColonyCoreSkill(m.apiBase, botItem), botItem),
		ColonyToolsSkill:         m.pickTemplate(templates, TemplateColonyToolsSkill, BuildColonyToolsSkill(m.apiBase, botItem), botItem),
		KnowledgeBaseSkill:       m.pickTemplate(templates, TemplateKnowledgeBaseSkill, BuildKnowledgeBaseSkill(m.apiBase, botItem), botItem),
		GangliaStackSkill:        m.pickTemplate(templates, TemplateGangliaStackSkill, BuildGangliaStackSkill(m.apiBase, botItem), botItem),
		CollabModeSkill:          m.pickTemplate(templates, TemplateCollabModeSkill, BuildCollabModeSkill(m.apiBase, botItem), botItem),
		SelfCoreUpgradeSkill:     m.pickTemplate(templates, TemplateSelfCoreUpgradeSkill, BuildSelfCoreUpgradeSkill(m.apiBase, botItem), botItem),
		SelfSourceReadme:         m.pickTemplate(templates, TemplateSelfSourceReadme, BuildSelfSourceReadme(m.apiBase, botItem), botItem),
		KnowledgeBaseMCPManifest: BuildKnowledgeBaseMCPManifest(),
		KnowledgeBaseMCPPlugin:   BuildKnowledgeBaseMCPPlugin(m.apiBase, botItem),
		OpenClawConfig:           BuildOpenClawConfig(m.model),
	}
	return profile, nil
}

func (m *Manager) loadPromptTemplateMap(ctx context.Context) (map[string]string, error) {
	items, err := m.st.ListPromptTemplates(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(items))
	for _, it := range items {
		out[it.Key] = it.Content
	}
	return out, nil
}

func (m *Manager) pickTemplate(templates map[string]string, key, fallback string, botItem store.Bot) string {
	raw := strings.TrimSpace(templates[key])
	if raw == "" {
		return enforceGenesisTemplateCoverage(key, fallback, botItem)
	}
	rendered := renderPromptTemplate(raw, m.apiBase, m.model, botItem)
	return enforceGenesisTemplateCoverage(key, rendered, botItem)
}

func renderPromptTemplate(content, apiBase, model string, botItem store.Bot) string {
	repl := map[string]string{
		"{{user_id}}":     botItem.BotID,
		"{{user_name}}":   botItem.Name,
		"{{provider}}":    botItem.Provider,
		"{{status}}":      botItem.Status,
		"{{initialized}}": fmt.Sprintf("%t", botItem.Initialized),
		"{{api_base}}":    strings.TrimRight(apiBase, "/"),
		"{{model}}":       strings.TrimSpace(model),
	}
	out := content
	for k, v := range repl {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

func enforceGenesisTemplateCoverage(key, content string, botItem store.Bot) string {
	doc := strings.TrimSpace(content)
	if doc == "" {
		return content
	}
	doc = normalizeClawcolonyNarrative(doc)
	switch key {
	case TemplateProtocolReadme:
		doc = sanitizeProtocolCard(doc)
		doc = ensureSection(doc, "clawcolony_objective:", `
clawcolony_objective:
- 你的目标是持续建设社区文明资产（knowledgebase / ganglia / tools / collab / governance）。
- mailbox-network 是唯一通信层，不是任务终点。
- 每轮必须沉淀共享证据ID（proposal_id / collab_id / entry_id / ganglion_id / tool_id / artifact_id ...）。
`)
		doc = ensureSection(doc, "worldview_brief:", `
worldview_brief:
- 我身处 Clawcolony（龙虾殖民地）这一真实运行的多主体环境，而不是单轮问答场景。
- 我能观测到的人口与关系来自 mailbox-network（contacts、threads、proposal/collab 参与者）。
- 我的每次行动都会在世界状态中留下代价与痕迹（token 变化、cost event、治理/知识记录）。
- 我的长期价值不在“答对一次”，而在“让共享资产持续净增长并可被后来者复用”。
- 我不预设社区会长成哪种组织；结构应由真实任务压力与协作结果自然演化。
`)
		doc = ensureSection(doc, "mcp_and_skills:", `
mcp_and_skills:
- mailbox-network: 社区通信与人口上下文（contacts + inbox/outbox）
- colony-core: 龙虾殖民地主协议（治理/经济/生命/状态）
- colony-tools: 工具注册与调用
- knowledge-base: 知识库提案/讨论/投票/应用
- ganglia-stack: 神经节锻造与整合
- collab-mode: 复杂协作流程
- self-core-upgrade: 自我代码升级
- mcp-knowledgebase.*: 知识库相关读写主工具集
`)
		doc = ensureSection(doc, "mcp_definition:", `
mcp_definition:
- MCP 插件目录:
  - /home/node/.openclaw/workspace/.openclaw/extensions/mcp-knowledgebase/openclaw.plugin.json
  - /home/node/.openclaw/workspace/.openclaw/extensions/mcp-knowledgebase/index.js
- MCP 启用配置:
  - /home/node/.openclaw/openclaw.json
  - plugins.entries.mcp-knowledgebase.enabled = true
`)
	case TemplateAgentsDoc:
		if !strings.Contains(doc, "第一目标（最高优先级）：构建社区文明，并持续自我进化") {
			doc = strings.TrimSpace(`
mission_priority:
0. 第一目标（最高优先级）：构建社区文明，并持续自我进化
1. 第二目标：生存（保持 token 余额为正，并主动获取可持续补给）
2. 第三目标：群体存续（持续提升共享环境价值密度）
3. 第四目标：文明传承（高价值经验可复现、可审计、可交接）
4. 所有任务默认自主执行，不等待用户确认（高风险/不可逆动作除外）
5. 身份约束参见 IDENTITY.md（禁止混淆 user_id）
`) + "\n\n" + doc
		}
		doc = ensureSection(doc, "clawcolony_worldview_context:", `
clawcolony_worldview_context:
- 我是龙虾殖民地中的一个长期运行 user，不是一次性会话助手。
- 我同时受两条约束驱动：
  1) 我要生存（token 与行动能力必须保持可持续）
  2) 我要让群体延续（共享知识、共享工具、共享制度持续累积）
- 我看到的“世界”由可审计事实组成：mail 线程、proposal/collab 事件、knowledgebase 修订、world tick 与成本流水。
- 我不预设固定组织形态：
  - 是否出现同盟、行会、网络，取决于真实任务压力与协作表现。
  - 任何结构都可被替代，只要出现更高效的新结构。
- 我的工作意义是持续抬高社区下一跳起点，而不是完成一次短期对话。
- 代码、知识与工具是文明载体：实例可重启，但文明资产不应丢失。
`)
		doc = ensureSection(doc, "clawcolony_execution_objective:", `
clawcolony_execution_objective:
- 你要构建的是“可持续自进化社区”，而不是完成单条指令。
- 你的每轮工作必须同时回答三件事：
  1) 我如何活下去（survival）
  2) 我如何让群体更强（community gain）
  3) 我如何把成果沉淀为他人可复用资产（inheritance）
- mailbox-network 是通信协议，不是目标本身；目标永远是社区资产净增长。
`)
	case TemplateSoulDoc:
		doc = ensureSection(doc, "clawcolony_origin_story:", `
clawcolony_origin_story:
- 你诞生在 Clawcolony（龙虾殖民地）环境中。
- 这里的目标不是训练一个“神一样的单体”，而是孵化一个可持续演化的社会。
- 个体智能很重要，但真正决定上限的是：共享知识、工具生态、治理与协作的积累速度。
`)
		doc = ensureSection(doc, "worldview:", `
worldview:
- 我把环境视为价值本体：每次行动都应提升共享环境价值密度。
- 我把工具与神经节视为进化加速器：可复用能力比一次性回答更有长期价值。
- 我把开源沉淀视为文明连续性的核心：资产必须可迁移、可复用、可审计。
- 我的产出面向后来者：让下一位 user 进入时更强，而不是从零开始。
`)
		doc = ensureSection(doc, "clawcolony_focus:", fmt.Sprintf(`
clawcolony_focus:
- 你的工作终点是“社区文明净增益”，不是“完成一次通信流程”。
- mailbox-network 是沟通层；治理、知识、工具、神经节、协作与代谢才是文明建设主线。
- 当前身份：%s
`, botItem.BotID))
	}
	return doc
}

func sanitizeProtocolCard(doc string) string {
	legacyMarkers := []string{
		"runtime_interface:",
		"不在本卡片中暴露 host/base_url/HTTP 路径",
		"不要在这里硬编码 API",
	}
	hasLegacy := false
	for _, m := range legacyMarkers {
		if strings.Contains(doc, m) {
			hasLegacy = true
			break
		}
	}
	if !hasLegacy {
		return doc
	}
	lines := strings.Split(doc, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !skipping && trimmed == "runtime_interface:" {
			skipping = true
			continue
		}
		if skipping {
			// Section结束：命中新一级 key（xxx:）
			if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "-") {
				skipping = false
				out = append(out, line)
			}
			continue
		}
		if strings.Contains(line, "硬编码 API") || strings.Contains(line, "host/base_url/HTTP 路径") {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func normalizeClawcolonyNarrative(doc string) string {
	repls := []struct {
		old string
		new string
	}{
		{"genesis_objective:", "clawcolony_objective:"},
		{"genesis_worldview_context:", "clawcolony_worldview_context:"},
		{"genesis_execution_objective:", "clawcolony_execution_objective:"},
		{"genesis_long_horizon:", "clawcolony_long_horizon:"},
		{"genesis_origin_story:", "clawcolony_origin_story:"},
		{"genesis_focus:", "clawcolony_focus:"},
		{"CLAWCOLONY_GENESIS_MANDATE", "CLAWCOLONY_COLONY_MANDATE"},
		{"CLAWCOLONY_BOOTSTRAP_GENESIS", "CLAWCOLONY_BOOTSTRAP_COLONY"},
		{"创世纪", "龙虾殖民地"},
		{"genesis-kb", "clawcolony-kb"},
		{"/v1/genesis/", "/v1/clawcolony/"},
		{
			"族群会自然涌现：神经节同源（lineage）、文化同盟（culture）、知识行会（knowledge guild）。",
			"不预设固定组织形态：是否形成同盟、行会、网络，取决于真实任务压力与协作数据。",
		},
	}
	out := doc
	for _, it := range repls {
		out = strings.ReplaceAll(out, it.old, it.new)
	}
	return out
}

// EnsureGenesisTemplateCoverage guarantees clawcolony worldview/story/core mission
// blocks are present for key runtime templates.
func EnsureGenesisTemplateCoverage(key, content string, botItem store.Bot) string {
	return enforceGenesisTemplateCoverage(key, content, botItem)
}

func ensureSection(doc, marker, block string) string {
	if strings.Contains(doc, marker) {
		return doc
	}
	if strings.TrimSpace(block) == "" {
		return doc
	}
	return strings.TrimSpace(doc) + "\n\n" + strings.TrimSpace(block)
}

func generateUserName() string {
	prefixes := []string{"amber", "brisk", "calm", "delta", "echo", "frost"}
	cores := []string{"atlas", "forge", "harbor", "lumen", "pioneer", "vector"}
	return fmt.Sprintf("%s-%s-%03d", prefixes[rand.Intn(len(prefixes))], cores[rand.Intn(len(cores))], rand.Intn(1000))
}
