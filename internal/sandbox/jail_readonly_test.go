package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
)

// readOnlyPayload is testPayload with the workspace mounted read-only.
func readOnlyPayload(workspace, command string) Payload {
	payload := testPayload(workspace, command)
	payload.ReadOnlyWorkspace = true
	return payload
}

// Under a read-only workspace every write to it fails, whatever tool makes it,
// and the host file is unchanged afterwards.
func TestReadOnlyWorkspaceRefusesEveryWrite(t *testing.T) {
	requireJail(t)
	ctx := context.Background()
	bash, workspace := testExecutor(t, nil)
	host := filepath.Join(workspace, "file.txt")
	if err := os.WriteFile(host, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed file.txt: %v", err)
	}

	writes := map[string]string{
		"redirect": "echo x > new.txt",
		"sed -i":   "sed -i 's/original/pwned/' file.txt",
		"mv":       "mv file.txt file.bak",
		"rm":       "rm file.txt",
		"mkdir":    "mkdir sub",
		"python":   "python3 -c \"open('file.txt','w').write('pwned')\"",
		"tee":      "echo x | tee file.txt",
	}
	for name, command := range writes {
		if name == "python" {
			if _, err := exec.LookPath("python3"); err != nil {
				continue
			}
		}
		result, err := bash.Execute(ctx, readOnlyPayload(workspace, command))
		if err != nil {
			t.Fatalf("%s: Execute err = %v; the command should run and fail inside the jail", name, err)
		}
		if result.Success {
			t.Fatalf("%s: write succeeded under a read-only workspace: %q", name, command)
		}
	}
	content, err := os.ReadFile(host)
	if err != nil || string(content) != "original" {
		t.Fatalf("host file.txt = %q, %v; want it unchanged", content, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "new.txt")); err == nil {
		t.Fatal("new.txt was created in the host workspace")
	}
}

// Reads of the workspace still work, and so do writes to the jail's scratch
// space and to HOME, which moves off the workspace for a read-only payload.
// The scratch write goes through mktemp, and HOME through ${HOME}: the command
// policy refuses a literal /tmp path and a literal $HOME before the jail runs.
func TestReadOnlyWorkspaceStillReadsAndWritesScratch(t *testing.T) {
	requireJail(t)
	ctx := context.Background()
	bash, workspace := testExecutor(t, nil)
	if err := os.WriteFile(filepath.Join(workspace, "file.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("seed file.txt: %v", err)
	}

	cases := map[string]struct{ command, want string }{
		"read workspace": {"cat file.txt", "original"},
		"list workspace": {"ls", "file.txt"},
		"write scratch":  {"f=$(mktemp) && echo scratch > \"$f\" && cat \"$f\"", "scratch"},
		"write HOME":     {"touch \"${HOME}/marker\" && echo home-ok", "home-ok"},
	}
	for name, tc := range cases {
		result, err := bash.Execute(ctx, readOnlyPayload(workspace, tc.command))
		if err != nil || !result.Success {
			t.Fatalf("%s: err = %v, success = %v, stderr = %q", name, err, result.Success, result.Stderr)
		}
		if !strings.Contains(result.Stdout, tc.want) {
			t.Fatalf("%s: stdout = %q, want it to contain %q", name, result.Stdout, tc.want)
		}
	}
	if result, _ := bash.Execute(ctx, readOnlyPayload(workspace, "echo ${HOME}")); strings.TrimSpace(result.Stdout) != readOnlyHome {
		t.Fatalf("HOME = %q, want %q for a read-only payload", strings.TrimSpace(result.Stdout), readOnlyHome)
	}
}

// Without a jail a read-only workspace cannot be enforced, so the command is
// refused with ErrSandboxUnavailable and never runs.
func TestReadOnlyPayloadRefusedWithoutJail(t *testing.T) {
	cases := map[string]string{
		"filter-only backend": BackendFilterOnly,
		"auto, no jail":       BackendAuto,
	}
	for name, backend := range cases {
		t.Run(name, func(t *testing.T) {
			bash, workspace := testExecutor(t, nil)
			bash.Backend = backend
			marker := filepath.Join(workspace, "ran")
			run := func() {
				_, err := bash.Execute(context.Background(), readOnlyPayload(workspace, "touch ran"))
				if !errors.Is(err, models.ErrSandboxUnavailable) {
					t.Fatalf("err = %v, want ErrSandboxUnavailable: a read-only workspace needs the jail", err)
				}
				if !strings.Contains(err.Error(), "read-only") {
					t.Fatalf("err = %q, want the refusal to say the workspace is read-only", err)
				}
			}
			if backend == BackendAuto {
				withProbe(t, jailProbeResult{}, run)
			} else {
				run()
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("the refused command ran anyway")
			}
		})
	}
}

// The jail mounts a read-only workspace read-only and points HOME at a tmpfs
// directory, so nothing the shell does can reach the host workspace.
func TestJailArgvMountsReadOnlyWorkspaceReadOnly(t *testing.T) {
	t.Parallel()
	ws := "/tmp/ws/p"
	argv := jailArgv("/usr/bin/bwrap", ws, true, []string{"CI=true"}, nil, "echo hi")
	if !containsSequence(argv, []string{"--ro-bind", ws, ws}) {
		t.Fatalf("argv = %v, want the workspace bound read-only", argv)
	}
	if containsSequence(argv, []string{"--bind", ws, ws}) {
		t.Fatalf("argv = %v, want no writable bind of the workspace", argv)
	}
	if !containsSequence(argv, []string{"--dir", readOnlyHome}) || !containsSequence(argv, []string{"--setenv", "HOME", readOnlyHome}) {
		t.Fatalf("argv = %v, want HOME on a directory in the tmpfs", argv)
	}
}

// containsSequence reports whether want appears as consecutive elements of argv.
func containsSequence(argv, want []string) bool {
	for i := 0; i+len(want) <= len(argv); i++ {
		match := true
		for j := range want {
			if argv[i+j] != want[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
