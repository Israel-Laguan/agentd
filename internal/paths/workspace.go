package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPathRequired           = errors.New("path is required")
	ErrAbsolutePathNotAllowed = errors.New("absolute paths are not allowed")
	ErrPathEscapesWorkspace   = errors.New("path escapes workspace")
)

func IsWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

func EvalWorkspaceRoot(workspacePath string) (string, error) {
	root, err := filepath.EvalSymlinks(workspacePath)
	if err != nil {
		return "", err
	}
	return filepath.Clean(root), nil
}

// ResolveWorkspaceFile resolves a relative path within a workspace and validates it
// stays within workspace boundaries. Deprecated: Use ResolveWorkspaceFileWithRoot for
// better performance by caching workspace root evaluation.
func ResolveWorkspaceFile(workspacePath, rel string) (string, error) {
	workspaceRoot, err := EvalWorkspaceRoot(workspacePath)
	if err != nil {
		return "", fmt.Errorf("workspace path is invalid: %w", err)
	}
	return ResolveWorkspaceFileWithRoot(workspacePath, workspaceRoot, rel)
}

func ResolveWorkspaceFileWithRoot(workspacePath, workspaceRoot, rel string) (string, error) {
	clean := filepath.Clean(rel)
	if clean == "." || clean == "" {
		return "", ErrPathRequired
	}
	if filepath.IsAbs(clean) {
		return "", ErrAbsolutePathNotAllowed
	}

	candidate := filepath.Clean(filepath.Join(workspacePath, clean))
	targetReal, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	targetReal = filepath.Clean(targetReal)
	if !IsWithinRoot(workspaceRoot, targetReal) {
		return "", ErrPathEscapesWorkspace
	}
	return targetReal, nil
}
