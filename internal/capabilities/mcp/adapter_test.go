package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentd/internal/gateway"
)

func TestNewMCPAdapter_EmptyServerURL(t *testing.T) {
	_, err := NewMCPAdapter(context.Background(), "test", "", nil)
	require.Error(t, err)
	assert.Equal(t, "server_url is required", err.Error())
}

func TestMCPAdapter_Name(t *testing.T) {
	a := &MCPAdapter{name: "my-mcp"}
	assert.Equal(t, "my-mcp", a.Name())
}

func TestMCPAdapter_ListTools_NotConnected(t *testing.T) {
	a := &MCPAdapter{name: "test"}
	_, err := a.ListTools(context.Background())
	require.Error(t, err)
	assert.Equal(t, "not connected", err.Error())
}

func TestMCPAdapter_CallTool_NotConnected(t *testing.T) {
	a := &MCPAdapter{name: "test"}
	_, err := a.CallTool(context.Background(), "tool", nil)
	require.Error(t, err)
	assert.Equal(t, "not connected", err.Error())
}

func TestMCPAdapter_Close_NilSession(t *testing.T) {
	a := &MCPAdapter{name: "test"}
	require.NoError(t, a.Close())
}

func TestAuthTransport_SetsBearerHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	transport := &authTransport{Authorization: "Bearer secret-token"}
	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "Bearer secret-token", gotAuth)
}

type fakeSession struct {
	listTools func(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	callTool  func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error)
	closeErr  error
}

func (f *fakeSession) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	if f.listTools != nil {
		return f.listTools(ctx, params)
	}
	return &mcp.ListToolsResult{}, nil
}

func (f *fakeSession) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	if f.callTool != nil {
		return f.callTool(ctx, params)
	}
	return &mcp.CallToolResult{}, nil
}

func (f *fakeSession) Close() error {
	return f.closeErr
}

func TestMCPAdapter_ListTools_Success(t *testing.T) {
	a := &MCPAdapter{
		name: "test",
		session: &fakeSession{
			listTools: func(_ context.Context, _ *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
				return &mcp.ListToolsResult{
					Tools: []*mcp.Tool{
						{
							Name:        "search",
							Description: "search the web",
							InputSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"q": map[string]any{"type": "string"},
								},
								"required": []any{"q"},
							},
						},
						{Name: "ping", Description: "no schema"},
					},
				}, nil
			},
		},
	}

	tools, err := a.ListTools(context.Background())
	require.NoError(t, err)
	require.Len(t, tools, 2)
	assert.Equal(t, "search", tools[0].Name)
	require.NotNil(t, tools[0].Parameters)
	assert.Equal(t, []string{"q"}, tools[0].Parameters.Required)
	assert.Equal(t, "ping", tools[1].Name)
	assert.Nil(t, tools[1].Parameters)
}

func TestMCPAdapter_ListTools_Error(t *testing.T) {
	want := errors.New("rpc failed")
	a := &MCPAdapter{
		name: "test",
		session: &fakeSession{
			listTools: func(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
				return nil, want
			},
		},
	}
	_, err := a.ListTools(context.Background())
	assert.ErrorIs(t, err, want)
}

func TestMCPAdapter_CallTool_TextSuccess(t *testing.T) {
	a := &MCPAdapter{
		name: "test",
		session: &fakeSession{
			callTool: func(_ context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				assert.Equal(t, "echo", params.Name)
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "hello"},
					},
				}, nil
			},
		},
	}

	result, err := a.CallTool(context.Background(), "echo", map[string]any{"msg": "hi"})
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestMCPAdapter_CallTool_EmptyContent(t *testing.T) {
	a := &MCPAdapter{
		name:    "test",
		session: &fakeSession{},
	}
	_, err := a.CallTool(context.Background(), "tool", nil)
	require.Error(t, err)
	assert.Equal(t, "empty response from MCP server", err.Error())
}

func TestMCPAdapter_CallTool_NonTextContent(t *testing.T) {
	a := &MCPAdapter{
		name: "test",
		session: &fakeSession{
			callTool: func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.ImageContent{MIMEType: "image/png", Data: []byte("x")},
					},
				}, nil
			},
		},
	}
	result, err := a.CallTool(context.Background(), "tool", nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestMCPAdapter_CallTool_RPCError(t *testing.T) {
	want := errors.New("tool failed")
	a := &MCPAdapter{
		name: "test",
		session: &fakeSession{
			callTool: func(context.Context, *mcp.CallToolParams) (*mcp.CallToolResult, error) {
				return nil, want
			},
		},
	}
	_, err := a.CallTool(context.Background(), "tool", nil)
	assert.ErrorIs(t, err, want)
}

func TestMCPAdapter_CallTool_MarshalError(t *testing.T) {
	a := &MCPAdapter{name: "test", session: &fakeSession{}}
	_, err := a.CallTool(context.Background(), "tool", map[string]any{"bad": make(chan int)})
	require.Error(t, err)
}

func TestMCPAdapter_Close_WithSession(t *testing.T) {
	want := errors.New("close failed")
	a := &MCPAdapter{name: "test", session: &fakeSession{closeErr: want}}
	assert.ErrorIs(t, a.Close(), want)
}

func TestConvertInputSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected *gateway.FunctionParameters
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "non-map input",
			input:    "invalid",
			expected: nil,
		},
		{
			name: "map with properties and required",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo":  map[string]any{"type": "string"},
					"issue": map[string]any{"type": "integer"},
				},
				"required": []any{"repo"},
			},
			expected: &gateway.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"repo":  map[string]any{"type": "string"},
					"issue": map[string]any{"type": "integer"},
				},
				Required: []string{"repo"},
			},
		},
		{
			name: "map without properties",
			input: map[string]any{
				"type": "object",
			},
			expected: &gateway.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
		{
			name: "map with empty properties",
			input: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			expected: &gateway.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
		{
			name: "map with required but not string",
			input: map[string]any{
				"type":     "object",
				"required": []any{123, 456},
			},
			expected: &gateway.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
				Required:   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInputSchema(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected.Type, result.Type)
				assert.Equal(t, tt.expected.Properties, result.Properties)
				assert.Equal(t, tt.expected.Required, result.Required)
			}
		})
	}
}
