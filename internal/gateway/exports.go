// Package gateway re-exports symbols from gateway/spec and subpackages for stable imports.
package gateway

import (
	"context"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/routing"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
	"agentd/internal/models"
)

// TruncationMarker is re-exported for tests and callers that check log output.
const TruncationMarker = truncation.TruncationMarker

// Wire types (spec).
type (
	PromptMessage       = spec.PromptMessage
	Role                = spec.Role
	RoleTarget          = spec.RoleTarget
	AIRequest           = spec.AIRequest
	AIResponse          = spec.AIResponse
	ProviderConfig      = spec.ProviderConfig
	Provider            = spec.Provider
	AIGateway           = spec.AIGateway
	ContractAdapter     = spec.ContractAdapter
	Truncator           = spec.Truncator
	ScopeAnalysis       = spec.ScopeAnalysis
	ScopeOption         = spec.ScopeOption
	IntentAnalysis      = spec.IntentAnalysis
	BudgetTracker       = spec.BudgetTracker
	ToolDefinition      = spec.ToolDefinition
	FunctionParameters  = spec.FunctionParameters
)

// Role constants.
const (
	RoleChat   = spec.RoleChat
	RoleWorker = spec.RoleWorker
	RoleMemory = spec.RoleMemory
)

// Provider constants.
const (
	ProviderOpenAI    = spec.ProviderOpenAI
	ProviderAnthropic = spec.ProviderAnthropic
	ProviderOllama    = spec.ProviderOllama
	ProviderLlamaCpp  = spec.ProviderLlamaCpp
	ProviderHorde     = spec.ProviderHorde
)

// Errors and markers.
var ErrContextBudgetExceeded = spec.ErrContextBudgetExceeded

// Truncation policy names (truncator).
const (
	TruncatorPolicyMiddleOut = truncation.TruncatorPolicyMiddleOut
	TruncatorPolicyHeadTail  = truncation.TruncatorPolicyHeadTail
	TruncatorPolicySummarize = truncation.TruncatorPolicySummarize
	TruncatorPolicyReject    = truncation.TruncatorPolicyReject
)

// Truncation strategy names (config-driven strategies).
const (
	TruncationStrategyMiddleOut = truncation.TruncationStrategyMiddleOut
	TruncationStrategyHeadTail  = truncation.TruncationStrategyHeadTail
)

// Strategy types.
type (
	MiddleOutStrategy = truncation.MiddleOutStrategy
	HeadTailStrategy  = truncation.HeadTailStrategy
	TruncationStrategy = truncation.TruncationStrategy
	StrategyTruncator  = truncation.StrategyTruncator
	SummarizeTruncator = truncation.SummarizeTruncator
	RejectTruncator    = truncation.RejectTruncator
)

// Router and constructors.
type Router = routing.Router

var (
	NewRouter            = routing.NewRouter
	NewRouterFromConfigs = routing.NewRouterFromConfigs
	NewTruncator         = truncation.NewTruncator
)

// House rules (routing).
var (
	WithHouseRules        = routing.WithHouseRules
	HouseRulesFromContext = routing.HouseRulesFromContext
)

// Providers.
var (
	NewOpenAI    = providers.NewOpenAI
	NewAnthropic = providers.NewAnthropic
	NewOllama    = providers.NewOllama
	NewLlamaCpp  = providers.NewLlamaCpp
	NewHorde     = providers.NewHorde
)

// GenerateJSON re-exports generic JSON repair.
func GenerateJSON[T any](ctx context.Context, gw AIGateway, req AIRequest) (T, error) {
	return correction.GenerateJSON[T](ctx, gw, req)
}

// Validatable types implementing semantic validation after JSON parse.
type Validatable = spec.Validatable

// BreakerChecker for summarize truncator and queue integration.
type BreakerChecker = spec.BreakerChecker

// MiddleOut is a convenience wrapper for middle-out string truncation.
func MiddleOut(input string, maxChars int) string {
	return truncation.MiddleOut(input, maxChars)
}

// EnforcePhaseCap trims oversized draft plans (used by tests and extensions).
func EnforcePhaseCap(plan models.DraftPlan, maxTasksPerPhase int) models.DraftPlan {
	return correction.EnforcePhaseCap(plan, maxTasksPerPhase)
}
