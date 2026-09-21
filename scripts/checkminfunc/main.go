// Command checkminfunc fails when functions have fewer than the minimum number of
// significant code lines. Blank lines and comments are excluded from the count,
// so padding a trivial wrapper with comments does not satisfy the check.
// Such functions are typically trivial renames or wrappers that add no value.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

var (
	minLines       = flag.Int("min-lines", 3, "minimum number of significant code lines per function (blank lines and comments excluded)")
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
	"internal/testutil/**",
}

func main() {
	flag.Parse()

	violations, err := check(*minLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkminfunc: %v\n", err)
		os.Exit(2)
	}
	if *updateBaseline {
		if err := validateBaselineWritePath(*baselinePath); err != nil {
			fmt.Fprintf(os.Stderr, "checkminfunc: validate baseline: %v\n", err)
			os.Exit(2)
		}
		backupPath, err := writeBaseline(*baselinePath, violations, *minLines)
		if err != nil {
			fmt.Fprintf(os.Stderr, "checkminfunc: write baseline: %v\n", err)
			os.Exit(2)
		}
		msg := fmt.Sprintf("checkminfunc: updated baseline with %d function(s).", len(violations))
		if backupPath != "" {
			msg += fmt.Sprintf(" Previous baseline saved to %s.", backupPath)
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

	fmt.Printf("checkminfunc: %d new function(s) have fewer than %d significant lines:\n", len(violations), *minLines)
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
	fullPath := filepath.Join(root, relPath)
	src, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, fullPath, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var violations []violation
	ast.Inspect(node, func(n ast.Node) bool {
		funcDecl, ok := n.(*ast.FuncDecl)
		if !ok || funcDecl.Body == nil {
			return true
		}

		start := fset.Position(funcDecl.Body.Pos()).Line
		lineCount := countSignificantLines(fset, fullPath, src, funcDecl.Body)
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

// countSignificantLines counts the physical lines inside body that contain at
// least one code token. Blank lines and comments (including trailing comments
// and multi-line block comments) are excluded. A separate scanner pass is used
// instead of heuristics so comment-like text inside string literals is still
// counted as code. Tokens are matched to the body by byte offset, so single-line
// bodies such as `func f() int { return 1 }` are measured correctly.
func countSignificantLines(fset *token.FileSet, filename string, src []byte, body *ast.BlockStmt) int {
	lbraceOffset := fset.Position(body.Lbrace).Offset
	rbraceOffset := fset.Position(body.Rbrace).Offset

	scanSet := token.NewFileSet()
	scanFile := scanSet.AddFile(filename, -1, len(src))
	var s scanner.Scanner
	s.Init(scanFile, src, nil, scanner.ScanComments)

	lines := make(map[int]struct{})
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT {
			continue
		}
		off := scanFile.Offset(pos)
		if off <= lbraceOffset || off >= rbraceOffset {
			continue
		}
		lines[scanSet.Position(pos).Line] = struct{}{}
	}
	return len(lines)
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
