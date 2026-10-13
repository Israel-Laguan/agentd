package sandbox

import "testing"

// Absolute paths outside the workspace must be refused however the command
// text attaches them: quoted, glued to a redirect operator, or after key=.
func TestValidateCommandPaths_AttachedPathsEscapeWorkspace(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"double quoted", `cat "/etc/passwd"`},
		{"single quoted", `cat '/etc/passwd'`},
		{"redirect out glued", `echo hi >/etc/agentd-probe`},
		{"redirect append glued", `echo hi >>/etc/agentd-probe`},
		{"redirect in glued", `cat </etc/passwd`},
		{"stderr redirect glued", `echo hi 2>/etc/x`},
		{"long option value", `cat --file=/etc/passwd`},
		{"env assignment", `FOO=/etc/passwd cat $FOO`},
		{"quoted redirect target", `echo hi > "/etc/x"`},
		{"proc self environ", `cat /proc/self/environ`},
		{"proc self environ quoted", `cat "/proc/self/environ"`},
		{"proc daemon environ", `cat </proc/1/environ`},
		{"proc self root", `ls /proc/self/root/etc`},
		{"proc self root quoted", `ls '/proc/self/root/etc'`},
		{"symlink out", `ln -s /etc/passwd link`},
		{"symlink out quoted", `ln -s "/proc/self/environ" link`},
		{"os-release", `cat /etc/os-release`},
		{"os-release quoted", `cat "/etc/os-release"`},
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

// Ordinary introspection and text-processing commands must keep working.
func TestValidateCommandPaths_AttachedPathsAllowsBenignCommands(t *testing.T) {
	tests := []string{
		`uname -a; lscpu; free -h; df -h`,
		`hostnamectl`,
		`lsb_release -a 2>/dev/null`,
		`uname -a >/dev/null`,
		`uname -a 2>/dev/null`,
		`uname -a > report.md`,
		`uname -a >report.md`,
		`cat "report.md"`,
		`cat "/workspace/report.md"`,
		`echo hi >/workspace/out.txt`,
		`free -m | awk '/Mem/ {print $2}'`,
		`grep "/Mem/" report.md`,
		`curl -s https://example.com/a/b`,
		`curl -s --url=https://example.com/a`,
	}
	for _, command := range tests {
		t.Run(command, func(t *testing.T) {
			if err := validateCommandPaths(command, "/workspace"); err != nil {
				t.Fatalf("validateCommandPaths(%q) = %v, want nil", command, err)
			}
		})
	}
}
