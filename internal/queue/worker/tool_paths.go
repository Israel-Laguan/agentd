package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"agentd/internal/paths"
)

func (t *ToolExecutor) getWorkspaceRoot() (string, error) {
	t.workspaceRootOnce.Do(func() {
		root, err := paths.EvalWorkspaceRoot(t.workspacePath)
		if err != nil {
			t.workspaceRootErr = fmt.Errorf("workspace path is invalid: %w", err)
			return
		}
		t.workspaceRoot = root
	})
	return t.workspaceRoot, t.workspaceRootErr
}

func (t *ToolExecutor) resolvePath(relPath string, forWrite bool) (string, error) {
	clean := filepath.Clean(relPath)
	if clean == "." || clean == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	workspaceRoot, err := t.getWorkspaceRoot()
	if err != nil {
		return "", err
	}

	candidate := filepath.Clean(filepath.Join(t.workspacePath, clean))

	if forWrite {
		parentReal, err := evalExistingAncestor(filepath.Dir(candidate))
		if err != nil {
			return "", fmt.Errorf("failed to resolve parent directory: %w", err)
		}
		if !paths.IsWithinRoot(workspaceRoot, parentReal) {
			return "", fmt.Errorf("path escapes workspace")
		}
		_, statErr := os.Lstat(candidate)
		if statErr == nil {
			targetReal, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return "", fmt.Errorf("failed to resolve path: %w", err)
			}
			targetReal = filepath.Clean(targetReal)
			if !paths.IsWithinRoot(workspaceRoot, targetReal) {
				return "", fmt.Errorf("path escapes workspace")
			}
			return targetReal, nil
		}
		if os.IsNotExist(statErr) {
			return candidate, nil
		}
		return "", fmt.Errorf("failed to stat path: %w", statErr)
	}

	return paths.ResolveWorkspaceFileWithRoot(t.workspacePath, workspaceRoot, clean)
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

func readFileWithContext(ctx context.Context, path string, maxBytes int64, preInfo os.FileInfo) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if preInfo != nil && preInfo.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds max size %d", maxBytes)
	}

	openFlags := os.O_RDONLY
	if runtime.GOOS != "windows" {
		openFlags |= syscall.O_NONBLOCK
	}
	f, err := os.OpenFile(path, openFlags, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds max size %d", maxBytes)
	}

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
