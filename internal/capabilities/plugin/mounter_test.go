package plugin

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentd/internal/agent/hooks"
	"agentd/internal/capabilities"
)

func TestMounter_MountProject_LoadsFromWorkspace(t *testing.T) {
	t.Parallel()
	ws := t.TempDir()
	pluginDir := filepath.Join(ws, ProjectPluginsSubdir, "proj-plug")
	writeManifest(t, pluginDir, `{"name":"proj","version":"1.0.0"}`)

	m := NewMounter(t.TempDir())
	chain := hooks.NewHookChain()
	registry := capabilities.NewRegistry()

	err := m.MountProject(ws, chain, registry)
	require.NoError(t, err)
}

func TestMounter_MountProject_MissingDirIsNonFatal(t *testing.T) {
	t.Parallel()
	m := NewMounter(t.TempDir())
	chain := hooks.NewHookChain()
	registry := capabilities.NewRegistry()

	err := m.MountProject(filepath.Join(t.TempDir(), "no-ws"), chain, registry)
	require.NoError(t, err)
}

func TestMounter_MountSession_LoadsByName(t *testing.T) {
	t.Parallel()
	globalDir := t.TempDir()
	seedPlugin(t, globalDir, "plug-a", `{"name":"alpha","version":"1.0.0"}`)
	seedPlugin(t, globalDir, "plug-b", `{"name":"beta","version":"1.0.0"}`)

	m := NewMounter(globalDir)
	chain := hooks.NewHookChain()
	registry := capabilities.NewRegistry()

	err := m.MountSession([]string{"alpha"}, chain, registry)
	require.NoError(t, err)
}

func TestMounter_MountSession_EmptyNamesIsNoop(t *testing.T) {
	t.Parallel()
	m := NewMounter(t.TempDir())
	chain := hooks.NewHookChain()
	registry := capabilities.NewRegistry()

	err := m.MountSession(nil, chain, registry)
	require.NoError(t, err)
}

func TestMounter_ImplementsPluginMounter(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, NewMounter(""))
}
