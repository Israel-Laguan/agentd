package sandbox

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// stagingPrefix names the scratch directories StageSeed makes under the
// workspace root. Nothing enumerates the root, and a crash between StageSeed and
// Promote leaves at most one dot-directory behind, never a half-seeded project.
const stagingPrefix = ".seed-"

// StagedSeed is a finished copy of a source_path that is not yet anyone's
// workspace.
type StagedSeed interface {
	// Promote makes the copy the workspace of projectID. The project's directory
	// must already exist (EnsureProjectDir) and be empty.
	Promote(ctx context.Context, projectID string) error
	// Discard removes the copy. It is safe to call after Promote and repeatedly.
	Discard()
}

type stagedSeed struct {
	m   *FSWorkspaceManager
	dir string
}

// StageSeed copies the contents of sourcePath into a fresh directory under the
// workspace root, by a recursive walk that skips symlinks. Doing the copy before
// the project is persisted (B-021) means a failure partway, which a path check
// cannot foresee, leaves no project row and no half-populated directory.
func (m *FSWorkspaceManager) StageSeed(ctx context.Context, sourcePath string) (StagedSeed, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	src, err := ValidateSourcePath(sourcePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace root %s: %w", m.Root, err)
	}
	stage, err := os.MkdirTemp(m.Root, stagingPrefix)
	if err != nil {
		return nil, fmt.Errorf("create seed staging directory: %w", err)
	}
	if err := copyTree(ctx, src, stage); err != nil {
		_ = os.RemoveAll(stage)
		return nil, err
	}
	return &stagedSeed{m: m, dir: stage}, nil
}

func (s *stagedSeed) Promote(ctx context.Context, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.dir == "" {
		return fmt.Errorf("staged seed already promoted or discarded")
	}
	dest := s.m.ProjectDir(projectID)
	if _, err := JailPath(s.m.Root, dest); err != nil {
		return err
	}
	// os.Remove only deletes an empty directory, so a workspace that already
	// holds anything is refused rather than overwritten.
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("replace empty workspace %s: %w", dest, err)
	}
	if err := os.Rename(s.dir, dest); err != nil {
		_ = os.MkdirAll(dest, 0o755)
		return fmt.Errorf("move staged seed into %s: %w", dest, err)
	}
	s.dir = ""
	return nil
}

func (s *stagedSeed) Discard() {
	if s.dir == "" {
		return
	}
	_ = os.RemoveAll(s.dir)
	s.dir = ""
}

func copyTree(ctx context.Context, src, destDir string) error {
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
