package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *gatewayScenario) messageWithChars(_ context.Context, n int) error {
	s.inputContent = strings.Repeat("x", n)
	return nil
}

func (s *gatewayScenario) setStrategy(_ context.Context, name string) error {
	switch name {
	case "middle_out":
		s.strategy = MiddleOutStrategy{}
	case "head_tail":
		s.strategy = HeadTailStrategy{HeadRatio: 0.5}
	default:
		return fmt.Errorf("unknown strategy %q", name)
	}
	s.truncator = StrategyTruncator{Strategy: s.strategy}
	return nil
}

func (s *gatewayScenario) setStrategyWithRatio(_ context.Context, name string, ratio float64) error {
	switch name {
	case "head_tail":
		s.strategy = HeadTailStrategy{HeadRatio: ratio}
	default:
		return fmt.Errorf("strategy %q does not support ratio", name)
	}
	s.headRatio = ratio
	s.truncator = StrategyTruncator{Strategy: s.strategy}
	return nil
}

func (s *gatewayScenario) setPolicy(_ context.Context, name string) error {
	switch name {
	case "reject":
		s.truncator = RejectTruncator{}
	default:
		return fmt.Errorf("unknown policy %q", name)
	}
	return nil
}

func (s *gatewayScenario) applyTruncation(_ context.Context, budget int) error {
	s.budget = budget
	msgs := []PromptMessage{{Role: "user", Content: s.inputContent}}
	s.outputMsg, s.truncErr = s.truncator.Apply(context.Background(), msgs, budget)
	return nil
}

func (s *gatewayScenario) outputAtMost(_ context.Context, max int) error {
	if s.truncErr != nil {
		return nil
	}
	for _, msg := range s.outputMsg {
		if len(msg.Content) > max {
			return fmt.Errorf("output length %d > %d", len(msg.Content), max)
		}
	}
	return nil
}

func (s *gatewayScenario) outputStartsWithHead(context.Context) error {
	if len(s.outputMsg) == 0 {
		return fmt.Errorf("no output messages")
	}
	if !strings.HasPrefix(s.outputMsg[0].Content, s.inputContent[:10]) {
		return fmt.Errorf("output does not start with head of original")
	}
	return nil
}

func (s *gatewayScenario) outputEndsWithTail(context.Context) error {
	if len(s.outputMsg) == 0 {
		return fmt.Errorf("no output messages")
	}
	if !strings.HasSuffix(s.outputMsg[0].Content, s.inputContent[len(s.inputContent)-10:]) {
		return fmt.Errorf("output does not end with tail of original")
	}
	return nil
}

func (s *gatewayScenario) outputContainsMarker(context.Context) error {
	if len(s.outputMsg) == 0 {
		return fmt.Errorf("no output messages")
	}
	if !strings.Contains(s.outputMsg[0].Content, TruncationMarker) {
		return fmt.Errorf("output missing truncation marker")
	}
	return nil
}

func (s *gatewayScenario) headLargerThanTail(context.Context) error {
	if len(s.outputMsg) == 0 {
		return fmt.Errorf("no output messages")
	}
	content := s.outputMsg[0].Content
	idx := strings.Index(content, TruncationMarker)
	if idx < 0 {
		return fmt.Errorf("truncation marker not found")
	}
	head := content[:idx]
	tail := content[idx+len(TruncationMarker):]
	if len(head) <= len(tail) {
		return fmt.Errorf("head (%d) not larger than tail (%d)", len(head), len(tail))
	}
	return nil
}

func (s *gatewayScenario) truncationRejected(context.Context) error {
	if !errors.Is(s.truncErr, ErrContextBudgetExceeded) {
		return fmt.Errorf("err = %v, want ErrContextBudgetExceeded", s.truncErr)
	}
	return nil
}

func (s *gatewayScenario) outputEqualsOriginal(context.Context) error {
	if len(s.outputMsg) == 0 {
		return fmt.Errorf("no output messages")
	}
	if s.outputMsg[0].Content != s.inputContent {
		return fmt.Errorf("output differs from original")
	}
	return nil
}

// Token truncation legacy steps

func (s *gatewayScenario) taskLogWithChars(_ context.Context, n int) error {
	s.inputContent = strings.Repeat("L", n)
	return nil
}

func (s *gatewayScenario) maxTokensLimit(_ context.Context, budget int) error {
	s.budget = budget
	return nil
}

func (s *gatewayScenario) applyMiddleOut(context.Context) error {
	result := MiddleOut(s.inputContent, s.budget)
	s.outputMsg = []PromptMessage{{Role: "user", Content: result}}
	return nil
}

func (s *gatewayScenario) textTruncMarkerInMiddle(context.Context) error {
	if len(s.outputMsg) == 0 {
		return fmt.Errorf("no output messages")
	}
	content := s.outputMsg[0].Content
	idx := strings.Index(content, TruncationMarker)
	if idx < 0 {
		return fmt.Errorf("output missing truncation marker %q", TruncationMarker)
	}
	if idx == 0 || idx+len(TruncationMarker) >= len(content) {
		return fmt.Errorf("truncation marker should appear between head and tail segments")
	}
	return nil
}

func (s *gatewayScenario) outputContainsDelimiter(_ context.Context, delim string) error {
	if len(s.outputMsg) == 0 {
		return fmt.Errorf("no output messages")
	}
	if !strings.Contains(s.outputMsg[0].Content, delim) {
		return fmt.Errorf("output missing delimiter %q", delim)
	}
	return nil
}
