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
	batchLegacySystemSuffix = `You are processing multiple independent tasks in one response.
Return ONLY valid JSON with a "results" array. Each element must have:
- "slot": zero-based index matching the task slot in the user message
- either {"command":"..."} for a safe shell command, or {"too_complex":true,"subtasks":[{"title":"...","description":"..."}]}`
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

func (w *Worker) processRunningTask(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
) {
	defer w.recoverPanic(ctx, task)
	if w.providerBreakers != nil && profile.Provider != "" {
		if w.providerBreakers.Get(profile.Provider).IsOpen() {
			w.handoffOrFail(ctx, task,
				fmt.Errorf("%w: provider %s circuit breaker is open",
					models.ErrLLMQuotaExceeded, profile.Provider))
			return
		}
	}
	if profile.RequireReview {
		if done, err := w.tryFinalizeApprovedReview(ctx, task); err != nil {
			w.failHard(ctx, task, err)
			return
		} else if done {
			return
		}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		w.handlePhasePlanning(ctx, task, project)
		return
	}
	if profile.AgenticMode {
		if result, ok := w.processAgentic(ctx, task, project, profile); ok {
			w.handleLoopResult(ctx, task, result)
		}
		return
	}
	w.runLegacyTask(ctx, task, project, profile, false)
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

type batchLegacySlot struct {
	Slot       int             `json:"slot"`
	Command    string          `json:"command,omitempty"`
	TooComplex bool            `json:"too_complex,omitempty"`
	Subtasks   []workerSubtask `json:"subtasks,omitempty"`
}

type batchLegacyResponse struct {
	Results []batchLegacySlot `json:"results"`
}

func validateBatchLegacySlot(slot batchLegacySlot) error {
	if slot.TooComplex {
		if len(slot.Subtasks) == 0 {
			return fmt.Errorf("too_complex without subtasks")
		}
		return nil
	}
	if strings.TrimSpace(slot.Command) == "" {
		return fmt.Errorf("missing command")
	}
	return nil
}
