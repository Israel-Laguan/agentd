package plugin

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"agentd/internal/capabilities"
	"agentd/internal/queue/worker"
)

// LoadByNames loads only the plugins whose manifest name matches one of
// the provided names. This is used for session-scoped activation where
// an AgentProfile lists specific plugin names.
func (pl *PluginLoader) LoadByNames(names []string) ([]LoadResult, error) {
	if len(names) == 0 {
		return nil, nil
	}
	entries, err := pl.readPluginDirs()
	if err != nil {
		return nil, err
	}

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}

	var results []LoadResult
	for _, entry := range entries {
		dir := filepath.Join(pl.pluginsDir, entry.Name())
		res, err := pl.loadOne(dir)
		if err != nil {
			slog.Warn("failed to load session plugin, skipping",
				"plugin_dir", entry.Name(),
				"error", err,
			)
			continue
		}
		if !wanted[res.Manifest.Name] {
			continue
		}
		res.Scope = pl.scope
		results = append(results, res)
		delete(wanted, res.Manifest.Name)
	}

	if len(wanted) > 0 {
		var missing []string
		for n := range wanted {
			missing = append(missing, n)
		}
		sort.Strings(missing)
		slog.Warn("session-scoped plugins not found",
			"missing", strings.Join(missing, ", "),
		)
	}

	sortResults(results)
	return results, nil
}

// MountByNames loads plugins matching the given names and registers
// their hooks and capabilities.
func (pl *PluginLoader) MountByNames(
	names []string,
	chain *worker.HookChain, registry *capabilities.Registry,
) ([]Manifest, error) {
	results, err := pl.LoadByNames(names)
	if err != nil {
		return nil, err
	}
	return mountResults(results, pl.scope, chain, registry), nil
}
