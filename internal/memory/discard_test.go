package memory

import (
	"context"
	"encoding/json"
	"testing"

	"agentd/internal/config"
)

func TestCurateTask_DiscardsEmptyExtraction(t *testing.T) {
	store := &fakeStore{events: testEvents(3)}
	sink := &fakeSink{}
	summary := memorySummary{Symptom: "", Solution: ""}
	summaryJSON, _ := json.Marshal(summary)

	gw := &extractOnFinalGateway{
		mapResponse: "nothing useful",
		extractJSON: string(summaryJSON),
	}

	lib := &Librarian{
		Store:   store,
		Gateway: gw,
		Breaker: &fakeBreaker{open: false},
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ChunkChars: 50000, MaxReducePasses: 3, FallbackHeadTailChars: 2000, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask: %v", err)
	}

	if store.recordedMemory != nil {
		t.Error("should NOT record memory when extraction is empty")
	}
	if !store.curated {
		t.Error("events should still be marked curated even when discarded")
	}

	var discarded bool
	for _, e := range sink.events {
		if e.Type == "MEMORY_DISCARDED" {
			discarded = true
		}
	}
	if !discarded {
		t.Error("expected MEMORY_DISCARDED event")
	}
}

func TestCurateTask_DiscardsJunkExtraction(t *testing.T) {
	store := &fakeStore{events: testEvents(3)}
	sink := &fakeSink{}
	summary := memorySummary{Symptom: "N/A", Solution: "none"}
	summaryJSON, _ := json.Marshal(summary)

	gw := &extractOnFinalGateway{
		mapResponse: "nothing",
		extractJSON: string(summaryJSON),
	}

	lib := &Librarian{
		Store:   store,
		Gateway: gw,
		Breaker: &fakeBreaker{open: false},
		Sink:    sink,
		Cfg:     config.LibrarianConfig{ChunkChars: 50000, MaxReducePasses: 3, FallbackHeadTailChars: 2000, ArchiveGraceDays: 7},
		HomeDir: t.TempDir(),
	}

	if err := lib.CurateTask(context.Background(), testTask()); err != nil {
		t.Fatalf("CurateTask: %v", err)
	}

	if store.recordedMemory != nil {
		t.Error("should NOT record memory for junk extraction")
	}
}

func TestIsMeaningful(t *testing.T) {
	tests := []struct {
		symptom, solution string
		want              bool
	}{
		{"", "", false},
		{"  ", "  ", false},
		{"N/A", "none", false},
		{"null", "unknown", false},
		{"real symptom", "", true},
		{"", "real solution", true},
		{"ran tests", "all passed", true},
	}
	for _, tt := range tests {
		ms := memorySummary{Symptom: tt.symptom, Solution: tt.solution}
		got := ms.IsMeaningful()
		if got != tt.want {
			t.Errorf("IsMeaningful(%q, %q) = %v, want %v", tt.symptom, tt.solution, got, tt.want)
		}
	}
}
