package worker

import (
	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agentsubagent "agentd/internal/agent/subagent"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/config"
	"agentd/internal/gateway"
)

// === Runtime Context Aliases ===

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
type Plan = agentcontext.Plan
type PlanStep = agentcontext.PlanStep
type MessageEditor = agentcontext.MessageEditor
type EditResult = agentcontext.EditResult

const (
	CorrectionSourceTool     = agentcontext.CorrectionSourceTool
	CorrectionSourceHuman    = agentcontext.CorrectionSourceHuman
	CorrectionSourceReviewer = agentcontext.CorrectionSourceReviewer
	DefaultStallThreshold    = agentcontext.DefaultStallThreshold
	EditAnchorUserTurn       = agentcontext.EditAnchorUserTurn
)

var (
	NewContextManager           = agentcontext.NewContextManager
	NewMessageEditor            = agentcontext.NewMessageEditor
	resolveEditContextManager   = agentcontext.ResolveEditContextManager
	DetectContradictions        = agentcontext.DetectContradictions
	ParseCorrectionComment      = agentcontext.ParseCorrectionComment
	ParseGoalProgress           = agentcontext.ParseGoalProgress
	NewGoalTracker              = agentcontext.NewGoalTracker
	WithStallThreshold          = agentcontext.WithStallThreshold
	WithCriteriaStore           = agentcontext.WithCriteriaStore
	GoalFromTask                = agentcontext.GoalFromTask
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

// === Runtime Loop Aliases ===

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

// === Audit Aliases ===

type AuditLogger = agentruntime.AuditLogger
type AuditSink = agentruntime.AuditSink
type AuditRecord = agentruntime.AuditRecord
type HistoryEditRecord = agentruntime.HistoryEditRecord
type TurnSnapshotRecord = agentruntime.TurnSnapshotRecord
type TaskAuditRecord = agentruntime.TaskAuditRecord
type DaemonStartRecord = agentruntime.DaemonStartRecord
type FileAuditSink = agentruntime.FileAuditSink

var (
	NewAuditLogger      = agentruntime.NewAuditLogger
	NewFileAuditSink    = agentruntime.NewFileAuditSink
	EnsureAuditFile     = agentruntime.EnsureAuditFile
	StructuredAuditHook = agentruntime.StructuredAuditHook

	normalizeOutputFormat     = agentcontext.NormalizeOutputFormat
	ValidateOutput            = agentcontext.ValidateOutput
	extractSection            = agentcontext.ExtractSection
	formatPlanOutputForCommit = agentcontext.FormatPlanOutputForCommit
	preparePlanCommitContent  = agentcontext.PreparePlanCommitContent
	replaceSection            = agentcontext.ReplaceSection
	stepValidationError       = agentcontext.StepValidationError
)

const (
	recordTypeToolDispatch = agentruntime.RecordTypeToolDispatch
	recordTypeTurnSnapshot = agentruntime.RecordTypeTurnSnapshot
	recordTypeHistoryEdit  = agentruntime.RecordTypeHistoryEdit
	recordTypeTaskStart    = agentruntime.RecordTypeTaskStart
	recordTypeTaskComplete = agentruntime.RecordTypeTaskComplete
	recordTypeTaskFail     = agentruntime.RecordTypeTaskFail
	recordTypeTaskReview   = agentruntime.RecordTypeTaskReview
	recordTypeDaemonStart  = agentruntime.RecordTypeDaemonStart
)

func newAuditLogger(cfg config.AuditConfig) *AuditLogger {
	if !cfg.Enabled || cfg.Path == "" {
		return nil
	}
	return NewAuditLogger(NewFileAuditSink(cfg.Path), true)
}

// === Subagent Aliases ===

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

// === Hook Aliases ===

type FailurePolicy = agenthooks.FailurePolicy
type HookVerdict = agenthooks.HookVerdict
type HookContext = agenthooks.HookContext
type PreHook = agenthooks.PreHook
type PostHook = agenthooks.PostHook
type SessionStartHook = agenthooks.SessionStartHook
type HookChain = agenthooks.HookChain
type ResultCache = agenthooks.ResultCache
type RateLimitCounter = agenthooks.RateLimitCounter
type RateLimitStore = agenthooks.RateLimitStore

const (
	FailOpen   = agenthooks.FailOpen
	FailClosed = agenthooks.FailClosed

	externalContentInstruction = agenthooks.ExternalContentInstruction
)

var (
	NewHookChain = agenthooks.NewHookChain

	SchemaValidationHook            = agenthooks.SchemaValidationHook
	CredentialDetectionHook         = agenthooks.CredentialDetectionHook
	CredentialInjectionHook         = agenthooks.CredentialInjectionHook
	CredentialValidationSessionHook = agenthooks.CredentialValidationSessionHook
	ScrubResultHook                 = agenthooks.ScrubResultHook
	InjectionResistanceHook         = agenthooks.InjectionResistanceHook
	AuditHook                       = agenthooks.AuditHook
	CacheLookupHook                 = agenthooks.CacheLookupHook
	CacheStoreHook                  = agenthooks.CacheStoreHook
	NewResultCache                  = agenthooks.NewResultCache
	DryRunHook                      = agenthooks.DryRunHook
	RateLimitHook                   = agenthooks.RateLimitHook
	resolveLimit                    = agenthooks.ResolveLimit
	NewRateLimitStore               = agenthooks.NewRateLimitStore
	DenylistHook                    = agenthooks.DenylistHook
	cacheKey                        = agenthooks.CacheKey
	canonicalizeArgs                = agenthooks.CanonicalizeArgs

	externalToolsSet    = agenthooks.ExternalToolsSet
	isExternalTool      = agenthooks.IsExternalTool
	wrapExternalContent = agenthooks.WrapExternalContent
)

// === Tool Aliases ===

type ToolExecutor = agenttools.ToolExecutor
type ToolResult = agenttools.ToolResult
type ToolStatus = agenttools.ToolStatus
type ToolError = agenttools.ToolError
type RetryConfig = agenttools.RetryConfig
type RetryDispatchFunc = agenttools.RetryDispatchFunc
type RetryingExecutor = agenttools.RetryingExecutor
type ToolManifest = agenttools.ToolManifest
type TaskClassification = agenttools.TaskClassification
type TaskClassifier = agenttools.TaskClassifier
type toolFailureTracker = agenttools.ToolFailureTracker

const (
	toolNameBash             = agenttools.ToolNameBash
	toolNameRead             = agenttools.ToolNameRead
	toolNameWrite            = agenttools.ToolNameWrite
	toolNameDelegate         = agenttools.ToolNameDelegate
	toolNameDelegateParallel = agenttools.ToolNameDelegateParallel
	toolErrorPrefix          = agenttools.ToolErrorPrefix

	ToolStatusSuccess = agenttools.ToolStatusSuccess
	ToolStatusError   = agenttools.ToolStatusError
	ToolStatusTimeout = agenttools.ToolStatusTimeout
	ToolStatusVetoed  = agenttools.ToolStatusVetoed
	ToolStatusFatal   = agenttools.ToolStatusFatal

	TaskTypeSummarize   = agenttools.TaskTypeSummarize
	TaskTypeCodeGen     = agenttools.TaskTypeCodeGen
	TaskTypeDocQA       = agenttools.TaskTypeDocQA
	TaskTypeWebResearch = agenttools.TaskTypeWebResearch
	TaskTypeFullAgent   = agenttools.TaskTypeFullAgent
)

var (
	NewToolExecutor     = agenttools.NewToolExecutor
	NewRetryingExecutor = agenttools.NewRetryingExecutor
	NewToolManifest     = agenttools.NewToolManifest
	NewTaskClassifier   = agenttools.NewTaskClassifier
	BuildSandboxEnv     = agenttools.BuildSandboxEnv

	SuccessResult           = agenttools.SuccessResult
	ErrorResult             = agenttools.ErrorResult
	NonRetryableErrorResult = agenttools.NonRetryableErrorResult
	TimeoutResult           = agenttools.TimeoutResult
	VetoedResult            = agenttools.VetoedResult
	FatalResult             = agenttools.FatalResult

	SchemaRegistryFromDefinitions = agenttools.SchemaRegistryFromDefinitions
	filterToolsByNames            = agenttools.FilterToolsByNames

	jsonErrorf           = agenttools.JSONErrorf
	sandboxFailureJSON   = agenttools.SandboxFailureJSON
	isToolErrorPayload   = agenttools.IsToolErrorPayload
	stripToolErrorPrefix = agenttools.StripToolErrorPrefix
)

func newToolFailureTracker(threshold int) *toolFailureTracker {
	return agenttools.NewToolFailureTracker(threshold)
}

func classifyPrecomputedToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyPrecomputedToolResult(callID, toolName, raw, elapsedMs)
}

func classifyBuiltinToolResult(callID, toolName, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyBuiltinToolResult(callID, toolName, raw, elapsedMs)
}

func classifyCapabilityRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyCapabilityRawResult(callID, raw, elapsedMs)
}

func classifyDelegateRawResult(callID, raw string, elapsedMs int64) ToolResult {
	return agenttools.ClassifyDelegateRawResult(callID, raw, elapsedMs)
}
