package worker

import (
	"context"
	"fmt"
	"hash/fnv"

	"agentd/internal/gateway/spec"
)

func (cm *ContextManager) partitionAnchor(messages []spec.PromptMessage) ([]spec.PromptMessage, []spec.PromptMessage) {
	if len(messages) == 0 {
		return nil, nil
	}
	anchorEnd := 0
	for i, m := range messages {
		if m.Role == "system" {
			anchorEnd = i + 1
		} else {
			break
		}
	}
	for i := anchorEnd; i < len(messages); i++ {
		if messages[i].Role == "user" {
			anchorEnd = i + 1
			break
		}
	}
	return messages[:anchorEnd], messages[anchorEnd:]
}

func (cm *ContextManager) groupTurns(messages []spec.PromptMessage) []Turn {
	var turns []Turn
	var currentTurn Turn
	for _, m := range messages {
		if m.Role == "user" && len(currentTurn.Messages) > 0 {
			turns = append(turns, currentTurn)
			currentTurn = Turn{}
		}
		currentTurn.Messages = append(currentTurn.Messages, m)
	}
	if len(currentTurn.Messages) > 0 {
		turns = append(turns, currentTurn)
	}
	return turns
}

func (cm *ContextManager) applyRollingSummarization(ctx context.Context, anchor []spec.PromptMessage, turns []Turn) ([]spec.PromptMessage, error) {
	keepCount := cm.cfg.KeepRecentTurns
	if keepCount >= len(turns) {
		return cm.flatten(anchor, turns), nil
	}
	splitIdx := len(turns) - keepCount
	compressedTurns, workingTurns := cm.goalAwarePartition(turns, splitIdx)
	cm.reconcileSummaryState(compressedTurns)
	if len(compressedTurns) == 0 {
		return cm.flatten(anchor, workingTurns), nil
	}

	summary, err := cm.summarizeTurns(ctx, compressedTurns)
	if err != nil {
		return nil, fmt.Errorf("summarize turns: %w", err)
	}
	out := append([]spec.PromptMessage{}, anchor...)
	out = append(out, spec.PromptMessage{Role: "system", Content: cm.formatSummary(summary)})
	for _, t := range workingTurns {
		out = append(out, t.Messages...)
	}
	return out, nil
}

func (cm *ContextManager) summarizeTurns(ctx context.Context, turns []Turn) (TurnSummary, error) {
	var newTurns []Turn
	cm.cacheMu.RLock()
	for _, t := range turns {
		if !cm.summarizedTurns[cm.hashTurn(t)] {
			newTurns = append(newTurns, t)
		}
	}
	prev := cm.runningSummary
	cm.cacheMu.RUnlock()

	if len(newTurns) == 0 && prev != nil {
		return *prev, nil
	}
	s, err := cm.generateSummary(ctx, newTurns)
	if err != nil {
		return TurnSummary{}, err
	}
	if prev != nil {
		s = mergeSummaries(*prev, s)
	}
	cm.cacheMu.Lock()
	for _, t := range newTurns {
		cm.summarizedTurns[cm.hashTurn(t)] = true
	}
	cm.runningSummary = &s
	cm.cacheMu.Unlock()
	return s, nil
}

func (cm *ContextManager) hashTurn(t Turn) uint64 {
	h := fnv.New64a()
	for _, m := range t.Messages {
		h.Write([]byte{0x1F})
		h.Write([]byte(m.Role))
		h.Write([]byte{0x00})
		h.Write([]byte(m.Content))
		h.Write([]byte{0x00})
		h.Write([]byte{byte(len(m.ToolCalls))})
		for _, tc := range m.ToolCalls {
			h.Write([]byte(tc.ID))
			h.Write([]byte{0x00})
			h.Write([]byte(tc.Function.Name))
			h.Write([]byte{0x00})
			h.Write([]byte(tc.Function.Arguments))
			h.Write([]byte{0x00})
		}
		h.Write([]byte(m.ToolCallID))
	}
	return h.Sum64()
}

func (cm *ContextManager) flatten(anchor []spec.PromptMessage, turns []Turn) []spec.PromptMessage {
	out := append([]spec.PromptMessage{}, anchor...)
	for _, t := range turns {
		out = append(out, t.Messages...)
	}
	return out
}
