package hooks

import "testing"

func TestEvaluateCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		command string
		want    CommandVerdict
	}{
		// Read-only inspection and build or test commands skip the gate.
		{"ls -la", CommandAllow},
		{"git status", CommandAllow},
		{"git diff HEAD~1 -- internal", CommandAllow},
		{"go test ./...", CommandAllow},
		{"make check", CommandAllow},
		{"npm run lint", CommandAllow},
		{"python3 -m pytest -q", CommandAllow},
		// Anything else, and any chain, pipe, redirection or substitution, asks.
		{"touch notes.txt", CommandAsk},
		{"git push", CommandAsk},
		{"npm install left-pad", CommandAsk},
		{"make", CommandAsk},
		{"ls | head", CommandAsk},
		{"ls; touch x", CommandAsk},
		{"go test ./... > out.txt", CommandAsk},
		{"cat $(echo notes.txt)", CommandAsk},
		{"", CommandAsk},
		// The denylist.
		{"sudo id", CommandDeny},
		{"su - root", CommandDeny},
		{"rm -rf /", CommandDeny},
		{"rm -rf /*", CommandDeny},
		{"mkfs.ext4 /dev/sda1", CommandDeny},
		{"dd if=/dev/zero of=/dev/sda", CommandDeny},
		{"chmod -R 777 /", CommandDeny},
		{"cat image > /dev/sda", CommandDeny},
		{":(){ :|:& };:", CommandDeny},
		{"rm -rf /tmp/build", CommandAsk},
		{"grep pseudocode notes.md", CommandAllow},
		// Bypass attempts the normalisation catches.
		{`"rm" -rf "/"`, CommandDeny},
		{`r\m -rf /`, CommandDeny},
		{"su'do' id", CommandDeny},
		{"echo `sudo id`", CommandDeny},
		{"ls`sudo id`", CommandDeny},
		{"echo $(sudo id)", CommandDeny},
		{"rm -fr /", CommandDeny},
		{"rm -r -f /", CommandDeny},
		{"rm --recursive --force /", CommandDeny},
		{"rm -rf --no-preserve-root /", CommandDeny},
		{"ls\nsudo id", CommandDeny},
		// Bypasses text cannot see: they only ask, and the jail contains them.
		{"x=/; rm -rf $x", CommandAsk},
		{"echo cm0gLXJmIC8= | base64 -d | sh", CommandAsk},
		{"cd / && rm -rf .", CommandAsk},
	}
	for _, tc := range cases {
		if got, reason := EvaluateCommand(tc.command); got != tc.want || (got == CommandDeny) != (reason != "") {
			t.Errorf("EvaluateCommand(%q) = %d, %q; want %d", tc.command, got, reason, tc.want)
		}
	}
}

func TestEvaluateToolCallJudgesBashOnly(t *testing.T) {
	t.Parallel()
	if got, _ := EvaluateToolCall(ToolNameBash, `{"command":"sudo id"}`); got != CommandDeny {
		t.Errorf("bash sudo = %d; want CommandDeny", got)
	}
	if got, _ := EvaluateToolCall(ToolNameRead, `{"command":"sudo id"}`); got != CommandAsk {
		t.Errorf("read = %d; want CommandAsk: only bash runs a command", got)
	}
	if got, _ := EvaluateToolCall(ToolNameBash, `not json`); got != CommandAsk {
		t.Errorf("malformed args = %d; want CommandAsk", got)
	}
}
