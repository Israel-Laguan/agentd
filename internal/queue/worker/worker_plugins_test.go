package worker

import (
	"testing"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubMounter is a test double for PluginMounter.
type stubMounter struct {
	projectCalled bool
	sessionCalled bool
	projectErr    error
	sessionErr    error
	sessionNames  []string
}

func (s *stubMounter) MountProject(
	_ string, chain *HookChain, _ *capabilities.Registry,
) error {
	s.projectCalled = true
	if s.projectErr != nil {
		return s.projectErr
	}
	chain.RegisterPre(PreHook{
		Name: "proj-hook", Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{}, nil
		},
	})
	return nil
}

func (s *stubMounter) MountSession(
	names []string, chain *HookChain, _ *capabilities.Registry,
) error {
	s.sessionCalled = true
	s.sessionNames = names
	if s.sessionErr != nil {
		return s.sessionErr
	}
	chain.RegisterPre(PreHook{
		Name: "sess-hook", Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{}, nil
		},
	})
	return nil
}

func TestMountScopedPlugins_NilMounter(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	hooks, caps := w.mountScopedPlugins(models.Project{}, models.AgentProfile{})
	assert.Nil(t, hooks)
	assert.Nil(t, caps)
}

func TestMountScopedPlugins_ProjectOnly(t *testing.T) {
	t.Parallel()
	m := &stubMounter{}
	w := &Worker{pluginMounter: m}

	hooks, caps := w.mountScopedPlugins(
		models.Project{WorkspacePath: "/ws"},
		models.AgentProfile{},
	)
	require.NotNil(t, hooks)
	require.NotNil(t, caps)
	assert.True(t, m.projectCalled)
	assert.False(t, m.sessionCalled)
}

func TestMountScopedPlugins_SessionOnly(t *testing.T) {
	t.Parallel()
	m := &stubMounter{}
	w := &Worker{pluginMounter: m}

	hooks, caps := w.mountScopedPlugins(
		models.Project{},
		models.AgentProfile{Plugins: []string{"alpha"}},
	)
	require.NotNil(t, hooks)
	require.NotNil(t, caps)
	assert.False(t, m.projectCalled)
	assert.True(t, m.sessionCalled)
	assert.Equal(t, []string{"alpha"}, m.sessionNames)
}

func TestMountScopedPlugins_BothScopes(t *testing.T) {
	t.Parallel()
	m := &stubMounter{}
	w := &Worker{pluginMounter: m}

	hooks, caps := w.mountScopedPlugins(
		models.Project{WorkspacePath: "/ws"},
		models.AgentProfile{Plugins: []string{"beta"}},
	)
	require.NotNil(t, hooks)
	require.NotNil(t, caps)
	assert.True(t, m.projectCalled)
	assert.True(t, m.sessionCalled)
}

func TestMountScopedPlugins_ErrorsAreNonFatal(t *testing.T) {
	t.Parallel()
	m := &stubMounter{
		projectErr: assert.AnError,
		sessionErr: assert.AnError,
	}
	w := &Worker{pluginMounter: m}

	hooks, caps := w.mountScopedPlugins(
		models.Project{WorkspacePath: "/ws"},
		models.AgentProfile{Plugins: []string{"alpha"}},
	)
	require.NotNil(t, hooks)
	require.NotNil(t, caps)
}

func TestDispatchToolWithHooks_NilHooksPassesThrough(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: NewHookChain()}
	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "unknown-tool"},
	}
	result := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", call, nil, nil, nil,
	)
	assert.Contains(t, result, "error")
}

func TestDispatchToolWithHooks_PreHookVeto(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: NewHookChain()}
	taskHooks := NewHookChain()
	taskHooks.RegisterPre(PreHook{
		Name:   "blocker",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{Veto: true, Reason: "denied"}, nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash"},
	}
	result := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", call, nil, nil, taskHooks,
	)
	assert.Contains(t, result, "vetoed")
}

func TestDispatchToolWithHooks_PostHookModifiesResult(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: NewHookChain()}
	taskHooks := NewHookChain()
	taskHooks.RegisterPost(PostHook{
		Name: "tagger",
		Fn: func(_ HookContext, result string) (string, error) {
			return result + " [tagged]", nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "unknown-tool"},
	}
	result := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", call, nil, nil, taskHooks,
	)
	assert.Contains(t, result, "[tagged]")
}

func TestDispatchToolWithHooks_ShortCircuit(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: NewHookChain()}
	taskHooks := NewHookChain()
	taskHooks.RegisterPre(PreHook{
		Name:   "shortcut",
		Policy: FailOpen,
		Fn: func(HookContext) (HookVerdict, error) {
			return HookVerdict{Veto: true, ShortCircuit: true, Result: "cached"}, nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command":"ls"}`,
		},
	}
	result := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", call, nil, nil, taskHooks,
	)
	assert.Equal(t, "cached", result)
}

func TestAgenticToolsWithExtras_NilExtra(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: NewHookChain()}
	te := NewToolExecutor(nil, t.TempDir(), nil, 0)
	tools, idx := w.agenticToolsWithExtras(t.Context(), te, nil)
	assert.NotEmpty(t, tools, "built-in tools should always be present")
	assert.Nil(t, idx, "no capability adapter index without capabilities")
}

// Verify HookContext is populated correctly for scoped hooks.
func TestDispatchToolWithHooks_HookContextFields(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: NewHookChain()}

	var captured HookContext
	taskHooks := NewHookChain()
	taskHooks.RegisterPre(PreHook{
		Name:   "capture",
		Policy: FailOpen,
		Fn: func(hc HookContext) (HookVerdict, error) {
			captured = hc
			return HookVerdict{}, nil
		},
	})

	call := gateway.ToolCall{
		ID:       "call-42",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"cmd":"ls"}`},
	}

	_ = w.dispatchToolWithHooks(
		t.Context(), "sess-1", "proj-1", call, nil, nil, taskHooks,
	)

	assert.Equal(t, "bash", captured.ToolName)
	assert.Equal(t, `{"cmd":"ls"}`, captured.Args)
	assert.Equal(t, "call-42", captured.CallID)
	assert.Equal(t, "sess-1", captured.SessionID)
	assert.Equal(t, "proj-1", captured.ProjectID)
	assert.False(t, captured.Timestamp.IsZero())
	assert.True(t, captured.Timestamp.Before(time.Now().Add(time.Second)))
}
