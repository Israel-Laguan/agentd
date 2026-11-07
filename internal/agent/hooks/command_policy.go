package hooks

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// CommandVerdict is the command policy's answer for a tool call (T-062).
type CommandVerdict int

const (
	// CommandAsk is the zero value: the call goes to the approval gate, if any.
	CommandAsk CommandVerdict = iota
	// CommandAllow skips the approval gate: read-only inspection, or one of the
	// usual build and test commands.
	CommandAllow
	// CommandDeny refuses the call outright, whatever a hook or approval says.
	CommandDeny
)

// allowedCommands are the leading words of a command that skips the approval
// gate. A build or test command still runs code the agent may have written (a
// Makefile, a test); the jail contains it, the allowlist only saves the ask.
var allowedCommands = [][]string{
	{"ls"}, {"cat"}, {"head"}, {"tail"}, {"wc"}, {"pwd"}, {"grep"}, {"rg"},
	{"tree"}, {"stat"}, {"file"}, {"du"}, {"df"}, {"which"},
	{"git", "status"}, {"git", "log"}, {"git", "diff"}, {"git", "show"},
	{"go", "build"}, {"go", "test"}, {"go", "vet"},
	{"make", "test"}, {"make", "check"}, {"make", "build"}, {"make", "lint"},
	{"npm", "test"}, {"npm", "run", "test"}, {"npm", "run", "build"}, {"npm", "run", "lint"},
	{"pytest"}, {"python", "-m", "pytest"}, {"python3", "-m", "pytest"},
	{"cargo", "build"}, {"cargo", "test"}, {"cargo", "check"},
}

// shellMetachars make a command more than one simple command (a chain, a pipe,
// a redirection, a substitution), so it is never allowed unasked.
const shellMetachars = "`$;&|<>(){}\\\n\r"

// writeExecOptions are options of allowlisted inspection commands that write
// files or execute programs. An allowlisted command carrying one goes to the
// approval gate instead of skipping it. The match is on the option text before
// any =value, so --output=notes.txt hits --output. Options that only list or
// print matches stay allowed (grep -o, --files-with-matches, rg --files-with).
var writeExecOptions = []string{
	"--output", "--exec", "--exec-batch", "--pre", "--pre-glob", "--passthru", "--pager", "--tui", "--pager-cmd", "--replace", "--in-place", "--edit", "--extcmd", "--open", "--execute", "--allow-unsafe", "--load", "--template", "--format", "--trace", "--watch",
}

// dangerousGitConfig marks a git -c / git --config option, which can name a
// pager, editor, hook or other executable. Only git subcommands in the allowlist
// reach this check; a plain -c on go or npm is their own flag.
var dangerousGitConfig = []string{"-c", "--config"}

// EvaluateToolCall is the command policy for one tool call: a bash call is
// judged by its command, any other tool is CommandAsk. reason is set on a deny.
func EvaluateToolCall(toolName, argsJSON string) (verdict CommandVerdict, reason string) {
	if toolName != ToolNameBash {
		return CommandAsk, ""
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return CommandAsk, ""
	}
	return EvaluateCommand(args.Command)
}

// EvaluateCommand matches the denylist on the normalised command, then the
// allowlist on the command as written. The denylist is a tripwire, not a
// boundary: variable expansion, aliases and encoded payloads evade text.
func EvaluateCommand(command string) (verdict CommandVerdict, reason string) {
	normalized := normalizeCommand(command)
	for _, d := range dangerousCommands {
		if d.re.MatchString(normalized) {
			return CommandDeny, fmt.Sprintf("command blocked: contains dangerous pattern %q", d.name)
		}
	}
	if allowedCommand(command) {
		return CommandAllow, ""
	}
	return CommandAsk, ""
}

// normalizeCommand undoes the quoting a shell would: quotes and escapes are
// dropped (r"m" and r\m both run rm), a backtick becomes a space, since it
// starts a command as ; does, and whitespace collapses to single spaces.
func normalizeCommand(command string) string {
	stripped := strings.NewReplacer(`"`, "", `'`, "", `\`, "", "`", " ").Replace(command)
	return strings.Join(strings.Fields(stripped), " ")
}

func allowedCommand(command string) bool {
	if strings.ContainsAny(command, shellMetachars) {
		return false
	}
	words := strings.Fields(command)
	for _, prefix := range allowedCommands {
		if len(words) >= len(prefix) && slices.Equal(words[:len(prefix)], prefix) {
			return !hasWriteOrExecOption(command, isGitPrefix(prefix))
		}
	}
	return false
}

func isGitPrefix(prefix []string) bool { return len(prefix) > 0 && prefix[0] == "git" }

// hasWriteOrExecOption reports whether command carries an option that writes
// files or executes a program. The option text is matched before any =value,
// so --output=notes.txt hits --output. -o is only read-only (grep -o) for the
// allowlisted commands, so it is not listed. git -c / --config is checked
// separately: it can name a pager, editor or hook.
func hasWriteOrExecOption(command string, isGit bool) bool {
	words := strings.Fields(command)
	for _, w := range words[1:] {
		opt := w
		if i := strings.Index(w, "="); i != -1 {
			opt = w[:i]
		}
		if slices.Contains(writeExecOptions, opt) {
			return true
		}
		if isGit && slices.Contains(dangerousGitConfig, opt) {
			return true
		}
	}
	return false
}
