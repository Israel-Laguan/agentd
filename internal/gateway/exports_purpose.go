package gateway

import "agentd/internal/gateway/spec"

// Purpose is the closed taxonomy of why agentd calls a model (see spec.Purpose).
type Purpose = spec.Purpose

// Purpose constants re-exported for call sites.
const (
	PurposeIntakeClassifyIntent   = spec.PurposeIntakeClassifyIntent
	PurposeIntakeAnalyzeScope     = spec.PurposeIntakeAnalyzeScope
	PurposeIntakeDraftPlan        = spec.PurposeIntakeDraftPlan
	PurposeCreateReplanComment    = spec.PurposeCreateReplanComment
	PurposeCreateNextPhase        = spec.PurposeCreateNextPhase
	PurposeCreateElicit           = spec.PurposeCreateElicit
	PurposeCreateSplitTask        = spec.PurposeCreateSplitTask
	PurposeExecTurn               = spec.PurposeExecTurn
	PurposeExecWorkPlan           = spec.PurposeExecWorkPlan
	PurposeExecBatch              = spec.PurposeExecBatch
	PurposeExecRespecTurn         = spec.PurposeExecRespecTurn
	PurposeExecRepairSection      = spec.PurposeExecRepairSection
	PurposeExecPermissionAlt      = spec.PurposeExecPermissionAlt
	PurposeExecSuggestCommand     = spec.PurposeExecSuggestCommand
	PurposeOverheadTopicDrift     = spec.PurposeOverheadTopicDrift
	PurposeOverheadCompactContext = spec.PurposeOverheadCompactContext
	PurposeSubagentTurn           = spec.PurposeSubagentTurn
	PurposeOverheadEmbedFiles     = spec.PurposeOverheadEmbedFiles
	PurposeMaintSummarizeLog      = spec.PurposeMaintSummarizeLog
	PurposeMaintExtractMemory     = spec.PurposeMaintExtractMemory
	PurposeMaintMergeMemories     = spec.PurposeMaintMergeMemories
	PurposeHiddenTruncateSummary  = spec.PurposeHiddenTruncateSummary
	PurposeSystemWarmup           = spec.PurposeSystemWarmup
	PurposeUnlabelled             = spec.PurposeUnlabelled
)

// Purpose context helpers.
var (
	WithPurpose        = spec.WithPurpose
	PurposeFromContext = spec.PurposeFromContext
)

// Attempt kinds for retry loops that sit above the router.
type AttemptKind = spec.AttemptKind

const (
	AttemptFirst      = spec.AttemptFirst
	AttemptJSONRepair = spec.AttemptJSONRepair
)
