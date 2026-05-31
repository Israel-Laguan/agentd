package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	wsession "agentd/internal/agent/session"
)

// EditAnchorUserTurn is the turnIndex value that rewrites the anchor user message
// (task prompt) and truncates all post-anchor history.
const EditAnchorUserTurn = -1

var (
	errEmptyEditContent    = errors.New("message editor: newContent must be non-empty")
	errInvalidTurnIndex    = errors.New("message editor: turn index out of range")
	errTurnNotUserEditable = errors.New("message editor: turn has no editable user message")
	errAnchorUserNotFound  = errors.New("message editor: anchor has no user message to edit")
)

// EditResult describes the outcome of a history edit operation.
type EditResult struct {
	CheckpointID   string
	TurnIndex      int
	MessagesBefore int
	MessagesAfter  int
}

// MessageEditor supports turn-level history rewrite and normal message commits.
type MessageEditor struct {
	checkpoints wsession.CheckpointStore
	audit       *AuditLogger
	cm          *ContextManager
}

func resolveEditContextManager(sessionCM, editorCM *ContextManager) *ContextManager {
	if sessionCM != nil {
		return sessionCM
	}
	if editorCM != nil {
		return editorCM
	}
	return &ContextManager{}
}

// NewMessageEditor returns an editor backed by checkpoints and optional audit logging.
func NewMessageEditor(checkpoints wsession.CheckpointStore, audit *AuditLogger, cm *ContextManager) *MessageEditor {
	if cm == nil {
		cm = &ContextManager{}
	}
	return &MessageEditor{checkpoints: checkpoints, audit: audit, cm: cm}
}

// Edit checkpoints the current history, rewrites the user message at editable turnIndex,
// and truncates that turn's suffix plus all subsequent turns.
// cm is the session ContextManager when available; nil falls back to the editor's default.
func (e *MessageEditor) Edit(
	ctx context.Context,
	sessionID, turnID string,
	messages *[]gateway.PromptMessage,
	turnIndex int,
	newContent string,
	cm *ContextManager,
) (EditResult, error) {
	if e == nil {
		return EditResult{}, errors.New("message editor: nil editor")
	}
	if strings.TrimSpace(newContent) == "" {
		return EditResult{}, errEmptyEditContent
	}
	if messages == nil {
		return EditResult{}, errors.New("message editor: nil messages slice")
	}
	before := len(*messages)

	edited, err := applyTurnEdit(*messages, turnIndex, newContent, resolveEditContextManager(cm, e.cm))
	if err != nil {
		return EditResult{}, err
	}

	var checkpointID string
	if e.checkpoints != nil {
		id, err := e.checkpoints.Create(ctx, sessionID, *messages)
		if err != nil {
			return EditResult{}, fmt.Errorf("message editor: checkpoint: %w", err)
		}
		checkpointID = id
	}
	*messages = edited
	after := len(*messages)

	if e.audit != nil && e.audit.Enabled() {
		e.audit.RecordHistoryEdit(HistoryEditRecord{
			SessionID:       sessionID,
			TurnID:          turnID,
			TurnIndex:       turnIndex,
			CheckpointID:    checkpointID,
			MessagesBefore:  before,
			MessagesAfter:   after,
			NewContentHash:  hashArgs(newContent),
		})
	}

	return EditResult{
		CheckpointID:   checkpointID,
		TurnIndex:      turnIndex,
		MessagesBefore: before,
		MessagesAfter:  after,
	}, nil
}

// Commit appends a message without truncation (normal assistant/tool commit path).
func (e *MessageEditor) Commit(messages *[]gateway.PromptMessage, msg gateway.PromptMessage) {
	if messages == nil {
		return
	}
	out := *messages
	if len(msg.ToolCalls) > 0 {
		out = append(out, gateway.PromptMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			ToolCalls: append([]gateway.ToolCall(nil), msg.ToolCalls...),
		})
	} else {
		out = append(out, msg)
	}
	*messages = out
}

// CommitAssistant appends an assistant turn from a gateway response.
func (e *MessageEditor) CommitAssistant(messages *[]gateway.PromptMessage, resp gateway.AIResponse) {
	e.Commit(messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})
}

// applyTurnEdit rewrites editable turn turnIndex and drops its suffix and later turns.
// Use EditAnchorUserTurn to rewrite the anchor task user message and drop all of rest.
func applyTurnEdit(messages []gateway.PromptMessage, turnIndex int, newContent string, cm *ContextManager) ([]gateway.PromptMessage, error) {
	if cm == nil {
		cm = &ContextManager{}
	}
	anchor, rest := cm.partitionAnchor(messages)
	if turnIndex == EditAnchorUserTurn {
		return applyAnchorUserEdit(anchor, newContent)
	}
	if len(rest) == 0 {
		return nil, errInvalidTurnIndex
	}
	turns := cm.groupTurns(rest)
	if turnIndex < 0 || turnIndex >= len(turns) {
		return nil, errInvalidTurnIndex
	}
	target := turns[turnIndex]
	userIdx := -1
	for i, m := range target.Messages {
		if m.Role == "user" {
			userIdx = i
			break
		}
	}
	if userIdx < 0 {
		return nil, errTurnNotUserEditable
	}

	rewritten := wsession.ClonePromptMessages(target.Messages)
	rewritten[userIdx].Content = newContent
	// Truncate within turn: keep messages up to and including rewritten user only.
	rewritten = rewritten[:userIdx+1]

	out := wsession.ClonePromptMessages(anchor)
	for i := 0; i < turnIndex; i++ {
		out = append(out, wsession.ClonePromptMessages(turns[i].Messages)...)
	}
	out = append(out, rewritten...)
	return out, nil
}

// applyAnchorUserEdit replaces the anchor user message and removes all post-anchor history.
func applyAnchorUserEdit(anchor []gateway.PromptMessage, newContent string) ([]gateway.PromptMessage, error) {
	userIdx := -1
	for i, m := range anchor {
		if m.Role == "user" {
			userIdx = i
			break
		}
	}
	if userIdx < 0 {
		return nil, errAnchorUserNotFound
	}
	out := wsession.ClonePromptMessages(anchor)
	out[userIdx].Content = newContent
	return out, nil
}
