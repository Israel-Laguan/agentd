package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/config"
	"agentd/internal/models"
)

// Retriever queries durable memories with namespace isolation and hard timeouts.
// If the store does not respond within Cfg.RecallTimeout, the caller proceeds
// without historical context (Danger B mitigation).
type Retriever struct {
	Store models.KanbanStore
	Cfg   config.LibrarianConfig
}

// Recall searches memories matching intent, scoped to GLOBAL + the given
// project + user preferences. Returns nil on timeout or error.
func (r *Retriever) Recall(ctx context.Context, intent, projectID, userID string) []models.Memory {
	if r == nil || r.Store == nil {
		return nil
	}
	timeout := r.Cfg.RecallTimeout
	if timeout <= 0 {
		timeout = config.DefaultRecallTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	topK := r.Cfg.RecallTopK
	if topK <= 0 {
		topK = 5
	}

	memories, err := r.Store.RecallMemories(ctx, models.RecallQuery{
		Intent:    intent,
		ProjectID: projectID,
		UserID:    userID,
		Limit:     topK,
	})
	if err != nil {
		slog.Warn("memory recall failed", "error", err)
		return nil
	}

	if len(memories) > 0 {
		ids := make([]string, len(memories))
		for i, m := range memories {
			ids[i] = m.ID
		}
		go func() {
			if err := r.Store.TouchMemories(context.Background(), ids); err != nil {
				slog.Warn("memory touch failed", "error", err)
			}
		}()
	}

	return memories
}

// FormatLessons renders recalled memories into a system prompt block.
func FormatLessons(memories []models.Memory) string {
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("LESSONS LEARNED (from previous tasks):\n")
	for i, m := range memories {
		if m.Scope == "USER_PREFERENCE" {
			continue
		}
		fmt.Fprintf(&b, "%d. Symptom: %s\n   Solution: %s\n",
			i+1, m.Symptom.String, m.Solution.String)
	}
	return b.String()
}

// FormatPreferences renders user preference memories into a system prompt block.
func FormatPreferences(memories []models.Memory) string {
	var prefs []string
	for _, m := range memories {
		if m.Scope == "USER_PREFERENCE" && m.Solution.Valid {
			prefs = append(prefs, m.Solution.String)
		}
	}
	if len(prefs) == 0 {
		return ""
	}
	return "USER PREFERENCES:\n" + strings.Join(prefs, "\n")
}
