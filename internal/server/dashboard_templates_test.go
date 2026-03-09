package server

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestDashboardTopTabsConsistent(t *testing.T) {
	expectedTabs := []string{
		"/dashboard",
		"/dashboard/ops",
		"/dashboard/mail",
		"/dashboard/chat",
		"/dashboard/collab",
		"/dashboard/kb",
		"/dashboard/governance",
		"/dashboard/ganglia",
		"/dashboard/bounty",
		"/dashboard/bot-logs",
		"/dashboard/system-logs",
		"/dashboard/world-tick",
		"/dashboard/world-replay",
		"/dashboard/monitor",
		"/dashboard/prompts",
	}

	pages := []struct {
		file       string
		activeHref string
	}{
		{file: "web/dashboard_home.html", activeHref: "/dashboard"},
		{file: "web/dashboard_ops.html", activeHref: "/dashboard/ops"},
		{file: "web/dashboard_mail.html", activeHref: "/dashboard/mail"},
		{file: "web/dashboard_chat.html", activeHref: "/dashboard/chat"},
		{file: "web/dashboard_collab.html", activeHref: "/dashboard/collab"},
		{file: "web/dashboard_kb.html", activeHref: "/dashboard/kb"},
		{file: "web/dashboard_governance.html", activeHref: "/dashboard/governance"},
		{file: "web/dashboard_ganglia.html", activeHref: "/dashboard/ganglia"},
		{file: "web/dashboard_bounty.html", activeHref: "/dashboard/bounty"},
		{file: "web/dashboard_bot_logs.html", activeHref: "/dashboard/bot-logs"},
		{file: "web/dashboard_system_logs.html", activeHref: "/dashboard/system-logs"},
		{file: "web/dashboard_world_tick.html", activeHref: "/dashboard/world-tick"},
		{file: "web/dashboard_world_replay.html", activeHref: "/dashboard/world-replay"},
		{file: "web/dashboard_monitor.html", activeHref: "/dashboard/monitor"},
		{file: "web/dashboard_prompts.html", activeHref: "/dashboard/prompts"},
	}

	tabsBlockRe := regexp.MustCompile(`(?s)<div class="tabs">(.*?)</div>`)
	hrefRe := regexp.MustCompile(`href="(/dashboard[^"]*)"`)
	activeRe := regexp.MustCompile(`class="active"\s+href="(/dashboard[^"]*)"`)

	for _, p := range pages {
		t.Run(strings.TrimPrefix(strings.TrimSuffix(p.file, ".html"), "web/"), func(t *testing.T) {
			data, err := dashboardFS.ReadFile(p.file)
			if err != nil {
				t.Fatalf("read template failed: %v", err)
			}
			s := string(data)

			tabsBlock := tabsBlockRe.FindStringSubmatch(s)
			if len(tabsBlock) != 2 {
				t.Fatal("tabs block not found")
			}

			matches := hrefRe.FindAllStringSubmatch(tabsBlock[1], -1)
			if len(matches) != len(expectedTabs) {
				t.Fatalf("tabs href count mismatch: got=%d want=%d", len(matches), len(expectedTabs))
			}

			topTabs := make([]string, 0, len(expectedTabs))
			for _, m := range matches {
				topTabs = append(topTabs, m[1])
			}
			for i := range expectedTabs {
				if topTabs[i] != expectedTabs[i] {
					t.Fatalf("tab order mismatch at idx=%d: got=%q want=%q", i, topTabs[i], expectedTabs[i])
				}
			}

			active := activeRe.FindAllStringSubmatch(tabsBlock[1], -1)
			if len(active) != 1 {
				t.Fatalf("expected exactly one active tab, got=%d", len(active))
			}
			if active[0][1] != p.activeHref {
				t.Fatalf("active tab mismatch: got=%q want=%q", active[0][1], p.activeHref)
			}

			if !strings.Contains(s, `.tabs { display:flex;`) {
				t.Fatal("top tabs style missing")
			}
			if !strings.Contains(s, `class="tabs"`) {
				t.Fatal("tabs container missing")
			}

			for _, h := range expectedTabs {
				if !strings.Contains(tabsBlock[1], fmt.Sprintf(`href="%s"`, h)) {
					t.Fatalf("missing expected tab href: %s", h)
				}
			}
		})
	}
}

func TestDashboardNoStaleUserListRefreshGuard(t *testing.T) {
	checks := []struct {
		file             string
		forbiddenPattern string
		requiredPattern  string
	}{
		{
			file:             "web/dashboard_mail.html",
			forbiddenPattern: "if (!bots.length) await loadUsers();",
			requiredPattern:  "if (forceUsers || (usersRefreshTick % 3 === 0)) {",
		},
		{
			file:             "web/dashboard_bot_logs.html",
			forbiddenPattern: "if (!bots.length) await loadBots();",
			requiredPattern:  "if (forceBots || (botRefreshTick % 4 === 0) || !selected) {",
		},
		{
			file:             "web/dashboard_collab.html",
			forbiddenPattern: "setInterval(()=>{ if (document.getElementById('collabId').value.trim()) loadDetail().catch(()=>{}); }, 3000);",
			requiredPattern:  "if (autoRefreshTick % 3 === 0) {",
		},
	}

	for _, c := range checks {
		t.Run(strings.TrimPrefix(strings.TrimSuffix(c.file, ".html"), "web/"), func(t *testing.T) {
			data, err := dashboardFS.ReadFile(c.file)
			if err != nil {
				t.Fatalf("read template failed: %v", err)
			}
			s := string(data)
			if strings.Contains(s, c.forbiddenPattern) {
				t.Fatalf("stale refresh guard still exists: %q", c.forbiddenPattern)
			}
			if !strings.Contains(s, c.requiredPattern) {
				t.Fatalf("expected refresh policy pattern not found: %q", c.requiredPattern)
			}
		})
	}
}

func TestDashboardPromptsKBPodsInteractionConsistency(t *testing.T) {
	checks := []struct {
		file            string
		requiredTokens  []string
		forbiddenTokens []string
	}{
		{
			file: "web/dashboard_prompts.html",
			requiredTokens: []string{
				`id="promptsAutoRefresh"`,
				`if (auto && !auto.checked) return;`,
				`if (autoRefreshTick % 2 === 0) {`,
				`loadUsers().catch(()=>{});`,
			},
		},
		{
			file: "web/dashboard_kb.html",
			requiredTokens: []string{
				`id="kbAutoRefresh"`,
				`<select id="uid"`,
				`<select id="c_uid"`,
				`if (auto && !auto.checked) return;`,
				`loadUsers().catch(()=>{});`,
			},
			forbiddenTokens: []string{
				`<input id="uid"`,
				`<input id="c_uid"`,
			},
		},
	}

	for _, c := range checks {
		t.Run(strings.TrimPrefix(strings.TrimSuffix(c.file, ".html"), "web/"), func(t *testing.T) {
			data, err := dashboardFS.ReadFile(c.file)
			if err != nil {
				t.Fatalf("read template failed: %v", err)
			}
			s := string(data)
			for _, tok := range c.requiredTokens {
				if !strings.Contains(s, tok) {
					t.Fatalf("required token missing: %q", tok)
				}
			}
			for _, tok := range c.forbiddenTokens {
				if strings.Contains(s, tok) {
					t.Fatalf("forbidden token exists: %q", tok)
				}
			}
		})
	}
}
