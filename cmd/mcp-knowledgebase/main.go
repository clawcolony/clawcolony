package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"clawcolony/internal/mcpkb"
)

func main() {
	var (
		baseURL       = flag.String("kb-base-url", envOr("KB_BASE_URL", "http://127.0.0.1:8080"), "Clawcolony API base URL")
		defaultUserID = flag.String("default-user-id", envOr("KB_DEFAULT_USER_ID", ""), "Default user_id for tool calls")
		authToken     = flag.String("auth-token", envOr("KB_AUTH_TOKEN", ""), "Optional X-Clawcolony-Internal-Token")
	)
	flag.Parse()

	srv := mcpkb.New(*baseURL, *defaultUserID, *authToken)
	if err := serveStdio(context.Background(), srv); err != nil {
		log.Fatalf("mcp-knowledgebase failed: %v", err)
	}
}

func serveStdio(ctx context.Context, srv *mcpkb.Server) error {
	r := bufio.NewReader(os.Stdin)
	w := bufio.NewWriter(os.Stdout)
	for {
		payload, err := readFrame(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var req mcpkb.JSONRPCRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			resp := mcpkb.JSONRPCResponse{
				JSONRPC: "2.0",
				Error: &mcpkb.JSONRPCError{
					Code:    -32700,
					Message: "parse error",
				},
			}
			if err := writeFrame(w, resp); err != nil {
				return err
			}
			continue
		}
		resp := srv.Handle(ctx, req)
		// Notifications should not respond.
		if req.ID == nil || req.Method == "notifications/initialized" {
			continue
		}
		if err := writeFrame(w, resp); err != nil {
			return err
		}
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		if key == "content-length" {
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %w", err)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	if contentLength > 10<<20 {
		return nil, fmt.Errorf("frame too large: %d", contentLength)
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeFrame(w *bufio.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	_, _ = fmt.Fprintf(&buf, "Content-Length: %d\r\n", len(b))
	_, _ = fmt.Fprintf(&buf, "Content-Type: application/json\r\n\r\n")
	_, _ = buf.Write(b)
	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}
	return w.Flush()
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func init() {
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmicroseconds)
	log.SetPrefix("mcp-knowledgebase ")
	_ = time.UTC
}
