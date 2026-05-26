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

var (
	ErrCircularDependency     = errors.New("circular task dependency")
	ErrInvalidDraftPlan       = errors.New("invalid draft plan")
	ErrInvalidJSONResponse    = errors.New("invalid JSON response")
	ErrInvalidStateTransition = errors.New("invalid task state transition")
	ErrExecutionTimeout       = errors.New("execution timed out")
	ErrLLMQuotaExceeded       = errors.New("LLM provider quota exceeded")
	ErrLLMUnreachable         = errors.New("LLM provider unreachable")
	ErrOptimisticLock         = errors.New("optimistic lock conflict")
	ErrProjectNotFound        = errors.New("project not found")
	ErrSandboxViolation       = errors.New("sandbox path violation")
	ErrStateConflict          = errors.New("task state conflict")
	ErrTaskBlocked            = errors.New("task is blocked")
	ErrTaskNotFound           = errors.New("task not found")
	ErrBudgetExceeded         = errors.New("task token budget exceeded")
	ErrAgentProfileNotFound   = errors.New("agent profile not found")
	ErrAgentProfileInvalid    = errors.New("invalid agent profile")
	ErrAgentProfileProtected  = errors.New("agent profile is protected")
	ErrAgentProfileInUse      = errors.New("agent profile in use by tasks")
	ErrMessageTooLarge        = errors.New("message exceeds max size")
	ErrMessageInvalid         = errors.New("invalid inbound message")
	ErrChannelRateLimited     = errors.New("channel rate limit exceeded")
	ErrDispatchNack           = errors.New("dispatch nacked")
)
