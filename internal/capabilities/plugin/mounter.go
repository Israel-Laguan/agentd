package plugin

import (
	"path/filepath"

	"agentd/internal/agent/hooks"
	"agentd/internal/capabilities"
)

// ProjectPluginsSubdir is the relative path within a project workspace
// where project-scoped plugins are stored.
const ProjectPluginsSubdir = ".agentd/plugins"

// Mounter uses PluginLoader instances
// for project-scoped and session-scoped plugin activation.
type Mounter struct {
	globalPluginsDir string
}

// NewMounter creates a Mounter that resolves session-scoped plugins
// from the given global plugins directory.
func NewMounter(globalPluginsDir string) *Mounter {
	return &Mounter{globalPluginsDir: globalPluginsDir}
}

// MountProject loads plugins from <workspacePath>/.agentd/plugins/ and
// registers them into the provided HookChain and Registry.
func (m *Mounter) MountProject(
	workspacePath string,
	chain *hooks.HookChain, registry *capabilities.Registry,
) error {
	dir := filepath.Join(workspacePath, ProjectPluginsSubdir)
	loader := NewScopedPluginLoader(dir, ScopeProject)
	_, err := loader.MountAll(chain, registry)
	return err
}

// MountSession loads plugins by name from the global plugins directory
// and registers them into the provided HookChain and Registry.
func (m *Mounter) MountSession(
	names []string,
	chain *hooks.HookChain, registry *capabilities.Registry,
) error {
	loader := NewScopedPluginLoader(m.globalPluginsDir, ScopeSession)
	_, err := loader.MountByNames(names, chain, registry)
	return err
}
