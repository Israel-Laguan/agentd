//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"agentd/internal/models"
)

// getReport reads GET /api/v1/reports/<name>?<query> and decodes the data object.
func getReport(ctx context.Context, t *testing.T, client *APIClient, name string, query url.Values, out any) {
	t.Helper()
	resp, err := client.Get(ctx, "/api/v1/reports/"+name+"?"+query.Encode())
	if err != nil {
		t.Fatalf("report %s: %v", name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil || resp.StatusCode != 200 {
		t.Fatalf("report %s: HTTP %d, decode err %v", name, resp.StatusCode, err)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("report %s: decode %s: %v", name, env.Data, err)
	}
}

// execTurnRow waits for the task's exec.turn records to reach ai_calls (the
// writer batches every 2 s) and returns the aggregated row.
func execTurnRow(ctx context.Context, t *testing.T, client *APIClient, taskID string, minCalls int) models.AIUsageRow {
	t.Helper()
	q := url.Values{"task_id": {taskID}, "group_by": {"purpose"}, "purpose": {"exec.turn"}}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var rep models.AIUsageReport
		getReport(ctx, t, client, "ai-usage", q, &rep)
		if len(rep.Rows) == 1 && rep.Rows[0].Calls >= minCalls {
			return rep.Rows[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s: ai_calls never reached %d exec.turn records, report: %+v", taskID, minCalls, rep)
		}
		time.Sleep(time.Second)
	}
}

// toolUse returns the tool use report of one task.
func toolUse(ctx context.Context, t *testing.T, client *APIClient, taskID string) models.ToolUseReport {
	t.Helper()
	var rep models.ToolUseReport
	getReport(ctx, t, client, "tool-use", url.Values{"task_id": {taskID}}, &rep)
	return rep
}
