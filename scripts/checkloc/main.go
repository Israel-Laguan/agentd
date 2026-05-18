// Command checkloc fails when tracked text files exceed configured LOC limits.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const maxGitHubAnnotations = 10

var defaultExcludes = []string{
	"vendor/**",
	"dist/**",
	"build/**",
	"coverage/**",
	"**/generated/**",
	"*.min.js",
	"*.min.css",
	"go.sum",
	"web/package-lock.json",
}

var categoryLimits = []struct {
	pattern string
	limit   int
}{
	{"*_test.go", 500},
	{"docs/**", 400},
}

func main() {
	maxLines := flag.Int("max-lines", 300, "default maximum lines per file")
	flag.Parse()

	violations, err := check(*maxLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkloc: %v\n", err)
		os.Exit(2)
	}
	if len(violations) == 0 {
		fmt.Println("LOC check passed: no tracked files exceed their limits.")
		return
	}

	fmt.Printf("LOC check failed: %d file(s) exceed their limits:\n", len(violations))
	for _, v := range violations {
		fmt.Printf("  %4d/%4d  %s\n", v.lines, v.limit, v.path)
	}
	emitGitHubAnnotations(violations)
	emitStepSummary(violations)
	os.Exit(1)
}

type violation struct {
	lines int
	path  string
	limit int
}

func check(defaultMax int) ([]violation, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	var violations []violation
	for _, relPath := range trackedFiles() {
		if excluded(relPath, defaultExcludes) {
			continue
		}
		fullPath := filepath.Join(root, relPath)
		info, err := os.Stat(fullPath)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relPath, err)
		}
		if bytes.Contains(raw, []byte{0}) {
			continue
		}
		lineCount := countLines(raw)
		limit := maxLinesFor(relPath, defaultMax)
		if lineCount > limit {
			violations = append(violations, violation{
				lines: lineCount,
				path:  relPath,
				limit: limit,
			})
		}
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].lines != violations[j].lines {
			return violations[i].lines > violations[j].lines
		}
		return violations[i].path < violations[j].path
	})
	return violations, nil
}

func trackedFiles() []string {
	out, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkloc: git ls-files: %v\n", err)
		os.Exit(2)
	}
	parts := strings.Split(string(out), "\x00")
	files := make([]string, 0, len(parts))
	for _, entry := range parts {
		if entry != "" {
			files = append(files, entry)
		}
	}
	return files
}

func excluded(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if fnmatch(pattern, relPath) {
			return true
		}
	}
	return false
}

func maxLinesFor(relPath string, defaultMax int) int {
	for _, category := range categoryLimits {
		if fnmatch(category.pattern, relPath) {
			return category.limit
		}
	}
	return defaultMax
}

func countLines(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	text := string(raw)
	n := strings.Count(text, "\n")
	if strings.HasSuffix(text, "\n") {
		return n
	}
	return n + 1
}

// fnmatch matches path against pattern using shell-style rules (as Python fnmatch).
func fnmatch(pattern, name string) bool {
	var re strings.Builder
	re.WriteByte('^')
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			re.WriteString(".*")
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
	matched, _ := regexp.MatchString(re.String(), name)
	return matched
}

func emitStepSummary(violations []violation) {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return
	}
	lines := []string{
		"### LOC check failed",
		"",
		fmt.Sprintf("%d file(s) exceed their limits:", len(violations)),
		"",
		"| Lines | Limit | File |",
		"| ---: | ---: | --- |",
	}
	limit := len(violations)
	if limit > maxGitHubAnnotations {
		limit = maxGitHubAnnotations
	}
	for i := 0; i < limit; i++ {
		v := violations[i]
		lines = append(lines, fmt.Sprintf("| %d | %d | `%s` |", v.lines, v.limit, v.path))
	}
	remaining := len(violations) - maxGitHubAnnotations
	if remaining > 0 {
		lines = append(lines, "", fmt.Sprintf("_%d more file(s) not shown; see step log._", remaining))
	}
	lines = append(lines, "")
	f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "checkloc: close summary file: %v\n", err)
		}
	}()
	_, _ = f.WriteString(strings.Join(lines, "\n") + "\n")
}

func emitGitHubAnnotations(violations []violation) {
	if os.Getenv("GITHUB_ACTIONS") == "" {
		return
	}
	limit := len(violations)
	if limit > maxGitHubAnnotations {
		limit = maxGitHubAnnotations
	}
	for i := 0; i < limit; i++ {
		v := violations[i]
		fmt.Printf(
			"::error file=%s,line=1,endLine=1,title=LOC limit exceeded::LOC %d/%d — file exceeds limit\n",
			v.path, v.lines, v.limit,
		)
	}
	remaining := len(violations) - maxGitHubAnnotations
	if remaining > 0 {
		fmt.Printf(
			"::error title=LOC check failed::LOC check failed — %d file(s) exceed limits (%d more not shown as annotations; see step log)\n",
			len(violations), remaining,
		)
	}
}
