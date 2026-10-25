package tools

import (
	"encoding/json"
	"errors"
	"strings"

	"agentd/internal/gateway"
)

// AskHumanArgs are the arguments of an ask_human call.
type AskHumanArgs struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// AskHumanToolDefinition is the ask_human builtin. The task blocks until a
// human answers the question, and the run resumes with the answer.
func AskHumanToolDefinition() gateway.ToolDefinition {
	return gateway.ToolDefinition{
		Name:        ToolNameAskHuman,
		Description: "Ask a human one question when a detail the task needs is missing and the workspace cannot answer it. The task waits for the answer and then continues with it. Use this instead of guessing.",
		Parameters: &gateway.FunctionParameters{
			Type: "object",
			Properties: map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The one question to ask, in full",
				},
				"options": map[string]any{
					"type":        "array",
					"description": "Optional answers the human can choose from",
					"items":       map[string]any{"type": "string"},
				},
			},
			Required: []string{"question"},
		},
	}
}

// ParseAskHumanArgs decodes an ask_human call and checks that it has a question.
func ParseAskHumanArgs(raw string) (AskHumanArgs, error) {
	var args AskHumanArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return AskHumanArgs{}, err
	}
	args.Question = strings.TrimSpace(args.Question)
	if args.Question == "" {
		return AskHumanArgs{}, errors.New("question is required")
	}
	return args, nil
}
