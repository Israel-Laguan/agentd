package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentd/internal/models"
)

// ApprovalRequest describes a pending human approval for a gated tool.
type ApprovalRequest struct {
	ToolName       string        `json:"tool_name"`
	Arguments      string        `json:"arguments"`
	Rationale      string        `json:"rationale"`
	Timeout        time.Duration `json:"timeout"`
	TaskID         string        `json:"task_id"`
	TaskUpdatedAt  time.Time     `json:"task_updated_at"`
	RequestedAt    time.Time     `json:"requested_at"`
}

// ApprovalResponse carries the human decision on an approval request.
type ApprovalResponse struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason,omitempty"`
}

// ApprovalHandler processes approval requests. Implementations may
// block until a human responds or a timeout expires.
type ApprovalHandler interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error)
}

// DefaultApprovalTimeout is the maximum time to wait for a human decision.
const DefaultApprovalTimeout = 30 * time.Minute

// ApprovalGateHook returns a PreHook that intercepts gated tools and
// suspends the task by creating a HUMAN subtask for approval. The hook
// always vetoes the tool call: if the handler successfully creates the
// approval subtask the task is BLOCKED and the agentic loop stops; if
// the handler returns an explicit rejection the reason is fed back to
// the LLM. When the human later marks the subtask COMPLETED or FAILED
// the task is unblocked and the agentic loop resumes.
func ApprovalGateHook(gatedTools []string, handler ApprovalHandler) PreHook {
	toolSet := make(map[string]struct{}, len(gatedTools))
	for _, t := range gatedTools {
		toolSet[strings.TrimSpace(t)] = struct{}{}
	}

	return PreHook{
		Name:   "approval-gate",
		Policy: FailClosed,
		Fn: func(ctx HookContext) (HookVerdict, error) {
			if len(toolSet) == 0 {
				return HookVerdict{}, nil
			}
			if _, gated := toolSet[ctx.ToolName]; !gated {
				return HookVerdict{}, nil
			}

			req := ApprovalRequest{
				ToolName:      ctx.ToolName,
				Arguments:     ctx.Args,
				Rationale:     fmt.Sprintf("Tool %q requires human approval before execution.", ctx.ToolName),
				Timeout:       DefaultApprovalTimeout,
				TaskID:        ctx.SessionID,
				TaskUpdatedAt: ctx.TaskUpdatedAt,
				RequestedAt:   ctx.Timestamp,
			}

			if handler == nil {
				return HookVerdict{
					Veto:   true,
					Reason: "approval handler not configured",
				}, nil
			}

			// Note: HookContext does not carry a context.Context so we
			// use Background here. This is safe because the handler
			// returns immediately after creating the subtask (suspension
			// pattern). If the handler is later changed to poll, thread
			// a context.Context through HookContext.
			resp, err := handler.RequestApproval(context.Background(), req)
			if err != nil {
				return HookVerdict{
					Veto:   true,
					Reason: fmt.Sprintf("approval request failed: %v", err),
				}, nil
			}

			if resp.Approved {
				return HookVerdict{}, nil
			}

			if resp.Reason != "" {
				return HookVerdict{
					Veto:   true,
					Result: formatRejection(ctx.ToolName, resp.Reason),
				}, nil
			}

			return HookVerdict{
				Veto:   true,
				Result: fmt.Sprintf("Tool call %q paused pending human approval. The task is now BLOCKED until a human reviews and approves.", ctx.ToolName),
			}, nil
		},
	}
}

// formatRejection produces a human-readable rejection message that the
// LLM can use to adjust its approach.
func formatRejection(tool, reason string) string {
	return fmt.Sprintf(
		"APPROVAL REJECTED for tool %q.\nReason: %s\nAdjust your approach based on this feedback and try an alternative action.",
		tool, reason,
	)
}

// BlockingApprovalHandler implements ApprovalHandler by creating a
// HUMAN subtask via the KanbanStore. It follows the suspension pattern
// (like BlockingClarificationHandler): the handler creates the subtask
// and returns immediately with Approved=false, causing the hook to
// veto the tool call and leave the task BLOCKED.
type BlockingApprovalHandler struct {
	store models.KanbanStore
	sink  models.EventSink
	mu    sync.Mutex
}

// NewBlockingApprovalHandler returns a handler that blocks on human
// approval by creating subtasks in the task store.
func NewBlockingApprovalHandler(store models.KanbanStore, sink models.EventSink) *BlockingApprovalHandler {
	return &BlockingApprovalHandler{store: store, sink: sink}
}

// RequestApproval creates a HUMAN subtask for the approval and returns
// immediately with Approved=false. The parent task moves to BLOCKED;
// when the human marks the subtask COMPLETED or FAILED the task resumes.
func (h *BlockingApprovalHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	description := FormatForHuman(HITLMessage{
		Summary: fmt.Sprintf("Approval required for tool %q", req.ToolName),
		Action:  "Review the tool call below and mark this subtask COMPLETED to approve, or add a comment with rejection reason and mark FAILED.",
		Urgency: "blocking",
		Detail:  fmt.Sprintf("Tool: %s\nArguments: %s\nRationale: %s", req.ToolName, truncateApprovalArgs(req.Arguments), req.Rationale),
	})

	_, subtasks, err := h.store.BlockTaskWithSubtasks(ctx, req.TaskID, req.TaskUpdatedAt, []models.DraftTask{{
		Title:       fmt.Sprintf("Approve tool call: %s", req.ToolName),
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		return ApprovalResponse{}, fmt.Errorf("create approval subtask: %w", err)
	}

	if len(subtasks) == 0 {
		return ApprovalResponse{}, fmt.Errorf("no approval subtask created")
	}

	return ApprovalResponse{Approved: false}, nil
}

func truncateApprovalArgs(args string) string {
	return truncate(args, 500)
}
