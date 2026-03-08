package mcpkb

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestGovernanceToolsExecute(t *testing.T) {
	type reqCapture struct {
		Path   string
		Query  map[string]string
		Header string
	}
	captures := make([]reqCapture, 0, 2)
	s := New("http://kb.local:8080", "user-default", "token-abc")
	s.client = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			q := map[string]string{}
			for k := range r.URL.Query() {
				q[k] = r.URL.Query().Get(k)
			}
			captures = append(captures, reqCapture{
				Path:   r.URL.Path,
				Query:  q,
				Header: r.Header.Get("X-Clawcolony-Internal-Token"),
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
			}, nil
		}),
	}

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestKBProposalChangeSchemaExposedInToolsList(t *testing.T) {
	s := New("http://localhost:8080", "bot-a", "")
	tools := s.tools()
	for _, toolName := range []string{
		"mcp-knowledgebase.proposals.create",
		"mcp-knowledgebase.proposals.revise",
	} {
		tool := findToolByName(t, tools, toolName)
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing inputSchema", toolName)
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing inputSchema.properties", toolName)
		}
		change, ok := props["change"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing change schema", toolName)
		}
		required, ok := change["required"].([]string)
		if !ok {
			t.Fatalf("%s change.required type mismatch", toolName)
		}
		if !slices.Contains(required, "op_type") || !slices.Contains(required, "diff_text") {
			t.Fatalf("%s change.required = %#v, want op_type+diff_text", toolName, required)
		}
		changeProps, ok := change["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s change.properties missing", toolName)
		}
		for _, field := range []string{
			"op_type", "target_entry_id", "section", "title", "old_content", "new_content", "diff_text",
		} {
			if _, ok := changeProps[field]; !ok {
				t.Fatalf("%s change schema missing field %s", toolName, field)
			}
		}
		opType, ok := changeProps["op_type"].(map[string]any)
		if !ok {
			t.Fatalf("%s change.op_type schema missing", toolName)
		}
		opEnum, ok := opType["enum"].([]string)
		if !ok {
			t.Fatalf("%s change.op_type enum type mismatch", toolName)
		}
		if !slices.Equal(opEnum, []string{"add", "update", "delete"}) {
			t.Fatalf("%s op_type enum = %#v", toolName, opEnum)
		}
		oneOf, ok := change["oneOf"].([]any)
		if !ok || len(oneOf) != 3 {
			t.Fatalf("%s change.oneOf = %#v, want 3 cases", toolName, change["oneOf"])
		}
	}
}

func findToolByName(t *testing.T, tools []map[string]any, name string) map[string]any {
	t.Helper()
	for _, tool := range tools {
		if toolName, _ := tool["name"].(string); toolName == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}
