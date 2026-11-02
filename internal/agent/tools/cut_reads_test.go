package tools

import (
	"strings"
	"testing"

	"agentd/internal/gateway"
)

// The truncator can put a note in front of a read it keeps whole ("2 tool
// exchanges collapsed"). The model still saw all of the file: no cut.
func TestCutReadsANoteBeforeAWholeReadIsNoCut(t *testing.T) {
	read := gateway.ToolCall{ID: "1", Function: gateway.ToolCallFunction{Name: toolNameRead}}
	write := gateway.ToolCall{ID: "2", Function: gateway.ToolCallFunction{Name: toolNameWrite}}
	var c CutReads
	c.NoteRead(read, "/ws/a.go", SuccessResult("1", "package a", 0))
	c.Note([]gateway.PromptMessage{{Role: "tool", ToolCallID: "1", Content: "pack"}})
	if _, refused := c.WriteRefusal(write, "/ws/a.go"); !refused {
		t.Fatal("a cut read does not refuse the write; the test would prove nothing")
	}
	c.Note([]gateway.PromptMessage{{Role: "tool", ToolCallID: "1", Content: "[2 tool exchanges collapsed] package a"}})
	if _, refused := c.WriteRefusal(write, "/ws/a.go"); refused {
		t.Fatal("a whole read with a note in front of it refuses the write")
	}
}

// Only a file's latest read decides it, so a reread lets the older read's
// content go: rereading a large file does not grow what the tracker holds.
func TestCutReadsKeepsOnlyTheLatestReadOfAFile(t *testing.T) {
	var c CutReads
	for _, id := range []string{"1", "2", "3"} {
		read := gateway.ToolCall{ID: id, Function: gateway.ToolCallFunction{Name: toolNameRead}}
		c.NoteRead(read, "/ws/a.go", SuccessResult(id, strings.Repeat("x", 1<<20), 0))
	}
	if len(c.reads) != 1 {
		t.Fatalf("holds %d reads of /ws/a.go, want only the latest", len(c.reads))
	}
	// An older read is passed over; the latest read, read "3", decides the file.
	write := gateway.ToolCall{ID: "w", Function: gateway.ToolCallFunction{Name: toolNameWrite}}
	c.Note([]gateway.PromptMessage{{Role: "tool", ToolCallID: "1", Content: "cut"}})
	if _, refused := c.WriteRefusal(write, "/ws/a.go"); refused {
		t.Fatal("an older cut read refuses the write")
	}
	c.Note([]gateway.PromptMessage{{Role: "tool", ToolCallID: "3", Content: "cut"}})
	if _, refused := c.WriteRefusal(write, "/ws/a.go"); !refused {
		t.Fatal("the latest read, cut, does not refuse the write: it is not kept under its call ID")
	}
	c.Note([]gateway.PromptMessage{{Role: "tool", ToolCallID: "3", Content: strings.Repeat("x", 1<<20)}})
	if _, refused := c.WriteRefusal(write, "/ws/a.go"); refused {
		t.Fatal("the latest read, whole, refuses the write")
	}
}
