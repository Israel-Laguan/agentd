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
	// Heredoc bodies are stdin payloads, never path operands: strip them
	// before any path scan (B-040). Operator lines are kept and still
	// checked, so real operands next to `<<` keep being refused.
	trimmed = stripHeredocBodies(trimmed)
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

// heredocDelim is one `<<` redirect's end marker: the word plus whether
// `<<-` strips leading tabs from the closing line.
type heredocDelim struct {
	word      string
	stripTabs bool
}

// interpreterPattern matches a command that executes text as code or turns
// stdin lines into operands: shells, scripting interpreters, and the shell
// builtins/utilities that run or expand a file (`source`, `.`, `eval`, `exec`,
// `env`, `xargs`).
var interpreterPattern = regexp.MustCompile(`(^|[;&|()\s])\s*(bash|sh|dash|zsh|python[0-9.]*|perl|ruby|node|nodejs|php|awk|gawk|xargs|source|eval|exec|env|\.)(\s|$|[;&|()<>])`)

// stripHeredocBodies drops `<<[ -]DELIM ... DELIM` stdin bodies from command
// text before path checks run. Data bodies (`cat > main.go <<EOF`) are payload,
// never path operands, so comment markers (`//`, `#`), globs (`/*`) and
// path-looking text inside them must not be refused (B-040). Lines carrying
// the `<<` operator itself are kept: real operands next to `<<` are still
// scanned.
//
// The allowance ends where the command can run what it wrote: if any line
// outside a body executes an interpreter (`sh x.sh`, `python3 a.py`,
// `xargs cat`, `source x`), the whole command is returned unstripped, so a
// script body that reads host paths stays subject to the path checks. Bodies
// are not searched for interpreter words. Runners such as `go test` are not
// treated as interpreters; closing that gap is SP-019's isolation work.
func stripHeredocBodies(command string) string {
	lines := strings.Split(command, "\n")
	kept := make([]string, 0, len(lines))
	var pending []heredocDelim
	for _, line := range lines {
		if len(pending) == 0 {
			pending = append(pending, parseHeredocDelims(line)...)
			kept = append(kept, line)
			continue
		}
		closing := line
		if pending[0].stripTabs {
			closing = strings.TrimLeft(closing, "\t")
		}
		if closing == pending[0].word {
			pending = pending[1:]
		}
	}
	stripped := strings.Join(kept, "\n")
	if interpreterPattern.MatchString(shellQuoteRemover.Replace(stripped)) {
		return command
	}
	return stripped
}

// shellQuoteRemover mimics bash quote removal so `'sh'`, `s"h"` and `\sh` are
// seen as the command word `sh` before interpreterPattern runs.
var shellQuoteRemover = strings.NewReplacer(`'`, "", `"`, "", `\`, "")

// parseHeredocDelims returns the end markers for every unquoted `<<`
// operator on one line. `<<<` herestrings are not heredocs and are skipped;
// a `<<` inside quotes is literal text, not an operator.
func parseHeredocDelims(line string) []heredocDelim {
	var out []heredocDelim
	var single, double bool
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case single:
			if c == '\'' {
				single = false
			}
		case double:
			switch c {
			case '\\':
				i++
			case '"':
				double = false
			}
		case c == '\'':
			single = true
		case c == '"':
			double = true
		case c == '\\':
			i++
		case c == '<' && i+1 < len(line) && line[i+1] == '<':
			delim, next, ok := heredocOperator(line, i)
			if ok {
				out = append(out, delim)
			}
			i = next - 1
		}
	}
	return out
}

// heredocOperator parses a `<<` operator at line[i] (the first `<`). It
// returns the delimiter and the first index past the operator; ok is false
// for `<<<` herestrings and for a missing delimiter word.
func heredocOperator(line string, i int) (heredocDelim, int, bool) {
	if i+2 < len(line) && line[i+2] == '<' {
		return heredocDelim{}, i + 3, false
	}
	word, stripTabs, next := parseHeredocWord(line, i+2)
	if word == "" {
		return heredocDelim{}, next, false
	}
	return heredocDelim{word: word, stripTabs: stripTabs}, next, true
}

// parseHeredocWord reads the delimiter word at line[start:] (just past `<<`
// or `<<-`) and returns it with the first index past it. Like bash, adjacent
// quoted and unquoted segments form one word (`<<E'O'F` closes on `EOF`).
func parseHeredocWord(line string, start int) (string, bool, int) {
	i := start
	stripTabs := false
	if i < len(line) && line[i] == '-' {
		stripTabs = true
		i++
	}
	i = skipHeredocBlanks(line, i)
	var word strings.Builder
	var quote byte
	for i < len(line) {
		c := line[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			i = writeHeredocChar(&word, line, i)
		case c == '\'' || c == '"':
			quote = c
		case isHeredocWordEnd(c):
			return word.String(), stripTabs, i
		default:
			i = writeHeredocChar(&word, line, i)
		}
		i++
	}
	return word.String(), stripTabs, i
}

// writeHeredocChar appends line[i] to word, taking a backslash-escaped
// character as the escaped byte, and returns the index of the last byte used.
func writeHeredocChar(word *strings.Builder, line string, i int) int {
	if line[i] == '\\' && i+1 < len(line) {
		i++
	}
	word.WriteByte(line[i])
	return i
}

// skipHeredocBlanks advances past the spaces and tabs between `<<` and its
// delimiter word.
func skipHeredocBlanks(line string, i int) int {
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i
}

// isHeredocWordEnd reports shell characters that end an unquoted heredoc
// delimiter word.
func isHeredocWordEnd(c byte) bool {
	switch c {
	case ' ', '\t', ';', '|', '&', '(', ')', '<', '>':
		return true
	}
	return false
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
		// A bare root reference (`/`, `//`) names no host file: `grep "//"`
		// searches Go comments, it does not open a path. Anything deeper
		// (`/etc/passwd`, `/proc/...`) is still refused below.
		if path == "/" {
			continue
		}
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
