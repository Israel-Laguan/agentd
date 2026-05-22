package queue

import (
	"context"
	"strings"

	"agentd/internal/config"
	"agentd/internal/memory"
	"agentd/internal/models"
)

func defaultContextProviders(store models.KanbanStore, librarian config.LibrarianConfig) ContextProviderRegistry {
	retriever := &memory.Retriever{Store: store, Cfg: librarian}
	return ContextProviderRegistry{
		"static":        staticContextProvider{},
		"memory_recall": memoryRecallProvider{retriever: retriever},
		"house_rules":   houseRulesProvider{store: store},
	}
}

type staticContextProvider struct{}

func (staticContextProvider) Fetch(_ context.Context, entry models.ScheduledTask, _ string) (string, error) {
	if body := entry.ContextArgs["body"]; body != "" {
		return body, nil
	}
	return entry.ContextArgs["text"], nil
}

type memoryRecallProvider struct {
	retriever *memory.Retriever
}

func (p memoryRecallProvider) Fetch(ctx context.Context, entry models.ScheduledTask, projectID string) (string, error) {
	if p.retriever == nil {
		return "", nil
	}
	intent := entry.ContextArgs["intent"]
	if intent == "" {
		intent = entry.Title
	}
	userID := entry.ContextArgs["user_id"]
	memories := p.retriever.Recall(ctx, intent, projectID, userID)
	lessons := memory.FormatLessons(memories)
	prefs := memory.FormatPreferences(memories)
	if lessons == "" && prefs == "" {
		return "", nil
	}
	return strings.TrimSpace(lessons + "\n" + prefs), nil
}

type houseRulesProvider struct {
	store models.KanbanStore
}

func (p houseRulesProvider) Fetch(ctx context.Context, _ models.ScheduledTask, _ string) (string, error) {
	if p.store == nil {
		return "", nil
	}
	return models.LoadHouseRules(ctx, p.store), nil
}
