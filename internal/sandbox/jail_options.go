package sandbox

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"agentd/internal/models"
)

// jailOnly names what in this execution only the jail can enforce, or "".
// Plain bash could write anywhere in a read-only workspace, and a pattern
// filter cannot stop every writing command. An egress command unjailed would
// have the host's whole network, the exposure B-086 closed.
func (e *BashExecutor) jailOnly(payload Payload) string {
	switch {
	case payload.ReadOnlyWorkspace:
		return "a read-only workspace"
	case e.Egress != nil:
		return "egress"
	}
	return ""
}

// refuseUnjailed refuses a command that needs the jail when none can run, rather
// than run it with weaker isolation than it asked for.
func refuseUnjailed(workspace string, payload Payload, what, reason string) error {
	slog.Error("sandbox: command needs the jail; command refused",
		"needs", what, "workspace", workspace, "task_id", payload.TaskID, "reason", reason)
	return fmt.Errorf("%w: %s needs the bubblewrap jail, and %s; command refused",
		models.ErrSandboxUnavailable, what, reason)
}

// Jail-side paths and loopback addresses of the egress bridge (see Egress).
// The jail's loopback is its own, so fixed ports cannot collide with the host.
const (
	jailForwarder   = "/run/agentd/agentd"
	jailProxySocket = "/run/agentd/egress.sock"
	jailMCPSocket   = "/run/agentd/mcp.sock"
	jailProxyAddr   = "127.0.0.1:3128"
	// JailMCPAddr is agentd's MCP server inside an egress jail, for an agent's
	// http MCP URL (opencode).
	JailMCPAddr = "127.0.0.1:3129"
)

// bridge is one host socket, where it is bound in the jail, and the loopback
// address jail-forward serves it on.
type bridge struct{ host, jail, addr string }

func (g *Egress) bridges() []bridge {
	all := []bridge{{g.ProxySocket, jailProxySocket, jailProxyAddr}, {g.MCPSocket, jailMCPSocket, JailMCPAddr}}
	return slices.DeleteFunc(all, func(b bridge) bool { return b.host == "" })
}

// binds are the egress mounts. They follow the /tmp tmpfs, like the workspace,
// since a per-run home or socket usually sits under /tmp.
func (g *Egress) binds() []string {
	if g == nil {
		return nil
	}
	var argv []string
	for _, b := range g.bridges() {
		argv = append(argv, "--bind", b.host, b.jail)
	}
	if len(argv) > 0 {
		argv = append(argv, "--ro-bind", g.Forwarder, jailForwarder)
	}
	if g.Executable != "" {
		argv = append(argv, "--ro-bind", g.Executable, g.Executable)
	}
	if g.Home != "" {
		argv = append(argv, "--bind", g.Home, g.Home)
	}
	return argv
}

// env points proxy-aware clients (Go, curl, Node with NODE_USE_ENV_PROXY) at
// the forwarded proxy. It follows jailEnv, so a host proxy variable passed
// through the allowlist cannot win; NO_PROXY keeps the jail's loopback, where
// the MCP port is, off the proxy.
func (g *Egress) env() []string {
	if g == nil || g.ProxySocket == "" {
		return nil
	}
	proxy := "http://" + jailProxyAddr
	return []string{"HTTPS_PROXY=" + proxy, "HTTP_PROXY=" + proxy, "NO_PROXY=127.0.0.1,localhost", "NODE_USE_ENV_PROXY=1"}
}

// launch starts jail-forward as the shell's child, waits for its ready line,
// then runs command, so the agent never starts before its network path exists.
func (g *Egress) launch(command string) string {
	if g == nil || len(g.bridges()) == 0 {
		return command
	}
	pairs := make([]string, 0, 2)
	for _, b := range g.bridges() {
		pairs = append(pairs, b.addr+"="+b.jail)
	}
	return "exec 3< <(exec " + jailForwarder + " jail-forward " + strings.Join(pairs, " ") + ") && read -r _ <&3 || " +
		"{ echo 'agentd: jail-forward did not start' >&2; exit 125; }\nexec 3<&-\n" + command
}
