package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/models"
)

// An ACP agent's process runs in the egress jail with its stdin and stdout
// passed through, since the driver speaks ACP over them; HOME is the agent's
// per-run home and the working directory is the workspace.
func TestAgentCommandPipesStdioThroughTheJail(t *testing.T) {
	requireJail(t)
	exec, workspace := testExecutor(t, nil)
	home := t.TempDir()
	exec.Egress = &Egress{Home: home}
	cmd, err := exec.AgentCommand(context.Background(), workspace, []string{"bash", "-c", `read -r line; echo "$line in $HOME at $PWD"`}, false)
	if err != nil {
		t.Fatalf("AgentCommand: %v", err)
	}
	if filepath.Base(cmd.Path) != "bwrap" {
		t.Fatalf("agent runs %q; want it under bwrap", cmd.Path)
	}
	if cmd.Stdin != nil {
		t.Fatal("cmd.Stdin is set; the ACP driver's StdinPipe needs it nil")
	}
	cmd.Stdin = strings.NewReader("ping\n")
	out, err := cmd.Output()
	if want := "ping in " + home + " at " + workspace; err != nil || strings.TrimSpace(string(out)) != want {
		t.Fatalf("output = %q, err = %v; want %q", out, err, want)
	}
}

// A read-only stage's agent cannot write the workspace with its own tools.
func TestAgentCommandBindsAReadOnlyWorkspace(t *testing.T) {
	requireJail(t)
	exec, workspace := testExecutor(t, nil)
	exec.Egress = &Egress{Home: t.TempDir()}
	cmd, err := exec.AgentCommand(context.Background(), workspace, []string{"bash", "-c", "touch written"}, true)
	if err != nil {
		t.Fatalf("AgentCommand: %v", err)
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("the agent wrote in a read-only workspace")
	}
	if _, err := os.Stat(filepath.Join(workspace, "written")); !os.IsNotExist(err) {
		t.Fatalf("stat written: %v; want no file", err)
	}
}

// The agent never runs on the host: B-086 closed the host network and
// loopback, so with no usable jail, or no egress, the command is refused.
func TestAgentCommandIsRefusedOutsideTheJail(t *testing.T) {
	t.Parallel()
	exec, workspace := testExecutor(t, nil)
	exec.Backend = BackendFilterOnly
	exec.Egress = &Egress{}
	if _, err := exec.AgentCommand(context.Background(), workspace, []string{"opencode", "acp"}, false); !errors.Is(err, models.ErrSandboxUnavailable) {
		t.Fatalf("filter-only: err = %v; want ErrSandboxUnavailable", err)
	}
	exec.Backend, exec.Egress = BackendBubblewrap, nil
	if _, err := exec.AgentCommand(context.Background(), workspace, []string{"opencode", "acp"}, false); !errors.Is(err, models.ErrSandboxUnavailable) {
		t.Fatalf("no egress: err = %v; want ErrSandboxUnavailable", err)
	}
}

// A workspace symlink that points outside it is refused before the agent
// starts, as it is before a bash command runs (B-042).
func TestAgentCommandRefusesAnEscapingSymlink(t *testing.T) {
	t.Parallel()
	exec, workspace := testExecutor(t, nil)
	exec.Egress = &Egress{}
	if err := os.Symlink(t.TempDir(), filepath.Join(workspace, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.AgentCommand(context.Background(), workspace, []string{"opencode", "acp"}, false); !errors.Is(err, models.ErrSandboxViolation) {
		t.Fatalf("err = %v; want ErrSandboxViolation", err)
	}
}
