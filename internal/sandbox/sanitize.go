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

// devNull is the one absolute path outside the workspace commands may name:
// reads return EOF and writes are discarded, so it exposes no host data. Only
// the exact cleaned path matches; /dev/null/x and /dev/nullx are still refused.
const devNull = "/dev/null"

// attachedPathPattern finds an absolute path glued to a quote, a redirect
// operator (>, >>, <, 2>) or an option/assignment value (--file=/x, FOO=/x).
// The whitespace tokenizer in validateAbsolutePathTokens never sees these
// because the token does not start with "/".
var attachedPathPattern = regexp.MustCompile("[\"'=<>`](/[^\\s\"'<>;|&()`]*)")

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
	if err := validateAttachedPaths(trimmed, workspace); err != nil {
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
		if filepath.Clean(path) == filepath.Clean(workspace) || strings.HasPrefix(path, workspace+string(os.PathSeparator)) {
			continue
		}
		if path == devNull {
			continue
		}
		return fmt.Errorf("%w: absolute path escapes workspace: %s", models.ErrSandboxViolation, token)
	}
	return nil
}

// validateAttachedPaths refuses attached absolute paths (see
// attachedPathPattern) that leave the workspace. A quoted "/Mem/" in an awk or
// grep program is not a path to anything on the host, so only paths whose
// top-level directory exists are refused; the whitespace-separated form stays
// strict in validateAbsolutePathTokens.
func validateAttachedPaths(command, workspace string) error {
	for _, match := range attachedPathPattern.FindAllStringSubmatch(command, -1) {
		path := filepath.Clean(match[1])
		if path == devNull || path == filepath.Clean(workspace) ||
			strings.HasPrefix(path, workspace+string(os.PathSeparator)) {
			continue
		}
		top := "/" + strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
		if _, err := os.Lstat(top); err != nil {
			continue
		}
		return fmt.Errorf("%w: absolute path escapes workspace: %s", models.ErrSandboxViolation, match[1])
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
		if filepath.Clean(resolved) == filepath.Clean(workspace) || strings.HasPrefix(resolved, workspace+string(os.PathSeparator)) {
			continue
		}
		return fmt.Errorf("%w: directory change escapes workspace", models.ErrSandboxViolation)
	}
	return nil
}
