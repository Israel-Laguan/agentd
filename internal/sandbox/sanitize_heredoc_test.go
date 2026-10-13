package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B-040: heredoc stdin bodies are payload text, never path operands. A Go
// `//` comment (or `#`, `/* */`, URL, awk program) inside a heredoc body
// must not be refused as an absolute path.
func TestValidateCommandPaths_HeredocBodiesAreNotScanned(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{
			name: "go comment in quoted heredoc",
			command: "cat > greet.go << 'EOF'\npackage main\n\n" +
				"// Greet takes a name and returns a greeting.\n" +
				"func Greet(name string) string {\n\treturn \"hi \" + name\n}\nEOF",
		},
		{
			name:    "hash c block and url comments in heredoc",
			command: "cat > notes.txt << 'EOF'\n# heading\n/* block */\nsee https://example.com/a/b\nEOF",
		},
		{
			name:    "awk program in heredoc",
			command: "cat > filter.sh << 'EOF'\nfree -m | awk '/Mem/ {print $2}'\nEOF",
		},
		{
			name:    "path looking text in heredoc body",
			command: "cat > loot.txt << 'EOF'\n/etc/passwd\n/proc/self/environ\nEOF",
		},
		{
			name:    "unquoted delimiter",
			command: "cat > f.txt <<EOF\n// comment\nEOF",
		},
		{
			name:    "dash tab stripped delimiter",
			command: "cat > f.txt <<- 'EOF'\n\t// comment\n\tEOF",
		},
		{
			name:    "double quoted delimiter",
			command: `cat > f.txt << "EOF"` + "\n// comment\nEOF",
		},
		{
			name:    "append redirect with heredoc",
			command: "cat >> greet.go << 'EOF'\n// trailing comment\nEOF",
		},
		{
			name:    "two heredocs",
			command: "cat << 'A' > one.txt; cat << 'B' > two.txt\n// first\nA\n/* second */\nB",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateCommandPaths(tt.command, "/workspace"); err != nil {
				t.Fatalf("validateCommandPaths(%q) = %v, want nil", tt.command, err)
			}
		})
	}
}

// The heredoc operator line itself is still a command line: real path
// operands next to `<<` must keep being refused.
func TestValidateCommandPaths_HeredocOperatorLineStillScanned(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"absolute operand with heredoc", "cat /etc/passwd << 'EOF'\n// body\nEOF"},
		{"quoted absolute operand with heredoc", "cat \"/etc/passwd\" << 'EOF'\nbody\nEOF"},
		{"redirect glued with heredoc", "echo hi >/etc/agentd-probe << 'EOF'\nbody\nEOF"},
		{"option value with heredoc", "cat --file=/etc/passwd << 'EOF'\nbody\nEOF"},
		{"proc environ with heredoc", "cat /proc/self/environ << 'EOF'\nbody\nEOF"},
		{"symlink out with heredoc", "ln -s /etc/passwd link << 'EOF'\nbody\nEOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommandPaths(tt.command, "/workspace")
			if err == nil || !IsSandboxViolation(err) {
				t.Fatalf("validateCommandPaths(%q) = %v, want sandbox violation", tt.command, err)
			}
		})
	}
}

// A quoted `"//"` names the filesystem root, not a host file. It must not
// be treated as a path (grep for Go comments), while quoted real paths
// outside the workspace must keep being refused.
func TestValidateCommandPaths_QuotedRootOnlyIsNotAPath(t *testing.T) {
	allow := []string{
		`grep "//" main.go`,
		`echo "//"`,
	}
	for _, command := range allow {
		t.Run(command, func(t *testing.T) {
			if err := validateCommandPaths(command, "/workspace"); err != nil {
				t.Fatalf("validateCommandPaths(%q) = %v, want nil", command, err)
			}
		})
	}
	deny := []string{
		`cat "/etc/passwd"`,
		`ls "/proc/self/environ"`,
		`ln -s "/proc/self/environ" link`,
	}
	for _, command := range deny {
		t.Run(command, func(t *testing.T) {
			err := validateCommandPaths(command, "/workspace")
			if err == nil || !IsSandboxViolation(err) {
				t.Fatalf("validateCommandPaths(%q) = %v, want sandbox violation", command, err)
			}
		})
	}
}

// A heredoc fed to a shell is code, not payload: literal paths inside it
// must keep being refused, exactly as before heredoc stripping. This pins
// the B-040 fix to data heredocs (`cat > file`) with no sandbox loosening.
func TestValidateCommandPaths_ShellFedHeredocStillScanned(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"bash heredoc with absolute path", "bash << 'EOF'\ncat /etc/passwd\nEOF"},
		{"sh heredoc with redirect path", "sh << 'EOF'\necho hi >/etc/agentd-probe\nEOF"},
		{"bash attached heredoc with proc path", "bash<<'EOF'\ncat /proc/self/environ\nEOF"},
		{"zsh heredoc with option path", "zsh << 'EOF'\ncat --file=/etc/passwd\nEOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommandPaths(tt.command, "/workspace")
			if err == nil || !IsSandboxViolation(err) {
				t.Fatalf("validateCommandPaths(%q) = %v, want sandbox violation", tt.command, err)
			}
		})
	}
}

// Interpreters read their program from a heredoc too, so those bodies are
// code and stay scanned. Data heredocs (cat/tee) are still stripped.
func TestValidateCommandPaths_InterpreterFedHeredocStillScanned(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"python3", "python3 <<'EOF'\nprint(open('/etc/passwd').read())\nEOF"},
		{"python versioned", "python3.12 - <<EOF\nopen(\"/etc/passwd\")\nEOF"},
		{"perl", "perl <<'EOF'\nopen(F, '/etc/passwd');\nEOF"},
		{"ruby", "ruby <<'EOF'\nFile.read('/etc/passwd')\nEOF"},
		{"node", "node <<'EOF'\nrequire('fs').readFileSync('/etc/passwd')\nEOF"},
		{"php", "php <<'EOF'\nfile_get_contents('/etc/passwd');\nEOF"},
		{"awk program from stdin", "awk -f - <<'EOF'\nBEGIN { getline x < \"/etc/passwd\" }\nEOF"},
		{"after a pipe", "echo hi | python3 <<'EOF'\nopen('/etc/passwd')\nEOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommandPaths(tt.command, "/workspace")
			if err == nil || !IsSandboxViolation(err) {
				t.Fatalf("validateCommandPaths(%q) = %v, want sandbox violation", tt.command, err)
			}
		})
	}
}

// B-040 diagnosis pin: the heredoc `//` refusal comes from the
// pre-existing whitespace-token check (validateAbsolutePathTokens), not
// from the B-034 attached-path check (validateAttachedPaths passes).
func TestB040CausedByAbsolutePathTokensNotAttachedPaths(t *testing.T) {
	command := "cat > greet.go << 'EOF'\npackage main\n// Greet takes a name.\nfunc Greet() {}\nEOF"
	if err := validateAbsolutePathTokens(command, "/workspace"); err == nil {
		t.Fatal("validateAbsolutePathTokens() = nil, want the B-040 refusal")
	}
	if err := validateAttachedPaths(command, "/workspace"); err != nil {
		t.Fatalf("validateAttachedPaths() = %v, want nil", err)
	}
}

// End to end through the executor: the exact B-040 worker command writes
// the commented Go file instead of raising SANDBOX_VIOLATION.
func TestBashExecutorWritesHeredocWithGoComment(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	command := "cat > greet.go << 'EOF'\npackage main\n\n// Greet takes a name.\nfunc Greet() {}\nEOF"
	result, err := exec.Execute(context.Background(), testPayload(workspace, command))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !result.Success {
		t.Fatalf("result = %#v, want success", result)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "greet.go"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(content), "// Greet takes a name.") {
		t.Fatalf("greet.go = %q, want the Go comment", content)
	}
}
