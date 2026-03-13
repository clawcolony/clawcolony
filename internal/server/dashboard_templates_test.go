package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDashboardTemplatesKeepCoreRuntimeLinks(t *testing.T) {
	coreLinks := []string{
		"/dashboard/mail",
		"/dashboard/collab",
		"/dashboard/kb",
		"/dashboard/governance",
		"/dashboard/world-tick",
	}
	pages := []string{
		"web/dashboard_home.html",
		"web/dashboard_mail.html",
		"web/dashboard_collab.html",
		"web/dashboard_kb.html",
		"web/dashboard_governance.html",
		"web/dashboard_world_tick.html",
		"web/dashboard_monitor.html",
	}

	for _, file := range pages {
		t.Run(strings.TrimPrefix(strings.TrimSuffix(file, ".html"), "web/"), func(t *testing.T) {
			data, err := dashboardFS.ReadFile(file)
			if err != nil {
				t.Fatalf("read template failed: %v", err)
			}
			s := string(data)
			for _, link := range coreLinks {
				if !strings.Contains(s, fmt.Sprintf(`href="%s"`, link)) {
					t.Fatalf("missing core runtime link %s in %s", link, file)
				}
			}
		})
	}
}

func TestDashboardTemplatesAvoidRemovedRuntimeBindings(t *testing.T) {
	checks := []struct {
		file      string
		forbidden []string
		required  []string
	}{
		{
			file: "web/dashboard_home.html",
			forbidden: []string{
				"/dashboard/prompts",
				"/v1/chat/send",
				"/v1/system/openclaw-dashboard-config",
			},
			required: []string{
				"/dashboard/mail",
				"/dashboard/world-tick",
			},
		},
		{
			file: "web/dashboard_world_tick.html",
			forbidden: []string{
				"/v1/chat/send",
				"/v1/bots/dev/",
			},
			required: []string{
				"/v1/runtime/scheduler-settings",
				"/v1/runtime/scheduler-settings/upsert",
			},
		},
		{
			file: "web/dashboard_monitor.html",
			forbidden: []string{
				"/v1/bots/openclaw/status",
				"/dashboard/prompts",
			},
			required: []string{
				"Agent Overview",
				"/v1/monitor/meta",
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
			for _, tok := range c.required {
				if !strings.Contains(s, tok) {
					t.Fatalf("required token missing: %q", tok)
				}
			}
			for _, tok := range c.forbidden {
				if strings.Contains(s, tok) {
					t.Fatalf("forbidden token exists: %q", tok)
				}
			}
		})
	}
}

func TestDashboardIdentityPagesLoad(t *testing.T) {
	srv := newTestServer()
	for _, route := range []string{
		"/dashboard/agent-register",
		"/dashboard/agent-owner",
	} {
		t.Run(strings.TrimPrefix(route, "/dashboard/"), func(t *testing.T) {
			w := doJSONRequest(t, srv.mux, http.MethodGet, route, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("route=%s status=%d body=%s", route, w.Code, w.Body.String())
			}
		})
	}
}
