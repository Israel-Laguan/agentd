package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "manifest.json"), []byte(content), 0o644,
	))
}

func TestParseManifest_Valid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"name": "test-plugin",
		"version": "1.0.0",
		"priority": 5,
		"hooks": {
			"pre_tool_use": [
				{"name": "block-sudo", "script": "check.sh", "policy": "fail_closed"}
			],
			"post_tool_use": [
				{"name": "log-result", "script": "log.sh", "policy": "fail_open", "timeout": "5s"}
			]
		},
		"capabilities": ["github"],
		"env": {
			"required": ["API_KEY"],
			"optional": ["DEBUG"]
		}
	}`)

	m, err := ParseManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "test-plugin", m.Name)
	assert.Equal(t, "1.0.0", m.Version)
	assert.Equal(t, 5, m.Priority)
	assert.Equal(t, dir, m.Dir)
	require.Len(t, m.Hooks.PreToolUse, 1)
	assert.Equal(t, "block-sudo", m.Hooks.PreToolUse[0].Name)
	assert.Equal(t, "check.sh", m.Hooks.PreToolUse[0].Script)
	assert.Equal(t, "fail_closed", m.Hooks.PreToolUse[0].Policy)
	require.Len(t, m.Hooks.PostToolUse, 1)
	assert.Equal(t, "log-result", m.Hooks.PostToolUse[0].Name)
	assert.Equal(t, "5s", m.Hooks.PostToolUse[0].Timeout)
	assert.Equal(t, []string{"github"}, m.Capabilities)
	assert.Equal(t, []string{"API_KEY"}, m.Env.Required)
	assert.Equal(t, []string{"DEBUG"}, m.Env.Optional)
}

func TestParseManifest_NotFound(t *testing.T) {
	_, err := ParseManifest(t.TempDir())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrManifestNotFound))
}

func TestParseManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{invalid json}`)

	_, err := ParseManifest(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrManifestInvalid))
}

func TestParseManifest_MissingName(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"version": "1.0.0"}`)

	_, err := ParseManifest(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrManifestInvalid))
	assert.Contains(t, err.Error(), "name is required")
}

func TestParseManifest_MissingVersion(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"name": "p"}`)

	_, err := ParseManifest(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrManifestInvalid))
	assert.Contains(t, err.Error(), "version is required")
}

func TestParseManifest_InvalidHookPolicy(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"name": "p",
		"version": "1.0.0",
		"hooks": {
			"pre_tool_use": [
				{"name": "h", "policy": "invalid"}
			]
		}
	}`)

	_, err := ParseManifest(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrManifestInvalid))
	assert.Contains(t, err.Error(), "fail_open or fail_closed")
}

func TestParseManifest_HookMissingName(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"name": "p",
		"version": "1.0.0",
		"hooks": {
			"post_tool_use": [{"script": "x.sh"}]
		}
	}`)

	_, err := ParseManifest(dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrManifestInvalid))
	assert.Contains(t, err.Error(), "name is required")
}

func TestParseManifest_MinimalValid(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"name": "minimal", "version": "0.1.0"}`)

	m, err := ParseManifest(dir)
	require.NoError(t, err)
	assert.Equal(t, "minimal", m.Name)
	assert.Empty(t, m.Hooks.PreToolUse)
	assert.Empty(t, m.Hooks.PostToolUse)
	assert.Empty(t, m.Capabilities)
	assert.Empty(t, m.Env.Required)
}
