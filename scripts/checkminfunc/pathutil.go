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

	root, err := os.Getwd()
	if err != nil {
		return err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return err
	}

	targetAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	targetReal, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if info, lstatErr := os.Lstat(targetAbs); lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("baseline path %q must not be a symlink", path)
		}
		targetReal, err = realExistingAncestor(targetAbs)
		if err != nil {
			return err
		}
	}

	rel, err := filepath.Rel(rootReal, targetReal)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("baseline path %q must be inside repository root %q", path, rootReal)
	}
	return nil
}

func realExistingAncestor(path string) (string, error) {
	for {
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
			re.WriteByte('.')
		default:
			if strings.ContainsRune(`\.+^$()[]{}|`, rune(pattern[i])) {
				re.WriteByte('\\')
			}
			re.WriteByte(pattern[i])
		}
	}
	re.WriteByte('$')
	matched, _ := regexpMatch(re.String(), name)
	return matched
}

func regexpMatch(pattern, name string) (bool, error) {
	return regexp.MatchString(pattern, name)
}
