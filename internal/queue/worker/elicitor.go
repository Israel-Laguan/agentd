package worker

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

const (
	elicitationSkipMinDescriptionRunes = 200
	maxElicitationQuestions            = 5
)

const elicitorSystemPrompt = `You are a pre-task ambiguity analyzer for agentd. Given a task title, description, and optional file context, decide whether the task is specific enough to execute without human clarification.

Return ONLY strict JSON with keys: needs_clarification (boolean), questions (array, optional), reason (string, optional).

When needs_clarification is true, provide 3 to 5 precise questions about missing reproduction steps, scope boundaries, target environment, acceptance criteria, or conflicting requirements. Each question may include an "options" array of suggested answers when helpful.

When the description already states clear constraints, reproduction, scope, and success criteria, set needs_clarification to false.`

const clarificationsBlockHeader = "--- Clarifications ---"

var (
	// Match imperative/spec language, not casual narrative ("as expected", "should we").
	elicitationConstraintKeywords = regexp.MustCompile(`(?i)(?:` +
		`\bmust\s+(?:not\s+)?\w+` +
		`|\bshould\s+not\b|\bshall\s+not\b` +
		`|\bacceptance\s*(?:criteria|:)` +
		`|\brepro(?:duction)?\s+steps?\b|\bsteps?\s+to\s+repro(?:duce)?\b` +
		`|\bexpected\s+(?:behavior|result|outcome|output|response)\b` +
		`|\bconstraints?\s*:` +
		`|\brequirements?\s*:` +
		`)`)
	elicitationListPattern     = regexp.MustCompile(`(?m)^\s*([-*•]|\d+[.)])\s+\S`)
	elicitationFilePathPattern = regexp.MustCompile(`(?:^|[\s(])(?:[\w.-]+/)*[\w.-]+\.(?:go|ts|tsx|js|jsx|py|rs|java|md|yaml|yml|json)\b`)
)

// ElicitationQuestion is one structured question for human clarification.
type ElicitationQuestion struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// ElicitationAnalysis is the JSON shape returned by the cheap elicitor model.
type ElicitationAnalysis struct {
	NeedsClarification bool                  `json:"needs_clarification"`
	Questions          []ElicitationQuestion `json:"questions,omitempty"`
	Reason             string                `json:"reason,omitempty"`
}

// Elicitor runs a cheap-model pass to detect task ambiguity before agentic execution.
type Elicitor struct {
	gateway gateway.AIGateway
}

// NewElicitor returns an elicitor backed by the worker gateway.
func NewElicitor(gw gateway.AIGateway) *Elicitor {
	return &Elicitor{gateway: gw}
}

// Analyze calls the memory-tier model to detect ambiguities in the task description.
func (e *Elicitor) Analyze(ctx context.Context, task models.Task, project models.Project, fileContext string) (*ElicitationAnalysis, error) {
	if e == nil || e.gateway == nil {
		return &ElicitationAnalysis{NeedsClarification: false}, nil
	}
	userContent := buildElicitationUserContent(task, project, fileContext)
	req := gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: elicitorSystemPrompt},
			{Role: "user", Content: userContent},
		},
		Temperature: 0.1,
		JSONMode:    true,
		Role:        gateway.RoleMemory,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
	}
	analysis, err := gateway.GenerateJSON[ElicitationAnalysis](ctx, e.gateway, req)
	if err != nil {
		return nil, err
	}
	if analysis.NeedsClarification {
		analysis.Questions = normalizeElicitationQuestions(analysis.Questions)
	}
	return &analysis, nil
}

func buildElicitationUserContent(task models.Task, project models.Project, fileContext string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n", project.ID)
	fmt.Fprintf(&b, "Task: %s\n", task.Title)
	if d := strings.TrimSpace(task.Description); d != "" {
		b.WriteString("Description:\n")
		b.WriteString(d)
		b.WriteByte('\n')
	}
	if len(task.SuccessCriteria) > 0 {
		b.WriteString("Success criteria:\n")
		for _, c := range task.SuccessCriteria {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	if ctx := strings.TrimSpace(fileContext); ctx != "" {
		b.WriteString("\nFile context:\n")
		b.WriteString(ctx)
		b.WriteByte('\n')
	}
	return b.String()
}

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

func shouldSkipElicitation(task models.Task) bool {
	desc := task.Description
	if strings.Contains(desc, clarificationsBlockHeader) {
		return true
	}
	if utf8.RuneCountInString(strings.TrimSpace(desc)) > elicitationSkipMinDescriptionRunes && hasExplicitConstraints(task) {
		return true
	}
	return false
}

func hasExplicitConstraints(task models.Task) bool {
	if len(task.SuccessCriteria) > 0 {
		return true
	}
	desc := task.Description
	if elicitationListPattern.MatchString(desc) {
		return true
	}
	if elicitationConstraintKeywords.MatchString(desc) {
		return true
	}
	if elicitationFilePathPattern.MatchString(desc) {
		return true
	}
	// Multi-section specs usually have several paragraph breaks; avoid skipping long prose.
	if strings.Count(desc, "\n") >= 5 {
		return true
	}
	return false
}
