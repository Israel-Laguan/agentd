package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agentd/internal/models"

	"github.com/cucumber/godog"
)

const (
	malformedTrailingCommaJSON = `{"key": "value", }`
	minimalValidJSON           = `{"key": "value"}`
)

func registerJSONSyntaxSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^an AIRequest specifies JSONMode: true$`, state.jsonSyntaxRequestSetup)
	sc.Step(`^the mock LLM returns JSON with a trailing comma on the first attempt$`, state.jsonSyntaxInvalidFirst)
	sc.Step(`^the mock LLM returns minimal valid JSON on the second attempt$`, state.jsonSyntaxValidSecond)
	sc.Step(`^the mock LLM returns JSON with a trailing comma on three consecutive attempts$`, state.jsonSyntaxInvalidThreeTimes)
	sc.Step(`^the Gateway processes the request$`, state.jsonSyntaxProcess)
	sc.Step(`^the Gateway should detect the JSON syntax error internally$`, state.jsonSyntaxDetectsError)
	sc.Step(`^the Gateway should automatically submit a retry prompt asking the LLM to fix the error$`, state.jsonSyntaxRetryPrompt)
	sc.Step(`^the final AIResponse should contain the valid JSON string from the second attempt$`, state.jsonSyntaxFinalResponseValid)
	sc.Step(`^the calling Go function should receive no errors$`, state.jsonSyntaxNoError)
	sc.Step(`^the Gateway should stop retrying after the maximum retry limit$`, state.jsonSyntaxStopsRetrying)
	sc.Step(`^the Gateway should return a specific ErrInvalidJSONResponse$`, state.jsonSyntaxErrInvalidJSON)
	sc.Step(`^the raw, broken output should be included in the error details$`, state.jsonSyntaxRawInError)
}

func (s *gatewayScenario) jsonSyntaxRequestSetup(context.Context) error {
	s.jsonSeq = &sequenceProvider{}
	return nil
}

func (s *gatewayScenario) jsonSyntaxInvalidFirst(context.Context) error {
	s.jsonSeq.values = append(s.jsonSeq.values, malformedTrailingCommaJSON)
	return nil
}

func (s *gatewayScenario) jsonSyntaxValidSecond(context.Context) error {
	s.jsonSeq.values = append(s.jsonSeq.values, minimalValidJSON)
	return nil
}

func (s *gatewayScenario) jsonSyntaxInvalidThreeTimes(context.Context) error {
	s.jsonSeq = &sequenceProvider{
		values: []string{malformedTrailingCommaJSON, malformedTrailingCommaJSON, malformedTrailingCommaJSON},
	}
	return nil
}

func (s *gatewayScenario) jsonSyntaxProcess(context.Context) error {
	s.router = NewRouter(s.jsonSeq)
	var err error
	s.aiResp, err = s.router.Generate(context.Background(), AIRequest{
		JSONMode: true,
		Messages: []PromptMessage{{Role: "user", Content: "json please"}},
	})
	s.aiErr = err
	return nil
}

func (s *gatewayScenario) jsonSyntaxDetectsError(context.Context) error {
	if len(s.jsonSeq.requests) < 2 {
		return fmt.Errorf("want at least 2 provider calls for JSON retry, got %d", len(s.jsonSeq.requests))
	}
	return nil
}

func (s *gatewayScenario) jsonSyntaxRetryPrompt(context.Context) error {
	if len(s.jsonSeq.requests) < 2 {
		return fmt.Errorf("want retry request")
	}
	req := s.jsonSeq.requests[len(s.jsonSeq.requests)-1]
	if len(req.Messages) < 2 {
		return fmt.Errorf("expected retry prompt messages, got %d", len(req.Messages))
	}
	if !strings.Contains(req.Messages[1].Content, "invalid JSON") {
		return fmt.Errorf("retry prompt should mention invalid JSON, got %q", req.Messages[1].Content)
	}
	return nil
}

func (s *gatewayScenario) jsonSyntaxFinalResponseValid(context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("unexpected error: %v", s.aiErr)
	}
	if s.aiResp.Content != minimalValidJSON {
		return fmt.Errorf("Content = %q, want %q", s.aiResp.Content, minimalValidJSON)
	}
	return nil
}

func (s *gatewayScenario) jsonSyntaxNoError(context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("expected nil error, got %v", s.aiErr)
	}
	return nil
}

func (s *gatewayScenario) jsonSyntaxStopsRetrying(context.Context) error {
	if len(s.jsonSeq.requests) != 3 {
		return fmt.Errorf("want exactly 3 attempts, got %d", len(s.jsonSeq.requests))
	}
	return nil
}

func (s *gatewayScenario) jsonSyntaxErrInvalidJSON(context.Context) error {
	if !errors.Is(s.aiErr, models.ErrInvalidJSONResponse) {
		return fmt.Errorf("error = %v, want ErrInvalidJSONResponse", s.aiErr)
	}
	return nil
}

func (s *gatewayScenario) jsonSyntaxRawInError(context.Context) error {
	if s.aiErr == nil {
		return fmt.Errorf("expected error")
	}
	if !strings.Contains(s.aiErr.Error(), malformedTrailingCommaJSON) {
		return fmt.Errorf("error %q should contain raw output %q", s.aiErr.Error(), malformedTrailingCommaJSON)
	}
	return nil
}
