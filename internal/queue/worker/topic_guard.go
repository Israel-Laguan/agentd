package worker

import (
	"context"
	"log/slog"
	"strings"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

const (
	topicGuardMaxInputChars = 4000
	topicGuardMaxTokens     = 16
)

// TopicGuard compares new user input against the session topic via a cheap model call.
type TopicGuard struct {
	gateway gateway.AIGateway
	cfg     config.TopicGuardConfig
}

// NewTopicGuard returns a TopicGuard wired to the worker gateway.
func NewTopicGuard(gw gateway.AIGateway, cfg config.TopicGuardConfig) *TopicGuard {
	return &TopicGuard{gateway: gw, cfg: cfg}
}

// DetectDrift returns true when newInput is a completely different topic from sessionTopic.
// Disabled globally, per-profile, or on gateway failure returns false (fail open).
func (tg *TopicGuard) DetectDrift(
	ctx context.Context,
	sessionTopic, newInput string,
	profile models.AgentProfile,
) (bool, error) {
	if tg == nil || !tg.cfg.Enabled || profile.DisableTopicDrift {
		return false, nil
	}
	sessionTopic = strings.TrimSpace(sessionTopic)
	newInput = strings.TrimSpace(newInput)
	if sessionTopic == "" || newInput == "" {
		return false, nil
	}
	if tg.gateway == nil {
		return false, nil
	}

	prompt := buildTopicDriftPrompt(sessionTopic, newInput, tg.cfg.Sensitivity)
	req := gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: "You classify whether two messages are about completely different topics. Reply with exactly YES or NO."},
			{Role: "user", Content: prompt},
		},
		Role:      gateway.RoleMemory,
		MaxTokens: topicGuardMaxTokens,
	}
	resp, err := tg.gateway.Generate(ctx, req)
	if err != nil {
		slog.Warn("topic guard drift check failed; continuing session", "error", err)
		return false, nil
	}
	return parseTopicDriftResponse(resp.Content), nil
}

func buildTopicDriftPrompt(sessionTopic, newInput string, sensitivity float64) string {
	sessionTopic = truncateForTopicGuard(sessionTopic, topicGuardMaxInputChars)
	newInput = truncateForTopicGuard(newInput, topicGuardMaxInputChars)
	strictness := topicGuardStrictnessHint(sensitivity)
	var b strings.Builder
	b.WriteString("Session topic (what the agent has been working on):\n")
	b.WriteString(sessionTopic)
	b.WriteString("\n\nNew user input:\n")
	b.WriteString(newInput)
	b.WriteString("\n\nQuestion: Is the new input a completely different topic from the session topic? ")
	b.WriteString(strictness)
	b.WriteString("\nReply with exactly YES or NO.")
	return b.String()
}

func topicGuardStrictnessHint(sensitivity float64) string {
	switch {
	case sensitivity >= 0.75:
		return "Be liberal: answer YES when the new input shifts to a distinct subject area, even if loosely related."
	case sensitivity <= 0.25:
		return "Be conservative: answer YES only when the new input is clearly unrelated to the session topic."
	default:
		return "Answer YES only when the new input is about a completely different subject, not a follow-up or refinement."
	}
}

func truncateForTopicGuard(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "... [truncated]"
}

func parseTopicDriftResponse(content string) bool {
	answer := strings.TrimSpace(strings.ToUpper(content))
	// Take first token in case the model adds punctuation.
	if idx := strings.IndexAny(answer, " \t\n\r.,;:"); idx > 0 {
		answer = answer[:idx]
	}
	return answer == "YES"
}
