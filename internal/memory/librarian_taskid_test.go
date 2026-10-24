package memory

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
)

// taskIDRecordingGateway records the TaskID of every model call. Map calls
// answer long enough to force a reduce pass; JSON-mode calls get a valid
// memory summary, the rest a short text.
type taskIDRecordingGateway struct {
	fakeGateway
	taskIDs     []string
	reduceCalls int
}

func (g *taskIDRecordingGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.taskIDs = append(g.taskIDs, req.TaskID)
	if len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Content, "Condense the following summaries") {
		g.reduceCalls++
		return gateway.AIResponse{Content: "short"}, nil
	}
	if req.JSONMode {
		return gateway.AIResponse{Content: `{"symptom":"s","solution":"x"}`}, nil
	}
	return gateway.AIResponse{Content: strings.Repeat("chunk summary ", 20)}, nil
}

// T-057: the librarian's map, reduce and extract calls belong to the task
// they curate.
func TestLibrarianCurateCallsCarryTheTaskID(t *testing.T) {
	t.Parallel()

	gw := &taskIDRecordingGateway{}
	lib := &Librarian{
		Store:   &fakeStore{events: testEvents(3)},
		Gateway: gw,
		Breaker: &fakeBreaker{open: false},
		Sink:    &fakeSink{},
		Cfg:     config.LibrarianConfig{ChunkChars: 100, MaxReducePasses: 3, FallbackHeadTailChars: 100, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask: %v", err)
	}
	if gw.reduceCalls == 0 {
		t.Fatal("want at least one reduce call so its TaskID is checked")
	}
	if len(gw.taskIDs) < 3 {
		t.Fatalf("want map, reduce and extract calls, got %d", len(gw.taskIDs))
	}
	for i, id := range gw.taskIDs {
		if id != testTask().ID {
			t.Errorf("librarian call %d: TaskID = %q, want %q", i, id, testTask().ID)
		}
	}
}
