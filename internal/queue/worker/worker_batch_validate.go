package worker

import (
	"fmt"
	"strings"
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
