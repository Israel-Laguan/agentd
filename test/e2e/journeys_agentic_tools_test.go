//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
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

// TestJ19_AgenticDenylistRefusesSudo tests J19: an agentic task whose model asks
// for `sudo id` through the bash tool has the call refused by the denylist hook,
// and the privilege recovery asks the model for one non-privileged alternative.
//
// The mock answers the loop's first turn with that tool call (T-053), so the
// worker's real hook chain runs. The alternative request gets the mock's
// default command (build_command), which the sandbox runs. Pass criteria: the
// first TOOL_RESULT is the hook's refusal (exit code -1, names sudo), the
// second is the alternative's successful run, no event carries the refused
// command's output, and the model's next turn carries the alternative's output.
func TestJ19_AgenticDenylistRefusesSudo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J19 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)
	task := agenticTask(ctx, t, client, "J19", ScriptedToolCall{Name: "bash", Arguments: map[string]any{"command": "sudo id"}})

	// The refusal, then the alternative the privilege recovery ran.
	results := toolResultEvents(ctx, t, client, "J19", task.ID)
	if len(results) != 2 {
		t.Fatalf("J19 [events] want two TOOL_RESULTs (refusal, alternative), got %+v", results)
	}
	got := results[0]
	if got.ToolName != "bash" || got.ExitCode != -1 || !strings.Contains(strings.ToLower(got.OutputSummary), "sudo") {
		t.Fatalf("J19 [denylist] TOOL_RESULT = %+v, want bash refused with exit -1 naming sudo", got)
	}
	if strings.Contains(got.OutputSummary, "uid=") {
		t.Fatalf("J19 [denylist] the command ran: output %q", got.OutputSummary)
	}
	alternative := results[1]
	if alternative.ToolName != "bash" || alternative.ExitCode != 0 || !strings.Contains(alternative.OutputSummary, "executed via litellm proxy") {
		t.Fatalf("J19 [alternative] TOOL_RESULT = %+v, want the non-privileged alternative to run", alternative)
	}

	if use := toolUse(ctx, t, client, task.ID); len(use.Tools) != 1 || use.Tools[0].Tool != "bash" || use.Tools[0].Calls != 2 || use.Tools[0].Failed != 1 {
		t.Fatalf("J19 [tool use] report = %+v, want two bash calls with one failed", use.Tools)
	}

	prompts, err := NewMockLLMClient(mockLLMBaseURL).WorkerPrompts(ctx, task.ID)
	if err != nil {
		t.Fatalf("J19 [mock capture] %v", err)
	}
	if len(prompts) < 2 || !strings.Contains(prompts[len(prompts)-1], "executed via litellm proxy") {
		t.Fatalf("J19 [mock capture] the next turn did not carry the alternative's output: %d prompts", len(prompts))
	}
}

// TestJ22_AgentdReportAIInsideContainer tests J22: `agentd report ai`, run inside
// the agentd container, prints the task's AI calls grouped by purpose, and the
// count it prints is the count the API reports.
//
// The task is one scripted agentic run (J19's shape), so the exec.turn rows
// exist. Pass criteria: the CLI's table has a PURPOSE header and an exec.turn
// row, and its CALLS for exec.turn equals the ai-usage report's calls.
func TestJ22_AgentdReportAIInsideContainer(t *testing.T) {
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
