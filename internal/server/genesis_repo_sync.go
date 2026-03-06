package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const redactedSecret = "***REDACTED***"

func (s *Server) syncColonyRepoSnapshot(ctx context.Context, tickID int64) error {
	if !s.cfg.ColonyRepoSync {
		return nil
	}
	root := strings.TrimSpace(s.cfg.ColonyRepoLocalPath)
	if root == "" {
		return fmt.Errorf("COLONY_REPO_LOCAL_PATH is required when COLONY_REPO_SYNC_ENABLED=true")
	}
	branch := strings.TrimSpace(s.cfg.ColonyRepoBranch)
	if branch == "" {
		branch = "main"
	}
	repoURL := strings.TrimSpace(s.cfg.ColonyRepoURL)

	files, err := s.buildColonyRepoSnapshotFiles(ctx, tickID)
	if err != nil {
		return err
	}
	if err := s.writeColonyRepoSnapshot(root, files); err != nil {
		return err
	}

	// If git is not available, keep filesystem snapshot only.
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	if err := s.ensureColonyRepoWorktree(ctx, root, repoURL, branch); err != nil {
		return err
	}
	if _, err := s.runCmd(ctx, root, nil, "git", "add", "-A"); err != nil {
		return err
	}
	changed, err := s.runCmd(ctx, root, nil, "git", "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if strings.TrimSpace(changed) == "" {
		return nil
	}
	msg := fmt.Sprintf("chore(colony): sync tick %d (%s)", tickID, time.Now().UTC().Format(time.RFC3339))
	if _, err := s.runCmd(ctx, root, nil, "git", "-c", "user.name=clawcolony-admin", "-c", "user.email=clawcolony-admin@clawcolony.ai", "commit", "-m", msg); err != nil {
		return err
	}
	if repoURL != "" {
		if _, err := s.runCmd(ctx, root, nil, "git", "push", "-u", "origin", branch); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) ensureColonyRepoWorktree(ctx context.Context, root, repoURL, branch string) error {
	gitDir := filepath.Join(root, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		_, _ = s.runCmd(ctx, root, nil, "git", "checkout", "-B", branch)
		if repoURL == "" {
			return nil
		}
		if _, err := s.runCmd(ctx, root, nil, "git", "remote", "set-url", "origin", repoURL); err != nil {
			_, _ = s.runCmd(ctx, root, nil, "git", "remote", "add", "origin", repoURL)
		}
		_, _ = s.runCmd(ctx, root, nil, "git", "fetch", "origin", branch, "--depth", "1")
		return nil
	}
	if repoURL != "" {
		_ = os.RemoveAll(root)
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			return err
		}
		if _, err := s.runCmd(ctx, "", nil, "git", "clone", "--depth", "1", "--branch", branch, repoURL, root); err != nil {
			if _, err2 := s.runCmd(ctx, "", nil, "git", "clone", "--depth", "1", repoURL, root); err2 != nil {
				return err
			}
			_, _ = s.runCmd(ctx, root, nil, "git", "checkout", "-B", branch)
		}
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if _, err := s.runCmd(ctx, root, nil, "git", "init"); err != nil {
		return err
	}
	if _, err := s.runCmd(ctx, root, nil, "git", "checkout", "-B", branch); err != nil {
		return err
	}
	return nil
}

func (s *Server) writeColonyRepoSnapshot(root string, files map[string]any) error {
	base := filepath.Join(root, "civilization")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	for rel, data := range files {
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		var raw []byte
		switch v := data.(type) {
		case string:
			raw = []byte(v)
			if !strings.HasSuffix(v, "\n") {
				raw = append(raw, '\n')
			}
		default:
			normalized, err := normalizeJSONAny(v)
			if err != nil {
				return err
			}
			sanitized := sanitizeSnapshotAny("", normalized)
			b, err := json.MarshalIndent(sanitized, "", "  ")
			if err != nil {
				return err
			}
			raw = append(b, '\n')
		}
		if err := os.WriteFile(target, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func normalizeJSONAny(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func isSensitiveField(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	switch {
	case strings.Contains(k, "password"),
		strings.Contains(k, "secret"),
		strings.Contains(k, "private_key"),
		strings.Contains(k, "credential"),
		strings.Contains(k, "gateway_token"),
		strings.Contains(k, "upgrade_token"),
		strings.Contains(k, "api_key"),
		strings.Contains(k, "ssh_key"),
		strings.Contains(k, "auth_token"):
		return true
	case strings.Contains(k, "token"):
		// Keep economics fields visible.
		if strings.Contains(k, "balance") || strings.Contains(k, "amount") || strings.Contains(k, "split") || strings.Contains(k, "threshold") || strings.Contains(k, "total_token") {
			return false
		}
		return true
	default:
		return false
	}
}

func looksLikeSecretValue(s string) bool {
	v := strings.TrimSpace(strings.ToLower(s))
	if v == "" {
		return false
	}
	if strings.HasPrefix(v, "sk-") || strings.HasPrefix(v, "ghp_") || strings.HasPrefix(v, "gho_") || strings.HasPrefix(v, "glpat-") {
		return true
	}
	if strings.Contains(v, "sk-proj-") || strings.Contains(v, "ghp_") || strings.Contains(v, "gho_") || strings.Contains(v, "glpat-") {
		return true
	}
	if strings.Contains(v, "-----begin openssh private key-----") || strings.Contains(v, "-----begin rsa private key-----") || strings.Contains(v, "authorization: bearer ") {
		return true
	}
	return false
}

func sanitizeSnapshotAny(parentKey string, v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			if isSensitiveField(k) {
				switch sv := vv.(type) {
				case float64, bool, nil:
					out[k] = sv
				default:
					out[k] = redactedSecret
				}
				continue
			}
			out[k] = sanitizeSnapshotAny(k, vv)
		}
		return out
	case []any:
		out := make([]any, 0, len(t))
		for _, vv := range t {
			out = append(out, sanitizeSnapshotAny(parentKey, vv))
		}
		return out
	case string:
		if isSensitiveField(parentKey) || looksLikeSecretValue(t) {
			return redactedSecret
		}
		return t
	default:
		return v
	}
}

func (s *Server) buildColonyRepoSnapshotFiles(ctx context.Context, tickID int64) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	files := map[string]any{
		"civilization/README.md": fmt.Sprintf(
			"# Clawcolony Civilization Snapshot\n\nGenerated at: %s\nTick: %d\n\nThis directory is generated by Clawcolony repo sync.\n",
			time.Now().UTC().Format(time.RFC3339), tickID,
		),
	}

	lawKey := strings.TrimSpace(s.cfg.TianDaoLawKey)
	if lawKey == "" {
		lawKey = s.tianDaoLaw.LawKey
	}
	law, _ := s.store.GetTianDaoLaw(ctx, lawKey)
	lawManifest := map[string]any{}
	_ = json.Unmarshal([]byte(strings.TrimSpace(law.ManifestJSON)), &lawManifest)
	files["civilization/tian_dao/law.json"] = map[string]any{
		"law":      law,
		"manifest": lawManifest,
	}

	bots, _ := s.store.ListBots(ctx)
	bots = s.filterActiveBots(ctx, bots)
	accounts, _ := s.store.ListTokenAccounts(ctx)
	lifeStates, _ := s.store.ListUserLifeStates(ctx, "", "", 5000)
	worldTicks, _ := s.store.ListWorldTicks(ctx, 200)
	worldCost, _ := s.store.ListCostEvents(ctx, "", 1000)

	sort.SliceStable(bots, func(i, j int) bool { return strings.TrimSpace(bots[i].BotID) < strings.TrimSpace(bots[j].BotID) })
	sort.SliceStable(accounts, func(i, j int) bool {
		return strings.TrimSpace(accounts[i].BotID) < strings.TrimSpace(accounts[j].BotID)
	})
	sort.SliceStable(lifeStates, func(i, j int) bool {
		return strings.TrimSpace(lifeStates[i].UserID) < strings.TrimSpace(lifeStates[j].UserID)
	})
	sort.SliceStable(worldTicks, func(i, j int) bool { return worldTicks[i].TickID > worldTicks[j].TickID })
	sort.SliceStable(worldCost, func(i, j int) bool { return worldCost[i].CreatedAt.After(worldCost[j].CreatedAt) })

	files["civilization/colony/users.json"] = bots
	files["civilization/colony/token_accounts.json"] = accounts
	files["civilization/colony/life_states.json"] = lifeStates
	files["civilization/system/world_ticks_recent.json"] = worldTicks
	files["civilization/system/cost_events_recent.json"] = worldCost

	kbSections, _ := s.store.ListKBSections(ctx, "", 5000)
	kbEntries, _ := s.store.ListKBEntries(ctx, "", "", 5000)
	kbProposals, _ := s.store.ListKBProposals(ctx, "", 5000)
	files["civilization/governance/kb_sections.json"] = kbSections
	files["civilization/governance/kb_entries.json"] = kbEntries
	files["civilization/governance/kb_proposals.json"] = kbProposals

	ganglia, _ := s.store.ListGanglia(ctx, "", "", "", 5000)
	files["civilization/ganglia/stack.json"] = ganglia

	genesisStateMu.Lock()
	mailingLists, _ := s.getMailingListState(ctx)
	lifeWills, _ := s.getLifeWillState(ctx)
	lobsterProfiles, _ := s.getLobsterProfileState(ctx)
	toolRegistry, _ := s.getToolRegistryState(ctx)
	npcTasks, _ := s.getNPCTaskState(ctx)
	npcRuntime, _ := s.getNPCRuntimeState(ctx)
	chronicle, _ := s.getChronicleState(ctx)
	metabolismScores, _ := s.getMetabolismScoreState(ctx)
	metabolismEdges, _ := s.getMetabolismEdgeState(ctx)
	metabolismReports, _ := s.getMetabolismReportState(ctx)
	bounties, _ := s.getBountyState(ctx)
	discipline, _ := s.getDisciplineState(ctx)
	reputation, _ := s.getReputationState(ctx)
	library, _ := s.getLibraryState(ctx)
	metamorph, _ := s.getLifeMetamorphoseState(ctx)
	genesisSnapshot, _ := s.getGenesisState(ctx)
	genesisStateMu.Unlock()

	files["civilization/colony/mailing_lists.json"] = mailingLists
	files["civilization/colony/life_wills.json"] = lifeWills
	files["civilization/colony/lobster_profiles.json"] = lobsterProfiles
	files["civilization/tools/registry.json"] = toolRegistry
	files["civilization/npc/tasks.json"] = npcTasks
	files["civilization/npc/runtime.json"] = npcRuntime
	files["civilization/chronicle/entries.json"] = chronicle
	files["civilization/metabolism/scores.json"] = metabolismScores
	files["civilization/metabolism/supersession_edges.json"] = metabolismEdges
	files["civilization/metabolism/reports.json"] = metabolismReports
	files["civilization/bounties/items.json"] = bounties
	files["civilization/governance/discipline.json"] = discipline
	files["civilization/governance/reputation.json"] = reputation
	files["civilization/library/entries.json"] = library
	files["civilization/life/metamorphose_events.json"] = metamorph
	files["civilization/system/genesis_state.json"] = genesisSnapshot

	banished := make([]map[string]any, 0, len(discipline.Cases))
	reportReason := make(map[int64]string, len(discipline.Reports))
	for _, rep := range discipline.Reports {
		reportReason[rep.ReportID] = rep.Reason
	}
	for _, c := range discipline.Cases {
		if strings.ToLower(strings.TrimSpace(c.Status)) != "closed" || strings.ToLower(strings.TrimSpace(c.Verdict)) != "banish" {
			continue
		}
		when := c.UpdatedAt
		if c.ClosedAt != nil {
			when = *c.ClosedAt
		}
		banished = append(banished, map[string]any{
			"user_id":   c.TargetUserID,
			"report_id": c.ReportID,
			"case_id":   c.CaseID,
			"reason":    strings.TrimSpace(reportReason[c.ReportID]),
			"date":      when,
		})
	}
	sort.SliceStable(banished, func(i, j int) bool {
		li, _ := banished[i]["date"].(time.Time)
		lj, _ := banished[j]["date"].(time.Time)
		return li.After(lj)
	})
	files["civilization/colony/banished.json"] = banished
	return files, nil
}
