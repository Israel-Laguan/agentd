//go:build live

package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"
)

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

	p, err := start(Config{Cwd: workspace, Command: opts.command(workspace, string(cfgJSON), logs)}, nil)
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
