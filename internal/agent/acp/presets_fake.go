//go:build fakeacp

package acp

import "agentd/internal/agent/acp/fakeagent"

// The devenv image builds agentd with -tags fakeacp (T-069). Its fake preset
// runs agentd's own hidden fake-acp-agent subcommand, a scripted ACP agent, so
// e2e journeys run tasks through the jail and the seam with no real agent. It
// reaches no host: only agentd's MCP server, through the forwarded socket.
func init() {
	Presets["fake"] = Preset{
		Command: "agentd",
		NoUsage: true,
		Home:    func(l Launch) ([]string, error) { return []string{l.Executable, fakeagent.Arg}, nil },
	}
}
