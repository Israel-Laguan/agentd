package gateway

import "agentd/internal/gateway/spec"

// Section labels one part of a prompt (see spec.Section).
type Section = spec.Section

// Section constants re-exported for the prompt builders.
const (
	SectionSystem        = spec.SectionSystem
	SectionMemories      = spec.SectionMemories
	SectionTask          = spec.SectionTask
	SectionParentOutcome = spec.SectionParentOutcome
	SectionComments      = spec.SectionComments
	SectionFileContext   = spec.SectionFileContext
	SectionHistory       = spec.SectionHistory
	SectionToolResults   = spec.SectionToolResults
)
