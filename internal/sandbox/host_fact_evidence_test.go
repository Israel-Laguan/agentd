package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hostIntent = "Compile System Report: write report.md with the OS, CPU, memory and disk of this machine"

// summaryIntent is the B-045 report task: a model-written summary must not
// introduce values that no collected file contains.
const summaryIntent = "Build report: Create system report summarizing the OS and hardware details from os_info.txt and cpu_info.txt"

func TestValidateHostFactEvidence(t *testing.T) {
	tests := []struct {
		name    string
		intent  string
		command string
		wantErr bool
	}{
		{"echo literal facts", hostIntent, `echo "Ubuntu 22.04, i7-9700K, 32 GB" > report.md`, true},
		{"printf literal facts", hostIntent, `printf 'OS: Debian 13\nCPU: Ultra 5\n' >> report.md`, true},
		{"heredoc literal facts", hostIntent, "cat > report.md <<EOF\nOS: Debian 13\nEOF", true},
		{"literal after real command", hostIntent, `uname -a > os.txt; echo "RAM: 32 GB" >> report.md`, true},
		{"literal piped to tee", hostIntent, `echo "RAM: 32 GB" | tee report.md`, true},
		{"literal after &&", hostIntent, `lscpu > cpu.txt && echo "RAM: 32 GB" > report.md`, true},
		{"prose summary with invented values", summaryIntent, `echo "This machine runs Ubuntu 22.04 on an i7 with 32 GB RAM" > report.md`, true},
		{"prose summary mixed with real files", summaryIntent, "cat os_info.txt > report.md; printf 'RAM: 32 GB\\n' >> report.md", true},
		{"summary heredoc with invented values", summaryIntent, "cat > report.md <<EOF\n## Summary\nUbuntu 22.04, 32 GB\nEOF", true},
		{"command output redirect", hostIntent, `{ uname -a; hostnamectl; lscpu; } > report.md`, false},
		{"substitution inside echo", hostIntent, `echo "$(uname -a)" > report.md`, false},
		{"backtick inside echo", hostIntent, "echo \"`uname -r`\" > report.md", false},
		{"heredoc with substitution", hostIntent, "cat > report.md <<EOF\nKernel: $(uname -r)\nEOF", false},
		{"compile from earlier outputs", hostIntent, `cat os.txt cpu.txt mem.txt > report.md`, false},
		{"plain echo to stdout", hostIntent, `echo "starting"`, false},
		{"echo to dev null", hostIntent, `echo hi > /dev/null`, false},
		{"unrelated task writes code", "Create main.go with a hello world handler", "cat > main.go <<EOF\npackage main\nEOF", false},
		{"unrelated task echo", "Write a README for the project", `echo "# demo" > README.md`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHostFactEvidence(tt.intent, tt.command)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateHostFactEvidence() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !IsSandboxViolation(err) {
				t.Fatalf("error = %v, want sandbox violation", err)
			}
		})
	}
}

func TestBashExecutorRefusesLiteralHostFactWrite(t *testing.T) {
	sink := &recordingSink{}
	exec, workspace := testExecutor(t, sink)
	payload := testPayload(workspace, `echo "Ubuntu 22.04, i7-9700K" > report.md`)
	payload.Intent = hostIntent
	if _, err := exec.Execute(context.Background(), payload); err == nil {
		t.Fatal("Execute() error = nil, want refusal")
	}
	if _, err := os.Stat(filepath.Join(workspace, "report.md")); err == nil {
		t.Fatal("report.md was written; the command must not run")
	}
	if len(sink.events) != 1 || !strings.Contains(sink.events[0].Payload, "hostnamectl") {
		t.Fatalf("events = %#v, want one SANDBOX_VIOLATION naming command-based alternatives", sink.events)
	}
	if got := sink.events[0].Payload; !strings.Contains(got, "Ubuntu 22.04") {
		t.Fatalf("payload = %q, want refused command recorded", got)
	}
}
