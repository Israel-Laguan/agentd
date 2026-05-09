package frontdesk

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// IntakeProcessor converts unprocessed human comments into follow-up tasks
// by generating a plan from the comment context.
type IntakeProcessor struct {
	store     models.KanbanStore
	gateway   gateway.AIGateway
	sink      models.EventSink
	truncator gateway.Truncator
	budget    int
}

func NewIntakeProcessor(store models.KanbanStore, gw gateway.AIGateway, sink models.EventSink, truncator gateway.Truncator, budget int) *IntakeProcessor {
	return &IntakeProcessor{store: store, gateway: gw, sink: sink, truncator: truncator, budget: budget}
}

func (p *IntakeProcessor) Process(ctx context.Context, ref models.CommentRef) error {
	task, err := p.store.GetTask(ctx, ref.TaskID)
	if err != nil {
		return err
	}
	if task.State != models.TaskStateInConsideration {
		return nil
	}
	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, p.store))
	intent, err := p.commentIntent(ctx, ref)
	if err != nil {
		return err
	}
	var (
		plan    *models.DraftPlan
		planErr error
	)
	if adapter, ok := any(p.gateway).(gateway.ContractAdapter); ok {
		var draft models.DraftPlan
		if planErr = adapter.GenerateStructuredJSON(ctx, intent, &draft); planErr == nil {
			plan = &draft
		}
	}
	if plan == nil && planErr == nil {
		plan, planErr = p.gateway.GeneratePlan(ctx, intent)
	}
	if planErr != nil {
		return planErr
	}
	if _, err := p.store.AppendTasksToProject(ctx, task.ProjectID, task.ID, plan.Tasks); err != nil {
		return err
	}
	if _, err := p.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateReady); err != nil {
		return err
	}
	if err := p.store.MarkCommentProcessed(ctx, task.ID, ref.CommentEventID); err != nil {
		return err
	}
	return p.emit(ctx, *task, len(plan.Tasks))
}

func (p *IntakeProcessor) commentIntent(ctx context.Context, ref models.CommentRef) (string, error) {
	comments, err := p.store.ListComments(ctx, ref.TaskID)
	if err != nil {
		return "", err
	}
	if len(comments) <= 1 || p.truncator == nil || p.budget <= 0 {
		return ref.Body, nil
	}
	older := olderCommentThread(comments, ref.CommentEventID)
	if len(older) <= p.budget/2 {
		return ref.Body, nil
	}
	messages, err := p.truncator.Apply(ctx, []gateway.PromptMessage{{Role: "user", Content: older}}, p.budget/2)
	if err != nil {
		return "", err
	}
	if len(messages) == 0 || strings.TrimSpace(messages[0].Content) == "" {
		return ref.Body, nil
	}
	return "Previous task comment context:\n" + strings.TrimSpace(messages[0].Content) + "\n\nNew human comment:\n" + strings.TrimSpace(ref.Body), nil
}

func olderCommentThread(comments []models.Comment, currentID string) string {
	var b strings.Builder
	for _, comment := range comments {
		if comment.ID == currentID {
			break
		}
		if strings.TrimSpace(comment.Body) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		author := strings.TrimSpace(string(comment.Author))
		if author == "" {
			author = "Comment"
		}
		b.WriteString(author)
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(comment.Body))
	}
	return b.String()
}

func (p *IntakeProcessor) emit(ctx context.Context, task models.Task, count int) error {
	if p.sink == nil {
		return nil
	}
	return p.sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		Type:      models.EventTypeCommentIntake,
		Payload:   fmt.Sprintf("converted human comment into %d follow-up tasks", count),
	})
}
