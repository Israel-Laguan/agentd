package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
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
	_ string, chain *agenthooks.HookChain, _ *capabilities.Registry,
) error {
	s.projectCalled = true
	if s.projectErr != nil {
		return s.projectErr
	}
	chain.RegisterPre(agenthooks.PreHook{
		Name: "proj-hook", Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{}, nil
		},
	})
	return nil
}

func (s *stubMounter) MountSession(
	names []string, chain *agenthooks.HookChain, _ *capabilities.Registry,
) error {
	s.sessionCalled = true
	s.sessionNames = names
	if s.sessionErr != nil {
		return s.sessionErr
	}
	chain.RegisterPre(agenthooks.PreHook{
		Name: "sess-hook", Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{}, nil
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
	w := &Worker{hooks: agenthooks.NewHookChain()}
	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "unknown-tool"},
	}
	result, suspended := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", "", time.Now(), call, nil, nil, nil, nil,
	)
	assert.Contains(t, result.Content, "unknown tool")
	assert.False(t, suspended)
}

func TestDispatchToolWithHooks_PreHookVeto(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "blocker",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, Reason: "denied"}, nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "bash"},
	}
	result, suspended := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", "", time.Now(), call, nil, nil, taskHooks, nil,
	)
	assert.Equal(t, agenttools.ToolStatusVetoed, result.Status)
	assert.Equal(t, "denied", result.Content)
	assert.False(t, suspended)
}

func TestDispatchToolWithHooks_PostHookModifiesResult(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPost(agenthooks.PostHook{
		Name: "tagger",
		Fn: func(_ agenthooks.HookContext, result string) (string, error) {
			return result + " [tagged]", nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "unknown-tool"},
	}
	result, suspended := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", "", time.Now(), call, nil, nil, taskHooks, nil,
	)
	assert.Contains(t, result.Content, "[tagged]")
	assert.False(t, suspended)
}

func TestDispatchToolWithHooks_ShortCircuit(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "shortcut",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, ShortCircuit: true, Result: "cached"}, nil
		},
	})

	call := gateway.ToolCall{
		ID: "c1",
		Function: gateway.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command":"ls"}`,
		},
	}
	result, suspended := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", "", time.Now(), call, nil, nil, taskHooks, nil,
	)
	assert.Equal(t, "cached", result.Content)
	assert.False(t, suspended)
}

func TestDispatchToolWithHooks_ShortCircuitRunsPostHooks(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "shortcut",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, ShortCircuit: true, Result: "cached"}, nil
		},
	})
	taskHooks.RegisterPost(agenthooks.PostHook{
		Name: "tagger",
		Fn: func(_ agenthooks.HookContext, result string) (string, error) {
			return result + " [scrubbed]", nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "read"},
	}
	result, suspended := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", "", time.Now(), call, nil, nil, taskHooks, nil,
	)
	assert.Equal(t, "cached [scrubbed]", result.Content)
	assert.False(t, suspended)
}

func TestDispatchToolWithHooks_SuspendRunsPostHooks(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "suspend-gate",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, Suspend: true, Result: "pause message"}, nil
		},
	})
	taskHooks.RegisterPost(agenthooks.PostHook{
		Name: "tagger",
		Fn: func(_ agenthooks.HookContext, result string) (string, error) {
			return result + " [scrubbed]", nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "deploy"},
	}
	result, suspended := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", "", time.Now(), call, nil, nil, taskHooks, nil,
	)
	assert.Equal(t, "pause message [scrubbed]", result.Content)
	assert.True(t, suspended)
}

func TestDispatchToolWithHooks_VetoResultRunsPostHooks(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "reject-gate",
		Policy: agenthooks.FailOpen,
		Fn: func(agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			return agenthooks.HookVerdict{Veto: true, Result: "rejected"}, nil
		},
	})
	taskHooks.RegisterPost(agenthooks.PostHook{
		Name: "tagger",
		Fn: func(_ agenthooks.HookContext, result string) (string, error) {
			return result + " [tagged]", nil
		},
	})

	call := gateway.ToolCall{
		ID:       "c1",
		Function: gateway.ToolCallFunction{Name: "deploy"},
	}
	result, suspended := w.dispatchToolWithHooks(
		t.Context(), "s1", "p1", "", time.Now(), call, nil, nil, taskHooks, nil,
	)
	assert.Equal(t, "rejected [tagged]", result.Content)
	assert.False(t, suspended)
}

func TestAgenticToolsWithExtras_NilExtra(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}
	te := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	tools, idx := w.agenticToolsWithExtras(t.Context(), te, nil)
	assert.NotEmpty(t, tools, "built-in tools should always be present")
	assert.Nil(t, idx, "no capability adapter index without capabilities")
}

// Verify HookContext is populated correctly for scoped hooks.
func TestDispatchToolWithHooks_HookContextFields(t *testing.T) {
	t.Parallel()
	w := &Worker{hooks: agenthooks.NewHookChain()}

	var captured agenthooks.HookContext
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(agenthooks.PreHook{
		Name:   "capture",
		Policy: agenthooks.FailOpen,
		Fn: func(hc agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			captured = hc
			return agenthooks.HookVerdict{}, nil
		},
	})

	call := gateway.ToolCall{
		ID:       "call-42",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"cmd":"ls"}`},
	}

	taskUpdatedAt := time.Now()
	_, _ = w.dispatchToolWithHooks(
		t.Context(), "sess-1", "proj-1", "", taskUpdatedAt, call, nil, nil, taskHooks, nil,
	)

	assert.Equal(t, "bash", captured.ToolName)
	assert.Equal(t, `{"cmd":"ls"}`, captured.Args)
	assert.Equal(t, "call-42", captured.CallID)
	assert.Equal(t, "sess-1", captured.SessionID)
	assert.Equal(t, "proj-1", captured.ProjectID)
	assert.False(t, captured.Timestamp.IsZero())
	assert.True(t, captured.Timestamp.Before(time.Now().Add(time.Second)))
	assert.Equal(t, taskUpdatedAt, captured.TaskUpdatedAt)
}
