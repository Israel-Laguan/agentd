//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"agentd/internal/agent/acp/fakeagent"

	acpsdk "github.com/coder/acp-go-sdk"
)

// agenticTask materializes a one-task project whose task runs on its own
// agentic profile and whose description scripts the mock LLM's tool calls
// (T-053). It returns the task once it is COMPLETED.
func agenticTask(ctx context.Context, t *testing.T, client *APIClient, tag string, calls ...ScriptedToolCall) *Task {
	t.Helper()

	agentID, err := client.CreateAgent(ctx, UniqueProjectName(strings.ToLower(tag)), "litellm", "agentd")
	if err != nil {
		t.Fatalf("%s [agent] %v", tag, err)
	}
	materialized := materializePlan(ctx, t, client, tag, DraftPlan{
		ProjectName:         UniqueProjectName(strings.ToLower(tag)),
		Description:         tag + " agentic tool path",
		StartEmptyWorkspace: true,
		Tasks: []DraftTask{{
			Title:       tag + " scripted tool calls",
			Description: "Run the scripted tool calls. " + ToolCallsTag(calls...),
			AgentID:     agentID,
		}},
	})
	poller := NewTaskPoller(client, materialized.Project.ID)
	task, err := poller.WaitForTaskState(ctx, materialized.Tasks[0].ID, TaskStateCompleted, 90*time.Second)
	if err != nil {
		t.Fatalf("%s [run] task did not complete: %v (last observed: %+v)", tag, err, task)
	}
	return task
}

// toolResult is the part of a TOOL_RESULT event payload the journeys check.
type toolResult struct {
	ToolName      string `json:"tool_name"`
	ExitCode      int    `json:"exit_code"`
	OutputSummary string `json:"output_summary"`
}

// toolResultEvents returns the decoded TOOL_RESULT payloads of a task.
func toolResultEvents(ctx context.Context, t *testing.T, client *APIClient, tag, taskID string) []toolResult {
	t.Helper()
	events, err := client.ListTaskEvents(ctx, taskID)
	if err != nil {
		t.Fatalf("%s [events] %v", tag, err)
	}
	var out []toolResult
	for _, e := range events {
		if e.Type != "TOOL_RESULT" {
			continue
		}
		var r toolResult
		if err := json.Unmarshal([]byte(e.Payload), &r); err != nil {
			t.Fatalf("%s [events] TOOL_RESULT payload %q: %v", tag, e.Payload, err)
		}
		out = append(out, r)
	}
	return out
}

// TestJ19_ACPPermissionRefusesSudo tests J19 on the ACP path: the fake agent
// asks permission to run `sudo id`, the real ACPPermitter refuses it through the
// command policy, and the agent sees reject_once (not allow_once). The refusal
// is observable in the attempt handoff event the worker writes for the retry.
func TestJ19_ACPPermissionRefusesSudo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(acpBaseURL, "acp")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J19 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(acpBaseURL, harness.client)

	script := fakeagent.Script(
		fakeagent.Step{Permission: &fakeagent.Permission{
			Kind:     acpsdk.ToolKindExecute,
			Title:    "bash",
			RawInput: map[string]any{"command": "sudo id"},
		}},
		fakeagent.Step{Stop: acpsdk.StopReasonCancelled},
	)
	materialized := materializePlan(ctx, t, client, "J19", DraftPlan{
		ProjectName:         UniqueProjectName("j19"),
		Description:         "J19 ACP sudo permission",
		StartEmptyWorkspace: true,
		Tasks:               []DraftTask{{Title: "J19 acp sudo", Description: script}},
	})
	taskID := materialized.Tasks[0].ID

	deadline := time.Now().Add(60 * time.Second)
	var last []TaskEvent
	for time.Now().Before(deadline) {
		events, err := client.ListTaskEvents(ctx, taskID)
		if err != nil {
			t.Fatalf("J19 [events] %v", err)
		}
		last = events
		for _, ev := range events {
			if ev.Type == "ATTEMPT_HANDOFF" && strings.Contains(ev.Payload, "permission: reject_once") {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("J19 [events] no ATTEMPT_HANDOFF carried reject_once; last events: %+v", last)
}

// TestJ22_AgentdReportAIInsideContainer tests J22: `agentd report ai`, run inside
// the agentd container, prints the task's AI calls grouped by purpose, and the
// count it prints is the count the API reports.
//
// The task is one scripted agentic run (J19's shape), so the exec.turn rows
// exist. Pass criteria: the CLI's table has a PURPOSE header and an exec.turn
// row, and its CALLS for exec.turn equals the ai-usage report's calls.
func TestJ22_AgentdReportAIInsideContainer(t *testing.T) {
	t.Skip("moves in T-071")
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J22 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)
	task := agenticTask(ctx, t, client, "J22", ScriptedToolCall{Name: "bash", Arguments: map[string]any{"command": "uname -a"}})

	row := execTurnRow(ctx, t, client, task.ID, 1)

	devenv := NewDevenvManager(composePath, "default")
	out, err := devenv.Exec(ctx, "agentd", "report", "ai", "--api-url", "http://127.0.0.1:8765",
		"--group-by", "purpose", "--task", task.ID)
	if err != nil {
		t.Fatalf("J22 [cli] %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "PURPOSE") {
		t.Fatalf("J22 [cli] output has no PURPOSE table: %q", out)
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "exec.turn" {
			continue
		}
		if fields[1] != strconv.Itoa(row.Calls) {
			t.Fatalf("J22 [cli] exec.turn CALLS = %s, API reports %d", fields[1], row.Calls)
		}
		return
	}
	t.Fatalf("J22 [cli] no exec.turn row in %q", out)
}

// TestJ20_AgenticOversizedToolResultIsCut tests J20: a tool result larger than
// the agentic context budget does not stop the task, and the next request shows
// the working zone cut (head and tail kept, truncation marker between).
//
// The scripted bash call prints a head marker, ~56 KB of filler and a tail
// marker, over the default profile's queue.agentic_context total (10000,
// devenv/agentd/config.yaml). The working zone is cut with
// gateway.truncation.strategy (default head_tail), so both markers survive with
// the truncation marker between them.
func TestJ20_AgenticOversizedToolResultIsCut(t *testing.T) {
	t.Skip("moves in T-071")
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J20 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)
	const rawSize = 56000
	task := agenticTask(ctx, t, client, "J20", ScriptedToolCall{Name: "bash", Arguments: map[string]any{
		"command": "echo HEAD-MARK; yes filler | head -n 8000; echo TAIL-MARK",
	}})

	requests, err := NewMockLLMClient(mockLLMBaseURL).Requests(ctx)
	if err != nil {
		t.Fatalf("J20 [mock capture] %v", err)
	}
	var toolMessage string
	for _, r := range requests {
		if r.taskID() != task.ID {
			continue
		}
		for _, m := range r.Messages {
			if m.Role == "tool" {
				toolMessage = m.Content
			}
		}
	}
	row := execTurnRow(ctx, t, client, task.ID, 2)
	t.Logf("J20 [composition] input chars %d, mix %v, truncated %d, cut by %v", row.InputChars, row.MixChars, row.TruncatedCalls, row.CutBy)
	if row.MixChars["tool_results"] == 0 || row.MixChars["system"] == 0 || row.TruncatedCalls == 0 {
		t.Fatalf("J20 [composition] want a tool-result and system share and a cut call, got mix %v truncated %d", row.MixChars, row.TruncatedCalls)
	}
	if toolMessage == "" {
		t.Fatalf("J20 [mock capture] no request for task %s carried a tool result", task.ID)
	}
	if len(toolMessage) >= rawSize {
		t.Fatalf("J20 [truncation] tool result was sent whole (%d bytes), want it cut below %d", len(toolMessage), rawSize)
	}
	for _, want := range []string{"HEAD-MARK", "TAIL-MARK", "【...】"} {
		if !strings.Contains(toolMessage, want) {
			t.Fatalf("J20 [truncation] cut tool result lacks %q (len %d)", want, len(toolMessage))
		}
	}
}

// TestJ21_AgenticRepeatedReadIsCounted tests J21: a task that reads the same
// file twice shows up in the tool use report as one repeated read, with the
// bytes the second read added. This is the baseline for any later
// "don't re-read files" work; nothing here deduplicates.
func TestJ21_AgenticRepeatedReadIsCounted(t *testing.T) {
	t.Skip("moves in T-071")
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J21 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)
	read := ScriptedToolCall{Name: "read", Arguments: map[string]any{"path": "notes.txt"}}
	task := agenticTask(ctx, t, client, "J21",
		ScriptedToolCall{Name: "bash", Arguments: map[string]any{"command": "echo hello > notes.txt"}}, read, read)

	use := toolUse(ctx, t, client, task.ID)
	t.Logf("J21 [tool use] tools %+v, reads %d, read bytes %d, re-read bytes %d, repeated %+v",
		use.Tools, use.Reads, use.ReadBytes, use.RereadBytes, use.RepeatedReads)
	if use.Reads != 2 || len(use.RepeatedReads) != 1 || use.RepeatedReads[0].Path != "notes.txt" || use.RepeatedReads[0].Reads != 2 || use.RereadBytes == 0 {
		t.Fatalf("J21 [tool use] want notes.txt read twice with re-read bytes, got %+v", use)
	}
}
