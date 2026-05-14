package worker

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// InstructionLoader reads instruction files from the filesystem.
type InstructionLoader struct {
	// ProjectFile is the default relative path within a workspace.
	// e.g., ".agentd/AGENTS.md"
	ProjectFile string

	// UserPreferencesPath is the absolute path to the user preferences file.
	// e.g., "/home/user/.agentd/prefs.yaml"
	UserPreferencesPath string
}

// LoadProjectInstructions reads and parses a project-level instruction file.
// It first tries <workspace>/<overridePath> (when overridePath is non-empty),
// then <workspace>/<loader.ProjectFile>, then <workspace>/AGENTS.md as a
// fallback. Returns (nil, nil) if no instruction file exists.
func (l *InstructionLoader) LoadProjectInstructions(workspacePath, overridePath string) (*ProjectInstructions, error) {
	candidates := l.instructionCandidates(workspacePath, overridePath)
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read project instructions %s: %w", path, err)
		}
		slog.Debug("loaded project instructions", "path", path)
		pi := parseAgentsMD(string(data))
		return pi, nil
	}
	return nil, nil
}

// instructionCandidates returns an ordered list of candidate file paths
// for project instructions. The first existing file wins.
func (l *InstructionLoader) instructionCandidates(workspacePath, overridePath string) []string {
	var candidates []string

	addIfLocal := func(rel string) {
		if rel == "" {
			return
		}
		clean := filepath.Clean(rel)
		full := clean
		if !filepath.IsAbs(clean) {
			full = filepath.Join(workspacePath, clean)
		}

		relToBase, err := filepath.Rel(workspacePath, full)
		if err != nil || relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(filepath.Separator)) {
			slog.Warn("instruction path escape attempt blocked", "path", rel, "workspace", workspacePath)
			return
		}
		candidates = append(candidates, full)
	}

	if overridePath != "" {
		addIfLocal(overridePath)
	}
	if l.ProjectFile != "" {
		addIfLocal(l.ProjectFile)
	}
	// Fallback: AGENTS.md at workspace root
	addIfLocal("AGENTS.md")

	return candidates
}

// LoadUserPreferences reads user preferences from the configured YAML path.
// Returns (nil, nil) if the file does not exist.
func (l *InstructionLoader) LoadUserPreferences() (*UserPreferences, error) {
	if l.UserPreferencesPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(l.UserPreferencesPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user preferences %s: %w", l.UserPreferencesPath, err)
	}
	var prefs UserPreferences
	if err := yaml.Unmarshal(data, &prefs); err != nil {
		return nil, fmt.Errorf("parse user preferences %s: %w", l.UserPreferencesPath, err)
	}
	slog.Debug("loaded user preferences", "path", l.UserPreferencesPath, "count", len(prefs.Entries))
	return &prefs, nil
}
