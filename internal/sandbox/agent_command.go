package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"agentd/internal/models"
)

// AgentCommand is an external agent's long-lived process (T-042): argv run in
// the egress jail with the workspace bound, stdin and stdout left unset for the
// ACP driver's pipes. It lives until ctx ends. It needs Egress, so an agent
// never runs on the host, whose network and loopback B-086 closed: with no
// egress, or no usable jail, it returns ErrSandboxUnavailable.
func (e *BashExecutor) AgentCommand(ctx context.Context, workspacePath string, argv []string) (*exec.Cmd, error) {
	if e.Egress == nil {
		return nil, fmt.Errorf("%w: an agent runs only in the egress jail, and none is configured", models.ErrSandboxUnavailable)
	}
	workspace, err := JailPath(e.Root, workspacePath)
	if err != nil {
		return nil, err
	}
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	// shortcut: no ulimit wrapper (withResourceLimits); agents need Node's memory. Add limits per preset if one runs away.
	cmd, _, err := e.startCommand(ctx, workspace, Payload{WorkspacePath: workspacePath}, strings.Join(quoted, " "))
	if err != nil {
		return nil, err
	}
	cmd.Stdin = nil
	return cmd, nil
}
