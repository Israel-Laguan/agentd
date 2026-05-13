package worker

import (
	"strings"
	"testing"
	"time"
)

func TestDenylistHook_BlocksSudo(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      `{"command":"sudo rm -rf /"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for sudo command")
	}
}

func TestDenylistHook_BlocksForkBomb(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      `{"command":":(){:|:&};:"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for fork bomb")
	}
}

func TestDenylistHook_BlocksRmRfSlash(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      `{"command":"rm -rf /"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for rm -rf /")
	}
}

func TestDenylistHook_AllowsSafeCommand(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      `{"command":"echo hello"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto: %s", verdict.Reason)
	}
}

func TestDenylistHook_CaseInsensitive(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      `{"command":"SUDO apt install vim"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for SUDO (case insensitive)")
	}
}

func TestDenylistHook_DangerousPatterns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		command string
		blocked bool
	}{
		{"mkfs", `{"command":"mkfs.ext4 /dev/sda1"}`, true},
		{"dd", `{"command":"dd if=/dev/zero of=/dev/sda"}`, true},
		{"su -", `{"command":"su - root"}`, true},
		{"safe ls", `{"command":"ls -la"}`, false},
		{"safe cat", `{"command":"cat file.txt"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hook := DenylistHook("/workspace")
			verdict, err := hook.Fn(HookContext{
				ToolName:  "bash",
				Args:      tt.command,
				Timestamp: time.Now(),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if verdict.Veto != tt.blocked {
				t.Fatalf("Veto = %v, want %v (reason: %s)", verdict.Veto, tt.blocked, verdict.Reason)
			}
		})
	}
}

func TestDenylistHook_PathTraversal_ReadTool(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "read",
		Args:      `{"path":"../../etc/passwd"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for path traversal on read")
	}
	if !strings.Contains(verdict.Reason, "escapes") {
		t.Fatalf("expected reason to mention escape, got %q", verdict.Reason)
	}
}

func TestDenylistHook_PathTraversal_WriteTool(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "write",
		Args:      `{"path":"../../.ssh/authorized_keys","content":"evil"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for path traversal on write")
	}
}

func TestDenylistHook_PathTraversal_SafePath(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "read",
		Args:      `{"path":"src/main.go"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto for safe path: %s", verdict.Reason)
	}
}

func TestDenylistHook_PathTraversal_MCPTool(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "mcp_file_editor",
		Args:      `{"file_path":"../../../etc/shadow"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verdict.Veto {
		t.Fatal("expected veto for path traversal on MCP tool")
	}
}

func TestDenylistHook_NoPathArgs_NoPanic(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      `{"command":"ls"}`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto: %s", verdict.Reason)
	}
}

func TestDenylistHook_InvalidJSON_NoPanic(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	verdict, err := hook.Fn(HookContext{
		ToolName:  "bash",
		Args:      `not json`,
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.Veto {
		t.Fatalf("unexpected veto for invalid JSON: %s", verdict.Reason)
	}
}

func TestDenylistHook_FailClosedPolicy(t *testing.T) {
	t.Parallel()
	hook := DenylistHook("/workspace")
	if hook.Policy != FailClosed {
		t.Fatalf("expected FailClosed policy, got %v", hook.Policy)
	}
}

func TestDenylistHook_IntegrationViaHookChain(t *testing.T) {
	t.Parallel()
	hc := NewHookChain()
	hc.RegisterPre(DenylistHook("/workspace"))

	verdict := hc.RunPre(HookContext{
		ToolName:  "bash",
		Args:      `{"command":"sudo reboot"}`,
		Timestamp: time.Now(),
	})
	if !verdict.Veto {
		t.Fatal("expected denylist veto through hook chain")
	}
}
