package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// summarize runs the chunked map-reduce pipeline against the LLM.
func (l *Librarian) summarize(ctx context.Context, rawLog string) (memorySummary, error) {
	if l.Breaker != nil && l.Breaker.IsOpen() {
		return memorySummary{}, fmt.Errorf("circuit breaker is open")
	}
	if l.Gateway == nil {
		return memorySummary{}, fmt.Errorf("no AI gateway configured")
	}
	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, l.Store))

	chunks := chunkLog(rawLog, l.chunkChars())

	summaries := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		s, err := l.mapChunk(ctx, chunk)
		if err != nil {
			return memorySummary{}, fmt.Errorf("map phase: %w", err)
		}
		summaries = append(summaries, s)
	}

	combined := strings.Join(summaries, "\n\n---\n\n")
	for pass := 0; pass < l.maxReducePasses() && len(combined) > l.chunkChars(); pass++ {
		reduced, err := l.reduceText(ctx, combined)
		if err != nil {
			return memorySummary{}, fmt.Errorf("reduce pass %d: %w", pass, err)
		}
		combined = reduced
	}

	return l.extractMemory(ctx, combined)
}

func (l *Librarian) mapChunk(ctx context.Context, chunk string) (string, error) {
	resp, err := l.Gateway.Generate(ctx, gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: "Summarize this task execution log chunk. Preserve errors, commands, outcomes, key decisions, and any environment observations. Output only the summary."},
			{Role: "user", Content: chunk},
		},
		Temperature:    0.1,
		SkipTruncation: true,
		Role:           gateway.RoleMemory,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (l *Librarian) reduceText(ctx context.Context, text string) (string, error) {
	resp, err := l.Gateway.Generate(ctx, gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: "Condense the following summaries of a task execution log into a single coherent summary. Preserve errors, final outcomes, and key decisions. Output only the condensed summary."},
			{Role: "user", Content: text},
		},
		Temperature:    0.1,
		SkipTruncation: true,
		Role:           gateway.RoleMemory,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (l *Librarian) extractMemory(ctx context.Context, condensed string) (memorySummary, error) {
	resp, err := l.Gateway.Generate(ctx, gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: `Given these summarized task execution logs, produce a JSON object with two fields:
- "symptom": describes what the task attempted and any failures encountered.
- "solution": describes the resolution, workaround, or final state.
Output only valid JSON.`},
			{Role: "user", Content: condensed},
		},
		Temperature:    0.1,
		JSONMode:       true,
		SkipTruncation: true,
		Role:           gateway.RoleMemory,
	})
	if err != nil {
		return memorySummary{}, err
	}

	var ms memorySummary
	if err := json.Unmarshal([]byte(resp.Content), &ms); err != nil {
		return memorySummary{}, fmt.Errorf("parse memory JSON: %w", err)
	}
	return ms, nil
}

func (l *Librarian) fallbackExtract(rawLog string) memorySummary {
	n := l.fallbackChars()
	return memorySummary{
		Symptom:  headChars(rawLog, n),
		Solution: tailChars(rawLog, n),
	}
}

// chunkLog splits text into sequential chunks of at most maxChars, breaking on
// double-newline event boundaries when possible.
func chunkLog(text string, maxChars int) []string {
	if len(text) <= maxChars {
		return []string{text}
	}
	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxChars {
			chunks = append(chunks, text)
			break
		}
		cut := maxChars
		if idx := strings.LastIndex(text[:cut], "\n\n"); idx > 0 {
			cut = idx + 2
		}
		chunks = append(chunks, text[:cut])
		text = text[cut:]
	}
	return chunks
}

func headChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func tailChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
