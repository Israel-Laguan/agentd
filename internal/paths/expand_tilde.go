package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandTildePrefix expands a path that begins with "~/".
// Resolution order: os.UserHomeDir when successful and non-empty, then
// environment variable HOME, then USERPROFILE (Windows-style). If no home
// directory can be determined, path is returned unchanged (fail-open).
func ExpandTildePrefix(path string) string {
	return expandTildePrefix(path, os.UserHomeDir, os.Getenv)
}

func expandTildePrefix(path string, userHomeDir func() (string, error), getenv func(string) string) string {
	if path == "" || !strings.HasPrefix(path, "~/") {
		return path
	}
	rest := path[2:]
	if h, err := userHomeDir(); err == nil && h != "" {
		return filepath.Join(h, rest)
	}
	if h := getenv("HOME"); h != "" {
		return filepath.Join(h, rest)
	}
	if h := getenv("USERPROFILE"); h != "" {
		return filepath.Join(h, rest)
	}
	return path
}
