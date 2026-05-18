package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (t *ToolExecutor) resolvePath(relPath string, forWrite bool) (string, error) {
	clean := filepath.Clean(relPath)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	workspaceRoot, err := filepath.EvalSymlinks(t.workspacePath)
	if err != nil {
		return "", fmt.Errorf("workspace path is invalid: %w", err)
	}
	workspaceRoot = filepath.Clean(workspaceRoot)

	candidate := filepath.Clean(filepath.Join(t.workspacePath, clean))

	if forWrite {
		parentReal, err := evalExistingAncestor(filepath.Dir(candidate))
		if err != nil {
			return "", fmt.Errorf("failed to resolve parent directory: %w", err)
		}
		if !isWithinRoot(workspaceRoot, parentReal) {
			return "", fmt.Errorf("path escapes workspace")
		}
		_, statErr := os.Lstat(candidate)
		if statErr == nil {
			targetReal, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", fmt.Errorf("failed to resolve path: %w", err)
			}
			targetReal = filepath.Clean(targetReal)
			if !isWithinRoot(workspaceRoot, targetReal) {
				return "", fmt.Errorf("path escapes workspace")
			}
			return targetReal, nil
		}
		if os.IsNotExist(statErr) {
			return candidate, nil
		}
		return "", fmt.Errorf("failed to stat path: %w", statErr)
	}

	targetReal, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	targetReal = filepath.Clean(targetReal)
	if !isWithinRoot(workspaceRoot, targetReal) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return targetReal, nil
}

func evalExistingAncestor(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		current = parent
	}
}

func isWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..")
}

// contextReader wraps an io.Reader and returns ctx.Err() on Read when cancelled.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *contextReader) Read(p []byte) (int, error) {
	if err := cr.ctx.Err(); err != nil {
		return 0, err
	}
	return cr.r.Read(p)
}

func readFileWithContext(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	limited := io.LimitReader(f, maxBytes+1)
	cr := &contextReader{ctx: ctx, r: limited}
	data, err := io.ReadAll(cr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds max size %d", maxBytes)
	}
	return data, nil
}
