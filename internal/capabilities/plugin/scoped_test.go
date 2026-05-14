package plugin

import (
	"errors"
	"path/filepath"
	"testing"

	"agentd/internal/capabilities"
	"agentd/internal/queue/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScopedPluginLoader_SetsScope(t *testing.T) {
	t.Parallel()
	loader := NewScopedPluginLoader("/tmp/fake", ScopeProject)
	assert.Equal(t, ScopeProject, loader.scope)
}

func TestScopedLoader_LoadAllTagsScope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedPlugin(t, dir, "p1", `{"name":"p1","version":"1.0.0"}`)

	loader := NewScopedPluginLoader(dir, ScopeProject)
	loader.envLookup = func(string) (string, bool) { return "", true }

	results, err := loader.LoadAll()
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, ScopeProject, results[0].Scope)
}

func TestScopedLoader_GlobalScopeErrorsOnMissingDir(t *testing.T) {
	t.Parallel()
	loader := NewScopedPluginLoader(filepath.Join(t.TempDir(), "nope"), ScopeGlobal)
	_, err := loader.LoadAll()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPluginDirNotFound))
}

func TestScopedLoader_ProjectScopeReturnsEmptyOnMissingDir(t *testing.T) {
	t.Parallel()
	loader := NewScopedPluginLoader(filepath.Join(t.TempDir(), "nope"), ScopeProject)
	results, err := loader.LoadAll()
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestScopedLoader_SessionScopeReturnsEmptyOnMissingDir(t *testing.T) {
	t.Parallel()
	loader := NewScopedPluginLoader(filepath.Join(t.TempDir(), "nope"), ScopeSession)
	results, err := loader.LoadAll()
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestLoadByNames_SelectsMatchingPlugins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedPlugin(t, dir, "a", `{"name":"alpha","version":"1.0.0"}`)
	seedPlugin(t, dir, "b", `{"name":"beta","version":"1.0.0"}`)
	seedPlugin(t, dir, "c", `{"name":"gamma","version":"1.0.0"}`)

	loader := NewScopedPluginLoader(dir, ScopeSession)
	loader.envLookup = func(string) (string, bool) { return "", true }

	results, err := loader.LoadByNames([]string{"alpha", "gamma"})
	require.NoError(t, err)
	require.Len(t, results, 2)

	names := []string{results[0].Manifest.Name, results[1].Manifest.Name}
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "gamma")
	assert.Equal(t, ScopeSession, results[0].Scope)
}

func TestLoadByNames_EmptyNamesReturnsNil(t *testing.T) {
	t.Parallel()
	loader := NewScopedPluginLoader(t.TempDir(), ScopeSession)
	results, err := loader.LoadByNames(nil)
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestLoadByNames_MissingPluginsLogsWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedPlugin(t, dir, "a", `{"name":"alpha","version":"1.0.0"}`)

	loader := NewScopedPluginLoader(dir, ScopeSession)
	loader.envLookup = func(string) (string, bool) { return "", true }

	results, err := loader.LoadByNames([]string{"alpha", "nonexistent"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "alpha", results[0].Manifest.Name)
}

func TestMountByNames_RegistersHooksAndCaps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seedPlugin(t, dir, "s1", `{
		"name":"sess-plug",
		"version":"1.0.0",
		"capabilities":["custom-cap"]
	}`)

	loader := NewScopedPluginLoader(dir, ScopeSession)
	loader.envLookup = func(string) (string, bool) { return "", true }

	chain := worker.NewHookChain()
	registry := capabilities.NewRegistry()
	manifests, err := loader.MountByNames(
		[]string{"sess-plug"}, chain, registry,
	)
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, "sess-plug", manifests[0].Name)
}
