//go:build live

package acp

import (
	"encoding/json"
	"sync"
)

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
