//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// This file wraps the MCP board server: a JSON-RPC 2.0 endpoint served over
// Streamable HTTP at POST /mcp (internal/mcp/server.go's HTTPHandler), not a
// REST route. There is no /api/v1/mcp/export — see docs/mcp-board-export.md.
//
// Two details a caller has to know, both verified against the running stack:
//
//   - The response body is an SSE stream carrying one `data:` frame with the
//     JSON-RPC result, not a bare JSON body. The server requires the request's
//     Accept header to cover both application/json and text/event-stream.
//     curl gets this for free with its default `*/*`; a programmatic client
//     does not — Go's http.Client sends no Accept at all and is answered 400.

//   - A tool's payload is a JSON *string* inside content[0].text, with the
//     parsed form repeated alongside it as structuredContent. Every tool result
//     is shaped that way — there is no per-tool response schema.

// MCPResponse is a decoded JSON-RPC response from /mcp.
type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  *MCPResult      `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

// MCPResult is the result object of a tools/call.
type MCPResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

// MCPError is a JSON-RPC error object (e.g. -32602 invalid params for an
// unknown tool). This is distinct from a tool-level failure, which comes back
// as a successful result with isError set.
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPTool is one entry of a tools/list result.
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// PostMCP sends a JSON-RPC payload to /mcp with the Accept header the
// Streamable HTTP transport requires. Every MCP call in the suite goes
// through here so that requirement lives in one place.
func (c *APIClient) PostMCP(ctx context.Context, payload any) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	return c.client.Do(req)
}

// CallMCPTool invokes a board tool and returns the result with its text
// payload parsed as JSON into out. Method errors and tool errors both surface
// here, so callers assert on the decoded payload and never on a status code.
func (c *APIClient) CallMCPTool(ctx context.Context, name string, args map[string]any, out any) error {
	resp, err := c.PostMCP(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		return fmt.Errorf("mcp call %s: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return fmt.Errorf("mcp call %s returned %d, want 200", name, resp.StatusCode)
	}
	var rpc MCPResponse
	if err := readMCPStream(resp, &rpc); err != nil {
		return fmt.Errorf("mcp call %s: %w", name, err)
	}
	if rpc.Error != nil {
		return fmt.Errorf("mcp call %s: jsonrpc error %d: %s", name, rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result == nil {
		return fmt.Errorf("mcp call %s: response had no result", name)
	}
	if rpc.Result.IsError {
		return fmt.Errorf("mcp call %s: tool reported an error: %s", name, rpc.Result.ContentText())
	}
	if out == nil {
		return nil
	}
	if len(rpc.Result.Content) == 0 {
		return fmt.Errorf("mcp call %s: result had no content blocks", name)
	}
	return json.Unmarshal([]byte(rpc.Result.Content[0].Text), out)
}

// ListMCPTools calls tools/list and returns the advertised tools by name.
func (c *APIClient) ListMCPTools(ctx context.Context) (map[string]MCPTool, error) {
	resp, err := c.PostMCP(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	if err != nil {
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("mcp tools/list returned %d, want 200", resp.StatusCode)
	}
	var rpc struct {
		Result struct {
			Tools []MCPTool `json:"tools"`
		} `json:"result"`
	}
	if err := readMCPStream(resp, &rpc); err != nil {
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}
	byName := make(map[string]MCPTool, len(rpc.Result.Tools))
	for _, tool := range rpc.Result.Tools {
		byName[tool.Name] = tool
	}
	return byName, nil
}

// CallMCPForError invokes a tool expected to fail and returns the JSON-RPC
// error, for asserting on the rejection path.
func (c *APIClient) CallMCPForError(ctx context.Context, name string, args map[string]any) (*MCPError, error) {
	resp, err := c.PostMCP(ctx, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp call %s: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("mcp call %s returned %d, want 200", name, resp.StatusCode)
	}
	var rpc MCPResponse
	if err := readMCPStream(resp, &rpc); err != nil {
		return nil, fmt.Errorf("mcp call %s: %w", name, err)
	}
	if rpc.Error == nil {
		return nil, fmt.Errorf("mcp call %s succeeded, want a jsonrpc error", name)
	}
	return rpc.Error, nil
}

// ContentText returns the first text content block of a tool result.
func (r *MCPResult) ContentText() string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	return r.Content[0].Text
}

// readMCPStream decodes the single JSON-RPC payload the /mcp SSE response
// carries. The server writes `event: message` followed by one `data:` line;
// anything else on the stream is ignored.
func readMCPStream(resp *http.Response, out any) error {
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		return fmt.Errorf("content-type = %q, want text/event-stream", ct)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		return json.Unmarshal([]byte(payload), out)
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return err
	}
	return fmt.Errorf("stream carried no data frame")
}

// MCPProject is one entry of board.list_projects / board.get_project
// (internal/mcp/tools_read.go). The field set is exactly what the tool builds —
// there is no task or output detail on this shape.
type MCPProject struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	WorkspacePath string `json:"workspace_path"`
	Status        string `json:"status"`
	// OriginalInput is only present on board.get_project.
	OriginalInput string `json:"original_input,omitempty"`
}

// MCPTask is one entry of board.list_tasks (internal/mcp/tools_read.go).
// Note what is absent: no description, no result and no output. The board
// export is a state summary, not a task transcript.
type MCPTask struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	State     string   `json:"state"`
	Assignee  string   `json:"assignee"`
	ProjectID string   `json:"project_id"`
	AgentID   string   `json:"agent_id"`
	DependsOn []string `json:"depends_on"`
}

// MCPGetTask is the board.get_task shape: the task plus counts, not bodies.
type MCPGetTask struct {
	MCPTask
	Description string `json:"description"`
	RetryCount  int    `json:"retry_count"`
	Events      int    `json:"events"`
	Comments    int    `json:"comments"`
}

// mcpToolNames returns the sorted tool names, for failure messages that should
// list what the server actually advertises.
func mcpToolNames(tools map[string]MCPTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
