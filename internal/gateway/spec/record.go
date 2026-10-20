package spec

import "time"

// CallKind separates model calls from embedding calls in a record.
type CallKind string

const (
	KindGenerate CallKind = "generate"
	KindEmbed    CallKind = "embed"
)

// AttemptKind says whether an attempt is the first try of a logical call or a
// retry made to repair an invalid JSON answer. Cascade (trying the next
// provider after a failure) is a separate flag on the record.
type AttemptKind string

const (
	AttemptFirst      AttemptKind = "first"
	AttemptJSONRepair AttemptKind = "json_repair"
)

// Outcome is how one provider attempt ended.
type Outcome string

const (
	OutcomeOK          Outcome = "ok"
	OutcomeFailed      Outcome = "failed"
	OutcomeInvalidJSON Outcome = "invalid_json"
)

// CallRecord describes one provider attempt: sizes, counts and timing only,
// never prompt or response bodies.
type CallRecord struct {
	At            time.Time     `json:"at"`
	Kind          CallKind      `json:"kind"`
	Purpose       Purpose       `json:"purpose"`
	Stage         string        `json:"stage"`
	Role          Role          `json:"role,omitempty"`
	TaskID        string        `json:"task_id,omitempty"`
	AgentID       string        `json:"agent_id,omitempty"`
	CorrelationID string        `json:"correlation_id,omitempty"`
	Provider      string        `json:"provider"`
	Model         string        `json:"model,omitempty"`
	Attempt       AttemptKind   `json:"attempt"`
	Cascade       bool          `json:"cascade"`
	Outcome       Outcome       `json:"outcome"`
	Truncated     bool          `json:"truncated"`
	MessageCount  int           `json:"message_count"`
	ToolCount     int           `json:"tool_count"`
	InputChars    int           `json:"input_chars"`
	OutputChars   int           `json:"output_chars"`
	Tokens        int           `json:"tokens"`
	CachedTokens  int           `json:"cached_tokens"`
	CacheWrite    int           `json:"cache_write_tokens"`
	Latency       time.Duration `json:"latency"`
}

// Recorder receives one record per provider attempt. Implementations must not
// block: the router calls Record on the request path.
type Recorder interface {
	Record(CallRecord)
}
