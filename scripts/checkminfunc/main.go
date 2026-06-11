// Command checkminfunc fails when functions have fewer than the minimum number of lines.
// Such functions are typically trivial renames or wrappers that add no value.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	minLines       = flag.Int("min-lines", 3, "minimum number of lines per function")
	exclude        = flag.String("exclude", "", "comma-separated patterns to exclude")
	warn           = flag.Bool("warn", false, "warn only, don't exit with error")
	baselinePath   = flag.String("baseline", filepath.Join("scripts", "checkminfunc", "baseline"), "path to accepted baseline")
	updateBaseline = flag.Bool("update-baseline", false, "write current violations to the baseline file")
)

var defaultExcludes = []string{
	"vendor/**",
	"dist/**",
	"build/**",
	"**/generated/**",
	"**/*_test.go",
}

func main() {
	flag.Parse()

	violations, err := check(*minLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkminfunc: %v\n", err)
		os.Exit(2)
	}
	if *updateBaseline {
		hadBaseline := fileExists(*baselinePath)
		if err := writeBaseline(*baselinePath, violations); err != nil {
			fmt.Fprintf(os.Stderr, "checkminfunc: write baseline: %v\n", err)
			os.Exit(2)
		}
		msg := fmt.Sprintf("checkminfunc: updated baseline with %d function(s).", len(violations))
		if hadBaseline {
			msg += fmt.Sprintf(" Previous baseline saved to %s.bak.", *baselinePath)
		}
		fmt.Println(msg)
		return
	}

	accepted, err := readBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkminfunc: read baseline: %v\n", err)
		os.Exit(2)
	}
	violations = newViolations(violations, accepted)
	if len(violations) == 0 {
		switch {
		case len(accepted) == 0:
			fmt.Println("checkminfunc: no functions violate the minimum line limit.")
		default:
			fmt.Printf("checkminfunc: no new violations (%d function(s) in baseline).\n", len(accepted))
		}
		return
	}

	fmt.Printf("checkminfunc: %d new function(s) have fewer than %d lines:\n", len(violations), *minLines)
	for _, v := range violations {
		fmt.Printf("  %4d  %s:%d  %s\n", v.lines, v.file, v.line, v.name)
	}
	emitGitHubAnnotations(violations)
	emitStepSummary(violations)
	if *warn {
		fmt.Println("checkminfunc: warning mode - not failing build.")
		return
	}
	os.Exit(1)
}

type violation struct {
	name, file  string
	line, lines int
}

func check(minLines int) ([]violation, error) {
	tracked, err := trackedGoFiles()
	if err != nil {
		return nil, err
	}

	excludes := buildExcludes()

	var violations []violation
	for _, relPath := range tracked {
		if excluded(relPath, excludes) {
			continue
		}
		v, err := checkFile(relPath, minLines)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", relPath, err)
		}
		violations = append(violations, v...)
	}

	sortViolations(violations)
	return violations, nil
}

func buildExcludes() []string {
	excludes := defaultExcludes
	if *exclude != "" {
		excludes = append(excludes, strings.Split(*exclude, ",")...)
	}
	return excludes
}

func checkFile(relPath string, minLines int) ([]violation, error) {
	root, _ := os.Getwd()
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filepath.Join(root, relPath), nil, parser.ParseComments)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var violations []violation
	ast.Inspect(node, func(n ast.Node) bool {
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok || funcDecl.Body == nil {
			return true
		}

		start := fset.Position(funcDecl.Body.Pos()).Line
		end := fset.Position(funcDecl.Body.Rbrace).Line
		lineCount := end - start - 1
		if lineCount < 1 {
			lineCount = 1
		}
		if lineCount < minLines {
			funcName := funcDecl.Name.Name
			if funcDecl.Recv != nil {
				funcName = fmt.Sprintf("(%s).%s", typeName(funcDecl.Recv.List[0].Type), funcName)
			}
			violations = append(violations, violation{funcName, relPath, start, lineCount})
		}
		return true
	})
	return violations, nil
}

func sortViolations(violations []violation) {
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].lines != violations[j].lines {
			return violations[i].lines < violations[j].lines
		}
		if violations[i].file != violations[j].file {
			return violations[i].file < violations[j].file
		}
		return violations[i].line < violations[j].line
	})
}

func trackedGoFiles() ([]string, error) {
	out, err := exec.Command("git", "ls-files", "-z", "*.go").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	parts := strings.Split(string(out), "\x00")
	files := make([]string, 0, len(parts))
	for _, entry := range parts {
		if entry != "" {
			files = append(files, entry)
		}
	}
	return files, nil
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
	matched, _ := regexp.MatchString(re.String(), name)
	return matched
}

const maxGitHubAnnotations = 10

func emitStepSummary(violations []violation) {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return
	}
	lines := []string{
		"### Function length check failed",
		"",
		fmt.Sprintf("%d function(s) have fewer than %d lines:", len(violations), *minLines),
		"",
		"| Lines | Function | File |",
		"| ---: | --- | --- |",
	}
	limit := len(violations)
	if limit > maxGitHubAnnotations {
		limit = maxGitHubAnnotations
	}
	for i := 0; i < limit; i++ {
		v := violations[i]
		lines = append(lines, fmt.Sprintf("| %d | `%s` | `%s:%d` |", v.lines, v.name, v.file, v.line))
	}
	remaining := len(violations) - maxGitHubAnnotations
	if remaining > 0 {
		lines = append(lines, "", fmt.Sprintf("_%d more function(s) not shown; see step log._", remaining))
	}
	lines = append(lines, "")
	f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "checkminfunc: close summary file: %v\n", err)
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
			"::error file=%s,line=%d,endLine=%d,title=Function too short::Function %s has only %d lines (min %d)\n",
			v.file, v.line, v.line, v.name, v.lines, *minLines,
		)
	}
	remaining := len(violations) - maxGitHubAnnotations
	if remaining > 0 {
		fmt.Printf(
			"::error title=Function length check failed::checkminfunc: %d more function(s) not shown\n",
			remaining,
		)
	}
}
