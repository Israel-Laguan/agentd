package acp

import (
	"regexp"
	"strings"

	acpsdk "github.com/coder/acp-go-sdk"
)

// agentdSessionTools are the MCP tools agentd attaches to a session, named
// server__tool by the agents that send them as kind other; the server is
// agentd's own, so the tool names read agentd__<tool>. The driver lets its own
// through without a hook, and refuses any other kind other call. The board.*
// entries are project-scoped read-only tools (tasks, projects, comments).
var agentdSessionTools = []string{"report_outcome", "get_context", "ask_human", "board.list_tasks", "board.get_task", "board.list_projects", "board.get_project", "board.list_comments"}

// ownTool reports whether p calls one of agentd's own session tools, which the
// driver allows without a hook: the session tools touch only agentd's session,
// and the board.* tools are project-scoped reads. The names must match exactly,
// never a prefix, so an agent that names an unmapped kind with a title of its
// own cannot ride the bypass.
func ownTool(p Permission, servers []acpsdk.McpServer) bool {
	if p.Tool != "" {
		return false
	}
	// cline 3.0.74 titles a call "server__tool: <arguments JSON>".
	name, _, _ := strings.Cut(p.Title, ": ")
	for _, s := range servers {
		if s.Http == nil || s.Http.Name != "agentd" {
			continue
		}
		for _, t := range agentdSessionTools {
			if ownToolName(name, s.Http.Name+"__", t) {
				return true
			}
		}
	}
	return false
}

// clineToolSuffix is the hash cline 3.0.74 appends to a tool whose name it
// rewrites: board.list_tasks is called board_list_tasks_18b7ab84.
var clineToolSuffix = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ownToolName matches name against tool exactly, or, for a dotted tool only,
// as cline rewrites it: dots to underscores and one 8-hex suffix.
func ownToolName(name, prefix, tool string) bool {
	if name == prefix+tool {
		return true
	}
	if !strings.Contains(tool, ".") {
		return false
	}
	hash, ok := strings.CutPrefix(name, prefix+strings.ReplaceAll(tool, ".", "_")+"_")
	return ok && clineToolSuffix.MatchString(hash)
}
