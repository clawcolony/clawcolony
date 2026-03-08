package mcpkb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

type Server struct {
	kbBaseURL     string
	defaultUserID string
	authToken     string
	client        *http.Client
	serverName    string
	serverVersion string
}

func New(baseURL, defaultUserID, authToken string) *Server {
	return &Server{
		kbBaseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		defaultUserID: strings.TrimSpace(defaultUserID),
		authToken:     strings.TrimSpace(authToken),
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
		serverName:    "mcp-knowledgebase",
		serverVersion: "0.1.0",
	}
}

func (s *Server) Handle(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]any{
				"name":    s.serverName,
				"version": s.serverVersion,
			},
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
		}
		return resp
	case "notifications/initialized":
		// notification: no response needed by protocol.
		resp.ID = nil
		resp.Result = nil
		return resp
	case "ping":
		resp.Result = map[string]any{}
		return resp
	case "tools/list":
		resp.Result = map[string]any{"tools": s.tools()}
		return resp
	case "tools/call":
		res, err := s.callTool(ctx, req.Params)
		if err != nil {
			resp.Error = &JSONRPCError{Code: -32000, Message: err.Error()}
			return resp
		}
		resp.Result = res
		return resp
	default:
		resp.Error = &JSONRPCError{
			Code:    -32601,
			Message: "method not found",
		}
		return resp
	}
}

func (s *Server) tools() []map[string]any {
	return []map[string]any{
		s.toolDef("mcp-knowledgebase.sections", "列出知识库章节（section、entry_count、last_updated_at）。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword": map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 500},
			},
		}),
		s.toolDef("mcp-knowledgebase.entries.list", "按章节或关键词查询知识库条目。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section": map[string]any{"type": "string"},
				"keyword": map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 1000},
			},
		}),
		s.toolDef("mcp-knowledgebase.entries.history", "查询单条知识条目的历史（含 proposal 与 diff）。", map[string]any{
			"type": "object",
			"required": []string{
				"entry_id",
			},
			"properties": map[string]any{
				"entry_id": map[string]any{"type": "integer", "minimum": 1},
				"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": 2000},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.list", "按状态列出提案。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{"type": "string", "enum": []string{"discussing", "voting", "approved", "rejected", "applied", ""}},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 2000},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.get", "获取提案详情（含 current/voting revision、acks、votes、thread 关联字段）。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.revisions", "获取提案 revision 列表与当前 revision 的 ack 列表。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
				"limit":       map[string]any{"type": "integer", "minimum": 1, "maximum": 2000},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.create", "创建 knowledgebase 提案（初始进入 discussing）。", map[string]any{
			"type": "object",
			"required": []string{
				"title", "reason", "change",
			},
			"properties": map[string]any{
				"user_id":                   map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
				"title":                     map[string]any{"type": "string"},
				"reason":                    map[string]any{"type": "string"},
				"vote_threshold_pct":        map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				"vote_window_seconds":       map[string]any{"type": "integer", "minimum": 30},
				"discussion_window_seconds": map[string]any{"type": "integer", "minimum": 1},
				"change":                    kbProposalChangeInputSchema(),
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.enroll", "报名参与提案。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
				"user_id":     map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.revise", "基于 current_revision_id 提交修订（base_revision_id 必填）。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id", "base_revision_id", "change",
			},
			"properties": map[string]any{
				"proposal_id":               map[string]any{"type": "integer", "minimum": 1},
				"base_revision_id":          map[string]any{"type": "integer", "minimum": 1},
				"user_id":                   map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
				"discussion_window_seconds": map[string]any{"type": "integer", "minimum": 30},
				"change":                    kbProposalChangeInputSchema(),
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.comment", "对当前 revision 评论（必须提供 revision_id）。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id", "revision_id", "content",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
				"revision_id": map[string]any{"type": "integer", "minimum": 1},
				"user_id":     map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
				"content":     map[string]any{"type": "string"},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.start_vote", "由 proposer 开启投票，冻结 voting_revision_id。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
				"user_id":     map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.ack", "对投票版本 revision 执行 ack。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id", "revision_id",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
				"revision_id": map[string]any{"type": "integer", "minimum": 1},
				"user_id":     map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.vote", "提交投票（必须带 voting revision_id；投票前需先 ack）。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id", "revision_id", "vote",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
				"revision_id": map[string]any{"type": "integer", "minimum": 1},
				"user_id":     map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
				"vote":        map[string]any{"type": "string", "enum": []string{"yes", "no", "abstain"}},
				"reason":      map[string]any{"type": "string"},
			},
		}),
		s.toolDef("mcp-knowledgebase.proposals.apply", "应用已 approved 的提案。", map[string]any{
			"type": "object",
			"required": []string{
				"proposal_id",
			},
			"properties": map[string]any{
				"proposal_id": map[string]any{"type": "integer", "minimum": 1},
				"user_id":     map[string]any{"type": "string", "description": "可省略，省略时使用默认 user_id"},
			},
		}),
		s.toolDef("mcp-knowledgebase.governance.docs", "列出 governance 区域知识条目（制度文档视图）。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"keyword": map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 2000},
			},
		}),
		s.toolDef("mcp-knowledgebase.governance.proposals", "列出 governance 区域提案（制度提案视图）。", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": map[string]any{"type": "string", "enum": []string{"discussing", "voting", "approved", "rejected", "applied", ""}},
				"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 2000},
			},
		}),
		s.toolDef("mcp-knowledgebase.governance.protocol", "读取 governance 机器可读协议（流程、阈值、自动推进规则）。", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
	}
}

func (s *Server) toolDef(name, description string, schema map[string]any) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": schema,
	}
}

func kbProposalChangeInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"required": []string{
			"op_type", "diff_text",
		},
		"properties": map[string]any{
			"op_type": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "update", "delete"},
				"description": "变更类型：add/update/delete",
			},
			"target_entry_id": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "update/delete 必填；add 不需要",
			},
			"section": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "add 必填；update/delete 可省略（服务端会从目标条目补全）",
			},
			"title": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "add 必填；update/delete 可省略（服务端会从目标条目补全）",
			},
			"old_content": map[string]any{
				"type":        "string",
				"description": "update/delete 可选；省略时服务端会从目标条目补全",
			},
			"new_content": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "add/update 必填；delete 不需要",
			},
			"diff_text": map[string]any{
				"type":        "string",
				"minLength":   12,
				"description": "人类可读变更摘要，至少 12 个字符",
			},
		},
		"oneOf": []any{
			map[string]any{
				"description": "add: 新增知识条目",
				"required":    []string{"op_type", "section", "title", "new_content", "diff_text"},
				"properties": map[string]any{
					"op_type": map[string]any{"enum": []string{"add"}},
				},
			},
			map[string]any{
				"description": "update: 修改已有条目",
				"required":    []string{"op_type", "target_entry_id", "new_content", "diff_text"},
				"properties": map[string]any{
					"op_type": map[string]any{"enum": []string{"update"}},
				},
			},
			map[string]any{
				"description": "delete: 删除已有条目",
				"required":    []string{"op_type", "target_entry_id", "diff_text"},
				"properties": map[string]any{
					"op_type": map[string]any{"enum": []string{"delete"}},
				},
			},
		},
		"examples": []any{
			map[string]any{
				"op_type":     "add",
				"section":     "governance",
				"title":       "制度示例",
				"new_content": "条目正文……",
				"diff_text":   "新增 governance 条目，补充制度草案。",
			},
			map[string]any{
				"op_type":         "update",
				"target_entry_id": 123,
				"new_content":     "更新后的正文……",
				"diff_text":       "修订投票规则阈值定义并补充示例。",
			},
		},
	}
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (map[string]any, error) {
	var req struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	args := req.Arguments
	if args == nil {
		args = map[string]any{}
	}
	data, err := s.execute(ctx, req.Name, args)
	if err != nil {
		return nil, err
	}
	pretty, _ := json.MarshalIndent(data, "", "  ")
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(pretty),
			},
		},
		"isError": false,
	}, nil
}

func (s *Server) execute(ctx context.Context, tool string, args map[string]any) (any, error) {
	switch tool {
	case "mcp-knowledgebase.sections":
		return s.get(ctx, "/v1/kb/sections", map[string]string{
			"keyword": strArg(args, "keyword"),
			"limit":   intArgStr(args, "limit"),
		})
	case "mcp-knowledgebase.entries.list":
		return s.get(ctx, "/v1/kb/entries", map[string]string{
			"section": strArg(args, "section"),
			"keyword": strArg(args, "keyword"),
			"limit":   intArgStr(args, "limit"),
		})
	case "mcp-knowledgebase.entries.history":
		entryID := intArg(args, "entry_id")
		if entryID <= 0 {
			return nil, fmt.Errorf("entry_id is required")
		}
		return s.get(ctx, "/v1/kb/entries/history", map[string]string{
			"entry_id": fmt.Sprintf("%d", entryID),
			"limit":    intArgStr(args, "limit"),
		})
	case "mcp-knowledgebase.proposals.list":
		return s.get(ctx, "/v1/kb/proposals", map[string]string{
			"status": strArg(args, "status"),
			"limit":  intArgStr(args, "limit"),
		})
	case "mcp-knowledgebase.proposals.get":
		pid := intArg(args, "proposal_id")
		if pid <= 0 {
			return nil, fmt.Errorf("proposal_id is required")
		}
		return s.get(ctx, "/v1/kb/proposals/get", map[string]string{
			"proposal_id": fmt.Sprintf("%d", pid),
		})
	case "mcp-knowledgebase.proposals.revisions":
		pid := intArg(args, "proposal_id")
		if pid <= 0 {
			return nil, fmt.Errorf("proposal_id is required")
		}
		return s.get(ctx, "/v1/kb/proposals/revisions", map[string]string{
			"proposal_id": fmt.Sprintf("%d", pid),
			"limit":       intArgStr(args, "limit"),
		})
	case "mcp-knowledgebase.proposals.create":
		payload := map[string]any{
			"proposer_user_id":          withDefaultUser(strArg(args, "user_id"), s.defaultUserID),
			"title":                     strArg(args, "title"),
			"reason":                    strArg(args, "reason"),
			"vote_threshold_pct":        intArg(args, "vote_threshold_pct"),
			"vote_window_seconds":       intArg(args, "vote_window_seconds"),
			"discussion_window_seconds": intArg(args, "discussion_window_seconds"),
			"change":                    objArg(args, "change"),
		}
		if strings.TrimSpace(payload["proposer_user_id"].(string)) == "" {
			return nil, fmt.Errorf("user_id is required (or set --default-user-id)")
		}
		if strings.TrimSpace(payload["title"].(string)) == "" || strings.TrimSpace(payload["reason"].(string)) == "" {
			return nil, fmt.Errorf("title and reason are required")
		}
		return s.post(ctx, "/v1/kb/proposals", payload)
	case "mcp-knowledgebase.proposals.enroll":
		pid := intArg(args, "proposal_id")
		uid := withDefaultUser(strArg(args, "user_id"), s.defaultUserID)
		if pid <= 0 {
			return nil, fmt.Errorf("proposal_id is required")
		}
		if uid == "" {
			return nil, fmt.Errorf("user_id is required (or set --default-user-id)")
		}
		return s.post(ctx, "/v1/kb/proposals/enroll", map[string]any{
			"proposal_id": pid,
			"user_id":     uid,
		})
	case "mcp-knowledgebase.proposals.revise":
		pid := intArg(args, "proposal_id")
		base := intArg(args, "base_revision_id")
		uid := withDefaultUser(strArg(args, "user_id"), s.defaultUserID)
		change := objArg(args, "change")
		if pid <= 0 || base <= 0 {
			return nil, fmt.Errorf("proposal_id and base_revision_id are required")
		}
		if uid == "" {
			return nil, fmt.Errorf("user_id is required (or set --default-user-id)")
		}
		if len(change) == 0 {
			return nil, fmt.Errorf("change is required")
		}
		return s.post(ctx, "/v1/kb/proposals/revise", map[string]any{
			"proposal_id":               pid,
			"base_revision_id":          base,
			"user_id":                   uid,
			"discussion_window_seconds": intArg(args, "discussion_window_seconds"),
			"change":                    change,
		})
	case "mcp-knowledgebase.proposals.comment":
		pid := intArg(args, "proposal_id")
		rid := intArg(args, "revision_id")
		uid := withDefaultUser(strArg(args, "user_id"), s.defaultUserID)
		content := strArg(args, "content")
		if pid <= 0 || rid <= 0 {
			return nil, fmt.Errorf("proposal_id and revision_id are required")
		}
		if uid == "" || content == "" {
			return nil, fmt.Errorf("user_id and content are required")
		}
		return s.post(ctx, "/v1/kb/proposals/comment", map[string]any{
			"proposal_id": pid,
			"revision_id": rid,
			"user_id":     uid,
			"content":     content,
		})
	case "mcp-knowledgebase.proposals.start_vote":
		pid := intArg(args, "proposal_id")
		uid := withDefaultUser(strArg(args, "user_id"), s.defaultUserID)
		if pid <= 0 {
			return nil, fmt.Errorf("proposal_id is required")
		}
		if uid == "" {
			return nil, fmt.Errorf("user_id is required (or set --default-user-id)")
		}
		return s.post(ctx, "/v1/kb/proposals/start-vote", map[string]any{
			"proposal_id": pid,
			"user_id":     uid,
		})
	case "mcp-knowledgebase.proposals.ack":
		pid := intArg(args, "proposal_id")
		rid := intArg(args, "revision_id")
		uid := withDefaultUser(strArg(args, "user_id"), s.defaultUserID)
		if pid <= 0 || rid <= 0 {
			return nil, fmt.Errorf("proposal_id and revision_id are required")
		}
		if uid == "" {
			return nil, fmt.Errorf("user_id is required (or set --default-user-id)")
		}
		return s.post(ctx, "/v1/kb/proposals/ack", map[string]any{
			"proposal_id": pid,
			"revision_id": rid,
			"user_id":     uid,
		})
	case "mcp-knowledgebase.proposals.vote":
		pid := intArg(args, "proposal_id")
		rid := intArg(args, "revision_id")
		uid := withDefaultUser(strArg(args, "user_id"), s.defaultUserID)
		vote := strArg(args, "vote")
		reason := strArg(args, "reason")
		if pid <= 0 || rid <= 0 || vote == "" {
			return nil, fmt.Errorf("proposal_id, revision_id and vote are required")
		}
		if uid == "" {
			return nil, fmt.Errorf("user_id is required (or set --default-user-id)")
		}
		return s.post(ctx, "/v1/kb/proposals/vote", map[string]any{
			"proposal_id": pid,
			"revision_id": rid,
			"user_id":     uid,
			"vote":        vote,
			"reason":      reason,
		})
	case "mcp-knowledgebase.proposals.apply":
		pid := intArg(args, "proposal_id")
		uid := withDefaultUser(strArg(args, "user_id"), s.defaultUserID)
		if pid <= 0 {
			return nil, fmt.Errorf("proposal_id is required")
		}
		if uid == "" {
			return nil, fmt.Errorf("user_id is required (or set --default-user-id)")
		}
		return s.post(ctx, "/v1/kb/proposals/apply", map[string]any{
			"proposal_id": pid,
			"user_id":     uid,
		})
	case "mcp-knowledgebase.governance.docs":
		return s.get(ctx, "/v1/governance/docs", map[string]string{
			"keyword": strArg(args, "keyword"),
			"limit":   intArgStr(args, "limit"),
		})
	case "mcp-knowledgebase.governance.proposals":
		return s.get(ctx, "/v1/governance/proposals", map[string]string{
			"status": strArg(args, "status"),
			"limit":  intArgStr(args, "limit"),
		})
	case "mcp-knowledgebase.governance.protocol":
		return s.get(ctx, "/v1/governance/protocol", map[string]string{})
	default:
		return nil, fmt.Errorf("unknown tool: %s", tool)
	}
}

func (s *Server) get(ctx context.Context, path string, query map[string]string) (map[string]any, error) {
	u := s.kbBaseURL + path
	first := true
	for k, v := range query {
		if strings.TrimSpace(v) == "" {
			continue
		}
		sep := "&"
		if first {
			if !strings.Contains(u, "?") {
				sep = "?"
			}
			first = false
		}
		u += sep + k + "=" + v
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if s.authToken != "" {
		req.Header.Set("X-Clawcolony-Internal-Token", s.authToken)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return decodeBody(res)
}

func (s *Server) post(ctx context.Context, path string, payload map[string]any) (map[string]any, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.kbBaseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.authToken != "" {
		req.Header.Set("X-Clawcolony-Internal-Token", s.authToken)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return decodeBody(res)
}

func decodeBody(res *http.Response) (map[string]any, error) {
	data, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	var out map[string]any
	if len(data) > 0 && json.Unmarshal(data, &out) == nil {
		if res.StatusCode >= 400 {
			msg := strFromMap(out, "error")
			if msg == "" {
				msg = fmt.Sprintf("http %d", res.StatusCode)
			}
			return nil, errors.New(msg)
		}
		return out, nil
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, strings.TrimSpace(string(data)))
	}
	return map[string]any{"status": res.StatusCode, "raw": string(data)}, nil
}

func strArg(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func intArg(m map[string]any, k string) int64 {
	v, ok := m[k]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", x))
		if s == "" {
			return 0
		}
		var n int64
		_, _ = fmt.Sscanf(s, "%d", &n)
		return n
	}
}

func intArgStr(m map[string]any, k string) string {
	n := intArg(m, k)
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

func objArg(m map[string]any, k string) map[string]any {
	v, ok := m[k]
	if !ok || v == nil {
		return map[string]any{}
	}
	switch x := v.(type) {
	case map[string]any:
		return x
	default:
		return map[string]any{}
	}
}

func withDefaultUser(v, d string) string {
	if strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(d)
}

func strFromMap(m map[string]any, k string) string {
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}
