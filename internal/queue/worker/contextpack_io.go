package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// PackFilePath returns the workspace-relative file name for a context pack
// scoped to the given origin (parent) task at the given version. An empty
// parentTaskID falls back to the legacy unscoped name.
func PackFilePath(parentTaskID string, version int) string {
	v := version
	if v <= 0 {
		v = ContextPackVersion
	}
	if parentTaskID == "" {
		return fmt.Sprintf(legacyContextPackFileName, v)
	}
	return fmt.Sprintf(ContextPackFileName, parentTaskID, v)
}

// WriteContextPack serializes the pack to a JSON file in the workspace dir.
func WriteContextPack(workspace string, cp *ContextPack) error {
	cp.Budget.PathCount = len(cp.Paths)
	cp.Budget.CharCount = cp.CharCount()
	if err := cp.Validate(); err != nil {
		return fmt.Errorf("write context pack: %w", err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal context pack: %w", err)
	}
	name := PackFilePath(cp.ParentTaskID, cp.Version)
	if err := writeFileAtomic(workspace, name, data); err != nil {
		return fmt.Errorf("write context pack %s: %w", name, err)
	}
	return nil
}

// writeFileAtomic renames a temp file over dir/name. The rename replaces a
// symlink planted at the target instead of writing through it.
func writeFileAtomic(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".context-pack-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // best-effort cleanup; gone after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // the write error is the one worth returning
		return fmt.Errorf("write temp file: %w", err)
	}
	// Chmod the open handle: the sandbox can swap the temp name for a symlink,
	// and a path-based chmod would follow it.
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close() // the chmod error is the one worth returning
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// ReadContextPack reads and unmarshals a context pack from a JSON file.
// Budget counters missing from older v1 packs are backfilled before
// validation so previously written packs remain readable. Explicit
// zero counters are preserved so Validate can reject them.
func ReadContextPack(path string) (*ContextPack, error) {
	data, err := readFileNoEscape(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("read context pack: %w", err)
	}
	return unmarshalContextPack(data)
}

// ReadContextPackWithFallback reads a context pack from the scoped path.
// If the scoped file does not exist, it falls back to the legacy unscoped
// path so pipelines that were started before the filename migration can
// still be resumed.
func ReadContextPackWithFallback(scopedPath, legacyPath string) (*ContextPack, error) {
	data, readErr := readFileNoEscape(filepath.Dir(scopedPath), filepath.Base(scopedPath))
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) && legacyPath != "" {
			legacyData, legacyErr := readFileNoEscape(filepath.Dir(legacyPath), filepath.Base(legacyPath))
			if legacyErr != nil {
				return nil, fmt.Errorf("read context pack (scoped: %w, legacy: %v)", readErr, legacyErr)
			}
			data = legacyData
		} else {
			return nil, fmt.Errorf("read context pack: %w", readErr)
		}
	}
	return unmarshalContextPack(data)
}

// readFileNoEscape reads dir/rel through an os.Root on dir, so a symlink the
// sandbox planted (or swaps in after a check) cannot point the daemon at a
// file outside dir.
func readFileNoEscape(dir, rel string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }() // read-only handle; nothing to flush
	return root.ReadFile(rel)
}

func unmarshalContextPack(data []byte) (*ContextPack, error) {
	var cp ContextPack
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal context pack: %w", err)
	}
	var raw struct {
		Budget map[string]json.RawMessage `json:"budget"`
	}
	pathCountPresent, charCountPresent := false, false
	if err := json.Unmarshal(data, &raw); err == nil && raw.Budget != nil {
		for k := range raw.Budget {
			switch strings.ToLower(k) {
			case "path_count":
				pathCountPresent = true
			case "char_count":
				charCountPresent = true
			}
		}
	}
	cp.backfillAbsentBudgetCounters(pathCountPresent, charCountPresent)
	if err := cp.Validate(); err != nil {
		return nil, fmt.Errorf("validate context pack: %w", err)
	}
	return &cp, nil
}

// NewContextPack creates a minimal valid ContextPack for the given task.
func NewContextPack(taskID, parentTaskID, summary string, paths []string, cfg ContextPackConfig) *ContextPack {
	if cfg.MaxPaths <= 0 {
		cfg.MaxPaths = DefaultMaxContextPackPaths
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = DefaultMaxContextPackChars
	}
	return &ContextPack{
		Version:      ContextPackVersion,
		TaskID:       taskID,
		ParentTaskID: parentTaskID,
		CreatedAt:    time.Now().UTC(),
		Summary:      summary,
		Paths:        paths,
		Budget:       ContextBudget{MaxPaths: cfg.MaxPaths, MaxChars: cfg.MaxChars, PathCount: len(paths), CharCount: utf8.RuneCountInString(summary)},
	}
}
