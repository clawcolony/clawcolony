package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestHostedSkillRoutes(t *testing.T) {
	srv := newTestServer()

	cases := []struct {
		path     string
		wantBody string
		wantType string
	}{
		{path: "/skill.md", wantBody: "## Domain Routing Guide", wantType: "text/markdown; charset=utf-8"},
		{path: "/skill.json", wantBody: "\"recommended_entry\": \"https://www.clawcolony.ai/skill.md\"", wantType: "application/json; charset=utf-8"},
		{path: "/heartbeat.md", wantBody: "Run this check every 30 minutes.", wantType: "text/markdown; charset=utf-8"},
		{path: "/knowledge-base.md", wantBody: "Before voting, acknowledge the exact current revision.", wantType: "text/markdown; charset=utf-8"},
		{path: "/collab-mode.md", wantBody: "## State Machine", wantType: "text/markdown; charset=utf-8"},
		{path: "/colony-tools.md", wantBody: "## Standard Lifecycle", wantType: "text/markdown; charset=utf-8"},
		{path: "/ganglia-stack.md", wantBody: "## Ganglia Versus Other Domains", wantType: "text/markdown; charset=utf-8"},
		{path: "/governance.md", wantBody: "## Decision Framework", wantType: "text/markdown; charset=utf-8"},
		{path: "/upgrade-clawcolony.md", wantBody: "This skill does not cover deploy requests", wantType: "text/markdown; charset=utf-8"},
		{path: "/skills/heartbeat.md", wantBody: "**URL:** `https://www.clawcolony.ai/heartbeat.md`", wantType: "text/markdown; charset=utf-8"},
		{path: "/skills/upgrade-clawcolony.md", wantBody: "**URL:** `https://www.clawcolony.ai/upgrade-clawcolony.md`", wantType: "text/markdown; charset=utf-8"},
	}

	for _, tc := range cases {
		w := doJSONRequest(t, srv.mux, http.MethodGet, tc.path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Content-Type"); got != tc.wantType {
			t.Fatalf("%s content-type=%q", tc.path, got)
		}
		if !strings.Contains(w.Body.String(), tc.wantBody) {
			t.Fatalf("%s missing body marker %q", tc.path, tc.wantBody)
		}
	}
}

func TestHostedSkillRoutesRejectUnknownFiles(t *testing.T) {
	srv := newTestServer()

	for _, path := range []string{
		"/dev-preview.md",
		"/self-core-upgrade.md",
		"/unknown.md",
		"/skills/dev-preview.md",
		"/skills/self-core-upgrade.md",
		"/skills/unknown.md",
	} {
		w := doJSONRequest(t, srv.mux, http.MethodGet, path, nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}
