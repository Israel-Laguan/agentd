package worker

import (
	"context"
	"fmt"
	"os"
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
		if s.Slot != i {
			return fmt.Errorf("results[%d].slot = %d, want %d", i, s.Slot, i)
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

func validateBatchLegacyResponse(want int, resp batchLegacyResponse) error {
	if len(resp.Results) != want {
		return fmt.Errorf("results length = %d, want %d", len(resp.Results), want)
	}
	seen := make(map[int]struct{}, want)
	for i, s := range resp.Results {
		if s.Slot != i {
			return fmt.Errorf("results[%d].slot = %d, want %d", i, s.Slot, i)
		}
		if s.TooComplex {
			if len(s.Subtasks) == 0 {
				return fmt.Errorf("results[%d]: too_complex without subtasks", i)
			}
		} else if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("results[%d]: missing command", i)
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

// ProcessBatch runs a batched LLM call for same-context tasks, with per-slot fallback.
func (w *Worker) ProcessBatch(ctx context.Context, tasks []models.Task) {
	if len(tasks) == 0 {
		return
	}
	if len(tasks) == 1 {
		w.Process(ctx, tasks[0])
		return
	}

	defer func() {
		if r := recover(); r != nil {
			for _, task := range tasks {
				w.emit(ctx, task, "PANIC", fmt.Sprintf("worker batch panic: %v", r))
				w.failHard(ctx, task, fmt.Errorf("worker batch panic: %v", r))
			}
		}
	}()

	project, profile, err := w.loadContext(ctx, tasks[0])
	if err != nil {
		for _, task := range tasks {
			w.failHard(ctx, task, err)
		}
		return
	}

	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, w.store))

	var runnable []models.Task
	heartbeats := make([]func(), 0, len(tasks))
	for _, task := range tasks {
		if planning.IsPhasePlanningTask(task.Title) {
			w.Process(ctx, task)
			continue
		}
		running, runErr := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, os.Getpid())
		if runErr != nil {
			continue
		}
		task = *running
		if profile.RequireReview {
			if done, finErr := w.tryFinalizeApprovedReview(ctx, task); finErr != nil {
				w.failHard(ctx, task, finErr)
				continue
			} else if done {
				continue
			}
		}
		heartbeats = append(heartbeats, w.startHeartbeat(ctx, task.ID))
		runnable = append(runnable, task)
	}
	defer func() {
		for _, stop := range heartbeats {
			stop()
		}
	}()

	if len(runnable) < 2 {
		for _, task := range runnable {
			w.Process(ctx, task)
		}
		return
	}

	if profile.AgenticMode {
		w.processBatchAgentic(ctx, runnable, *project, *profile)
		return
	}
	w.processBatchLegacy(ctx, runnable, *project, *profile)
}

func (w *Worker) processBatchAgentic(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) {
	resp, err := w.runBatchTextGateway(ctx, tasks, project, profile)
	if err != nil {
		for _, task := range tasks {
			w.Process(ctx, task)
		}
		return
	}
	bySlot := make(map[int]batchTextSlot, len(resp.Results))
	for _, r := range resp.Results {
		bySlot[r.Slot] = r
	}
	for i, task := range tasks {
		slot, ok := bySlot[i]
		if !ok || strings.TrimSpace(slot.Content) == "" || slot.Slot != i {
			w.Process(ctx, task)
			continue
		}
		p := profile
		w.commitTextWithProfile(ctx, task, slot.Content, &p)
	}
}

func (w *Worker) processBatchLegacy(
	ctx context.Context,
	tasks []models.Task,
	project models.Project,
	profile models.AgentProfile,
) {
	resp, err := w.runBatchLegacyGateway(ctx, tasks, project, profile)
	if err != nil {
		for _, task := range tasks {
			w.Process(ctx, task)
		}
		return
	}
	bySlot := make(map[int]batchLegacySlot, len(resp.Results))
	for _, r := range resp.Results {
		bySlot[r.Slot] = r
	}
	for i, task := range tasks {
		slot, ok := bySlot[i]
		if !ok {
			w.Process(ctx, task)
			continue
		}
		if slot.Slot != i || validateBatchLegacySlot(slot) != nil {
			w.Process(ctx, task)
			continue
		}
		w.applyBatchLegacySlot(ctx, task, project, profile, slot)
	}
}

func (w *Worker) applyBatchLegacySlot(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	slot batchLegacySlot,
) {
	if slot.TooComplex {
		w.handleTaskBreakdown(ctx, task, slot.Subtasks)
		return
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.registerCancel(task.ID, cancel)
	defer w.deregisterCancel(task.ID)
	result, runErr := w.sandbox.Execute(execCtx, w.payload(task, project, slot.Command))
	if w.isPromptHang(result, runErr) {
		w.handlePromptRecovery(ctx, task, project, slot.Command, result)
		return
	}
	if w.isPermissionFailure(result, runErr) {
		w.handlePermissionFailure(ctx, task, slot.Command, result)
		return
	}
	if profile.RequireReview && runErr == nil && result.Success {
		w.createReviewHandoff(ctx, task, result.Stdout)
		return
	}
	w.commit(ctx, task, result, runErr)
}

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
	profile = w.routeLegacyProfile(ctx, tasks[0], profile)
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
		system = legacyJSONCommandSystemSentinel + batchLegacySystemSuffix
		if profile.SystemPrompt.Valid {
			system = profile.SystemPrompt.String + "\n\n" + batchLegacySystemSuffix
		}
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
