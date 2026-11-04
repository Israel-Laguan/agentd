//go:build live

package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func (o liveOptions) command(workspace, configJSON string, logs liveLogs) func(context.Context, string) (*exec.Cmd, error) {
	return func(ctx context.Context, _ string) (*exec.Cmd, error) {
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
