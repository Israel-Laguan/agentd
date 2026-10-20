package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func toolCall(task, tool, path string, bytes int) models.ToolCall {
	return models.ToolCall{
		At: time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC), TaskID: task, CallID: "c-" + path,
		Tool: tool, Path: path, Status: "ok", ResultBytes: bytes,
	}
}

func TestInsertToolCallStoresEveryColumn(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	in := toolCall("t1", "read", "a.go", 120)
	in.Status = "failed"
	if err := store.InsertToolCall(context.Background(), in); err != nil {
		t.Fatalf("InsertToolCall: %v", err)
	}
	var got models.ToolCall
	var at string
	err := store.db.QueryRow(`SELECT at, task_id, call_id, tool, path, status, result_bytes FROM tool_calls`).
		Scan(&at, &got.TaskID, &got.CallID, &got.Tool, &got.Path, &got.Status, &got.ResultBytes)
	if err != nil {
		t.Fatal(err)
	}
	got.At = in.At
	if at != formatTime(in.At) || got != in {
		t.Fatalf("stored = %+v at %q, want %+v", got, at, in)
	}
}

func TestToolUseReportCountsToolsAndRepeatedReads(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	for _, c := range []models.ToolCall{
		toolCall("t1", "read", "a.go", 1000), toolCall("t1", "read", "a.go", 1000), toolCall("t1", "read", "a.go", 1000),
		toolCall("t1", "read", "b.go", 50), toolCall("t2", "read", "a.go", 1000),
		toolCall("t1", "bash", "", 10), {At: time.Now(), TaskID: "t1", CallID: "f", Tool: "bash", Status: "failed"},
	} {
		if err := store.InsertToolCall(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := store.ToolUseReport(ctx, models.ToolUseQuery{})
	if err != nil {
		t.Fatal(err)
	}
	byTool := map[string]models.ToolUseRow{}
	for _, r := range rep.Tools {
		byTool[r.Tool] = r
	}
	if r := byTool["read"]; r.Calls != 5 || r.ResultBytes != 4050 {
		t.Errorf("read row = %+v, want 5 calls / 4050 bytes", r)
	}
	if r := byTool["bash"]; r.Calls != 2 || r.Failed != 1 {
		t.Errorf("bash row = %+v, want 2 calls / 1 failed", r)
	}
	if len(rep.RepeatedReads) != 1 {
		t.Fatalf("repeated reads = %+v, want only t1:a.go", rep.RepeatedReads)
	}
	if r := rep.RepeatedReads[0]; r.TaskID != "t1" || r.Path != "a.go" || r.Reads != 3 || r.Bytes != 3000 || r.RereadBytes != 2000 {
		t.Errorf("repeated read = %+v, want t1 a.go 3 reads 3000 bytes 2000 re-read", r)
	}
	if rep.Reads != 5 || rep.ReadBytes != 4050 || rep.RereadBytes != 2000 {
		t.Errorf("totals = %d reads %d bytes %d re-read, want 5, 4050, 2000", rep.Reads, rep.ReadBytes, rep.RereadBytes)
	}
	one, _ := store.ToolUseReport(ctx, models.ToolUseQuery{TaskID: "t2"})
	if len(one.RepeatedReads) != 0 || one.Reads != 1 {
		t.Errorf("task filter: %+v", one)
	}
}
