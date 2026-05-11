package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"agentd/internal/gateway"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AuthConfig struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type MCPAdapter struct {
	name    string
	client  *mcp.Client
	session *mcp.ClientSession
}

func NewMCPAdapter(ctx context.Context, name, serverURL string, auth *AuthConfig) (*MCPAdapter, error) {
	if serverURL == "" {
		return nil, errors.New("server_url is required")
	}

	httpClient := &http.Client{}
	if auth != nil && auth.Token != "" {
		httpClient.Transport = &authTransport{
			Authorization: "Bearer " + auth.Token,
		}
	}

	mcpClient := mcp.NewClient(&mcp.Implementation{
		Name:    "agentd-mcp-client",
		Version: "1.0.0",
	}, nil)

	transport := &mcp.StreamableClientTransport{
		Endpoint:   serverURL,
		HTTPClient: httpClient,
	}

	session, err := mcpClient.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}

	return &MCPAdapter{
		name:    name,
		client:  mcpClient,
		session: session,
	}, nil
}

func (a *MCPAdapter) Name() string {
	return a.name
}

func (a *MCPAdapter) ListTools(ctx context.Context) ([]gateway.ToolDefinition, error) {
	if a.session == nil {
		return nil, errors.New("not connected")
	}

	resp, err := a.session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}

	tools := make([]gateway.ToolDefinition, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		var params *gateway.FunctionParameters
		if t.InputSchema != nil {
			params = convertInputSchema(t.InputSchema)
		}
		tools = append(tools, gateway.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}

	return tools, nil
}

func (a *MCPAdapter) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	if a.session == nil {
		return nil, errors.New("not connected")
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	resp, err := a.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      name,
		Arguments: argsJSON,
	})
	if err != nil {
		return nil, err
	}

	if len(resp.Content) == 0 {
		return nil, errors.New("empty response from MCP server")
	}

	var result any
	for _, content := range resp.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			result = tc.Text
			break
		}
	}

	return result, nil
}

func (a *MCPAdapter) Close() error {
	if a.session != nil {
		return a.session.Close()
	}
	return nil
}

func convertInputSchema(schema any) *gateway.FunctionParameters {
	if schema == nil {
		return nil
	}

	mapSchema, ok := schema.(map[string]any)
	if !ok {
		return nil
	}

	params := &gateway.FunctionParameters{
		Type:       "object",
		Properties: make(map[string]any),
	}

	if props, ok := mapSchema["properties"].(map[string]any); ok {
		params.Properties = props
	}
	if required, ok := mapSchema["required"].([]any); ok {
		var req []string
		for _, r := range required {
			if s, ok := r.(string); ok {
				req = append(req, s)
			}
		}
		params.Required = req
	}

	return params
}

type authTransport struct {
	Authorization string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", t.Authorization)
	return http.DefaultTransport.RoundTrip(req)
}
