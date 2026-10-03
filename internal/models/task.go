package models

import (
	"errors"
	"strings"
	"time"
)

// ExecutionPayload is the cross-box task payload exchanged between queue and
// sandbox boundaries. It keeps proposal-aligned fields while retaining the
// command/runtime fields currently used by agentd workers.
type ExecutionPayload struct {
	TaskID        string
	WorkspacePath string
	Intent        string
	Context       string

	// Legacy/compatibility fields currently used by the queue and sandbox path.
	ProjectID    string
	Command      string
	EnvVars      []string
	TimeoutLimit int
	WallTimeout  time.Duration
	Why          string
	What         string
}

// ExecutionResult is the cross-box return shape from sandbox to queue/store.
// FatalError keeps transport compatibility by storing an error message rather
// than a raw error value.
type ExecutionResult struct {
	Success    bool
	Output     string
	FatalError string

	// Legacy/compatibility fields currently used by queue recovery and prompts.
	ExitCode    int
	Stdout      string
	Stderr      string
	Duration    time.Duration
	TimedOut    bool
	OSProcessID int
}

// Err returns a materialized error from FatalError if present.
func (r ExecutionResult) Err() error {
	if strings.TrimSpace(r.FatalError) == "" {
		return nil
	}
	return errors.New(r.FatalError)
}
