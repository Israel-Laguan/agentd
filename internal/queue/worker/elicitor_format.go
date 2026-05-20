package worker

import (
	"fmt"
	"strings"
)

const clarificationsBlockHeader = "--- Clarifications ---"

func formatElicitationHITLDetail(questions []ElicitationQuestion, contextSummary string) string {
	var b strings.Builder
	b.WriteString("Pre-task clarification is required before the agent can proceed.\n\n")
	b.WriteString("Please answer all questions in a single comment on this subtask, then mark it COMPLETED.\n\n")
	b.WriteString("Questions:\n")
	for i, q := range questions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, q.Question)
		if len(q.Options) > 0 {
			b.WriteString("   Options:\n")
			for j, opt := range q.Options {
				fmt.Fprintf(&b, "   %d) %s\n", j+1, opt)
			}
		}
	}
	if contextSummary != "" {
		fmt.Fprintf(&b, "\nContext: %s\n", contextSummary)
	}
	return b.String()
}

func formatClarificationsBlock(questions []ElicitationQuestion, answer string) string {
	var b strings.Builder
	b.WriteString(clarificationsBlockHeader)
	b.WriteString("\n\nQuestions:\n")
	for i, q := range questions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, q.Question)
	}
	b.WriteString("\nAnswer:\n")
	b.WriteString(strings.TrimSpace(answer))
	return b.String()
}

func appendClarificationsToDescription(description, block string) string {
	if strings.Contains(description, clarificationsBlockHeader) {
		return description
	}
	desc := strings.TrimSpace(description)
	block = strings.TrimSpace(block)
	if desc == "" {
		return block
	}
	if block == "" {
		return desc
	}
	return desc + "\n\n" + block
}

func normalizeElicitationQuestions(questions []ElicitationQuestion) []ElicitationQuestion {
	out := make([]ElicitationQuestion, 0, len(questions))
	for _, q := range questions {
		q.Question = strings.TrimSpace(q.Question)
		if q.Question == "" {
			continue
		}
		opts := make([]string, 0, len(q.Options))
		for _, opt := range q.Options {
			if opt = strings.TrimSpace(opt); opt != "" {
				opts = append(opts, opt)
			}
		}
		q.Options = opts
		out = append(out, q)
		if len(out) >= maxElicitationQuestions {
			break
		}
	}
	return out
}
