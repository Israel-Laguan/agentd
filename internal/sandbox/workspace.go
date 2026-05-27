package sandbox

import (
	"context"
	"fmt"
	"io/fs"
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
	// SeedFromPath copies content from sourcePath into the project workspace.
	// The workspace directory must already exist (via EnsureProjectDir).
	SeedFromPath(ctx context.Context, projectID, sourcePath string) error
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
	if samePath(dir, m.Root) {
		return fmt.Errorf("%w: refusing to delete workspace root", models.ErrSandboxViolation)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("delete project workspace %s: %w", dir, err)
	}
	return nil
}

// JailPath resolves requested and verifies it remains inside workspaceRoot.
func JailPath(workspaceRoot, requested string) (string, error) {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	path, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolve requested path: %w", err)
	}
	if !samePath(path, root) && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s escapes %s", models.ErrSandboxViolation, path, root)
	}
	return path, nil
}

// SeedFromPath copies the contents of sourcePath into the project workspace
// using a recursive filesystem walk. It validates that sourcePath exists and
// that the destination is within the jailed workspace root.
func (m *FSWorkspaceManager) SeedFromPath(ctx context.Context, projectID, sourcePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	src, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve source path: %w", err)
	}
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("source_path must be a directory: %s", src)
	}
	destDir := m.ProjectDir(projectID)
	if _, err := JailPath(m.Root, destDir); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, info.Mode().Perm())
	})
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

func samePath(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	return err == nil && rel == "."
}
