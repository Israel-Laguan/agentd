package worker

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
	agentmcp "agentd/internal/mcp"
	"agentd/internal/sandbox"
)

// NewACPRuntime wires the routes' acp agents (T-042). It serves every
// session's MCP server on one unix socket, which each run's jail forwards to,
// and starts a run's egress on a copy of sb. forwarder is the agentd binary
// the jail runs jail-forward from. Nil when no agent is kind acp.
func NewACPRuntime(routes route.Table, sb sandbox.Executor, forwarder string, recorder spec.Recorder) (*ACPRuntime, error) {
	if !HasACPAgent(routes) {
		return nil, nil
	}
	bash, ok := sb.(*sandbox.BashExecutor)
	if !ok {
		return nil, fmt.Errorf("acp agents run in the bash executor's jail; the sandbox is %T", sb)
	}
	dir, err := os.MkdirTemp("", "agentd-mcp-")
	if err != nil {
		return nil, fmt.Errorf("acp: MCP socket: %w", err)
	}
	socket := filepath.Join(dir, "mcp.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		// Remove the directory the failed bind left behind, as StartEgressRun does.
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("acp: MCP socket: %w", err)
	}
	sessions := &agentmcp.SessionMux{}
	server := &http.Server{Handler: sessions, ReadHeaderTimeout: 10 * time.Second}
	// shortcut: the socket serves until the daemon shuts the runtime down, and
	// the cleanup handle removes the directory with it.
	go func() { _ = server.Serve(ln) }()
	return &ACPRuntime{
		Start: func(allow []string, executable string) (ACPRun, error) {
			run, err := bash.StartEgressRun(allow, socket, forwarder, executable)
			if err != nil {
				return ACPRun{}, err
			}
			return ACPRun{Home: run.Home, Command: run.Executor.AgentCommand, Close: run.Close}, nil
		},
		Sessions: sessions, MCPURL: "http://" + sandbox.JailMCPAddr, Recorder: recorder,
		cleanup: func() {
			_ = server.Close()
			_ = os.RemoveAll(dir)
		},
	}, nil
}

// HasACPAgent reports whether the routes name an acp agent, so a caller that
// wires or tears down the runtime can decide before touching it.
func HasACPAgent(routes route.Table) bool {
	for _, a := range routes.Agents {
		if a.Kind == route.KindACP {
			return true
		}
	}
	return false
}
