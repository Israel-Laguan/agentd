package hostreport

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxInputBytes bounds one collected file; real command output is far smaller.
const maxInputBytes = 256 << 10

// MissingFactsError reports a collection that carries some host facts but
// lacks core ones. The report task must fail with retry feedback naming what
// is missing and which files were read, instead of writing "not collected"
// and completing (B-046).
type MissingFactsError struct {
	Missing []string
	Files   []string
}

// Error names the missing facts and the files they were looked for in.
func (e *MissingFactsError) Error() string {
	files := strings.Join(e.Files, ", ")
	missing := strings.Join(e.Missing, ", ")
	return fmt.Sprintf("collected files (%s) are missing: %s", files, missing)
}

// WriteReport renders a report from the regular files in dir and writes it to
// dir/target. It reports false, writing nothing, when the files carry no facts
// or when target already exists: an existing file (a NOTES.md the user owns,
// a report from an earlier attempt) belongs to the model path, which can read
// and extend it, instead of being overwritten with a system report. When some
// facts exist but core ones are missing it writes nothing and returns a
// *MissingFactsError so the worker falls back to the model path instead of
// completing a gapped report. Symlinks in the workspace are never followed: a
// sandboxed command can create one, and the daemon must not read or overwrite
// what it points to.
func WriteReport(dir, target string) (string, bool, error) {
	if filepath.Base(target) != target {
		return "", false, fmt.Errorf("report target %q must be a bare file name", target)
	}
	if _, err := os.Lstat(filepath.Join(dir, target)); err == nil {
		return "", false, nil
	}
	inputs, err := readInputs(dir, target)
	if err != nil {
		return "", false, err
	}
	report, missing, found := Diagnose(inputs)
	if !found {
		return "", false, nil
	}
	if len(missing) > 0 {
		names := make([]string, len(inputs))
		for i, in := range inputs {
			names[i] = in.Name
		}
		return "", false, &MissingFactsError{Missing: missing, Files: names}
	}
	if err := writeAtomic(dir, target, report); err != nil {
		return "", false, err
	}
	return report, true, nil
}

func readInputs(dir, exclude string) ([]Input, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read workspace: %w", err)
	}
	var inputs []Input
	for _, e := range entries {
		if e.Name() == exclude || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > maxInputBytes {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		inputs = append(inputs, Input{Name: e.Name(), Content: string(data)})
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Name < inputs[j].Name })
	return inputs, nil
}

// writeAtomic renames a temp file over target, which replaces a symlink at
// target instead of writing through it.
func writeAtomic(dir, target, content string) error {
	tmp, err := os.CreateTemp(dir, ".report-*")
	if err != nil {
		return fmt.Errorf("create report temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // best-effort cleanup; gone after a successful rename
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close() // the write error is the one worth returning
		return fmt.Errorf("write report: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("chmod report: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, target)); err != nil {
		return fmt.Errorf("install report: %w", err)
	}
	return nil
}
