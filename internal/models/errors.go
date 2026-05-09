package models

import "errors"

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
	ErrAgentProfileProtected  = errors.New("agent profile is protected")
	ErrAgentProfileInUse      = errors.New("agent profile in use by tasks")
)
