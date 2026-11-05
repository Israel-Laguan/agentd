package models

import (
	"errors"
	"fmt"
	"testing"

	"agentd/agentkit/llm"
)

// The gateway returns llm errors; the rest of agentd matches them as models errors.
func TestLLMErrorsAreSharedWithTheKit(t *testing.T) {
	cases := []struct{ models, kit error }{
		{ErrLLMUnreachable, llm.ErrLLMUnreachable},
		{ErrLLMQuotaExceeded, llm.ErrLLMQuotaExceeded},
		{ErrInvalidJSONResponse, llm.ErrInvalidJSONResponse},
		{ErrBudgetExceeded, llm.ErrBudgetExceeded},
	}
	for _, c := range cases {
		if !errors.Is(fmt.Errorf("wrapped: %w", c.kit), c.models) {
			t.Errorf("errors.Is(%v wrapped, models) is false", c.kit)
		}
	}
}
