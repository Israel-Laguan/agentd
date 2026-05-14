package plugin

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"agentd/internal/capabilities"
	"agentd/internal/queue/worker"
)

// LoadResult contains the artefacts produced by a successful plugin
// load: the parsed manifest and the hooks ready for registration.
type LoadResult struct {
	Manifest  Manifest
	PreHooks  []worker.PreHook
	PostHooks []worker.PostHook
	Scope     PluginScope
}

// PluginScope controls which activation level a plugin directory
// represents. Global plugins apply to every session; project plugins
// are loaded from a workspace-local directory and only apply to tasks
// within that project; session plugins are activated by name from an
// AgentProfile.
type PluginScope string

const (
	// ScopeGlobal marks plugins loaded from the config-level directory.
	ScopeGlobal PluginScope = "global"
	// ScopeProject marks plugins loaded from a project workspace.
	ScopeProject PluginScope = "project"
	// ScopeSession marks plugins activated via AgentProfile.Plugins.
	ScopeSession PluginScope = "session"
)

// PluginLoader scans a directory for plugin sub-directories, parses
// their manifests, validates environment variables, and produces
// hooks and capability registrations.
type PluginLoader struct {
	pluginsDir string
	scope      PluginScope
	envLookup  func(string) (string, bool)
}

// NewPluginLoader creates a PluginLoader that scans the given directory.
// The loader defaults to ScopeGlobal.
func NewPluginLoader(pluginsDir string) *PluginLoader {
	return &PluginLoader{
		pluginsDir: pluginsDir,
		scope:      ScopeGlobal,
		envLookup:  os.LookupEnv,
	}
}

// NewScopedPluginLoader creates a PluginLoader with an explicit scope.
func NewScopedPluginLoader(pluginsDir string, scope PluginScope) *PluginLoader {
	return &PluginLoader{
		pluginsDir: pluginsDir,
		scope:      scope,
		envLookup:  os.LookupEnv,
	}
}

// LoadAll scans the plugins directory, parses each plugin, validates
// env vars, and returns ordered LoadResults. Plugins are ordered by
// numeric directory prefix (e.g. 01-security/) then by manifest
// priority field, then alphabetically.
func (pl *PluginLoader) LoadAll() ([]LoadResult, error) {
	entries, err := pl.readPluginDirs()
	if err != nil {
		return nil, err
	}

	var results []LoadResult
	for _, entry := range entries {
		dir := filepath.Join(pl.pluginsDir, entry.Name())
		res, err := pl.loadOne(dir)
		if err != nil {
			return nil, fmt.Errorf("plugin %s: %w", entry.Name(), err)
		}
		res.Scope = pl.scope
		results = append(results, res)
	}

	sortResults(results)
	return results, nil
}

// MountAll loads all plugins from the directory and registers their
// hooks into the HookChain and capabilities into the Registry.
func (pl *PluginLoader) MountAll(
	chain *worker.HookChain, registry *capabilities.Registry,
) ([]Manifest, error) {
	results, err := pl.LoadAll()
	if err != nil {
		return nil, err
	}
	return mountResults(results, pl.scope, chain, registry), nil
}

func mountResults(
	results []LoadResult, scope PluginScope,
	chain *worker.HookChain, registry *capabilities.Registry,
) []Manifest {
	var manifests []Manifest
	for _, r := range results {
		for _, h := range r.PreHooks {
			chain.RegisterPre(h)
		}
		for _, h := range r.PostHooks {
			chain.RegisterPost(h)
		}
		registerCapabilities(registry, r.Manifest)
		manifests = append(manifests, r.Manifest)
		slog.Info("plugin loaded",
			"name", r.Manifest.Name,
			"version", r.Manifest.Version,
			"scope", string(scope),
			"pre_hooks", len(r.PreHooks),
			"post_hooks", len(r.PostHooks),
			"capabilities", len(r.Manifest.Capabilities),
		)
	}
	return manifests
}

func (pl *PluginLoader) readPluginDirs() ([]os.DirEntry, error) {
	entries, err := os.ReadDir(pl.pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			if pl.scope != ScopeGlobal {
				return nil, nil
			}
			return nil, fmt.Errorf("%w: %s", ErrPluginDirNotFound, pl.pluginsDir)
		}
		return nil, fmt.Errorf("read plugins directory: %w", err)
	}

	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		}
	}
	return dirs, nil
}

func (pl *PluginLoader) loadOne(dir string) (LoadResult, error) {
	m, err := ParseManifest(dir)
	if err != nil {
		return LoadResult{}, err
	}
	if err := pl.validateEnv(m); err != nil {
		return LoadResult{}, err
	}
	return buildHooks(m), nil
}

func (pl *PluginLoader) validateEnv(m Manifest) error {
	var missing []string
	for _, key := range m.Env.Required {
		if _, ok := pl.envLookup(key); !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"%w: plugin %q requires: %s",
			ErrMissingEnvVar, m.Name, strings.Join(missing, ", "),
		)
	}
	return nil
}

func buildHooks(m Manifest) LoadResult {
	var pre []worker.PreHook
	for _, entry := range m.Hooks.PreToolUse {
		if entry.Script != "" {
			pre = append(pre, ShellPreHook(entry, m.Dir))
		}
	}

	var post []worker.PostHook
	for _, entry := range m.Hooks.PostToolUse {
		if entry.Script != "" {
			post = append(post, ShellPostHook(entry, m.Dir))
		}
	}

	return LoadResult{
		Manifest:  m,
		PreHooks:  pre,
		PostHooks: post,
	}
}

func registerCapabilities(
	registry *capabilities.Registry, m Manifest,
) {
	for _, cap := range m.Capabilities {
		adapter, ok := loadCapabilityAdapter(m, cap)
		if !ok {
			slog.Warn("capability adapter not found",
				"plugin", m.Name, "capability", cap,
			)
			continue
		}
		registry.Register(cap, adapter)
	}
}

// loadCapabilityAdapter attempts to load a compiled adapter package
// for the given capability name. This is a hook point for Go plugin
// loading via compiled adapter packages using the CapabilityAdapter
// interface. Returns false if no adapter is available for the name.
func loadCapabilityAdapter(
	_ Manifest, _ string,
) (capabilities.CapabilityAdapter, bool) {
	return nil, false
}

// sortResults orders plugins by directory numeric prefix, then by
// manifest priority, then alphabetically by name.
func sortResults(results []LoadResult) {
	sort.SliceStable(results, func(i, j int) bool {
		pi := dirPrefix(results[i].Manifest.Dir)
		pj := dirPrefix(results[j].Manifest.Dir)
		if pi != pj {
			return pi < pj
		}
		if results[i].Manifest.Priority != results[j].Manifest.Priority {
			return results[i].Manifest.Priority < results[j].Manifest.Priority
		}
		return results[i].Manifest.Name < results[j].Manifest.Name
	})
}

// dirPrefix extracts the leading numeric portion from a directory name
// for ordering (e.g. "01-security" → 1). Directories without a numeric
// prefix sort after numbered ones (max int).
func dirPrefix(dir string) int {
	base := filepath.Base(dir)
	parts := strings.SplitN(base, "-", 2)
	if len(parts) < 2 {
		return maxDirPrefix
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return maxDirPrefix
	}
	return n
}

const maxDirPrefix = 1<<31 - 1


