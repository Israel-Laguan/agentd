package worker

import (
	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	agentsubagent "agentd/internal/agent/subagent"
	"agentd/internal/gateway"
)

type CorrectionSource = agentcontext.CorrectionSource
type CorrectionRecord = agentcontext.CorrectionRecord
type TurnSummary = agentcontext.TurnSummary
type Turn = agentcontext.Turn
type ContextZone = agentcontext.ContextZone
type ContextManager = agentcontext.ContextManager
type AgentGoal = agentcontext.AgentGoal
type GoalTracker = agentcontext.GoalTracker
type GoalTrackerOption = agentcontext.GoalTrackerOption
type CriteriaUpdater = agentcontext.CriteriaUpdater

const (
	CorrectionSourceTool     = agentcontext.CorrectionSourceTool
	CorrectionSourceHuman    = agentcontext.CorrectionSourceHuman
	CorrectionSourceReviewer = agentcontext.CorrectionSourceReviewer
	DefaultStallThreshold    = agentcontext.DefaultStallThreshold
)

var (
	NewContextManager      = agentcontext.NewContextManager
	DetectContradictions   = agentcontext.DetectContradictions
	ParseCorrectionComment = agentcontext.ParseCorrectionComment
	ParseGoalProgress      = agentcontext.ParseGoalProgress
	NewGoalTracker         = agentcontext.NewGoalTracker
	WithStallThreshold     = agentcontext.WithStallThreshold
	WithCriteriaStore      = agentcontext.WithCriteriaStore
	GoalFromTask           = agentcontext.GoalFromTask
)

func totalChars(messages []gateway.PromptMessage) int {
	return agentcontext.TotalChars(messages)
}

func totalToolChars(tools []gateway.ToolDefinition) int {
	return agentruntime.TotalToolChars(tools)
}

func parseGoalProgress(content string) (completed, blocked []string) {
	return agentcontext.ParseGoalProgress(content)
}

type IterationGuard = agentruntime.IterationGuard
type BudgetGuard = agentruntime.BudgetGuard
type DeadlineGuard = agentruntime.DeadlineGuard
type ContextBudgetGuard = agentruntime.ContextBudgetGuard
type ComplexityScorer = agentruntime.ComplexityScorer
type ModelRouter = agentruntime.ModelRouter
type IntentClassification = agentruntime.IntentClassification
type IntentClassifier = agentruntime.IntentClassifier
type CapabilityRouteDecision = agentruntime.CapabilityRouteDecision
type CapabilityRouter = agentruntime.CapabilityRouter
type PromptTemplate = agentruntime.PromptTemplate
type RenderSession = agentruntime.RenderSession
type RenderedPrompt = agentruntime.RenderedPrompt
type PromptLibrary = agentruntime.PromptLibrary
type TopicGuard = agentruntime.TopicGuard

const TemplateCodePromptBuilder = agentruntime.TemplateCodePromptBuilder
const iterationExceededMessage = agentruntime.IterationExceededMessage

const (
	IntentGenerateImage  = agentruntime.IntentGenerateImage
	IntentRealTimeSearch = agentruntime.IntentRealTimeSearch
	IntentBrowseURL      = agentruntime.IntentBrowseURL
	IntentSpreadsheetOps = agentruntime.IntentSpreadsheetOps
)

var (
	NewIterationGuard     = agentruntime.NewIterationGuard
	NewBudgetGuard        = agentruntime.NewBudgetGuard
	NewDeadlineGuard      = agentruntime.NewDeadlineGuard
	NewContextBudgetGuard = agentruntime.NewContextBudgetGuard
	EstimateContextTokens = agentruntime.EstimateContextTokens
	TotalToolChars        = agentruntime.TotalToolChars
	NewModelRouter        = agentruntime.NewModelRouter
	NewIntentClassifier   = agentruntime.NewIntentClassifier
	NewCapabilityRouter   = agentruntime.NewCapabilityRouter
	NewPromptLibrary      = agentruntime.NewPromptLibrary
	NewTopicGuard         = agentruntime.NewTopicGuard
)

type SubagentStatus = agentsubagent.SubagentStatus
type SubagentDefinition = agentsubagent.SubagentDefinition
type SubagentResult = agentsubagent.SubagentResult
type SubagentDelegate = agentsubagent.SubagentDelegate
type ParallelTask = agentsubagent.ParallelTask
type SubagentLoader = agentsubagent.SubagentLoader

const (
	MaxDelegationDepth    = agentsubagent.MaxDelegationDepth
	SubagentStatusSuccess = agentsubagent.SubagentStatusSuccess
	SubagentStatusFailure = agentsubagent.SubagentStatusFailure
	SubagentStatusTimeout = agentsubagent.SubagentStatusTimeout
	SubagentDir           = agentsubagent.SubagentDir
)

var (
	NewSubagentDelegate            = agentsubagent.NewSubagentDelegate
	ErrDepthExceeded               = agentsubagent.ErrDepthExceeded
	DelegateToolDefinition         = agentsubagent.DelegateToolDefinition
	DelegateParallelToolDefinition = agentsubagent.DelegateParallelToolDefinition
)
