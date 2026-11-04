//go:build live

package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"agentd/internal/models"
)

func runLiveTask(t *testing.T, batch *liveBatch, pre *livePreflight, cell liveCell, task liveTask, opts liveOptions) {
	for run := 1; run <= opts.runs; run++ {
		var rec *liveRun
		for attempt := 1; attempt <= 1+opts.quotaRetries; attempt++ {
			rec = runLiveAttempt(t, opts, pre, cell, task, run, attempt)
			rec.Retries = attempt - 1
			batch.add(rec)
			if rec.ActualModel != "" && !isFreeModel(rec.ActualModel) {
				break
			}
			if rec.Quota && attempt <= opts.quotaRetries {
				t.Logf("%s/%s run %d attempt %d: quota error; waiting %s before retrying", cell.ID, task.Name, run, attempt, probeQuotaWait)
				time.Sleep(probeQuotaWait)
				continue
			}
			break
		}
		logRun(t, rec)
		if rec.ActualModel != "" && !isFreeModel(rec.ActualModel) {
			t.Errorf("run used non-free model %q; stopping cell %s", rec.ActualModel, cell.ID)
			return
		}
		if rec.TimedOut {
			t.Logf("%s/%s run %d timed out after %s; continuing", cell.ID, task.Name, run, probeCallTimeout)
		}
	}
}

func runLiveAttempt(t *testing.T, opts liveOptions, pre *livePreflight, cell liveCell, task liveTask, run, attempt int) *liveRun {
	dir := filepath.Join(opts.root, "runs", cell.ID, task.Name,
		fmt.Sprintf("run-%02d", run), fmt.Sprintf("attempt-%02d", attempt))
	workspace := filepath.Join(dir, "workspace")
	logs := liveLogs{
		In:  filepath.Join(dir, "in.jsonl"),
		Out: filepath.Join(dir, "out.jsonl"),
		Err: filepath.Join(dir, "err.log"),
	}
	rec := &liveRun{
		Cell:           cell.ID,
		Task:           task.Name,
		Run:            run,
		Attempt:        attempt,
		ConfigModel:    opts.model,
		PreflightModel: pre.ActualModel,
		Permit:         cell.Permit,
		Logs:           logs,
	}

	fail := func(format string, args ...any) *liveRun {
		rec.Error = fmt.Sprintf(format, args...)
		_ = writeJSON(filepath.Join(dir, "run.json"), rec)
		return rec
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fail("workspace: %v", err)
	}
	if err := task.Setup(workspace); err != nil {
		return fail("setup: %v", err)
	}

	cfgJSON, err := json.Marshal(cell.Config(opts.model))
	if err != nil {
		return fail("config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), append(cfgJSON, '\n'), 0o600); err != nil {
		return fail("write config: %v", err)
	}

	permitter := &livePermitter{allow: cell.Permit == "allow"}
	driver := NewDriver(Config{Cwd: workspace, Command: opts.command(workspace, string(cfgJSON), logs)})
	ctx, cancel := context.WithTimeout(context.Background(), probeCallTimeout)
	started := time.Now()
	result, callErr := driver.Call(ctx, Request{
		Prompt: task.Prompt,
		Permit: permitter.permit,
	})
	rec.WallMS = time.Since(started).Milliseconds()
	rec.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	cancel()
	if err := driver.Close(); err != nil {
		rec.CloseError = err.Error()
	}
	rec.SessionID = result.SessionID
	rec.StopReason = string(result.StopReason)
	if result.Usage != nil {
		if raw, err := json.Marshal(result.Usage); err == nil {
			rec.Usage = raw
		}
	}
	rec.Text = truncate(result.Text, 12000)
	if callErr != nil {
		rec.Error = callErr.Error()
		rec.Quota = errors.Is(callErr, models.ErrLLMQuotaExceeded) || isQuotaText(callErr.Error())
	}
	if rec.CloseError != "" && rec.Error == "" {
		rec.Error = "close: " + rec.CloseError
	}

	analysis, err := analyzeLogs(logs)
	if err != nil {
		rec.LogError = err.Error()
	} else {
		rec.BadInLines = len(analysis.BadInLines)
		rec.BadOutLines = len(analysis.BadOutLines)
		rec.SessionNew = analysis.NewSession
		rec.Prompt = analysis.Prompt
		rec.Permissions = analysis.Permissions
		if analysis.NewSession != nil && len(analysis.NewSession.ConfigOptions) > 0 {
			actual, _, err := modelInfo(analysis.NewSession.ConfigOptions)
			if err == nil {
				rec.ActualModel = actual
			} else {
				rec.LogError = appendError(rec.LogError, "model: "+err.Error())
			}
		}
		if analysis.Prompt != nil {
			if rec.StopReason == "" {
				rec.StopReason = analysis.Prompt.StopReason
			}
			if len(rec.Usage) == 0 && len(analysis.Prompt.Usage) > 0 {
				rec.Usage = analysis.Prompt.Usage
			}
		}
	}
	rec.PermitterCalls = permitter.snapshot()
	rec.PermitterCount = len(rec.PermitterCalls)
	markPermitterCalls(rec.Permissions, rec.PermitterCalls)
	rec.PermissionCount = len(rec.Permissions)
	for _, p := range rec.Permissions {
		if p.DriverRefused {
			rec.DriverRefused++
		}
	}
	rec.Files = task.Check(workspace)
	rec.WorkspaceFiles = workspaceFiles(workspace)
	rec.SawGoVersion = containsGoVersion(result.Text)
	rec.SawCowsay = containsCowsay(result.Text)
	if err := os.WriteFile(filepath.Join(dir, "result.txt"), []byte(result.Text), 0o600); err != nil {
		rec.LogError = appendError(rec.LogError, "write result.txt: "+err.Error())
	}
	if err := writeJSON(filepath.Join(dir, "run.json"), rec); err != nil {
		t.Errorf("write run.json: %v", err)
	}
	return rec
}

// livePermitter records every request that reaches the hook and answers it
// with the cell's policy.
type livePermitter struct {
	allow bool
	mu    sync.Mutex
	calls []livePermitterCall
}

func (p *livePermitter) permit(ctx context.Context, perm Permission) (Verdict, error) {
	at := time.Now()
	v := Verdict{Allow: p.allow}
	if !v.Allow {
		v.Reason = "probe: deny cell"
	}
	p.mu.Lock()
	p.calls = append(p.calls, livePermitterCall{
		ToolCallID: perm.ToolCallID,
		Kind:       perm.Kind,
		Tool:       perm.Tool,
		Command:    perm.Command,
		Args:       perm.Args,
		Title:      perm.Title,
		Allow:      v.Allow,
		Reason:     v.Reason,
		AtMS:       at.UnixMilli(),
	})
	p.mu.Unlock()
	return v, nil
}

func (p *livePermitter) snapshot() []livePermitterCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]livePermitterCall(nil), p.calls...)
}

func markPermitterCalls(perms []livePermission, calls []livePermitterCall) {
	byID := map[string]int{}
	for _, call := range calls {
		byID[call.ToolCallID]++
	}
	seen := map[string]int{}
	for i := range perms {
		id := perms[i].ToolCallID
		if seen[id] < byID[id] {
			perms[i].PermitterCalled = true
			seen[id]++
		}
		perms[i].DriverRefused = !perms[i].PermitterCalled
	}
}

// command returns the process for one agent run. bash wraps opencode so both
// directions of JSON-RPC are copied to disk while the Driver still owns the
// pipes. The whole process group is killed when the Driver cancels its
// context, so a child server cannot outlive the call.
