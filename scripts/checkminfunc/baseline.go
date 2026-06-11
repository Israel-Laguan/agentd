package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func readBaseline(path string) (map[string]struct{}, error) {
	if path == "" {
		return map[string]struct{}{}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	if info.IsDir() {
		return readBaselineDir(path)
	}
	return readBaselineFile(path)
}

func readBaselineDir(path string) (map[string]struct{}, error) {
	entries, err := filepath.Glob(filepath.Join(path, "*.txt"))
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)

	accepted := make(map[string]struct{})
	for _, entry := range entries {
		partAccepted, err := readBaselineFile(entry)
		if err != nil {
			return nil, err
		}
		for key := range partAccepted {
			accepted[key] = struct{}{}
		}
	}
	return accepted, nil
}

func readBaselineFile(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}

	accepted := make(map[string]struct{})
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		accepted[line] = struct{}{}
	}
	return accepted, nil
}

func writeBaseline(path string, violations []violation) error {
	if path == "" {
		return fmt.Errorf("baseline path is empty")
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && info.IsDir():
		return writeBaselineDir(path, violations)
	case err == nil:
		return writeBaselineFile(path, violations)
	case os.IsNotExist(err):
		if isDirectoryPath(path) {
			return writeBaselineDir(path, violations)
		}
		return writeBaselineFile(path, violations)
	case err != nil:
		return err
	default:
		return writeBaselineFile(path, violations)
	}
}

func writeBaselineDir(path string, violations []violation) error {
	const baselineChunkSize = 200
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if err := backupBaselineDirIfExists(path); err != nil {
		return fmt.Errorf("backup baseline: %w", err)
	}
	for start := 0; start < len(violations); start += baselineChunkSize {
		end := start + baselineChunkSize
		if end > len(violations) {
			end = len(violations)
		}
		name := fmt.Sprintf("part-%03d.txt", start/baselineChunkSize+1)
		if err := writeBaselineFile(filepath.Join(path, name), violations[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func writeBaselineFile(path string, violations []violation) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := backupBaselineIfExists(path); err != nil {
		return fmt.Errorf("backup baseline: %w", err)
	}

	var out strings.Builder
	for _, v := range violations {
		out.WriteString(violationKey(v))
		out.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func backupBaselineDirIfExists(path string) error {
	entries, err := filepath.Glob(filepath.Join(path, "*.txt"))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	backupDir := strings.TrimRight(path, `/\`) + ".bak"
	if err := os.RemoveAll(backupDir); err != nil {
		return err
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := os.ReadFile(entry)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(backupDir, filepath.Base(entry)), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func backupBaselineIfExists(path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path+".bak", data, 0o644)
}

func isDirectoryPath(path string) bool {
	return strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(os.PathSeparator)) || filepath.Ext(path) == ""
}

func newViolations(violations []violation, accepted map[string]struct{}) []violation {
	if len(accepted) == 0 {
		return violations
	}

	var out []violation
	for _, v := range violations {
		if _, ok := accepted[violationKey(v)]; !ok {
			out = append(out, v)
		}
	}
	return out
}

func violationKey(v violation) string {
	return fmt.Sprintf("%s %s", v.file, v.name)
}
