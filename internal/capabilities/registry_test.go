package capabilities

import (
	"context"
	"testing"

	"agentd/internal/gateway"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAdapter struct {
	name       string
	tools      []gateway.ToolDefinition
	callResult any
	callErr    error
}

func (m *mockAdapter) Name() string { return m.name }

func (m *mockAdapter) ListTools(ctx context.Context) ([]gateway.ToolDefinition, error) {
	return m.tools, nil
}

func (m *mockAdapter) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	return m.callResult, m.callErr
}

func (m *mockAdapter) Close() error { return nil }

func TestRegistry_Register(t *testing.T) {
	reg := NewRegistry()
	adapter := &mockAdapter{name: "test", tools: []gateway.ToolDefinition{{Name: "tool1"}}}

	reg.Register("test", adapter)

	got, ok := reg.GetAdapter("test")
	require.True(t, ok)
	assert.Equal(t, "test", got.Name())
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewRegistry()
	adapter := &mockAdapter{name: "test", tools: []gateway.ToolDefinition{{Name: "tool1"}}}

	reg.Register("test", adapter)
	reg.Unregister("test")

	_, ok := reg.GetAdapter("test")
	assert.False(t, ok)
}

func TestRegistry_GetTools_AggregatesFromAllAdapters(t *testing.T) {
	reg := NewRegistry()
	reg.Register("github", &mockAdapter{
		name:  "github",
		tools: []gateway.ToolDefinition{{Name: "get_issue"}, {Name: "create_issue"}},
	})
	reg.Register("filesystem", &mockAdapter{
		name:  "filesystem",
		tools: []gateway.ToolDefinition{{Name: "read_file"}, {Name: "write_file"}},
	})

	tools, err := reg.GetTools(context.Background())

	require.NoError(t, err)
	assert.Len(t, tools, 4)

	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	assert.Contains(t, names, "get_issue")
	assert.Contains(t, names, "create_issue")
	assert.Contains(t, names, "read_file")
	assert.Contains(t, names, "write_file")
}

func TestRegistry_GetTools_EmptyRegistry(t *testing.T) {
	reg := NewRegistry()

	tools, err := reg.GetTools(context.Background())

	require.NoError(t, err)
	assert.Empty(t, tools)
}

func TestRegistry_CallTool_RoutesToCorrectAdapter(t *testing.T) {
	reg := NewRegistry()
	reg.Register("github", &mockAdapter{
		name:       "github",
		callResult: "issue #123",
	})

	result, err := reg.CallTool(context.Background(), "github", "get_issue", map[string]any{"id": "123"})

	require.NoError(t, err)
	assert.Equal(t, "issue #123", result)
}

func TestRegistry_CallTool_UnknownAdapter(t *testing.T) {
	reg := NewRegistry()

	result, err := reg.CallTool(context.Background(), "unknown", "tool", nil)

	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestRegistry_Close_ClosesAllAdapters(t *testing.T) {
	reg := NewRegistry()
	adapter1 := &mockAdapter{name: "test1"}
	adapter2 := &mockAdapter{name: "test2"}
	reg.Register("test1", adapter1)
	reg.Register("test2", adapter2)

	reg.Close()
}
