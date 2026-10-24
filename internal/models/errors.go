package models

import (
	"errors"
	"fmt"
	"strings"
)

// ProviderNotConfiguredError is returned when an agent profile is created or
// patched with a provider name that is not present in the live gateway registry.
type ProviderNotConfiguredError struct {
	Provider  string
	Available []string
}

func (e *ProviderNotConfiguredError) Error() string {
	return fmt.Sprintf("provider '%s' is not configured; available: [%s]",
		e.Provider, strings.Join(e.Available, ", "))
}

// SystemProjectID and SystemProjectName identify the _system project, which
// holds diagnostics and operator items and is never an executable workspace.
const (
	SystemProjectID   = "00000000-0000-0000-0000-000000000001"
	SystemProjectName = "_system"
)

var (
	ErrCircularDependency        = errors.New("circular task dependency")
	ErrInvalidDraftPlan          = errors.New("invalid draft plan")
	ErrInvalidJSONResponse       = errors.New("invalid JSON response")
	ErrInvalidStateTransition    = errors.New("invalid task state transition")
	ErrExecutionTimeout          = errors.New("execution timed out")
	ErrLLMQuotaExceeded          = errors.New("LLM provider quota exceeded")
	ErrLLMUnreachable            = errors.New("LLM provider unreachable")
	ErrOptimisticLock            = errors.New("optimistic lock conflict")
	ErrProjectNotFound           = errors.New("project not found")
	ErrProviderNotFound          = errors.New("provider not found")
	ErrUnknownTaskState          = errors.New("unknown task state")
	ErrSandboxViolation          = errors.New("sandbox path violation")
	ErrSandboxUnavailable        = errors.New("sandbox backend unavailable")
	ErrBreakdownIncomplete       = errors.New("breakdown subtasks are not all completed")
	ErrStateConflict             = errors.New("task state conflict")
	ErrTaskBlocked               = errors.New("task is blocked")
	ErrTaskNotFound              = errors.New("task not found")
	ErrHumanHandoffInvalid       = errors.New("invalid human handoff")
	ErrBudgetExceeded            = errors.New("task token budget exceeded")
	ErrAgentProfileNotFound      = errors.New("agent profile not found")
	ErrAgentProfileInvalid       = errors.New("invalid agent profile")
	ErrAgentProfileProtected     = errors.New("agent profile is protected")
	ErrAgentProfileInUse         = errors.New("agent profile in use by tasks")
	ErrAgentProfileNoToolSupport = errors.New("agentic profile's provider cannot call tools")
	ErrMessageTooLarge           = errors.New("message exceeds max size")
	ErrMessageInvalid            = errors.New("invalid inbound message")
	ErrChannelRateLimited        = errors.New("channel rate limit exceeded")
	ErrDispatchNack              = errors.New("dispatch nacked")
	ErrWorkspaceNotReady         = errors.New("workspace not ready")
)
