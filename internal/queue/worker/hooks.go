package worker

import (
	"log/slog"
	"sync"
	"time"
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
}

// HookContext carries contextual information for hook evaluation without
// coupling hooks to internal types.
type HookContext struct {
	ToolName  string
	Args      string
	SessionID string
	Timestamp time.Time
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
	mu            sync.RWMutex
	preHooks      []PreHook
	postHooks     []PostHook
	sessionHooks  []SessionStartHook
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

// RunPre executes every registered PreHook in order. It short-circuits
// on the first veto verdict. If a hook returns an error, the failure
// policy determines the outcome: FailClosed treats the error as a veto,
// FailOpen logs it and continues.
func (hc *HookChain) RunPre(ctx HookContext) HookVerdict {
	hc.mu.RLock()
	hooks := append([]PreHook(nil), hc.preHooks...)
	hc.mu.RUnlock()

	for _, h := range hooks {
		verdict, err := h.Fn(ctx)
		if err != nil {
			slog.Warn("pre-hook error",
				"hook", h.Name,
				"policy", policyLabel(h.Policy),
				"error", err,
			)
			if h.Policy == FailClosed {
				return HookVerdict{Veto: true, Reason: "hook " + h.Name + " failed (fail_closed): " + err.Error()}
			}
			continue
		}
		if verdict.Veto {
			return verdict
		}
	}
	return HookVerdict{}
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
		mutated, err := h.Fn(ctx, result)
		if err != nil {
			slog.Warn("post-hook error",
				"hook", h.Name,
				"policy", policyLabel(h.Policy),
				"error", err,
			)
			if h.Policy == FailClosed {
				return "hook " + h.Name + " failed (fail_closed): " + err.Error()
			}
			continue
		}
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
