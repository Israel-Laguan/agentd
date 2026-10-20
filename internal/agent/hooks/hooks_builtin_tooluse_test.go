package hooks

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

type toolCallSink struct {
	got []models.ToolCall
	err error
}

func (s *toolCallSink) InsertToolCall(_ context.Context, c models.ToolCall) error {
	s.got = append(s.got, c)
	return s.err
}

func runToolUse(t *testing.T, sink ToolCallRecorder, hctx HookContext, result string) string {
	t.Helper()
	out, err := ToolUseHook(sink).Fn(hctx, result)
	if err != nil {
		t.Fatalf("hook error: %v", err)
	}
	return out
}

func TestToolUseHookRecordsPathStatusAndResultBytes(t *testing.T) {
	sink := &toolCallSink{}
	runToolUse(t, sink, HookContext{ToolName: "read", Args: `{"path":"cmd/main.go"}`, CallID: "c1", SessionID: "t1"}, "hello")
	failed := HookContext{ToolName: "bash", Args: `{"command":"false"}`, CallID: "c2", SessionID: "t1", ResultStatusSet: true, ResultStatus: ToolStatusError}
	out := runToolUse(t, sink, failed, "boom")
	if out != "boom" {
		t.Fatalf("hook changed the result to %q", out)
	}
	want := []models.ToolCall{
		{TaskID: "t1", CallID: "c1", Tool: "read", Path: "cmd/main.go", Status: "ok", ResultBytes: 5},
		{TaskID: "t1", CallID: "c2", Tool: "bash", Status: "failed", ResultBytes: 4},
	}
	if len(sink.got) != 2 {
		t.Fatalf("recorded %d calls, want 2", len(sink.got))
	}
	for i, w := range want {
		g := sink.got[i]
		g.At = w.At
		if g != w {
			t.Errorf("call %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestToolUseHookFailsOpenAndSkipsWithoutATask(t *testing.T) {
	failing := &toolCallSink{err: errors.New("disk full")}
	if out := runToolUse(t, failing, HookContext{ToolName: "read", SessionID: "t1"}, "x"); out != "x" {
		t.Fatalf("result = %q after a failed insert, want it unchanged", out)
	}
	none := &toolCallSink{}
	runToolUse(t, none, HookContext{ToolName: "read"}, "x")
	runToolUse(t, nil, HookContext{ToolName: "read", SessionID: "t1"}, "x")
	if len(none.got) != 0 {
		t.Fatalf("recorded %d calls with no task ID, want 0", len(none.got))
	}
}
