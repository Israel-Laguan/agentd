package hostreport

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// maxInputBytes bounds one collected file; real command output is far smaller.
const maxInputBytes = 256 << 10

// WriteReport renders a report from the regular files in dir and writes it to
// dir/target. It reports false, writing nothing, when the files carry no facts
// or when target exists as something other than a regular file. Symlinks in the
// workspace are never followed: a sandboxed command can create one, and the
// daemon must not read or overwrite what it points to.
func WriteReport(dir, target string) (string, bool, error) {
	if filepath.Base(target) != target {
		return "", false, fmt.Errorf("report target %q must be a bare file name", target)
	}
	if info, err := os.Lstat(filepath.Join(dir, target)); err == nil && !info.Mode().IsRegular() {
		return "", false, nil
	}
	inputs, err := readInputs(dir, target)
	if err != nil {
		return "", false, err
	}
	report, ok := Render(inputs)
	if !ok {
		return "", false, nil
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
