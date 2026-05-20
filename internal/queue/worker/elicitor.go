package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

const (
	elicitationSkipMinDescriptionRunes = 200
	maxElicitationQuestions              = 5
)

const elicitorSystemPrompt = `You are a pre-task ambiguity analyzer for agentd. Given a task title, description, and optional file context, decide whether the task is specific enough to execute without human clarification.

Return ONLY strict JSON with keys: needs_clarification (boolean), questions (array, optional), reason (string, optional).

When needs_clarification is true, provide 3 to 5 precise questions about missing reproduction steps, scope boundaries, target environment, acceptance criteria, or conflicting requirements. Each question may include an "options" array of suggested answers when helpful.

When the description already states clear constraints, reproduction, scope, and success criteria, set needs_clarification to false.`

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
