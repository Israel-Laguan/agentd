package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/queue/worker"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedPlugin(t *testing.T, base, dirName, manifest string) {
	t.Helper()
	dir := filepath.Join(base, dirName)
	writeManifest(t, dir, manifest)
}

func TestPluginLoader_MissingEnvVar(t *testing.T) {
	dir := t.TempDir()
	seedPlugin(t, dir, "my-plugin", `{
		"name": "needs-key",
		"version": "1.0.0",
		"env": {"required": ["TEST_KEY"]}
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(key string) (string, bool) {
		return "", false
	}

	_, err := loader.LoadAll()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMissingEnvVar))
	assert.Contains(t, err.Error(), "TEST_KEY")
}

func TestPluginLoader_EnvVarPresent(t *testing.T) {
	dir := t.TempDir()
	seedPlugin(t, dir, "my-plugin", `{
		"name": "has-key",
		"version": "1.0.0",
		"env": {"required": ["TEST_KEY"]}
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(key string) (string, bool) {
		if key == "TEST_KEY" {
			return "val", true
		}
		return "", false
	}

	results, err := loader.LoadAll()
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "has-key", results[0].Manifest.Name)
}

func TestPluginLoader_HooksRegisteredIntoChain(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "01-security")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	script := filepath.Join(pluginDir, "check.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755))

	writeManifest(t, pluginDir, `{
		"name": "security",
		"version": "1.0.0",
		"hooks": {
			"pre_tool_use": [
				{"name": "sec-check", "script": "check.sh", "policy": "fail_closed"}
			]
		}
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(string) (string, bool) { return "", true }

	chain := worker.NewHookChain()
	registry := capabilities.NewRegistry()
	manifests, err := loader.MountAll(chain, registry)
	require.NoError(t, err)
	require.Len(t, manifests, 1)
	assert.Equal(t, "security", manifests[0].Name)

	verdict := chain.RunPre(worker.HookContext{
		ToolName:  "bash",
		Args:      `{"command":"ls"}`,
		SessionID: "s1",
		Timestamp: time.Now(),
	})
	assert.False(t, verdict.Veto, "allow script should not veto")
}

func TestPluginLoader_CapabilitiesRegistered(t *testing.T) {
	dir := t.TempDir()
	seedPlugin(t, dir, "cap-plugin", `{
		"name": "cap-test",
		"version": "1.0.0",
		"capabilities": ["custom-tool"]
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(string) (string, bool) { return "", true }

	chain := worker.NewHookChain()
	registry := capabilities.NewRegistry()
	_, err := loader.MountAll(chain, registry)
	require.NoError(t, err)

	_, ok := registry.GetAdapter("custom-tool")
	assert.False(t, ok, "no compiled adapter available; should not be registered")
}

func TestPluginLoader_OrderByDirPrefix(t *testing.T) {
	dir := t.TempDir()
	seedPlugin(t, dir, "10-late", `{
		"name": "late",
		"version": "1.0.0"
	}`)
	seedPlugin(t, dir, "01-early", `{
		"name": "early",
		"version": "1.0.0"
	}`)
	seedPlugin(t, dir, "05-middle", `{
		"name": "middle",
		"version": "1.0.0"
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(string) (string, bool) { return "", true }

	results, err := loader.LoadAll()
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "early", results[0].Manifest.Name)
	assert.Equal(t, "middle", results[1].Manifest.Name)
	assert.Equal(t, "late", results[2].Manifest.Name)
}

func TestPluginLoader_OrderByPriority(t *testing.T) {
	dir := t.TempDir()
	seedPlugin(t, dir, "plugin-a", `{
		"name": "a-low",
		"version": "1.0.0",
		"priority": 10
	}`)
	seedPlugin(t, dir, "plugin-b", `{
		"name": "b-high",
		"version": "1.0.0",
		"priority": 1
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(string) (string, bool) { return "", true }

	results, err := loader.LoadAll()
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "b-high", results[0].Manifest.Name)
	assert.Equal(t, "a-low", results[1].Manifest.Name)
}

func TestPluginLoader_DirNotFound(t *testing.T) {
	missingDir := filepath.Join(t.TempDir(), "does-not-exist")
	loader := NewPluginLoader(missingDir)
	_, err := loader.LoadAll()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPluginDirNotFound))
}

func TestPluginLoader_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	loader := NewPluginLoader(dir)
	results, err := loader.LoadAll()
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestPluginLoader_MultipleEnvVarsMissing(t *testing.T) {
	dir := t.TempDir()
	seedPlugin(t, dir, "multi", `{
		"name": "multi",
		"version": "1.0.0",
		"env": {"required": ["KEY_A", "KEY_B"]}
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(string) (string, bool) { return "", false }

	_, err := loader.LoadAll()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "KEY_A")
	assert.Contains(t, err.Error(), "KEY_B")
}

func TestPluginLoader_SkipsNonDirs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "not-a-plugin.txt"), []byte("hi"), 0o644,
	))
	seedPlugin(t, dir, "real-plugin", `{
		"name": "real",
		"version": "1.0.0"
	}`)

	loader := NewPluginLoader(dir)
	loader.envLookup = func(string) (string, bool) { return "", true }

	results, err := loader.LoadAll()
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "real", results[0].Manifest.Name)
}
