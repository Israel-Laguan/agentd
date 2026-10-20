package models

import (
	"context"
	"time"
)

// ToolCall is one tool dispatch of a task: what ran, on which file, how it
// ended and how many bytes came back. It holds no arguments or output bodies.
type ToolCall struct {
	At          time.Time
	TaskID      string
	CallID      string
	Tool        string
	Path        string // set for file tools (read, write, edit); empty otherwise
	Status      string // "ok" or "failed"
	ResultBytes int
}

// ToolUseQuery filters the tool use report; zero values match everything.
type ToolUseQuery struct {
	From   time.Time
	To     time.Time
	TaskID string
}

// ToolUseRow aggregates one tool.
type ToolUseRow struct {
	Tool        string `json:"tool"`
	Calls       int    `json:"calls"`
	Failed      int    `json:"failed"`
	ResultBytes int    `json:"result_bytes"`
}

// RepeatedRead is a file read more than once within one task. RereadBytes is
// Bytes minus the first read: what the repeat reads added to the context.
type RepeatedRead struct {
	TaskID      string `json:"task_id"`
	Path        string `json:"path"`
	Reads       int    `json:"reads"`
	Bytes       int    `json:"bytes"`
	RereadBytes int    `json:"reread_bytes"`
}

// ToolUseReport is tool-call counts by tool and the repeated reads. The totals
// cover every repeated read; RepeatedReads lists the biggest few.
type ToolUseReport struct {
	Tools         []ToolUseRow   `json:"tools"`
	Reads         int            `json:"reads"`
	ReadBytes     int            `json:"read_bytes"`
	RereadBytes   int            `json:"reread_bytes"`
	RepeatedReads []RepeatedRead `json:"repeated_reads"`
}

// ToolCallStore persists and aggregates tool calls. Optional on a store, like
// AICallStore.
type ToolCallStore interface {
	InsertToolCall(ctx context.Context, c ToolCall) error
	ToolUseReport(ctx context.Context, q ToolUseQuery) (ToolUseReport, error)
}
