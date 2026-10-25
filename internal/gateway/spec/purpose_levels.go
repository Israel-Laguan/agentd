package spec

import "sort"

// Level is the provisional minimum agent level a purpose needs (SP-023): L1 is a
// single model call, L3 an ACP agent. It stays provisional until T-044 has numbers.
type Level string

const (
	LevelL1   Level = "L1"
	LevelL3   Level = "L3"
	LevelCode Level = "code"
	LevelDrop Level = "drop"
	LevelNone Level = "n/a"
)

// purposeLevels is the closed set. Brownfield planning raises L1 to L3 per task
// attribute, not per purpose.
var purposeLevels = map[Purpose]Level{
	PurposeIntakeClassifyIntent:   LevelL1,
	PurposeIntakeAnalyzeScope:     LevelL1,
	PurposeIntakeDraftPlan:        LevelL1,
	PurposeCreateReplanComment:    LevelL1,
	PurposeCreateNextPhase:        LevelL1,
	PurposeCreateElicit:           LevelL1,
	PurposeCreateSplitTask:        LevelL1,
	PurposeExecTurn:               LevelL3,
	PurposeExecWorkPlan:           LevelL1,
	PurposeExecBatch:              LevelL1,
	PurposeExecRespecTurn:         LevelL1,
	PurposeExecRepairSection:      LevelL1,
	PurposeExecPermissionAlt:      LevelL1,
	PurposeExecSuggestCommand:     LevelL1,
	PurposeOverheadTopicDrift:     LevelL1,
	PurposeOverheadCompactContext: LevelL1,
	PurposeSubagentTurn:           LevelL3,
	PurposeOverheadEmbedFiles:     LevelL1,
	PurposeMaintSummarizeLog:      LevelL1,
	PurposeMaintExtractMemory:     LevelL1,
	PurposeMaintMergeMemories:     LevelL1,
	PurposeHiddenTruncateSummary:  LevelL1,
	PurposeSystemWarmup:           LevelNone,
}

// MinLevel returns the provisional minimum level, or "" for an unknown purpose.
func (p Purpose) MinLevel() Level {
	if lvl, ok := purposeLevels[p]; ok {
		return lvl
	}
	return ""
}

// AllPurposes returns the closed set in a stable order (stage, then name).
func AllPurposes() []Purpose {
	out := make([]Purpose, 0, len(purposeLevels))
	for p := range purposeLevels {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
