package gateway

import (
	"context"
	"errors"
	"fmt"

	"agentd/internal/gateway/providers"
	"agentd/internal/models"
)

func (s *gatewayScenario) budgetTrackerWithCap(_ context.Context, cap int) error {
	s.budgetTracker = NewBudgetTracker(cap)
	return nil
}

func (s *gatewayScenario) mockProviderWithTokens(_ context.Context, tokens int) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: "mock",
		resp:         AIResponse{Content: "ok", ProviderUsed: "mock", TokenUsage: tokens},
	})
	return nil
}

func (s *gatewayScenario) routerWithBudget(context.Context) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...).WithBudget(s.budgetTracker)
	return nil
}

func (s *gatewayScenario) sendRequestForTask(_ context.Context, taskID string) error {
	s.budgetTaskID = taskID
	s.callsBefore = s.providers[0].calls
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		TaskID:   taskID,
	})
	return nil
}

func (s *gatewayScenario) sendSecondRequestForTask(_ context.Context, tokens int, taskID string) error {
	s.providers[0].resp.TokenUsage = tokens
	s.callsBefore = s.providers[0].calls
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "second"}},
		TaskID:   taskID,
	})
	return nil
}

func (s *gatewayScenario) sendThirdRequestForTask(_ context.Context, taskID string) error {
	s.callsBefore = s.providers[0].calls
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "third"}},
		TaskID:   taskID,
	})
	return nil
}

func (s *gatewayScenario) sendRequestNoTaskID(context.Context) error {
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "no task"}},
	})
	return nil
}

func (s *gatewayScenario) requestShouldSucceed(context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("request failed: %v", s.aiErr)
	}
	return nil
}

func (s *gatewayScenario) requestFailsBudgetExceeded(context.Context) error {
	if !errors.Is(s.aiErr, models.ErrBudgetExceeded) {
		return fmt.Errorf("error = %v, want ErrBudgetExceeded", s.aiErr)
	}
	return nil
}

func (s *gatewayScenario) usageShouldBe(_ context.Context, taskID string, want int) error {
	got := s.budgetTracker.Usage(taskID)
	if got != want {
		return fmt.Errorf("usage(%s) = %d, want %d", taskID, got, want)
	}
	return nil
}

func (s *gatewayScenario) providerNotCalledForThird(context.Context) error {
	if s.providers[0].calls != s.callsBefore {
		return fmt.Errorf("provider was called %d times (expected no call for third request)", s.providers[0].calls-s.callsBefore)
	}
	return nil
}
