package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"agentd/internal/models"
)

// AgentCommand is an external agent's long-lived process (T-042): argv run in
// the egress jail with the workspace bound, stdin and stdout left unset for the
// ACP driver's pipes. It lives until ctx ends. It needs Egress, so an agent
// never runs on the host, whose network and loopback B-086 closed: with no
// egress, or no usable jail, it returns ErrSandboxUnavailable. A readOnly
// workspace is bound read-only, which confines the agent's own tools in a
// read-only stage; the process then serves only read-only sessions.
func (e *BashExecutor) AgentCommand(ctx context.Context, workspacePath string, argv []string, readOnly bool) (*exec.Cmd, error) {
	if e.Egress == nil {
		return nil, fmt.Errorf("%w: an agent runs only in the egress jail, and none is configured", models.ErrSandboxUnavailable)
	}
	workspace, err := JailPath(e.Root, workspacePath)
	if err != nil {
		return nil, err
	}
	if err := refuseEscapingSymlinks(workspace); err != nil {
		return nil, err
	}
	if err := refuseEgressHomeInsideReadOnlyWorkspace(workspace, e.Egress, readOnly); err != nil {
		return nil, err
	}
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	// shortcut: no ulimit wrapper (withResourceLimits); agents need Node's memory. Add limits per preset if one runs away.
	cmd, _, err := e.startCommand(ctx, workspace, Payload{WorkspacePath: workspacePath, ReadOnlyWorkspace: readOnly}, strings.Join(quoted, " "))
	if err != nil {
		return nil, err
	}
	cmd.Stdin = nil
	return cmd, nil
}

// refuseEgressHomeInsideReadOnlyWorkspace refuses a home that overlaps the
// workspace. Egress binds its home after the workspace, so a home under or
// containing a read-only workspace would override that mount and make it
// writable again.
func refuseEgressHomeInsideReadOnlyWorkspace(workspace string, egress *Egress, readOnly bool) error {
	if !readOnly || egress == nil || egress.Home == "" {
		return nil
	}
	home, err := filepath.EvalSymlinks(egress.Home)
	if err != nil {
		return fmt.Errorf("resolve egress home: %w", err)
	}
	rel, err := filepath.Rel(workspace, home)
	if err != nil {
		return fmt.Errorf("compare egress home with workspace: %w", err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
		return fmt.Errorf("%w: egress home %s is inside the read-only workspace %s", models.ErrSandboxViolation, home, workspace)
	}
	up, err := filepath.Rel(home, workspace)
	if err != nil {
		return fmt.Errorf("compare read-only workspace with egress home: %w", err)
	}
	if up != ".." && !strings.HasPrefix(up, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("%w: egress home %s contains the read-only workspace %s", models.ErrSandboxViolation, home, workspace)
	}
	return nil
}
