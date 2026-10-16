package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"agentd/internal/models"
)

// refuseEscapingSymlinks resolves every symlink under the workspace and
// refuses the command when one points outside it (B-042 slice 1). The check
// runs at admission, before execution, so a workspace symlink planted by
// source_path materialization or an earlier command cannot be read by
// relative name. Dangling or unreadable links cannot prove an escape and are
// skipped; run-time-built paths and /proc aliases need execution-plane
// isolation (SP-019), not this check.
func refuseEscapingSymlinks(workspace string) error {
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	var escaped string
	walkErr := filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil || escaped != "" || d == nil {
			return nil
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil
		}
		if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			escaped = path
		}
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("scan workspace symlinks: %w", walkErr)
	}
	if escaped == "" {
		return nil
	}
	rel, err := filepath.Rel(workspace, escaped)
	if err != nil {
		rel = escaped
	}
	return fmt.Errorf("%w: workspace symlink %q points outside the workspace; remove it or copy the file contents in", models.ErrSandboxViolation, rel)
}
