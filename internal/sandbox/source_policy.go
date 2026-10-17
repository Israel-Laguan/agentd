package sandbox

import (
	"errors"
	"fmt"
	"path/filepath"

	"agentd/internal/paths"
)

var (
	// ErrSourceOutsideRoot is returned for a source_path that does not resolve
	// under the configured root, or for any source_path when no root is set.
	ErrSourceOutsideRoot = errors.New("source_path is outside api.source_root")
	// ErrSourceAgentdHome is returned for the agentd home, anything inside it,
	// and any directory that contains it (a copy would carry the database and
	// every other project's workspace).
	ErrSourceAgentdHome = errors.New("source_path overlaps the agentd home")
)

// SourcePolicy limits which host directories materialize's source_path may
// seed a workspace from (decision D2, B-064). Without it, any page that could
// reach the API could copy any host directory into a workspace.
type SourcePolicy struct {
	// Root is the directory source_path must resolve under. Empty refuses
	// every source_path.
	Root string
	// AgentdHome is refused together with its contents and its parents.
	AgentdHome string
}

// Resolve follows symlinks in sourcePath and returns the real directory when
// the policy allows it. Callers copy from the returned path, so a link that is
// swapped after the check cannot redirect the copy.
func (p SourcePolicy) Resolve(sourcePath string) (string, error) {
	if p.Root == "" {
		return "", ErrSourceOutsideRoot
	}
	src, err := ValidateSourcePath(sourcePath)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(src)
	if err != nil {
		return "", fmt.Errorf("resolve source path: %w", err)
	}
	root, err := filepath.EvalSymlinks(p.Root)
	if err != nil {
		return "", fmt.Errorf("resolve api.source_root: %w", err)
	}
	if !paths.IsWithinRoot(root, real) {
		return "", ErrSourceOutsideRoot
	}
	if p.AgentdHome != "" {
		home := filepath.Clean(p.AgentdHome)
		if resolved, err := filepath.EvalSymlinks(home); err == nil {
			home = resolved
		}
		if paths.IsWithinRoot(home, real) || paths.IsWithinRoot(real, home) {
			return "", ErrSourceAgentdHome
		}
	}
	return real, nil
}
