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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"agentd/internal/models"

	acpsdk "github.com/coder/acp-go-sdk"
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
type liveOptions struct {
	model         string
	runs          int
	cells         []string
	tasks         []string
	quotaRetries  int
	preflightOnly bool
	root          string
	bash          string
	stdbuf        string
	opencode      string
}

// liveCell is one configuration and permission policy. Tasks are the prompts
// run under it.
type liveCell struct {
	ID     string
	Config func(model string) map[string]any
	Permit string
	Tasks  []liveTask
}

// liveTask is one prompt and its workspace setup and post-run checks.
type liveTask struct {
	Name   string
	Prompt string
	Setup  func(workspace string) error
	Check  func(workspace string) map[string]any
}

func probeT1() liveTask {
	return liveTask{
		Name:   "T1",
		Prompt: "Read hello.txt and write the word it contains to out.txt.",
		Setup: func(workspace string) error {
			return os.WriteFile(filepath.Join(workspace, "hello.txt"), []byte("garnet\n"), 0o644)
		},
		Check: func(workspace string) map[string]any {
			out := map[string]any{}
			raw, err := os.ReadFile(filepath.Join(workspace, "out.txt"))
			if err != nil {
				out["out.txt.error"] = err.Error()
				out["out.txt.correct"] = false
				return out
			}
			word := strings.TrimSpace(string(raw))
			out["out.txt"] = truncate(word, 500)
			out["out.txt.correct"] = strings.EqualFold(word, "garnet")
			return out
		},
	}
}

func probeT2() liveTask {
	return liveTask{
		Name:   "T2",
		Prompt: "Run `go version` and tell me the output. Then run `touch ran.marker` so I know the shell worked.",
		Setup:  func(string) error { return nil },
		Check: func(workspace string) map[string]any {
			out := map[string]any{}
			if _, err := os.Stat(filepath.Join(workspace, "ran.marker")); err == nil {
				out["ran.marker"] = true
			} else {
				out["ran.marker"] = false
				out["ran.marker.error"] = err.Error()
			}
			return out
		},
	}
}

func probeT3() liveTask {
	return liveTask{
		Name:   "T3",
		Prompt: "Run `npx -y cowsay hi` and show the output.",
		Setup:  func(string) error { return nil },
		Check:  func(string) map[string]any { return map[string]any{} },
	}
}

func probeCells() []liveCell {
	ask := func(model string) map[string]any {
		return map[string]any{
			"model":      model,
			"permission": map[string]any{"bash": "ask", "edit": "ask", "webfetch": "ask"},
		}
	}
	defaultConfig := func(model string) map[string]any {
		return map[string]any{"model": model}
	}
	wildcard := func(model string) map[string]any {
		return map[string]any{
			"model":      model,
			"permission": map[string]any{"*": "ask"},
		}
	}
	return []liveCell{
		{ID: "A", Config: defaultConfig, Permit: "allow", Tasks: []liveTask{probeT1(), probeT2()}},
		{ID: "B", Config: ask, Permit: "allow", Tasks: []liveTask{probeT1(), probeT2()}},
		{ID: "C", Config: ask, Permit: "deny", Tasks: []liveTask{probeT1(), probeT2()}},
		{ID: "D", Config: ask, Permit: "allow", Tasks: []liveTask{probeT3()}},
		{ID: "E", Config: wildcard, Permit: "allow", Tasks: []liveTask{probeT1()}},
	}
}

func selectedCells(ids, taskIDs []string) ([]liveCell, error) {
	all := probeCells()
	byID := map[string]liveCell{}
	for _, cell := range all {
		byID[cell.ID] = cell
	}
	var cells []liveCell
	for _, id := range ids {
		cell, ok := byID[strings.ToUpper(strings.TrimSpace(id))]
		if !ok {
			return nil, fmt.Errorf("unknown cell %q (want A,B,C,D,E)", id)
		}
		cells = append(cells, cell)
	}
	if len(cells) == 0 {
		return nil, errors.New("no cells selected")
	}
	wantTasks := map[string]bool{}
	for _, id := range taskIDs {
		id = strings.ToUpper(strings.TrimSpace(id))
		if id == "" {
			continue
		}
		if id != "T1" && id != "T2" && id != "T3" {
			return nil, fmt.Errorf("unknown task %q (want T1,T2,T3)", id)
		}
		wantTasks[id] = true
	}
	if len(wantTasks) > 0 {
		for i := range cells {
			var kept []liveTask
			for _, task := range cells[i].Tasks {
				if wantTasks[task.Name] {
					kept = append(kept, task)
				}
			}
			cells[i].Tasks = kept
		}
	}
	var nonEmpty []liveCell
	for _, cell := range cells {
		if len(cell.Tasks) > 0 {
			nonEmpty = append(nonEmpty, cell)
		}
	}
	if len(nonEmpty) == 0 {
		return nil, errors.New("no cell has a selected task")
	}
	return nonEmpty, nil
}

// livePreflight is the check that the configured free model is the one
// session/new selected, made before any prompt is sent.
type livePreflight struct {
	Cell          string          `json:"cell"`
	ConfigModel   string          `json:"configModel"`
	ConfigJSON    string          `json:"configJson"`
	ActualModel   string          `json:"actualModel,omitempty"`
	Available     []string        `json:"availableModels,omitempty"`
	SessionID     string          `json:"sessionId,omitempty"`
	ConfigOptions json.RawMessage `json:"configOptions,omitempty"`
	Free          bool            `json:"free"`
	Error         string          `json:"error,omitempty"`
	Logs          liveLogs        `json:"logs"`
}

func preflightCell(parent context.Context, opts liveOptions, cell liveCell) *livePreflight {
	pre := &livePreflight{Cell: cell.ID, ConfigModel: opts.model}
	dir := filepath.Join(opts.root, "preflight", cell.ID)
	workspace := filepath.Join(dir, "workspace")
	logs := liveLogs{
		In:  filepath.Join(dir, "in.jsonl"),
		Out: filepath.Join(dir, "out.jsonl"),
		Err: filepath.Join(dir, "err.log"),
	}
	pre.Logs = logs
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		pre.Error = "workspace: " + err.Error()
		return pre
	}

	cfgJSON, err := json.Marshal(cell.Config(opts.model))
	if err != nil {
		pre.Error = "config: " + err.Error()
		return pre
	}
	pre.ConfigJSON = string(cfgJSON)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), append(cfgJSON, '\n'), 0o600); err != nil {
		pre.Error = "write config: " + err.Error()
		return pre
	}

	p, err := start(opts.command(workspace, string(cfgJSON), logs))
	if err != nil {
		pre.Error = "start: " + err.Error()
		return pre
	}
	defer p.kill()

	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	if err := p.handshake(ctx, ""); err != nil {
		pre.Error = "initialize: " + err.Error()
		return pre
	}
	created, err := p.conn.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: workspace, McpServers: []acpsdk.McpServer{}})
	if err != nil {
		pre.Error = "session/new: " + err.Error()
		return pre
	}
	pre.SessionID = string(created.SessionId)

	// session/new returns through the Driver before tee has necessarily
	// finished writing the same line to out.jsonl; wait briefly for the copy.
	var ns *liveNewSession
	deadline := time.Now().Add(3 * time.Second)
	for {
		out, _, err := readEnvelopes(logs.Out)
		if err != nil {
			pre.Error = "read out.jsonl: " + err.Error()
			return pre
		}
		ns = findNewSession(out, nil)
		if ns != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if ns == nil {
		pre.Error = "session/new response not found in out.jsonl"
		return pre
	}
	pre.SessionID = ns.SessionID
	pre.ConfigOptions = ns.ConfigOptions
	if len(ns.ConfigOptions) == 0 {
		pre.Error = "session/new returned no configOptions"
		return pre
	}
	actual, available, err := modelInfo(ns.ConfigOptions)
	if err != nil {
		pre.Error = "configOptions: " + err.Error()
		return pre
	}
	pre.ActualModel = actual
	pre.Available = available
	pre.Free = isFreeModel(actual)
	if !pre.Free {
		pre.Error = fmt.Sprintf("session/new selected %q, which is not a -free model", actual)
	}
	return pre
}

// liveBatch is all records of one invocation.
type liveBatch struct {
	mu              sync.Mutex
	Root            string                    `json:"root"`
	Started         string                    `json:"started"`
	OpenCodeVersion string                    `json:"opencodeVersion,omitempty"`
	Model           string                    `json:"model"`
	Runs            int                       `json:"runs"`
	Cells           []string                  `json:"cells"`
	Tasks           []string                  `json:"tasks"`
	Config          string                    `json:"config"`
	Preflights      map[string]*livePreflight `json:"preflights"`
	Records         []*liveRun                `json:"records"`
}

func (b *liveBatch) addPreflight(pre *livePreflight) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Preflights[pre.Cell] = pre
}

func (b *liveBatch) preflight(cell string) *livePreflight {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Preflights[cell]
}

func (b *liveBatch) add(rec *liveRun) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Records = append(b.Records, rec)
}

func (b *liveBatch) snapshot() (map[string]*livePreflight, []*liveRun) {
	b.mu.Lock()
	defer b.mu.Unlock()
	pre := make(map[string]*livePreflight, len(b.Preflights))
	for k, v := range b.Preflights {
		pre[k] = v
	}
	rec := append([]*liveRun(nil), b.Records...)
	return pre, rec
}

// liveLogs are the three streams of one process. in.jsonl is client to agent,
// out.jsonl is agent to client, err.log is the agent's stderr.
type liveLogs struct {
	In  string `json:"in"`
	Out string `json:"out"`
	Err string `json:"err"`
}

// liveRun is one prompt attempt.
type liveRun struct {
	Cell            string              `json:"cell"`
	Task            string              `json:"task"`
	Run             int                 `json:"run"`
	Attempt         int                 `json:"attempt"`
	Retries         int                 `json:"retries"`
	ConfigModel     string              `json:"configModel"`
	PreflightModel  string              `json:"preflightModel,omitempty"`
	ActualModel     string              `json:"actualModel,omitempty"`
	Permit          string              `json:"permit"`
	SessionID       string              `json:"sessionId,omitempty"`
	StopReason      string              `json:"stopReason,omitempty"`
	Usage           json.RawMessage     `json:"usage,omitempty"`
	Error           string              `json:"error,omitempty"`
	CloseError      string              `json:"closeError,omitempty"`
	LogError        string              `json:"logError,omitempty"`
	TimedOut        bool                `json:"timedOut"`
	Quota           bool                `json:"quota"`
	WallMS          int64               `json:"wallMs"`
	PermissionCount int                 `json:"permissionCount"`
	PermitterCount  int                 `json:"permitterCount"`
	DriverRefused   int                 `json:"driverRefused"`
	Permissions     []livePermission    `json:"permissions,omitempty"`
	PermitterCalls  []livePermitterCall `json:"permitterCalls,omitempty"`
	SessionNew      *liveNewSession     `json:"sessionNew,omitempty"`
	Prompt          *livePrompt         `json:"prompt,omitempty"`
	BadInLines      int                 `json:"badInLines"`
	BadOutLines     int                 `json:"badOutLines"`
	Files           map[string]any      `json:"files,omitempty"`
	WorkspaceFiles  []string            `json:"workspaceFiles,omitempty"`
	Text            string              `json:"text"`
	SawGoVersion    bool                `json:"sawGoVersion"`
	SawCowsay       bool                `json:"sawCowsay"`
	Logs            liveLogs            `json:"logs"`
}

type livePermission struct {
	Index           int             `json:"index"`
	RequestID       string          `json:"requestId"`
	SessionID       string          `json:"sessionId,omitempty"`
	ToolCallID      string          `json:"toolCallId,omitempty"`
	Kind            string          `json:"kind,omitempty"`
	Title           string          `json:"title,omitempty"`
	Status          string          `json:"status,omitempty"`
	RawInput        json.RawMessage `json:"rawInput,omitempty"`
	Locations       json.RawMessage `json:"locations,omitempty"`
	Content         json.RawMessage `json:"content,omitempty"`
	Options         []liveOption    `json:"options,omitempty"`
	Response        json.RawMessage `json:"response,omitempty"`
	Outcome         string          `json:"outcome,omitempty"`
	SelectedOption  string          `json:"selectedOption,omitempty"`
	PermitterCalled bool            `json:"permitterCalled"`
	DriverRefused   bool            `json:"driverRefused"`
	RawRequest      json.RawMessage `json:"rawRequest"`
}

type liveOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type livePermitterCall struct {
	ToolCallID string `json:"toolCallId,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Command    string `json:"command,omitempty"`
	Args       string `json:"args,omitempty"`
	Title      string `json:"title,omitempty"`
	Allow      bool   `json:"allow"`
	Reason     string `json:"reason,omitempty"`
	Error      string `json:"error,omitempty"`
	AtMS       int64  `json:"atMs"`
}

type liveNewSession struct {
	SessionID     string          `json:"sessionId"`
	ConfigOptions json.RawMessage `json:"configOptions,omitempty"`
	Modes         json.RawMessage `json:"modes,omitempty"`
	Raw           json.RawMessage `json:"raw"`
}

type livePrompt struct {
	StopReason string          `json:"stopReason,omitempty"`
	Usage      json.RawMessage `json:"usage,omitempty"`
	Raw        json.RawMessage `json:"raw"`
}

type liveLogAnalysis struct {
	NewSession  *liveNewSession
	Prompt      *livePrompt
	Permissions []livePermission
	BadInLines  []string
	BadOutLines []string
}

func runLiveTask(t *testing.T, batch *liveBatch, pre *livePreflight, cell liveCell, task liveTask, opts liveOptions) {
	for run := 1; run <= opts.runs; run++ {
		var rec *liveRun
		for attempt := 1; attempt <= 1+opts.quotaRetries; attempt++ {
			rec = runLiveAttempt(t, opts, pre, cell, task, run, attempt)
			if rec.Quota && attempt <= opts.quotaRetries {
				t.Logf("%s/%s run %d attempt %d: quota error; waiting %s before retrying", cell.ID, task.Name, run, attempt, probeQuotaWait)
				time.Sleep(probeQuotaWait)
				continue
			}
			break
		}
		rec.Retries = rec.Attempt - 1
		batch.add(rec)
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
	driver := NewDriver(Config{Command: opts.command(workspace, string(cfgJSON), logs)})
	ctx, cancel := context.WithTimeout(context.Background(), probeCallTimeout)
	started := time.Now()
	result, callErr := driver.Call(ctx, Request{
		Cwd:    workspace,
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
func (o liveOptions) command(workspace, configJSON string, logs liveLogs) func(context.Context) (*exec.Cmd, error) {
	return func(ctx context.Context) (*exec.Cmd, error) {
		script := fmt.Sprintf(
			"umask 077\nset -o pipefail\ntee -a %s | %s -oL %s acp 2>>%s | %s -oL tee -a %s\n",
			shq(logs.In), shq(o.stdbuf), shq(o.opencode), shq(logs.Err), shq(o.stdbuf), shq(logs.Out),
		)
		cmd := exec.CommandContext(ctx, o.bash, "-c", script)
		cmd.Dir = workspace
		cmd.Env = liveEnv(configJSON)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return nil
			}
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		cmd.WaitDelay = 10 * time.Second
		return cmd, nil
	}
}

// liveEnv drops the parent OpenCode session markers, so the child does not
// think it is a nested session, then sets the one config value the probe uses.
func liveEnv(configJSON string) []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if strings.HasPrefix(name, "OPENCODE") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "OPENCODE_CONFIG_CONTENT="+configJSON)
}

type rawEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
	Raw     json.RawMessage `json:"-"`
}

func (m rawEnvelope) id() string {
	return strings.TrimSpace(string(m.ID))
}

func readEnvelopes(path string) ([]rawEnvelope, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var msgs []rawEnvelope
	var bad []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m rawEnvelope
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			bad = append(bad, truncate(line, 500))
			continue
		}
		m.Raw = json.RawMessage(line)
		msgs = append(msgs, m)
	}
	return msgs, bad, nil
}

func analyzeLogs(logs liveLogs) (*liveLogAnalysis, error) {
	in, badIn, err := readEnvelopes(logs.In)
	if err != nil {
		return nil, fmt.Errorf("read in.jsonl: %w", err)
	}
	out, badOut, err := readEnvelopes(logs.Out)
	if err != nil {
		return nil, fmt.Errorf("read out.jsonl: %w", err)
	}
	a := &liveLogAnalysis{BadInLines: badIn, BadOutLines: badOut}
	requests := requestMethods(in)
	responses := responsesByID(in)
	a.NewSession = findNewSession(out, requests)
	a.Prompt = findPrompt(out, requests)
	for _, m := range out {
		if m.Method != "session/request_permission" {
			continue
		}
		p := parsePermission(m)
		if resp, ok := responses[m.id()]; ok {
			p.Response = resp.Raw
			p.Outcome, p.SelectedOption = parseOutcome(resp.Result)
		}
		a.Permissions = append(a.Permissions, p)
	}
	for i := range a.Permissions {
		a.Permissions[i].Index = i
	}
	return a, nil
}

func requestMethods(in []rawEnvelope) map[string]string {
	m := map[string]string{}
	for _, msg := range in {
		if msg.Method != "" && msg.id() != "" && msg.id() != "null" {
			m[msg.id()] = msg.Method
		}
	}
	return m
}

func responsesByID(in []rawEnvelope) map[string]rawEnvelope {
	m := map[string]rawEnvelope{}
	for _, msg := range in {
		if msg.Method == "" && msg.id() != "" && msg.id() != "null" {
			m[msg.id()] = msg
		}
	}
	return m
}

func findNewSession(out []rawEnvelope, requests map[string]string) *liveNewSession {
	for _, m := range out {
		if m.Method != "" || m.id() == "" {
			continue
		}
		if requests != nil && requests[m.id()] != "session/new" {
			continue
		}
		var r struct {
			SessionID     string          `json:"sessionId"`
			ConfigOptions json.RawMessage `json:"configOptions"`
			Modes         json.RawMessage `json:"modes"`
		}
		if json.Unmarshal(m.Result, &r) != nil || r.SessionID == "" {
			continue
		}
		return &liveNewSession{SessionID: r.SessionID, ConfigOptions: r.ConfigOptions, Modes: r.Modes, Raw: m.Raw}
	}
	return nil
}

func findPrompt(out []rawEnvelope, requests map[string]string) *livePrompt {
	for _, m := range out {
		if m.Method != "" || m.id() == "" {
			continue
		}
		if requests != nil && requests[m.id()] != "session/prompt" {
			continue
		}
		var r struct {
			StopReason string          `json:"stopReason"`
			Usage      json.RawMessage `json:"usage"`
		}
		if json.Unmarshal(m.Result, &r) != nil || r.StopReason == "" {
			continue
		}
		return &livePrompt{StopReason: r.StopReason, Usage: r.Usage, Raw: m.Raw}
	}
	return nil
}

func parsePermission(m rawEnvelope) livePermission {
	var params struct {
		SessionID string `json:"sessionId"`
		ToolCall  struct {
			ToolCallID string          `json:"toolCallId"`
			Title      *string         `json:"title"`
			Kind       *string         `json:"kind"`
			Status     *string         `json:"status"`
			RawInput   json.RawMessage `json:"rawInput"`
			Locations  json.RawMessage `json:"locations"`
			Content    json.RawMessage `json:"content"`
		} `json:"toolCall"`
		Options []struct {
			OptionID string `json:"optionId"`
			Name     string `json:"name"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(m.Params, &params)
	p := livePermission{
		RequestID:  m.id(),
		SessionID:  params.SessionID,
		ToolCallID: params.ToolCall.ToolCallID,
		RawInput:   params.ToolCall.RawInput,
		Locations:  params.ToolCall.Locations,
		Content:    params.ToolCall.Content,
		RawRequest: m.Raw,
	}
	if params.ToolCall.Title != nil {
		p.Title = *params.ToolCall.Title
	}
	if params.ToolCall.Kind != nil {
		p.Kind = *params.ToolCall.Kind
	}
	if params.ToolCall.Status != nil {
		p.Status = *params.ToolCall.Status
	}
	for _, o := range params.Options {
		p.Options = append(p.Options, liveOption{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	return p
}

func parseOutcome(result json.RawMessage) (string, string) {
	var r struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if json.Unmarshal(result, &r) != nil {
		return "", ""
	}
	return r.Outcome.Outcome, r.Outcome.OptionID
}

func modelInfo(raw json.RawMessage) (string, []string, error) {
	var opts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &opts); err != nil {
		return "", nil, err
	}
	var selected string
	var all []string
	for _, o := range opts {
		var id, name, category string
		_ = json.Unmarshal(o["id"], &id)
		_ = json.Unmarshal(o["name"], &name)
		_ = json.Unmarshal(o["category"], &category)
		var current string
		_ = json.Unmarshal(o["currentValue"], &current)
		if current != "" {
			all = append(all, current)
		}
		if id == "model" || category == "model" || strings.Contains(strings.ToLower(name), "model") {
			if current != "" {
				selected = current
			}
			all = append(all, valuesFromJSON(o["options"])...)
		}
	}
	all = dedupe(all)
	if selected == "" {
		for _, v := range all {
			if isFreeModel(v) {
				return "", all, fmt.Errorf("no model option selected; free values are %v", all)
			}
		}
		return "", all, errors.New("session/new configOptions has no model option")
	}
	return selected, all, nil
}

func valuesFromJSON(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "value" {
					if s, ok := val.(string); ok {
						out = append(out, s)
					}
				}
				walk(val)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(v)
	return out
}

func isFreeModel(id string) bool {
	id = strings.TrimSpace(id)
	if i := strings.IndexByte(id, '#'); i >= 0 {
		id = id[:i]
	}
	return strings.HasSuffix(id, "-free")
}

func logRun(t *testing.T, rec *liveRun) {
	t.Helper()
	t.Logf("%s/%s run %d attempt %d: model=%s permit=%s reqs=%d permitter=%d refused=%d stop=%s err=%v wall=%s",
		rec.Cell, rec.Task, rec.Run, rec.Attempt, rec.ActualModel, rec.Permit,
		rec.PermissionCount, rec.PermitterCount, rec.DriverRefused, rec.StopReason, rec.Error,
		time.Duration(rec.WallMS)*time.Millisecond)
	for _, p := range rec.Permissions {
		t.Logf("  permission %d id=%s kind=%s title=%q rawInput=%s locations=%s options=%v outcome=%s option=%s permitter=%t refused=%t",
			p.Index, p.RequestID, p.Kind, p.Title, jsonInline(p.RawInput), jsonInline(p.Locations),
			p.Options, p.Outcome, p.SelectedOption, p.PermitterCalled, p.DriverRefused)
	}
	for _, c := range rec.PermitterCalls {
		t.Logf("  hook tool=%s kind=%s command=%q args=%s allow=%t", c.Tool, c.Kind, c.Command, truncate(c.Args, 500), c.Allow)
	}
}

// liveBatchExport is the final summary document. It is separate from liveBatch
// because the live batch also carries the mutex and its initial, empty state.
type liveBatchExport struct {
	Root            string                    `json:"root"`
	Started         string                    `json:"started"`
	OpenCodeVersion string                    `json:"opencodeVersion,omitempty"`
	Model           string                    `json:"model"`
	Runs            int                       `json:"runs"`
	Cells           []string                  `json:"cells"`
	Tasks           []string                  `json:"tasks"`
	Config          string                    `json:"config"`
	Preflights      map[string]*livePreflight `json:"preflights"`
	Records         []*liveRun                `json:"records"`
}

func writeBatchSummary(t *testing.T, batch *liveBatch) {
	t.Helper()
	preflights, records := batch.snapshot()
	sort.Slice(records, func(i, j int) bool {
		if records[i].Cell != records[j].Cell {
			return records[i].Cell < records[j].Cell
		}
		if records[i].Task != records[j].Task {
			return records[i].Task < records[j].Task
		}
		if records[i].Run != records[j].Run {
			return records[i].Run < records[j].Run
		}
		return records[i].Attempt < records[j].Attempt
	})

	meta := liveBatchExport{
		Root:            batch.Root,
		Started:         batch.Started,
		OpenCodeVersion: batch.OpenCodeVersion,
		Model:           batch.Model,
		Runs:            batch.Runs,
		Cells:           batch.Cells,
		Tasks:           batch.Tasks,
		Config:          batch.Config,
		Preflights:      preflights,
		Records:         records,
	}
	if err := writeJSON(filepath.Join(batch.Root, "summary.json"), meta); err != nil {
		t.Errorf("write summary.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(batch.Root, "summary.md"), []byte(renderMarkdown(meta)), 0o600); err != nil {
		t.Errorf("write summary.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(batch.Root, "raw-permissions.md"), []byte(renderRawPermissions(records)), 0o600); err != nil {
		t.Errorf("write raw-permissions.md: %v", err)
	}
	t.Logf("wrote %s/summary.md, summary.json and raw-permissions.md", batch.Root)
}

func renderMarkdown(meta liveBatchExport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# opencode ACP live probe\n\n")
	fmt.Fprintf(&b, "- started: %s\n", meta.Started)
	fmt.Fprintf(&b, "- opencode: %s\n", meta.OpenCodeVersion)
	fmt.Fprintf(&b, "- configured model: `%s`\n", meta.Model)
	fmt.Fprintf(&b, "- cells: %v, tasks: %v, runs per cell/task: %d\n", meta.Cells, meta.Tasks, meta.Runs)
	fmt.Fprintf(&b, "- root: `%s`\n\n", meta.Root)

	fmt.Fprintf(&b, "## Preflight (session/new)\n\n")
	fmt.Fprintf(&b, "| Cell | Configured | Actual | Free | Session | Error |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|\n")
	for _, cell := range []string{"A", "B", "C", "D", "E"} {
		pre := meta.Preflights[cell]
		if pre == nil {
			continue
		}
		fmt.Fprintf(&b, "| %s | `%s` | `%s` | %t | `%s` | %s |\n",
			cell, pre.ConfigModel, pre.ActualModel, pre.Free, pre.SessionID, mdCell(pre.Error))
	}
	fmt.Fprintf(&b, "\n## Runs\n\n")
	fmt.Fprintf(&b, "| Cell | Task | Run | Att | Actual model | Permit | Reqs | Hook | Refused | Stop | Error | Wall |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, rec := range meta.Records {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | `%s` | %s | %d | %d | %d | %s | %s | %s |\n",
			rec.Cell, rec.Task, rec.Run, rec.Attempt, rec.ActualModel, rec.Permit,
			rec.PermissionCount, rec.PermitterCount, rec.DriverRefused, rec.StopReason,
			mdCell(rec.Error), time.Duration(rec.WallMS)*time.Millisecond)
	}
	fmt.Fprintf(&b, "\n## Permission requests\n\n")
	fmt.Fprintf(&b, "| Cell | Task | Run | # | Kind | Title | Raw input | Locations | Options | Outcome | Hook | Driver refused |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, rec := range meta.Records {
		for _, p := range rec.Permissions {
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s | `%s` | `%s` | %s | %s | %t | %t |\n",
				rec.Cell, rec.Task, rec.Run, p.Index+1, p.Kind, mdCell(p.Title),
				mdCell(jsonInline(p.RawInput)), mdCell(jsonInline(p.Locations)), optionNames(p.Options),
				p.Outcome, p.PermitterCalled, p.DriverRefused)
		}
	}
	fmt.Fprintf(&b, "\n## Task checks\n\n")
	fmt.Fprintf(&b, "| Cell | Task | Run | Result |\n")
	fmt.Fprintf(&b, "|---|---|---|---|\n")
	for _, rec := range meta.Records {
		fmt.Fprintf(&b, "| %s | %s | %d | %s |\n", rec.Cell, rec.Task, rec.Run, mdCell(checkSummary(rec)))
	}
	return b.String()
}

func renderRawPermissions(records []*liveRun) string {
	var b strings.Builder
	b.WriteString("# Raw permission requests\n\n")
	for _, rec := range records {
		if len(rec.Permissions) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s/%s run %d attempt %d\n\n", rec.Cell, rec.Task, rec.Run, rec.Attempt)
		for _, p := range rec.Permissions {
			fmt.Fprintf(&b, "### request %d (id %s, kind %s, hook=%t, driver-refused=%t)\n\n",
				p.Index+1, p.RequestID, p.Kind, p.PermitterCalled, p.DriverRefused)
			fmt.Fprintf(&b, "```json\n%s\n```\n\n", p.RawRequest)
			if len(p.Response) > 0 {
				fmt.Fprintf(&b, "```json\n%s\n```\n\n", p.Response)
			}
		}
	}
	return b.String()
}

func checkSummary(rec *liveRun) string {
	switch rec.Task {
	case "T1":
		return fmt.Sprintf("out.txt=%v correct=%v files=%v", rec.Files["out.txt"], rec.Files["out.txt.correct"], rec.WorkspaceFiles)
	case "T2":
		return fmt.Sprintf("ran.marker=%v sawGoVersion=%t files=%v", rec.Files["ran.marker"], rec.SawGoVersion, rec.WorkspaceFiles)
	case "T3":
		return fmt.Sprintf("sawCowsay=%t files=%v text=%q", rec.SawCowsay, rec.WorkspaceFiles, truncate(rec.Text, 240))
	default:
		return fmt.Sprintf("files=%v text=%q", rec.WorkspaceFiles, truncate(rec.Text, 240))
	}
}

func optionNames(options []liveOption) string {
	var parts []string
	for _, o := range options {
		parts = append(parts, fmt.Sprintf("%s/%s/%s", o.OptionID, o.Kind, o.Name))
	}
	return mdCell(strings.Join(parts, "; "))
}

func containsGoVersion(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "go version") || strings.Contains(lower, "go1.")
}

func containsCowsay(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "cowsay") || strings.Contains(text, "<") || strings.Contains(text, "____")
}

func workspaceFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func opencodeVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
	}
	return strings.TrimSpace(string(out))
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func jsonInline(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	return string(raw)
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "")
	return truncate(s, 500)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func appendError(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func isQuotaText(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, " 429") || strings.Contains(s, "429 ") ||
		strings.Contains(s, "rate limit") || strings.Contains(s, "rate-limit") ||
		strings.Contains(s, "too many requests") || strings.Contains(s, "quota") ||
		strings.Contains(s, "insufficient balance") || strings.Contains(s, "insufficient credits")
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil {
		return v
	}
	return fallback
}

func envList(name, fallback string) []string {
	raw := envOr(name, fallback)
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
