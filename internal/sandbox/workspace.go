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

func samePath(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	return err == nil && rel == "."
}
