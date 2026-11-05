package sandbox

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"agentd/internal/sandbox/egress"
)

// EgressRun is one external agent run's jail (T-042): Executor is a copy of
// the daemon's executor whose Egress gives the run a home of its own and a
// proxy that reaches only its preset's hosts.
type EgressRun struct {
	Executor *BashExecutor
	Home     string
	// Close stops the proxy and removes the home with the login copied into
	// it. Call it once the agent process has exited.
	Close func()
}

// StartEgressRun serves a proxy allowing only allow on a socket in a fresh
// owner-only directory, next to the run's home, and binds both into a copy of
// e with mcpSocket (agentd's MCP server), forwarder (the agentd binary) and
// executable (the agent's binary).
func (e *BashExecutor) StartEgressRun(allow []string, mcpSocket, forwarder, executable string) (EgressRun, error) {
	dir, err := os.MkdirTemp("", "agentd-acp-")
	if err != nil {
		return EgressRun{}, fmt.Errorf("egress run: %w", err)
	}
	home := filepath.Join(dir, "home")
	socket := filepath.Join(dir, "egress.sock")
	ln, err := listenRun(home, socket)
	if err != nil {
		_ = os.RemoveAll(dir)
		return EgressRun{}, fmt.Errorf("egress run: %w", err)
	}
	proxy := &egress.Proxy{Allow: allow}
	go func() { _ = proxy.Serve(ln) }()
	run := *e
	run.Egress = &Egress{Forwarder: forwarder, ProxySocket: socket, MCPSocket: mcpSocket, Home: home, Executable: executable}
	return EgressRun{Executor: &run, Home: home, Close: func() {
		_ = ln.Close()
		_ = os.RemoveAll(dir)
	}}, nil
}

func listenRun(home, socket string) (net.Listener, error) {
	if err := os.Mkdir(home, 0o700); err != nil {
		return nil, err
	}
	return net.Listen("unix", socket)
}
