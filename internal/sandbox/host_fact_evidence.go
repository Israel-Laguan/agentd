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
)

// validateHostFactEvidence refuses a command that writes literal text into a
// file when the task is about host facts: the facts must come from the output
// of commands that ran, not from the model's memory. Tasks about anything
// else, and writes whose content comes from a command substitution or
// from earlier output files, are untouched.
func validateHostFactEvidence(intent, command string) error {
	if !hostFactIntent.MatchString(intent) || !writesLiteralText(command) {
		return nil
	}
	return fmt.Errorf("%w: host facts must come from command output, not literal text; build the file from commands, e.g. { uname -a; hostnamectl; lscpu; free -h; lsblk; } > report.md", models.ErrSandboxViolation)
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
