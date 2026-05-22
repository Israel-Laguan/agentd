package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (w *Worker) runBatchTextGateway(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) (batchTextResponse, error) {
	req := w.buildBatchRequest(ctx, tasks, project, profile, true)
	resp, err := gateway.GenerateJSON[batchTextResponse](ctx, w.gateway, req)
	if err != nil {
		return batchTextResponse{}, err
	}
	return resp, nil
}

func (w *Worker) runBatchLegacyGateway(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) (batchLegacyResponse, error) {
	req := w.buildBatchRequest(ctx, tasks, project, profile, false)
	profile = w.routeLegacyProfile(ctx, tasks[0], project, profile)
	req.Provider = profile.Provider
	req.Model = profile.Model
	resp, err := gateway.GenerateJSON[batchLegacyResponse](ctx, w.gateway, req)
	if err != nil {
		return batchLegacyResponse{}, err
	}
	return resp, nil
}

func (w *Worker) buildBatchRequest(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
	agenticText bool,
) gateway.AIRequest {
	var system string
	if agenticText {
		system = w.buildSystemPromptContent(tasks[0], project, profile) + "\n\n" + batchTextSystemSuffix
		profile = w.applyModelRouting(tasks[0], profile, nil, nil)
	} else {
		system = legacyJSONCommandSystemContent(profile) + "\n\n" + batchLegacySystemSuffix
	}

	req := gateway.AIRequest{
		Messages: []gateway.PromptMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: batchUserPrompt(tasks)},
		},
		Temperature: profile.Temperature,
		JSONMode:    true,
		AgentID:     profile.ID,
		Role:        gateway.RoleWorker,
		TaskID:      tasks[0].ID,
		Provider:    profile.Provider,
		Model:       profile.Model,
		MaxTokens:   profile.MaxTokens,
	}
	req = w.applyTuning(req, tasks[0], profile, 0)
	return req
}

func countBatchSlots(userContent string) int {
	max := -1
	for i := 0; i < 64; i++ {
		if strings.Contains(userContent, fmt.Sprintf("Slot %d:", i)) {
			max = i
		}
	}
	return max + 1
}

func batchUserPrompt(tasks []models.Task) string {
	var b strings.Builder
	b.WriteString("Process each task slot independently.\n\n")
	for i, task := range tasks {
		fmt.Fprintf(&b, "Slot %d:\nTitle: %s\nDescription: %s\n\n", i, task.Title, task.Description)
	}
	return strings.TrimSpace(b.String())
}
