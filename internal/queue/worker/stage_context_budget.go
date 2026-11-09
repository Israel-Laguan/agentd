package worker

import (
	"fmt"
	"strings"

	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

// ACP agents do not expose their model window. This conservative byte limit
// applies to the complete prompt, including the outcome instruction.
const acpContextBudget = 64 * 1024

// fitStageContext sheds older named copies and exact duplicates before cutting
// optional content. The task and system instructions are never silently cut.
func fitStageContext(messages []spec.PromptMessage, budget int) ([]spec.PromptMessage, error) {
	if len(renderContext(messages)) <= budget {
		return messages, nil
	}
	out := shedStageCopies(messages)
	strategy := truncation.HeadTailStrategy{HeadRatio: .5}
	for {
		excess := len(renderContext(out)) - budget
		if excess <= 0 {
			return out, nil
		}
		largest := -1
		for i, m := range out {
			if protectedStageMessage(m) || m.Content == "" {
				continue
			}
			if largest == -1 || len(m.Content) > len(out[largest].Content) {
				largest = i
			}
		}
		if largest == -1 {
			return nil, fmt.Errorf("stage context: protected instructions exceed %d-byte budget", budget)
		}
		allowed := len(out[largest].Content) - excess
		// Do not leave a partial external-content wrapper or an unreadable
		// truncation marker when only a handful of bytes remain.
		if allowed < 64 {
			out = append(out[:largest], out[largest+1:]...)
			continue
		}
		out[largest].Content = strategy.Truncate(out[largest].Content, allowed)
	}
}

func protectedStageMessage(m spec.PromptMessage) bool {
	if m.Section == spec.SectionSystem || m.Section == spec.SectionTask || m.Name == "attempt_handoff" {
		return true
	}
	// Unclassified stage prompts can contain the only copy of the user's
	// instructions. Without provenance they cannot be safely shed or cut.
	return m.Section == "" && m.Name == "" && (m.Role == "system" || m.Role == "user")
}

func shedStageCopies(messages []spec.PromptMessage) []spec.PromptMessage {
	seen := map[string]bool{}
	keep := make([]bool, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		key := m.Role + "\x00" + string(m.Section) + "\x00"
		if strings.HasPrefix(m.Name, "read:") || strings.HasPrefix(m.Name, "task-list:") || m.Name == "attempt_handoff" {
			key += "name:" + m.Name
		} else {
			key += "content:" + m.Content
		}
		if !seen[key] {
			keep[i] = true
			seen[key] = true
		}
	}
	out := make([]spec.PromptMessage, 0, len(messages))
	for i, m := range messages {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out
}
