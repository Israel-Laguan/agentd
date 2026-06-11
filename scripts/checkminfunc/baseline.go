package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readBaseline(path string) (map[string]struct{}, error) {
	if path == "" {
		return map[string]struct{}{}, nil
	}
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var out strings.Builder
	for _, v := range violations {
		out.WriteString(violationKey(v))
		out.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
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
	return fmt.Sprintf(
		"%s:%d %s", v.file, v.line, v.name,
	)
}
