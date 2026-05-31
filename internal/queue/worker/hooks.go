package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"agentd/internal/models"
	wsession "agentd/internal/queue/worker/session"
	"agentd/internal/sandbox"
)

// FailurePolicy determines how a hook error is treated by the chain runner.
type FailurePolicy int

const (
	// FailOpen logs the error and continues the chain.
	FailOpen FailurePolicy = iota
	// FailClosed treats the error as a veto and stops the chain.
	FailClosed
)

// HookVerdict is the outcome of a PreHook evaluation.
type HookVerdict struct {
	// Veto is true when the hook wants to block tool execution.
	Veto bool
	// Reason provides a human-readable explanation for the veto.
	Reason string
	// ShortCircuit is true when the hook supplies a cached or
	// pre-computed result that should be returned directly, skipping
	// both tool execution and post-hooks.
	ShortCircuit bool
	// Result carries the pre-computed value when ShortCircuit is set.
	Result string
	// Suspend is true when the hook blocked the parent task for human
	// review; the agentic loop must stop without further LLM calls.
	Suspend bool
	// Env carries KEY=VALUE pairs to merge into the tool execution environment
	// for this call only. RunPre accumulates Env from all pre-hooks.
	Env []string
}

// HookContext carries contextual information for hook evaluation without
// coupling hooks to internal types.
type HookContext struct {
	ToolName      string
	Args          string
	CallID        string
	SessionID     string
	ProjectID     string
	Provider      string
	Timestamp     time.Time
	TaskUpdatedAt time.Time // persisted task version for optimistic locking
	ExecCtx       context.Context
	// ResultStatus, ResultExitCode, and their Set flags are populated by the
	// dispatch layer before RunPost so post-hooks (e.g. AuditHook) can derive
	// exit codes from the classified ToolResult instead of re-parsing content.
	ResultStatus      ToolStatus
	ResultStatusSet   bool
	ResultExitCode    int
	ResultExitCodeSet bool
	// TurnID identifies the agentic loop iteration (taskID:turnIndex).
	TurnID string
	// Verdicts, when non-nil, collects hook outcome strings during RunPre/RunPost.
	Verdicts *[]string
	// TokenCountBefore is the session token budget usage at dispatch start.
	TokenCountBefore int
	// TokenCountAfter is set by the dispatch layer after hooks complete.
	TokenCountAfter int
}

// PreHook is evaluated before tool execution. Returning a veto verdict
// short-circuits the chain and blocks the tool call.
type PreHook struct {
	Name   string
	Policy FailurePolicy
	Fn     func(ctx HookContext) (HookVerdict, error)
}

// PostHook is evaluated after tool execution. It may mutate the result
// string returned to the caller.
type PostHook struct {
	Name   string
	Policy FailurePolicy
	Fn     func(ctx HookContext, result string) (string, error)
}

// SessionStartHook runs once at session initialization for credential
// validation, environment checks, etc.
type SessionStartHook struct {
	Name   string
	Policy FailurePolicy
	Fn     func(ctx HookContext) error
}

// HookChain is the central registry for pre-tool, post-tool, and
// session-start hooks. All methods are safe for concurrent use.
type HookChain struct {
	mu           sync.RWMutex
	preHooks     []PreHook
	postHooks    []PostHook
	sessionHooks []SessionStartHook
}

// NewHookChain returns a HookChain with empty hook lists.
func NewHookChain() *HookChain {
	return &HookChain{}
}

// RegisterPre appends a pre-tool hook to the chain.
func (hc *HookChain) RegisterPre(h PreHook) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.preHooks = append(hc.preHooks, h)
}

// RegisterPost appends a post-tool hook to the chain.
func (hc *HookChain) RegisterPost(h PostHook) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.postHooks = append(hc.postHooks, h)
}

// RegisterSessionStart appends a session-start hook to the chain.
func (hc *HookChain) RegisterSessionStart(h SessionStartHook) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.sessionHooks = append(hc.sessionHooks, h)
}

// RunPre executes every registered PreHook in order, accumulating Env from
// each hook into acc. It returns early on Veto, ShortCircuit, or Suspend;
// on early return, acc.Env is merged into verdict.Env before returning.
// If a hook returns an error, the failure policy determines the outcome:
// FailClosed treats the error as a veto, FailOpen logs it and continues.
func (hc *HookChain) RunPre(ctx HookContext) HookVerdict {
	hc.mu.RLock()
	hooks := append([]PreHook(nil), hc.preHooks...)
	hc.mu.RUnlock()

	var acc HookVerdict
	for _, h := range hooks {
		if h.Fn == nil {
			slog.Warn("pre-hook error", "hook", h.Name, "policy", policyLabel(h.Policy), "error", "nil hook callback")
			if h.Policy == FailClosed {
				appendPreVerdict(ctx, h.Name, "error")
				return HookVerdict{Veto: true, Reason: "hook " + h.Name + " failed (fail_closed): nil hook callback"}
			}
			appendPreVerdict(ctx, h.Name, "error")
			continue
		}
		verdict, err := h.Fn(ctx)
		if err != nil {
			slog.Warn("pre-hook error",
				"hook", h.Name,
				"policy", policyLabel(h.Policy),
				"error", err,
			)
			if h.Policy == FailClosed {
				appendPreVerdict(ctx, h.Name, "error")
				return HookVerdict{Veto: true, Reason: "hook " + h.Name + " failed (fail_closed): " + err.Error()}
			}
			appendPreVerdict(ctx, h.Name, "error")
			continue
		}
		if len(verdict.Env) > 0 {
			acc.Env = append(acc.Env, verdict.Env...)
		}
		if verdict.Veto || verdict.ShortCircuit || verdict.Suspend {
			appendPreVerdict(ctx, h.Name, preVerdictOutcome(verdict))
			verdict.Env = append([]string(nil), acc.Env...)
			return verdict
		}
		appendPreVerdict(ctx, h.Name, "pass")
	}
	return acc
}

// RunPost executes every registered PostHook in order, threading the
// result string through each hook. If a hook returns an error, the
// failure policy determines the outcome: FailClosed returns the error
// reason as the result, FailOpen logs and continues with the previous
// result.
func (hc *HookChain) RunPost(ctx HookContext, result string) string {
	hc.mu.RLock()
	hooks := append([]PostHook(nil), hc.postHooks...)
	hc.mu.RUnlock()

	for _, h := range hooks {
		if h.Fn == nil {
			slog.Warn("post-hook error", "hook", h.Name, "policy", policyLabel(h.Policy), "error", "nil hook callback")
			if h.Policy == FailClosed {
				appendPostVerdict(ctx, h.Name, "error")
				return "hook " + h.Name + " failed (fail_closed): nil hook callback"
			}
			appendPostVerdict(ctx, h.Name, "error")
			continue
		}
		mutated, err := h.Fn(ctx, result)
		if err != nil {
			slog.Warn("post-hook error",
				"hook", h.Name,
				"policy", policyLabel(h.Policy),
				"error", err,
			)
			if h.Policy == FailClosed {
				appendPostVerdict(ctx, h.Name, "error")
				return "hook " + h.Name + " failed (fail_closed): " + err.Error()
			}
			appendPostVerdict(ctx, h.Name, "error")
			continue
		}
		appendPostVerdict(ctx, h.Name, "pass")
		result = mutated
	}
	return result
}

// RunSessionStart executes every registered SessionStartHook in order.
// If a hook errors with FailClosed policy the remaining hooks are
// skipped and the error is returned. FailOpen errors are logged.
func (hc *HookChain) RunSessionStart(ctx HookContext) error {
	hc.mu.RLock()
	hooks := append([]SessionStartHook(nil), hc.sessionHooks...)
	hc.mu.RUnlock()

	for _, h := range hooks {
		if h.Fn == nil {
			slog.Warn("session-start hook error", "hook", h.Name, "policy", policyLabel(h.Policy), "error", "nil hook callback")
			if h.Policy == FailClosed {
				return errors.New("hook " + h.Name + " failed (fail_closed): nil hook callback")
			}
			continue
		}
		if err := h.Fn(ctx); err != nil {
			slog.Warn("session-start hook error",
				"hook", h.Name,
				"policy", policyLabel(h.Policy),
				"error", err,
			)
			if h.Policy == FailClosed {
				return err
			}
		}
	}
	return nil
}

// Clone returns a deep copy of the HookChain so that mutations on the
// clone do not affect the original.
func (hc *HookChain) Clone() *HookChain {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return &HookChain{
		preHooks:     append([]PreHook(nil), hc.preHooks...),
		postHooks:    append([]PostHook(nil), hc.postHooks...),
		sessionHooks: append([]SessionStartHook(nil), hc.sessionHooks...),
	}
}

// PrependPost inserts a post-tool hook at the front of the chain so it
// runs before any previously registered PostHooks.
func (hc *HookChain) PrependPost(h PostHook) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.postHooks = append([]PostHook{h}, hc.postHooks...)
}

// resolveHooks returns hc if non-nil, otherwise a new empty HookChain.
func resolveHooks(hc *HookChain) *HookChain {
	if hc != nil {
		return hc
	}
	return NewHookChain()
}

func policyLabel(p FailurePolicy) string {
	if p == FailClosed {
		return "fail_closed"
	}
	return "fail_open"
}

func appendPreVerdict(ctx HookContext, hookName, outcome string) {
	if ctx.Verdicts == nil {
		return
	}
	*ctx.Verdicts = append(*ctx.Verdicts, hookName+":"+outcome)
}

func appendPostVerdict(ctx HookContext, hookName, outcome string) {
	if ctx.Verdicts == nil {
		return
	}
	*ctx.Verdicts = append(*ctx.Verdicts, hookName+":"+outcome)
}

func preVerdictOutcome(v HookVerdict) string {
	switch {
	case v.ShortCircuit:
		return "short_circuit"
	case v.Suspend:
		return "suspend"
	case v.Veto:
		return "veto"
	default:
		return "pass"
	}
}

// DryRunHook returns a PreHook that intercepts all tool calls and
// returns synthesized results without executing the real handler. The
// verdict uses Veto with a populated Result so the dispatch layer skips
// execution but still runs PostToolUse hooks (audit, scrubbing).
func DryRunHook(enabled bool) PreHook {
	return PreHook{
		Name:   "dry-run",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if !enabled {
				return HookVerdict{}, nil
			}
			return HookVerdict{
				Veto:   true,
				Result: simulatedResult(ctx.ToolName),
			}, nil
		},
	}
}

// simulatedResult returns a plausible synthesized response for each
// built-in tool. Unknown tools receive a generic JSON acknowledgement.
func simulatedResult(tool string) string {
	switch tool {
	case toolNameBash:
		return "(simulated) command executed successfully"
	case toolNameRead:
		return "(simulated) file contents"
	case toolNameWrite:
		return marshalWriteResult()
	default:
		return "(simulated) tool executed successfully"
	}
}

func marshalWriteResult() string {
	out, _ := json.Marshal(map[string]any{
		"success":   true,
		"simulated": true,
	})
	return string(out)
}

func buildWorkerHooks(
	opts WorkerOptions,
	toolExecutor *ToolExecutor,
	sink models.EventSink,
	scrubber sandbox.Scrubber,
) *HookChain {
	base := resolveHooks(opts.Hooks)
	hooks := base.Clone()
	hooks.RegisterPre(SchemaValidationHook(SchemaRegistryFromDefinitions(toolExecutor.Definitions())))
	if !opts.DisableCredentialDetection {
		hooks.RegisterPre(CredentialDetectionHook())
	}
	if len(opts.ToolCredentials) > 0 {
		store := wsession.NewEnvSecretStore(opts.ToolCredentials)
		hooks.RegisterPre(CredentialInjectionHook(store))
		hooks.RegisterSessionStart(CredentialValidationSessionHook(store))
	}
	hooks.PrependPost(ScrubResultHook(scrubber))
	hooks.RegisterPost(InjectionResistanceHook(externalToolsSet(opts.ExternalTools)))
	hooks.RegisterPost(AuditHook(sink, scrubber))
	return hooks
}
