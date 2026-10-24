package main

import (
	"context"
	"fmt"
	"log/slog"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// agenticProfilesWithoutTools returns the IDs of agentic profiles whose provider
// cannot call tools. Their tasks fail at dispatch, so startup names them.
func agenticProfilesWithoutTools(ctx context.Context, store models.KanbanStore, toolCapable func(provider string) bool) ([]string, error) {
	profiles, err := store.ListAgentProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agent profiles: %w", err)
	}
	var ids []string
	for _, p := range profiles {
		if p.AgenticMode && !toolCapable(p.Provider) {
			ids = append(ids, p.ID)
		}
	}
	return ids, nil
}

// warnAgenticProfilesWithoutTools logs each flagged profile. It does not stop the
// daemon, because the PATCH that fixes a profile goes through the API, which
// only serves once the daemon is up.
func warnAgenticProfilesWithoutTools(ctx context.Context, store models.KanbanStore, gw gateway.AIGateway) error {
	ids, err := agenticProfilesWithoutTools(ctx, store, func(provider string) bool {
		return gateway.ProviderSupportsChatTools(gw, provider)
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		slog.Error("agentic profile's provider cannot call tools; its tasks will fail until it is patched to a tool-capable provider",
			"agent_id", id)
	}
	return nil
}
