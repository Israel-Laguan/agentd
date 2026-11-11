package fakeagent

import (
	"context"
	"strings"

	acp "github.com/coder/acp-go-sdk"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callTool calls one of agentd's MCP tools and reports the result as a message.
func (a *agent) callTool(ctx context.Context, session acp.SessionId, t Tool) error {
	a.mu.Lock()
	url := a.mcp[session]
	a.mu.Unlock()
	client := mcp.NewClient(&mcp.Implementation{Name: "fakeagent", Version: "0"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		return a.say(ctx, session, "tool error: "+err.Error())
	}
	defer func() { _ = cs.Close() }()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: t.Name, Arguments: t.Args})
	if err != nil {
		return a.say(ctx, session, "tool error: "+err.Error())
	}
	var text []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text = append(text, tc.Text)
		}
	}
	prefix := "tool: "
	if res.IsError {
		prefix = "tool error: "
	}
	return a.say(ctx, session, prefix+strings.Join(text, "\n"))
}
