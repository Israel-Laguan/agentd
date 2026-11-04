//go:build live

package acp

// This file is a scratch probe, not a regression test. It drives the real
// opencode ACP server through this package's Driver and records the raw
// JSON-RPC lines, so the permission behaviour and wire shapes can be measured.
//
// It is skipped unless AGENTD_LIVE_OPENCODE=1. It costs real model calls and
// writes its logs under /tmp/opencode/probe/live by default.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const (
	probeDefaultModel = "opencode/deepseek-v4.1-flash-free"
	probeCallTimeout  = 3 * time.Minute
	probeQuotaWait    = 8 * time.Second
)

// TestLiveOpenCodeACPProbe runs the A/B/C/D/E matrix against opencode's ACP
// server. Each call gets a fresh process and workspace. The test only runs
// when AGENTD_LIVE_OPENCODE=1; every prompt is a real call.
//
// Environment:
//
//	AGENTD_LIVE_OPENCODE=1          required
//	AGENTD_LIVE_CELLS=A,B,C,D,E     optional cell filter
//	AGENTD_LIVE_TASKS=T1,T2,T3      optional task filter
//	AGENTD_LIVE_RUNS=3              runs per cell/task
//	AGENTD_LIVE_MODEL=...           a -free model id; default above
//	AGENTD_LIVE_OUT=/tmp/...        empty directory for this batch
//	AGENTD_LIVE_OPENCODE_BIN=...    defaults to opencode in PATH
//	AGENTD_LIVE_429_RETRIES=2       retries after a quota error
//	AGENTD_LIVE_PREFLIGHT_ONLY=1    only check session/new and the model
func TestLiveOpenCodeACPProbe(t *testing.T) {
	if os.Getenv("AGENTD_LIVE_OPENCODE") != "1" {
		t.Skip("set AGENTD_LIVE_OPENCODE=1 to run the live opencode ACP probe")
	}

	opts := liveOptions{
		model:         envOr("AGENTD_LIVE_MODEL", probeDefaultModel),
		runs:          envInt("AGENTD_LIVE_RUNS", 3),
		cells:         envList("AGENTD_LIVE_CELLS", "A,B,C,D,E"),
		tasks:         envList("AGENTD_LIVE_TASKS", "T1,T2,T3"),
		quotaRetries:  envInt("AGENTD_LIVE_429_RETRIES", 2),
		preflightOnly: os.Getenv("AGENTD_LIVE_PREFLIGHT_ONLY") == "1",
	}
	if opts.runs < 1 {
		t.Fatalf("AGENTD_LIVE_RUNS must be at least 1")
	}
	if opts.quotaRetries < 0 {
		opts.quotaRetries = 0
	}
	if !isFreeModel(opts.model) {
		t.Fatalf("AGENTD_LIVE_MODEL=%q does not end in -free", opts.model)
	}

	var err error
	opts.bash, err = exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash: %v", err)
	}
	opts.stdbuf, err = exec.LookPath("stdbuf")
	if err != nil {
		t.Fatalf("stdbuf: %v", err)
	}
	opts.opencode, err = exec.LookPath(envOr("AGENTD_LIVE_OPENCODE_BIN", "opencode"))
	if err != nil {
		t.Fatalf("opencode: %v", err)
	}
	opts.opencode, err = filepath.Abs(opts.opencode)
	if err != nil {
		t.Fatalf("opencode path: %v", err)
	}

	opts.root = os.Getenv("AGENTD_LIVE_OUT")
	if opts.root == "" {
		opts.root = filepath.Join("/tmp/opencode/probe/live", time.Now().Format("20060102-150405"))
	}
	opts.root, err = filepath.Abs(opts.root)
	if err != nil {
		t.Fatalf("output directory: %v", err)
	}
	if err := os.MkdirAll(opts.root, 0o755); err != nil {
		t.Fatalf("output directory: %v", err)
	}
	if entries, err := os.ReadDir(opts.root); err != nil {
		t.Fatalf("output directory: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("AGENTD_LIVE_OUT=%s is not empty; use a new directory for each batch", opts.root)
	}

	cells, err := selectedCells(opts.cells, opts.tasks)
	if err != nil {
		t.Fatalf("%v", err)
	}
	batch := &liveBatch{
		Root:       opts.root,
		Started:    time.Now().UTC().Format(time.RFC3339),
		Model:      opts.model,
		Runs:       opts.runs,
		Cells:      opts.cells,
		Tasks:      opts.tasks,
		Preflights: map[string]*livePreflight{},
	}
	batch.OpenCodeVersion = opencodeVersion(opts.opencode)
	batch.Config = fmt.Sprintf("cells=%v tasks=%v runs=%d model=%s", opts.cells, opts.tasks, opts.runs, opts.model)
	if err := writeJSON(filepath.Join(opts.root, "batch.json"), batch); err != nil {
		t.Fatalf("write batch.json: %v", err)
	}
	t.Logf("live probe root: %s", opts.root)
	t.Logf("opencode %s, model %s, cells %v, tasks %v, runs %d", batch.OpenCodeVersion, opts.model, opts.cells, opts.tasks, opts.runs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, cell := range cells {
		cell := cell
		t.Run("preflight/"+cell.ID, func(t *testing.T) {
			pre := preflightCell(ctx, opts, cell)
			batch.addPreflight(pre)
			if pre.Error != "" {
				t.Errorf("preflight %s: %s", cell.ID, pre.Error)
				return
			}
			t.Logf("preflight %s: actual model %s (free=%t, configured=%s)", cell.ID, pre.ActualModel, pre.Free, opts.model)
			if pre.ActualModel != opts.model {
				t.Logf("preflight %s: session/new did not keep the configured model; it selected %s", cell.ID, pre.ActualModel)
			}
			if !pre.Free {
				t.Errorf("preflight %s: actual model %q is not -free; refusing to prompt", cell.ID, pre.ActualModel)
			}
		})
	}

	if opts.preflightOnly {
		writeBatchSummary(t, batch)
		return
	}

	for _, cell := range cells {
		cell := cell
		for _, task := range cell.Tasks {
			task := task
			t.Run(cell.ID+"/"+task.Name, func(t *testing.T) {
				pre := batch.preflight(cell.ID)
				if pre == nil {
					t.Skipf("no preflight for cell %s", cell.ID)
				}
				if pre.Error != "" {
					t.Skipf("preflight for cell %s failed: %s", cell.ID, pre.Error)
				}
				if !pre.Free {
					t.Skipf("preflight for cell %s selected non-free model %q", cell.ID, pre.ActualModel)
				}
				runLiveTask(t, batch, pre, cell, task, opts)
			})
		}
	}

	writeBatchSummary(t, batch)
}

// liveOptions is one probe invocation.
