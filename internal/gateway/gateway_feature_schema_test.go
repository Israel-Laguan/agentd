package gateway

import (
	"context"
	"errors"
	"fmt"

	"agentd/internal/models"
)

func (s *gatewayScenario) schemaGWInvalidFirst(context.Context) error {
	if s.schemaGW == nil {
		s.schemaGW = &sequenceGateway{}
	}
	s.schemaGW.values = append(s.schemaGW.values, `{"name":"","tasks":[]}`)
	return nil
}

func (s *gatewayScenario) schemaGWValidSecond(context.Context) error {
	if s.schemaGW == nil {
		s.schemaGW = &sequenceGateway{}
	}
	s.schemaGW.values = append(s.schemaGW.values, `{"name":"project","tasks":[{"title":"t1"}]}`)
	return nil
}

func (s *gatewayScenario) schemaGWInvalidAll(context.Context) error {
	s.schemaGW = &sequenceGateway{values: []string{
		`{"name":"","tasks":[]}`,
		`{"name":"","tasks":[]}`,
		`{"name":"","tasks":[]}`,
	}}
	return nil
}

func (s *gatewayScenario) schemaGWBrokenFirst(context.Context) error {
	if s.schemaGW == nil {
		s.schemaGW = &sequenceGateway{}
	}
	s.schemaGW.values = append([]string{`{"name":"project"`}, s.schemaGW.values...)
	return nil
}

func (s *gatewayScenario) schemaGWInvalidSecond(context.Context) error {
	if s.schemaGW == nil {
		s.schemaGW = &sequenceGateway{}
	}
	s.schemaGW.values = append(s.schemaGW.values, `{"name":"","tasks":[]}`)
	return nil
}

func (s *gatewayScenario) schemaGWValidThird(context.Context) error {
	if s.schemaGW == nil {
		s.schemaGW = &sequenceGateway{}
	}
	s.schemaGW.values = append(s.schemaGW.values, `{"name":"done","tasks":[{"title":"ok"}]}`)
	return nil
}

func (s *gatewayScenario) callGenerateJSONValidatable(context.Context) error {
	result, err := GenerateJSON[validatableStruct](context.Background(), s.schemaGW, sampleAIRequest())
	s.schemaResult = &result
	s.aiErr = err
	s.schemaGWCallCount = len(s.schemaGW.requests)
	return nil
}

func (s *gatewayScenario) resultHasCorrectedValues(context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("error = %v", s.aiErr)
	}
	if s.schemaResult == nil || s.schemaResult.Name == "" {
		return fmt.Errorf("result Name is empty")
	}
	if len(s.schemaResult.Tasks) == 0 {
		return fmt.Errorf("result Tasks is empty")
	}
	return nil
}

func (s *gatewayScenario) gatewayCalledNTimes(_ context.Context, n int) error {
	if s.schemaGWCallCount != n {
		return fmt.Errorf("gateway calls = %d, want %d", s.schemaGWCallCount, n)
	}
	return nil
}

func (s *gatewayScenario) errorIsInvalidJSON(context.Context) error {
	if !errors.Is(s.aiErr, models.ErrInvalidJSONResponse) {
		return fmt.Errorf("error = %v, want ErrInvalidJSONResponse", s.aiErr)
	}
	return nil
}
