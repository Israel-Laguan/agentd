package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"agentd/internal/models"
)

var shellSplitSeparators = strings.NewReplacer(
	";", " ",
	"|", " ",
	"&", " ",
	"(", " ",
	")", " ",
)

var changeDirPattern = regexp.MustCompile(`(?:^|[;&|])\s*(cd|pushd|chdir)\s+([^\s;&|]+)`)

func validateCommandPaths(command, workspace string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil
	}
	if strings.Contains(trimmed, "~"+string(os.PathSeparator)) || strings.Contains(trimmed, "$HOME") {
		return fmt.Errorf("%w: home path reference is not allowed", models.ErrSandboxViolation)
	}
	if strings.Contains(trimmed, "../") || strings.Contains(trimmed, `..\`) {
		return fmt.Errorf("%w: directory traversal is not allowed", models.ErrSandboxViolation)
	}
	if err := validateAbsolutePathTokens(trimmed, workspace); err != nil {
		return err
	}
	return validateChangeDirTargets(trimmed, workspace)
}

func validateAbsolutePathTokens(command, workspace string) error {
	expanded := shellSplitSeparators.Replace(command)
	for _, token := range strings.Fields(expanded) {
		if !filepath.IsAbs(token) {
			continue
		}
		path, err := filepath.Abs(token)
		if err != nil {
			return fmt.Errorf("%w: invalid absolute path", models.ErrSandboxViolation)
		}
		if samePath(path, workspace) || strings.HasPrefix(path, workspace+string(os.PathSeparator)) {
			continue
		}
		return fmt.Errorf("%w: absolute path escapes workspace", models.ErrSandboxViolation)
	}
	return nil
}

func validateChangeDirTargets(command, workspace string) error {
	matches := changeDirPattern.FindAllStringSubmatch(command, -1)
	for _, match := range matches {
		target := strings.Trim(match[2], `"'`)
		if target == "" || target == "." {
			continue
		}
		candidate := target
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(workspace, candidate)
		}
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			return fmt.Errorf("%w: invalid directory target", models.ErrSandboxViolation)
		}
		if samePath(resolved, workspace) || strings.HasPrefix(resolved, workspace+string(os.PathSeparator)) {
			continue
		}
		return fmt.Errorf("%w: directory change escapes workspace", models.ErrSandboxViolation)
	}
	return nil
}
