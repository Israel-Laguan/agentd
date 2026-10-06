package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentd/internal/models"
)

// WorkspaceManager owns physical project workspace paths.
type WorkspaceManager interface {
	EnsureProjectDir(ctx context.Context, projectID string) (string, error)
	ProjectDir(projectID string) string
	SecureDelete(ctx context.Context, projectID string) error
	// StageSeed copies sourcePath into a staging area before any project exists,
	// so a copy that fails partway leaves nothing behind. The caller persists the
	// project, then calls StagedSeed.Promote, and Discards on every other path.
	StageSeed(ctx context.Context, sourcePath string) (StagedSeed, error)
	// IsWorkspacePopulated returns true if the workspace contains at least one
	// file or subdirectory.
	IsWorkspacePopulated(ctx context.Context, projectID string) (bool, error)
}

// FSWorkspaceManager creates project directories under Root.
type FSWorkspaceManager struct {
	Root string
}

var _ WorkspaceManager = (*FSWorkspaceManager)(nil)

// EnsureProjectDir creates the project workspace and returns its absolute path.
func (m *FSWorkspaceManager) EnsureProjectDir(ctx context.Context, projectID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir := m.ProjectDir(projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create project workspace %s: %w", dir, err)
	}
	return filepath.Abs(dir)
}

// ProjectDir resolves a project workspace path without touching the filesystem.
func (m *FSWorkspaceManager) ProjectDir(projectID string) string {
	return filepath.Join(m.Root, projectID)
}

// SecureDelete removes a project directory only after confirming it is jailed
// under the configured root.
func (m *FSWorkspaceManager) SecureDelete(ctx context.Context, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := JailPath(m.Root, m.ProjectDir(projectID))
	if err != nil {
		return err
	}
	if filepath.Clean(dir) == filepath.Clean(m.Root) {
		return fmt.Errorf("%w: refusing to delete workspace root", models.ErrSandboxViolation)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete project workspace %s: %w", dir, err)
	}
	return nil
}

// ResetProjectDir empties a project's workspace but keeps the directory itself,
// so the persisted workspace path stays valid for the re-run. Like SecureDelete
// it acts only on a path jailed under the configured root and refuses the root.
func (m *FSWorkspaceManager) ResetProjectDir(ctx context.Context, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := JailPath(m.Root, m.ProjectDir(projectID))
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(m.Root)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	if filepath.Clean(dir) == filepath.Clean(root) {
		return fmt.Errorf("%w: refusing to reset workspace root", models.ErrSandboxViolation)
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return fmt.Errorf("locate project workspace under root: %w", err)
	}
	// Enumerate and delete through a directory handle rooted at the workspace root
	// rather than through the resolved path. A concurrent writer can replace the
	// project directory with a symlink between the jail check above and the
	// removals below, and os.RemoveAll on a path then follows it out of the root.
	// os.Root resolves every component under the root it was opened on, so the
	// removal cannot leave the jail however the path is rewritten underneath us.
	jail, err := os.OpenRoot(m.Root)
	if err != nil {
		return fmt.Errorf("open workspace root %s: %w", m.Root, err)
	}
	defer func() { _ = jail.Close() }()
	projRoot, err := jail.OpenRoot(rel)
	if err != nil {
		return fmt.Errorf("open project workspace %s: %w", dir, err)
	}
	defer func() { _ = projRoot.Close() }()
	handle, err := projRoot.Open(".")
	if err != nil {
		return fmt.Errorf("open project workspace %s: %w", dir, err)
	}
	defer func() { _ = handle.Close() }()
	entries, err := handle.ReadDir(-1)
	if err != nil {
		return fmt.Errorf("read project workspace %s: %w", dir, err)
	}
	for _, entry := range entries {
		if err := projRoot.RemoveAll(entry.Name()); err != nil {
			return fmt.Errorf("reset project workspace %s: %w", dir, err)
		}
	}
	return nil
}

// JailPath resolves requested and verifies it remains inside workspaceRoot.
func JailPath(workspaceRoot, requested string) (string, error) {
	root, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	path, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return "", fmt.Errorf("resolve requested path: %w", err)
	}
	if filepath.Clean(path) != filepath.Clean(root) && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s escapes %s", models.ErrSandboxViolation, path, root)
	}
	return path, nil
}

// ValidateSourcePath resolves sourcePath to an absolute path and checks that
// it names an existing directory, returning it ready to copy from. It is
// exported so callers can reject a bad source_path *before* they persist any
// board state. A copy failure is covered separately by StageSeed, which copies
// before the project exists.
//
// It is deliberately not on the WorkspaceManager interface — it needs no
// receiver, and adding it there would force every fake implementation to grow
// a method that never touches workspace state.
func ValidateSourcePath(sourcePath string) (string, error) {
	src, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", fmt.Errorf("resolve source path: %w", err)
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("stat source path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source_path must be a directory: %s", src)
	}
	return src, nil
}

// IsWorkspacePopulated returns true if the project workspace contains at
// least one file or subdirectory.
func (m *FSWorkspaceManager) IsWorkspacePopulated(_ context.Context, projectID string) (bool, error) {
	dir := m.ProjectDir(projectID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return len(entries) > 0, nil
}
