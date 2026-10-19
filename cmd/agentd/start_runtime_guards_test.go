package main

import (
	"testing"
	"time"

	agenthooks "agentd/internal/agent/hooks"
	"agentd/internal/config"
)

func TestBuildDaemonOptions_PassesSchedulerTickInterval(t *testing.T) {
	cfg := config.Config{}
	cfg.Agentic.Scheduler.TickInterval = 15 * time.Second

	opts := buildDaemonOptions(cfg, &startOptions{}, daemonParts{})

	if opts.SchedulerTickInterval != 15*time.Second {
		t.Fatalf("DaemonOptions.SchedulerTickInterval = %v, want 15s", opts.SchedulerTickInterval)
	}
}

func TestBuildWorkerOptions_RegistersDenylistHook(t *testing.T) {
	cfg := config.Config{ProjectsDir: t.TempDir()}

	opts := buildWorkerOptions(nil, runtimeDeps{}, cfg, "", nil)

	if opts.Hooks == nil {
		t.Fatal("WorkerOptions.Hooks = nil, want a chain with the denylist hook")
	}
	for _, command := range []string{"sudo apt install x", "rm -rf /", ":(){:|:&};:"} {
		verdict := opts.Hooks.RunPre(agenthooks.HookContext{
			ToolName: "bash",
			Args:     `{"command":"` + command + `"}`,
		})
		if !verdict.Veto {
			t.Errorf("bash %q was not refused by the worker hooks", command)
		}
	}
	allowed := opts.Hooks.RunPre(agenthooks.HookContext{ToolName: "bash", Args: `{"command":"go test ./..."}`})
	if allowed.Veto {
		t.Errorf("an ordinary command was refused: %s", allowed.Reason)
	}
}
