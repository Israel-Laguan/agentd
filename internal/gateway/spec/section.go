package spec

// Section names one part of a prompt, so a record can say what the input was
// made of. Builders tag the messages they add; an untagged message falls back
// to the section its role implies.
type Section string

const (
	SectionSystem        Section = "system"         // system prompt, house rules, instructions, skills
	SectionMemories      Section = "memories"       // recalled lessons and user preferences
	SectionTask          Section = "task"           // task title and description
	SectionParentOutcome Section = "parent_outcome" // results of parent tasks
	SectionComments      Section = "comments"       // human comments, corrections, review feedback
	SectionFileContext   Section = "file_context"   // file content picked for the task
	SectionHistory       Section = "history"        // earlier assistant turns and summaries
	SectionToolResults   Section = "tool_results"   // tool output
)

// AllSections is the fixed order used by the report columns.
func AllSections() []Section {
	return []Section{
		SectionSystem, SectionMemories, SectionTask, SectionParentOutcome,
		SectionComments, SectionFileContext, SectionHistory, SectionToolResults,
	}
}

// SectionSize is the size of one section: characters and messages, no bodies.
type SectionSize struct {
	Chars    int `json:"chars"`
	Messages int `json:"messages"`
}

// Mix is the input size of one call by section.
type Mix map[Section]SectionSize

// Chars is the total across sections.
func (m Mix) Chars() int {
	n := 0
	for _, s := range m {
		n += s.Chars
	}
	return n
}

// MixOf sums message sizes by section.
func MixOf(msgs []PromptMessage) Mix {
	mix := Mix{}
	seenUser := false
	for _, m := range msgs {
		s := m.Section
		if s == "" {
			s = roleSection(m.Role, &seenUser)
		} else if m.Role == "user" {
			seenUser = true
		}
		size := mix[s]
		size.Chars += len(m.Content)
		size.Messages++
		mix[s] = size
	}
	return mix
}

// roleSection is the fallback for an untagged message. The first user message
// is the task; any later untagged user message is feedback from outside.
func roleSection(role string, seenUser *bool) Section {
	switch role {
	case "tool":
		return SectionToolResults
	case "assistant":
		return SectionHistory
	case "user":
		if !*seenUser {
			*seenUser = true
			return SectionTask
		}
		return SectionComments
	default:
		return SectionSystem
	}
}
