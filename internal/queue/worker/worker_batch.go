package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

const (
	batchTextSystemSuffix = `You are processing multiple independent tasks in one response.
Return ONLY valid JSON with a "results" array. Each element must have:
- "slot": zero-based index matching the task slot in the user message
- "content": your complete answer for that task (plain text, no tools)`
)

func (w *Worker) runBatchTextGateway(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) (batchTextResponse, error) {
	req := w.buildBatchRequest(ctx, tasks, project, profile)
	resp, err := gateway.GenerateJSON[batchTextResponse](ctx, w.gateway, req)
	if err != nil {
		return batchTextResponse{}, err
	}
	w.RecordProviderSuccess(ctx, tasks[0])
	return resp, nil
}

func (w *Worker) buildBatchRequest(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) gateway.AIRequest {
	system := w.buildSystemPromptContent(tasks[0], project, profile) + "\n\n" + batchTextSystemSuffix
	profile = w.ApplyModelRouting(tasks[0], profile, nil, nil)

	req := gateway.AIRequest{
		Purpose: gateway.PurposeExecBatch,
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
	req = w.ApplyTuning(req, tasks[0], profile, 0)
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

func (w *Worker) processRunningTask(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
) {
	defer w.recoverPanic(ctx, task)
	release, ok := w.gateProvider(ctx, task, profile.Provider)
	if !ok {
		return
	}
	defer release()
	if profile.RequireReview {
		if done, err := w.tryFinalizeApprovedReview(ctx, task); err != nil {
			w.FailHard(ctx, task, err)
			return
		} else if done {
			return
		}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		w.handlePhasePlanning(ctx, task, project)
		return
	}
	if w.tryRenderHostReport(ctx, task, project) {
		return
	}
	if result, ok := w.processAgentic(ctx, task, project, profile); ok {
		w.handleLoopResult(ctx, task, result)
	}
}

type batchTextSlot struct {
	Slot    int    `json:"slot"`
	Content string `json:"content"`
}

type batchTextResponse struct {
	Results []batchTextSlot `json:"results"`
}

func validateBatchTextResponse(want int, resp batchTextResponse) error {
	if len(resp.Results) != want {
		return fmt.Errorf("results length = %d, want %d", len(resp.Results), want)
	}
	seen := make(map[int]struct{}, want)
	for i, s := range resp.Results {
		if s.Slot < 0 || s.Slot >= want {
			return fmt.Errorf("results[%d].slot = %d, want range [0,%d)", i, s.Slot, want)
		}
		if strings.TrimSpace(s.Content) == "" {
			return fmt.Errorf("results[%d].content is empty", i)
		}
		if _, dup := seen[s.Slot]; dup {
			return fmt.Errorf("duplicate slot %d", s.Slot)
		}
		seen[s.Slot] = struct{}{}
	}
	for i := 0; i < want; i++ {
		if _, ok := seen[i]; !ok {
			return fmt.Errorf("missing slot %d", i)
		}
	}
	return nil
}
