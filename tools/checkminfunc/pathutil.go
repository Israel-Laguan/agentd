package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func validateBaselineWritePath(path string) error {
	if path == "" {
		return fmt.Errorf("baseline path is empty")
	}
	if hasParentTraversal(path) {
		return fmt.Errorf("baseline path %q must not contain parent traversal", path)
	}

	rootReal, err := repoRootReal()
	if err != nil {
		return err
	}

	targetAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := requireNotSymlink(path, targetAbs); err != nil {
		return err
	}

	targetReal, err := resolveTargetReal(targetAbs)
	if err != nil {
		return err
	}

	if !isInsideRepo(rootReal, targetReal) {
		return fmt.Errorf("baseline path %q must be inside repository root %q", path, rootReal)
	}
	return nil
}

// hasParentTraversal reports whether path contains a ".." path component.
// filepath.Abs cleans lexically, so "linkdir/../evil" would validate as inside
// the repo while the kernel resolves the symlink first and writes outside it.
func hasParentTraversal(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// repoRootReal resolves the current working directory to its real (symlink-free)
// absolute form, used as the containment boundary for baseline writes.
func repoRootReal() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(rootAbs)
}

// requireNotSymlink rejects the target when it already exists as a symlink,
// guarding against write-through-symlink escapes. A not-exists error is not
// fatal; the path may be a fresh write.
func requireNotSymlink(path, targetAbs string) error {
	info, lstatErr := os.Lstat(targetAbs)
	if lstatErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("baseline path %q must not be a symlink", path)
		}
		return nil
	}
	if !os.IsNotExist(lstatErr) {
		return lstatErr
	}
	return nil
}

// resolveTargetReal resolves the real filesystem location of targetAbs. When the
// target does not yet exist, it walks up to the nearest existing parent so that
// containment against the repo root still holds for fresh writes.
func resolveTargetReal(targetAbs string) (string, error) {
	targetReal, err := filepath.EvalSymlinks(targetAbs)
	if err == nil {
		return targetReal, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// EvalSymlinks on a path that crosses a symlink at an existing ancestor
	// resolves the ancestor chain; a not-exists here means the ancestor itself
	// is a symlink, which is disallowed.
	if info, lstatErr := os.Lstat(targetAbs); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("baseline path %q must not be a symlink", targetAbs)
	}
	return realExistingAncestor(targetAbs)
}

// isInsideRepo reports whether targetReal is contained within rootReal.
func isInsideRepo(rootReal, targetReal string) bool {
	rel, err := filepath.Rel(rootReal, targetReal)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func realExistingAncestor(path string) (string, error) {
	for {
		// Lstat each level without following the final component so a
		// symlink ancestor (including a dangling one, which Stat would
		// report as not-exist) is rejected instead of skipped over.
		if info, lstatErr := os.Lstat(path); lstatErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("baseline path %q must not traverse a symlink", path)
			}
		} else if !os.IsNotExist(lstatErr) {
			return "", lstatErr
		}

		if _, err := os.Stat(path); err == nil {
			return filepath.EvalSymlinks(path)
		} else if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Abs(path)
		}
		path = parent
	}
}

func excluded(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if fnmatch(pattern, relPath) {
			return true
		}
	}
	return false
}

func fnmatch(pattern, name string) bool {
	var re strings.Builder
	re.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		if i+1 < len(pattern) && pattern[i:i+2] == "**" {
			if i+2 < len(pattern) && pattern[i+2] == '/' {
				re.WriteString("(?:.*/)?")
				i += 2
				continue
			}
			re.WriteString(".*")
			i++
			continue
		}
		switch pattern[i] {
		case '*':
			re.WriteString("[^/]*")
		case '?':
			re.WriteString("[^/]")
		default:
			if strings.ContainsRune(`\.+^$()[]{}|`, rune(pattern[i])) {
				re.WriteByte('\\')
			}
			re.WriteByte(pattern[i])
		}
	}
	re.WriteByte('$')
	matched, _ := regexp.MatchString(re.String(), name)
	return matched
}
