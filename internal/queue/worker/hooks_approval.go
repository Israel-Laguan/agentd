package worker

import (
	"context"
	"fmt"
	"strings"
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
	CallID         string        `json:"call_id"`
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
				CallID:        ctx.CallID,
			}

			if handler == nil {
				return HookVerdict{
					Veto:   true,
					Reason: "approval handler not configured",
				}, nil
			}

			execCtx := ctx.ExecCtx
			if execCtx == nil {
				execCtx = context.Background()
			}
			resp, err := handler.RequestApproval(execCtx, req)
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
				Veto:    true,
				Suspend: true,
				Result:  fmt.Sprintf("Tool call %q paused pending human approval. The task is now BLOCKED until a human reviews and approves.", ctx.ToolName),
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
}

// NewBlockingApprovalHandler returns a handler that blocks on human
// approval by creating subtasks in the task store.
func NewBlockingApprovalHandler(store models.KanbanStore) *BlockingApprovalHandler {
	return &BlockingApprovalHandler{store: store}
}

// RequestApproval creates a HUMAN subtask for the approval and returns
// immediately with Approved=false. The parent task moves to BLOCKED;
// when the human marks the subtask COMPLETED or FAILED the task resumes.
func (h *BlockingApprovalHandler) RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}

	comments, err := h.store.ListComments(ctx, req.TaskID)
	if err != nil {
		return ApprovalResponse{}, fmt.Errorf("list task comments: %w", err)
	}

	if resp, ok, err := h.resolveExistingApproval(ctx, req, comments); err != nil || ok {
		return resp, err
	}
	if hitlExpired(comments, time.Now()) {
		return ApprovalResponse{}, fmt.Errorf("approval timed out")
	}
	return h.blockForApproval(ctx, req, timeout)
}

func (h *BlockingApprovalHandler) resolveExistingApproval(
	ctx context.Context, req ApprovalRequest, comments []models.Comment,
) (ApprovalResponse, bool, error) {
	children, err := h.store.ListChildTasks(ctx, req.TaskID)
	if err != nil {
		return ApprovalResponse{}, true, fmt.Errorf("list approval subtasks: %w", err)
	}
	latest := findLatestApprovalSubtask(children, req.ToolName)
	if latest == nil {
		return ApprovalResponse{}, false, nil
	}
	switch latest.State {
	case models.TaskStateCompleted:
		if isApprovalConsumed(comments, latest.ID) {
			return ApprovalResponse{}, false, nil
		}
		if err := markApprovalUsed(ctx, h.store, req.TaskID, latest.ID); err != nil {
			return ApprovalResponse{}, true, fmt.Errorf("mark approval used: %w", err)
		}
		return ApprovalResponse{Approved: true}, true, nil
	case models.TaskStateFailed:
		reason := rejectionReasonFromSubtask(ctx, h.store, latest.ID)
		if reason == "" {
			reason = "human rejected the tool call"
		}
		return ApprovalResponse{Approved: false, Reason: reason}, true, nil
	case models.TaskStateReady, models.TaskStateRunning, models.TaskStateQueued:
		return ApprovalResponse{}, true, fmt.Errorf("approval already pending for tool %q", req.ToolName)
	default:
		return ApprovalResponse{}, false, nil
	}
}

func (h *BlockingApprovalHandler) blockForApproval(ctx context.Context, req ApprovalRequest, timeout time.Duration) (ApprovalResponse, error) {
	description := FormatForHuman(HITLMessage{
		Summary: fmt.Sprintf("Approval required for tool %q", req.ToolName),
		Action:  "Review the tool call below and mark this subtask COMPLETED to approve, or add a comment with rejection reason and mark FAILED.",
		Urgency: "blocking",
		Detail:  fmt.Sprintf("Tool: %s\nArguments: %s\nRationale: %s\nCallID: %s", req.ToolName, truncateApprovalArgs(req.Arguments), req.Rationale, req.CallID),
	})
	if err := recordHITLExpiry(ctx, h.store, req.TaskID, time.Now().Add(timeout)); err != nil {
		return ApprovalResponse{}, fmt.Errorf("record approval expiry: %w", err)
	}
	_, subtasks, err := h.store.BlockTaskWithSubtasks(ctx, req.TaskID, req.TaskUpdatedAt, []models.DraftTask{{
		Title: approvalSubtaskTitle(req.ToolName), Description: description, Assignee: models.TaskAssigneeHuman,
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
