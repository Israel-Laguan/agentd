package sandbox

import (
	"fmt"
	"regexp"
	"strings"

	"agentd/internal/models"
)

var (
	// hostFactIntent marks a task whose own text is about facts of the host.
	hostFactIntent = regexp.MustCompile(`(?i)\b(operating system|os|hardware|cpu|processor|ram|memory|disk|storage|kernel|hostname|machine)\b`)
	// commandSegments splits a command into the simple commands run in order.
	commandSegments = regexp.MustCompile(`;|&&|\|\||\n`)
	// literalEchoWrite is echo/printf sending text into a file, directly or via tee.
	literalEchoWrite = regexp.MustCompile(`^\s*(echo|printf)\b.*(>>?\s*[^\s&>|]|\|\s*tee\b)`)
	// heredocWrite is a here-document written into a file.
	// substitution is a command substitution, whose output is real command output.
	substitution = regexp.MustCompile("\\$\\(|`|<\\(")
	heredocWrite = regexp.MustCompile(`(>>?\s*[^\s&>|]+\s*<<-?\s*['"]?\w+|\btee\b[^\n]*<<-?\s*['"]?\w+)`)
	// redirectDup is a descriptor duplication (`2>&1`), which redirects
	// nothing to a file and must not count as saving output.
	redirectDup = regexp.MustCompile(`\d*>&\d+`)
	// redirectFile is a stdout redirect into a file (`> file`, `>> file`).
	redirectFile = regexp.MustCompile(`>>?\s*([^\s&>|;()]+)`)
	// teePipe is a pipe into tee, which saves the piped output to files.
	teePipe = regexp.MustCompile(`\|\s*tee\b`)
	// stdoutChatter is a bare echo/printf to stdout: status text, not facts.
	stdoutChatter = regexp.MustCompile(`^\s*(echo|printf)\b`)
)

// validateHostFactEvidence refuses a command that writes literal text into a
// file when the task is about host facts: the facts must come from the output
// of commands that ran, not from the model's memory. It also refuses a chained
// `a && b > file` whose redirect binds only the last command, so the earlier
// commands' output is lost from the file (B-046). Tasks about anything
// else, and writes whose content comes from a command substitution or
// from earlier output files, are untouched.
func validateHostFactEvidence(intent, command string) error {
	if !hostFactIntent.MatchString(intent) {
		return nil
	}
	if writesLiteralText(command) {
		return fmt.Errorf("%w: host facts must come from command output, not literal text; build the file from commands, e.g. { uname -a; hostnamectl; lscpu; free -h; lsblk; } > report.md", models.ErrSandboxViolation)
	}
	if file, suggestion, ok := losesChainedOutput(command); ok {
		return fmt.Errorf("%w: host facts must reach %s from every command, but only the last command's output is redirected and earlier output is lost; group the commands so all output lands in the file, e.g. %s, or write one command per file", models.ErrSandboxViolation, file, suggestion)
	}
	return nil
}

func writesLiteralText(command string) bool {
	command = strings.ReplaceAll(command, devNull, "")
	if heredocWrite.MatchString(command) && !substitution.MatchString(command) {
		return true
	}
	for _, segment := range commandSegments.Split(command, -1) {
		if literalEchoWrite.MatchString(segment) && !substitution.MatchString(segment) {
			return true
		}
	}
	return false
}

// losesChainedOutput reports the file whose redirect captures only the last
// command of a chain: a later segment saves to file while an earlier flat
// (ungrouped) segment sends stdout nowhere, so its output never reaches the
// file. Grouped commands (`{ a; b; } > file`, `(a; b) > file`), one command
// per file, piped savers and status chatter are not losses. The suggestion
// regroups the actual segments so the retry feedback is directly actionable.
func losesChainedOutput(command string) (file, suggestion string, ok bool) {
	// Heredoc bodies are stdin payloads, never chained commands (B-040).
	scrubbed := redirectDup.ReplaceAllString(strings.ReplaceAll(stripHeredocBodies(command), devNull, ""), "")
	segments := commandSegments.Split(scrubbed, -1)
	if len(segments) < 2 {
		return "", "", false
	}
	flat, saver := scanSegments(segments)
	var target string
	for i, seg := range segments {
		if saver[i] == "" {
			continue
		}
		target = saver[i]
		if flat[i] || groupRedirect(seg) {
			break
		}
	}
	if target == "" {
		return "", "", false
	}
	for i, seg := range segments {
		if saver[i] != "" || !flat[i] || keepsStdout(seg) || chatters(seg, target) {
			continue
		}
		return target, groupSuggestion(segments, saver, target), true
	}
	return "", "", false
}

// scanSegments classifies every segment: flat means no group braces or
// parens surround it, saver names the file the segment saves to (empty when
// it saves nothing). Depth threads across segments because a chain separator
// always stands where the split happened.
func scanSegments(segments []string) (flat []bool, saver []string) {
	flat = make([]bool, len(segments))
	saver = make([]string, len(segments))
	brace, paren := 0, 0
	var prev byte
	for i, seg := range segments {
		if i > 0 {
			prev = '&'
		}
		flatBefore := brace == 0 && paren == 0
		brace, paren, prev = scanGroupDepth(seg, brace, paren, prev)
		flat[i] = flatBefore && brace == 0 && paren == 0
		saver[i], _ = saverFile(seg)
	}
	return flat, saver
}

// groupRedirect reports a redirect that closes a group (`} > file`,
// `) > file`): the file captures the whole group, not one flat command.
func groupRedirect(segment string) bool {
	trimmed := strings.TrimSpace(segment)
	if strings.HasPrefix(trimmed, "}") {
		return true
	}
	return strings.HasPrefix(trimmed, ")")
}

// keepsStdout reports a segment whose stdout reaches a file or a pipe, so
// chaining after it loses nothing. A bare `>` left over from stripping
// /dev/null still counts: discarding there is explicit.
func keepsStdout(segment string) bool {
	if strings.Contains(segment, ">") {
		return true
	}
	return strings.Contains(segment, "|")
}

// chatters reports a segment that only prints status text or displays the
// file being collected: stdout words the task never asked to save.
func chatters(segment, target string) bool {
	if strings.Contains(segment, target) {
		return true
	}
	return stdoutChatter.MatchString(segment) && !keepsStdout(segment)
}

// saverFile names the file a segment saves stdout to, via redirect or tee.
func saverFile(segment string) (string, bool) {
	if m := redirectFile.FindStringSubmatch(segment); m != nil {
		return m[1], true
	}
	if !teePipe.MatchString(segment) {
		return "", false
	}
	fields := strings.Fields(segment)
	for i, f := range fields {
		if strings.TrimPrefix(f, "|") != "tee" {
			continue
		}
		for _, g := range fields[i+1:] {
			if strings.HasPrefix(g, "-") || g == ">" || g == ">>" {
				continue
			}
			return strings.Trim(g, `"'`), true
		}
	}
	return "", false
}

// groupSuggestion rebuilds the chain as one group writing the target file,
// stripping each segment's own redirect so the example stays correct.
func groupSuggestion(segments, saver []string, target string) string {
	var parts []string
	for i, seg := range segments {
		cleaned := seg
		if saver[i] != "" {
			cleaned = redirectFile.ReplaceAllString(cleaned, "")
			cleaned = teePipe.ReplaceAllString(cleaned, "")
		}
		if trimmed := strings.TrimSpace(strings.Trim(cleaned, "|&; \t")); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return "{ " + strings.Join(parts, "; ") + "; } > " + target
}

// scanGroupDepth threads shell group depth through one segment: `{` opens
// where a command word can start one (never inside `$`-expansions or words),
// `}` closes on the same terms, and `(` opens a subshell except after `$`
// (command substitution) or a word (function definitions, arrays). Quote
// escapes are honored; anything fancier than this heuristic stays flat and
// can only suppress detection, never invent it.
func scanGroupDepth(segment string, brace, paren int, prev byte) (int, int, byte) {
	escaped := false
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		if escaped {
			escaped = false
			prev = c
			continue
		}
		if c == '\\' {
			escaped = true
			prev = c
			continue
		}
		switch c {
		case '{':
			if groupOpenPrev(prev) {
				brace++
			}
		case '}':
			if brace > 0 && groupClosePrev(prev) {
				brace--
			}
		case '(':
			if groupOpenPrev(prev) {
				paren++
			}
		case ')':
			if paren > 0 {
				paren--
			}
		}
		prev = c
	}
	return brace, paren, prev
}

// groupOpenPrev reports characters after which `{` or `(` starts a group:
// the start, whitespace, a separator, or an already-open group.
func groupOpenPrev(prev byte) bool {
	switch prev {
	case 0, ' ', '\t', '\n', ';', '&', '|', '(', '{':
		return true
	}
	return false
}

// groupClosePrev reports characters after which `}` ends a group: blanks, a
// separator, or the opener. A `}` glued to a word (`b}`, `${s}`) never
// closes, so brace expansions cannot unbalance the depth.
func groupClosePrev(prev byte) bool {
	switch prev {
	case ' ', '\t', '\n', ';', '{':
		return true
	}
	return false
}
