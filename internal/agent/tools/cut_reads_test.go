package tools

import (
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
