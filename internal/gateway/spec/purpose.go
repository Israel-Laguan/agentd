package spec

import (
	"context"
	"strings"
)

// Purpose says why agentd makes a model call. It is a closed set (SP-023,
// approved 2026-10-19) in the form "<stage>.<name>"; the stage is the prefix.
// The router picks the agent for a call from its purpose.
type Purpose string

// PurposeUnlabelled is reserved: the router assigns it to a call that arrives
// with an empty or unknown Purpose so the call is counted, never dropped.
// Call sites must not send it.
const PurposeUnlabelled Purpose = "unlabelled"

const (
	PurposeIntakeClassifyIntent Purpose = "intake.classify_intent"
	PurposeIntakeAnalyzeScope   Purpose = "intake.analyze_scope"
	PurposeIntakeDraftPlan      Purpose = "intake.draft_plan"

	PurposeCreateReplanComment Purpose = "create.replan_comment"
	PurposeCreateNextPhase     Purpose = "create.next_phase"
	PurposeCreateElicit        Purpose = "create.elicit"
	PurposeCreateSplitTask     Purpose = "create.split_task"

	PurposeExecTurn           Purpose = "exec.turn"
	PurposeExecWorkPlan       Purpose = "exec.work_plan"
	PurposeExecBatch          Purpose = "exec.batch"
	PurposeExecRespecTurn     Purpose = "exec.respec_turn"
	PurposeExecRepairSection  Purpose = "exec.repair_section"
	PurposeExecPermissionAlt  Purpose = "exec.permission_alt"
	PurposeExecSuggestCommand Purpose = "exec.suggest_command"

	PurposeOverheadTopicDrift     Purpose = "overhead.topic_drift"
	PurposeOverheadCompactContext Purpose = "overhead.compact_context"
	PurposeSubagentTurn           Purpose = "overhead.subagent_turn"
	PurposeOverheadEmbedFiles     Purpose = "overhead.embed_files"

	PurposeMaintSummarizeLog  Purpose = "maint.summarize_log"
	PurposeMaintExtractMemory Purpose = "maint.extract_memory"
	PurposeMaintMergeMemories Purpose = "maint.merge_memories"

	PurposeHiddenTruncateSummary Purpose = "hidden.truncate_summary"
	PurposeSystemWarmup          Purpose = "system.warmup"
)

// Stage returns the part before the dot ("exec" for "exec.turn").
func (p Purpose) Stage() string {
	stage, _, found := strings.Cut(string(p), ".")
	if !found {
		return string(p)
	}
	return stage
}

// Valid reports whether p is in the closed set. PurposeUnlabelled is not.
func (p Purpose) Valid() bool {
	if p == "" || p == PurposeUnlabelled {
		return false
	}
	_, ok := purposeLevels[p]
	return ok
}

// Normalize maps an empty or unknown purpose to PurposeUnlabelled.
func (p Purpose) Normalize() Purpose {
	if p.Valid() {
		return p
	}
	return PurposeUnlabelled
}

type purposeCtxKey struct{}

// WithPurpose attaches a purpose to ctx for entry points that have no request
// to put it on, so callers can say which plan they are asking for.
func WithPurpose(ctx context.Context, p Purpose) context.Context {
	if p == "" {
		return ctx
	}
	return context.WithValue(ctx, purposeCtxKey{}, p)
}

// PurposeFromContext returns the purpose set by WithPurpose, or "".
func PurposeFromContext(ctx context.Context) Purpose {
	if p, ok := ctx.Value(purposeCtxKey{}).(Purpose); ok {
		return p
	}
	return ""
}
