package worker

import (
	"fmt"
	"sync"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// attemptFailure is the first failing shell command of an agentic attempt and
// the failure it produced.
type attemptFailure struct {
	command string
	failure string
}

// attemptRecord is what the agentic path keeps for one task across its attempts
// (B-043). last is the first failing command of the attempt that finished
// before this one, nil when it had none. current is the first failing command of
// the attempt running now.
type attemptRecord struct {
	last    *attemptFailure
	current *attemptFailure
}

// attemptRecords holds the record of every task with an agentic attempt in flight
// or finished without success. The zero value is ready to use.
type attemptRecords struct {
	mu     sync.Mutex
	byTask map[string]*attemptRecord
}

// begin starts the record of a new attempt. The failure the last attempt left
// stays as the one this attempt must not repeat blind.
func (r *attemptRecords) begin(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec := r.byTask[taskID]; rec != nil {
		rec.current = nil
	}
}

// finish closes the attempt: a success forgets the task, and any other end keeps
// the attempt's first failure for the next one.
func (r *attemptRecords) finish(taskID string, succeeded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if succeeded {
		delete(r.byTask, taskID)
		return
	}
	if rec := r.byTask[taskID]; rec != nil {
		rec.last = rec.current
		rec.current = nil
	}
}

// recordFirst keeps failure as the current attempt's first failing command, if
// the attempt has none yet.
func (r *attemptRecords) recordFirst(taskID string, failure attemptFailure) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTask == nil {
		r.byTask = map[string]*attemptRecord{}
	}
	rec := r.byTask[taskID]
	if rec == nil {
		rec = &attemptRecord{}
		r.byTask[taskID] = rec
	}
	if rec.current == nil {
		rec.current = &failure
	}
}

// lastFailure returns the first failing command of the attempt before this one.
func (r *attemptRecords) lastFailure(taskID string) *attemptFailure {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec := r.byTask[taskID]; rec != nil {
		return rec.last
	}
	return nil
}

// RecordAttemptFailure remembers the first failing shell command of the attempt
// that is running.
func (w *Worker) RecordAttemptFailure(task models.Task, call gateway.ToolCall, tr agenttools.ToolResult) {
	command := bashCommandOf(call)
	if command == "" {
		return
	}
	w.attempts.recordFirst(task.ID, attemptFailure{command: command, failure: tr.Content})
}

// RepeatedAttemptFailure returns the refusal feedback for a bash call that sends
// the command that failed first in the previous attempt. The command is not run
// again blind: the failure goes back to the model instead (B-043).
func (w *Worker) RepeatedAttemptFailure(task models.Task, call gateway.ToolCall) (string, bool) {
	if call.Function.Name != agenttools.ToolNameBash {
		return "", false
	}
	previous := w.attempts.lastFailure(task.ID)
	if previous == nil || bashCommandOf(call) != previous.command {
		return "", false
	}
	return fmt.Sprintf("This command failed in the previous attempt and was not run again. Change the command or the approach before you retry it. Previous failure: %s", truncate(previous.failure, 500)), true
}
