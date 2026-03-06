package mcpkb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGovernanceToolsExecute(t *testing.T) {
	type reqCapture struct {
		Path   string
		Query  map[string]string
		Header string
	}
	captures := make([]reqCapture, 0, 2)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		captures = append(captures, reqCapture{
			Path:   r.URL.Path,
			Query:  q,
			Header: r.Header.Get("X-Clawcolony-Internal-Token"),
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	s := New(ts.URL, "user-default", "token-abc")

	if _, err := s.execute(context.Background(), "mcp-knowledgebase.governance.docs", map[string]any{
		"keyword": "charter",
		"limit":   12,
	}); err != nil {
		t.Fatalf("governance.docs execute failed: %v", err)
	}
	if _, err := s.execute(context.Background(), "mcp-knowledgebase.governance.proposals", map[string]any{
		"status": "voting",
		"limit":  20,
	}); err != nil {
		t.Fatalf("governance.proposals execute failed: %v", err)
	}
	if _, err := s.execute(context.Background(), "mcp-knowledgebase.governance.protocol", map[string]any{}); err != nil {
		t.Fatalf("governance.protocol execute failed: %v", err)
	}
	if len(captures) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(captures))
	}
	if captures[0].Path != "/v1/governance/docs" {
		t.Fatalf("docs path = %s, want /v1/governance/docs", captures[0].Path)
	}
	if captures[0].Query["keyword"] != "charter" || captures[0].Query["limit"] != "12" {
		t.Fatalf("docs query mismatch: %#v", captures[0].Query)
	}
	if captures[1].Path != "/v1/governance/proposals" {
		t.Fatalf("proposals path = %s, want /v1/governance/proposals", captures[1].Path)
	}
	if captures[1].Query["status"] != "voting" || captures[1].Query["limit"] != "20" {
		t.Fatalf("proposals query mismatch: %#v", captures[1].Query)
	}
	if captures[2].Path != "/v1/governance/protocol" {
		t.Fatalf("protocol path = %s, want /v1/governance/protocol", captures[2].Path)
	}
	if captures[0].Header != "token-abc" || captures[1].Header != "token-abc" || captures[2].Header != "token-abc" {
		t.Fatalf("auth token header mismatch: %#v", captures)
	}
}
