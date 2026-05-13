package gateway

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"

	"github.com/cucumber/godog"
)

type gatewayScenario struct {
	providers    []*fakeProvider
	router       *Router
	aiResp       AIResponse
	aiErr        error
	inputContent string
	outputMsg    []PromptMessage
	truncErr     error
	strategy     TruncationStrategy
	truncator    Truncator
	budget       int
	headRatio    float64

	// Phase cap fields
	phaseCap     int
	llmTaskCount int
	plan         *models.DraftPlan

	// Budget enforcement fields
	budgetTracker *InMemoryBudgetTracker
	budgetTaskID  string
	callsBefore   int

	// Role routing fields
	roleRoutes map[Role]RoleTarget

	// Schema validation fields
	schemaGW          *sequenceGateway
	schemaResult      *validatableStruct
	schemaGWCallCount int

	// Provider cancellation fields
	providerResp    AIResponse
	providerErr     error
	providerContent string

	// JSON syntax self-correction (Router.Generate JSONMode)
	jsonSeq *sequenceProvider

	// Tool definition fields
	toolReq AIRequest
}

func initializeGatewayScenario(sc *godog.ScenarioContext) {
	state := &gatewayScenario{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		*state = gatewayScenario{}
		return ctx, nil
	})
	registerResilienceSteps(sc, state)
	registerPhaseCapSteps(sc, state)
	registerTruncationSteps(sc, state)
	registerBudgetSteps(sc, state)
	registerFallbackAndRoutingSteps(sc, state)
	registerSchemaAndCancellationSteps(sc, state)
	registerJSONSyntaxSteps(sc, state)
	registerToolSteps(sc, state)
}

func registerResilienceSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^the primary provider "([^"]*)" is configured with an invalid API key$`, state.primaryProviderInvalid)
	sc.Step(`^the secondary provider "([^"]*)" is configured and running locally$`, state.secondaryProviderOK)
	sc.Step(`^a component requests an AIRequest generation from the Gateway$`, state.generateRequest)
	sc.Step(`^the Gateway should attempt the "([^"]*)" provider$`, state.providerAttempted)
	sc.Step(`^upon receiving a 401/5xx error, it should catch the error$`, noopGW)
	sc.Step(`^the Gateway should successfully route the request to "([^"]*)"$`, state.providerAttempted)
	sc.Step(`^the final AIResponse should indicate ProviderUsed: "([^"]*)"$`, state.providerUsed)
	sc.Step(`^the primary provider "([^"]*)" returns an error$`, state.providerFails)
	sc.Step(`^the secondary provider "([^"]*)" returns an error$`, state.providerFails)
	sc.Step(`^the tertiary provider "([^"]*)" is configured and available$`, state.tertiaryProviderOK)
	sc.Step(`^the tertiary provider "([^"]*)" returns an error$`, state.providerFails)
	sc.Step(`^the Gateway should cascade through all three providers$`, state.allProvidersAttempted)
	sc.Step(`^the Gateway should return an error mentioning all failed providers$`, state.errorMentionsAllProviders)
}

func registerPhaseCapSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^the gateway max_tasks_per_phase is (\d+)$`, state.setPhaseCap)
	sc.Step(`^the LLM returns a DraftPlan with (\d+) tasks$`, state.llmReturnsDraftPlan)
	sc.Step(`^GeneratePlan processes the intent "([^"]*)"$`, state.generatePlan)
	sc.Step(`^the returned DraftPlan should contain exactly (\d+) tasks$`, state.planShouldHaveTaskCount)
	sc.Step(`^the first (\d+) tasks should match the original order$`, state.firstNTasksMatchOriginalOrder)
	sc.Step(`^the (\d+)(?:st|nd|rd|th) task should be titled "([^"]*)"$`, state.nthTaskTitled)
	sc.Step(`^the continuation task description should reference the remaining work$`, state.continuationReferencesRemaining)
	sc.Step(`^no task should be titled "([^"]*)"$`, state.noTaskTitled)
}

func registerTruncationSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^a message with (\d+) characters of content$`, state.messageWithChars)
	sc.Step(`^the truncation strategy is "([^"]*)"$`, state.setStrategy)
	sc.Step(`^the truncation strategy is "([^"]*)" with head ratio ([0-9.]+)$`, state.setStrategyWithRatio)
	sc.Step(`^the truncation policy is "([^"]*)"$`, state.setPolicy)
	sc.Step(`^truncation is applied with a budget of (\d+) characters$`, state.applyTruncation)
	sc.Step(`^the output should be at most (\d+) characters$`, state.outputAtMost)
	sc.Step(`^the output should start with the first portion of the original$`, state.outputStartsWithHead)
	sc.Step(`^the output should end with the last portion of the original$`, state.outputEndsWithTail)
	sc.Step(`^the output should contain the truncation marker$`, state.outputContainsMarker)
	sc.Step(`^the head portion should be larger than the tail portion$`, state.headLargerThanTail)
	sc.Step(`^the truncation should return ErrContextBudgetExceeded$`, state.truncationRejected)
	sc.Step(`^the output should equal the original content$`, state.outputEqualsOriginal)

	// Token truncation (legacy scenario)
	sc.Step(`^a task execution log is (\d+) characters long$`, state.taskLogWithChars)
	sc.Step(`^the AI Gateway has a configured MaxTokens limit equivalent to (\d+) characters$`, state.maxTokensLimit)
	sc.Step(`^the text is passed to the Gateway's Truncation middleware$`, state.applyMiddleOut)
	sc.Step(`^the returned text should be strictly <= (\d+) characters$`, state.outputAtMost)
	sc.Step(`^the text should contain the first portion of the original log$`, state.outputStartsWithHead)
	sc.Step(`^the text should contain the last portion of the original log$`, state.outputEndsWithTail)
	sc.Step(`^the text should contain the truncation marker in the middle$`, state.textTruncMarkerInMiddle)
}

func registerBudgetSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^a budget tracker with a cap of (\d+) tokens$`, state.budgetTrackerWithCap)
	sc.Step(`^a mock provider that reports (\d+) tokens per call$`, state.mockProviderWithTokens)
	sc.Step(`^a router wired with the budget tracker$`, state.routerWithBudget)
	sc.Step(`^a request is sent for task "([^"]*)"$`, state.sendRequestForTask)
	sc.Step(`^a second request consuming (\d+) tokens is sent for task "([^"]*)"$`, state.sendSecondRequestForTask)
	sc.Step(`^a third request is sent for task "([^"]*)"$`, state.sendThirdRequestForTask)
	sc.Step(`^a request is sent without a task ID$`, state.sendRequestNoTaskID)
	sc.Step(`^the request should succeed$`, state.requestShouldSucceed)
	sc.Step(`^the request should fail with ErrBudgetExceeded$`, state.requestFailsBudgetExceeded)
	sc.Step(`^the recorded usage for "([^"]*)" should be (\d+)$`, state.usageShouldBe)
	sc.Step(`^the provider should not have been called for the third request$`, state.providerNotCalledForThird)
}

func registerFallbackAndRoutingSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^the provider "([^"]*)" returns an error$`, state.providerFails)
	sc.Step(`^the provider "([^"]*)" is configured and available$`, state.providerAvailable)
	sc.Step(`^the router processes a request$`, state.routerProcessesRequest)
	sc.Step(`^the response should indicate ProviderUsed: "([^"]*)"$`, state.providerUsed)
	sc.Step(`^the error should wrap ErrLLMUnreachable$`, state.errorWrapsUnreachable)
	sc.Step(`^the error should mention all four provider names$`, state.errorMentionsFourProviders)

	// Specialized model routing steps
	sc.Step(`^role routes map (\w+) to provider "([^"]*)" with model "([^"]*)"$`, state.setRoleRoute)
	sc.Step(`^three providers "([^"]*)", "([^"]*)", "([^"]*)" are configured$`, state.threeProvidersConfigured)
	sc.Step(`^two providers "([^"]*)", "([^"]*)" are configured$`, state.twoProvidersConfigured)
	sc.Step(`^a request with role "([^"]*)" is sent$`, state.sendRequestWithRole)
	sc.Step(`^a request with role "([^"]*)" and explicit provider "([^"]*)" is sent$`, state.sendRequestWithRoleAndProvider)
}

func registerSchemaAndCancellationSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^a mock gateway returns invalid-schema JSON on the first call$`, state.schemaGWInvalidFirst)
	sc.Step(`^the mock gateway returns valid-schema JSON on the second call$`, state.schemaGWValidSecond)
	sc.Step(`^a mock gateway returns invalid-schema JSON on all 3 calls$`, state.schemaGWInvalidAll)
	sc.Step(`^a mock gateway returns broken JSON on the first call$`, state.schemaGWBrokenFirst)
	sc.Step(`^the mock gateway returns invalid-schema JSON on the second call$`, state.schemaGWInvalidSecond)
	sc.Step(`^the mock gateway returns valid-schema JSON on the third call$`, state.schemaGWValidThird)
	sc.Step(`^GenerateJSON is called with a validatable type$`, state.callGenerateJSONValidatable)
	sc.Step(`^the result should contain the corrected values$`, state.resultHasCorrectedValues)
	sc.Step(`^the gateway should have been called exactly (\d+) times$`, state.gatewayCalledNTimes)
	sc.Step(`^the error should be ErrInvalidJSONResponse$`, state.errorIsInvalidJSON)

	// Provider cancellation steps
	sc.Step(`^an OpenAI provider configured with a (\d+)ms timeout$`, state.openAIWithTimeout)
	sc.Step(`^an Ollama provider configured with a (\d+)ms timeout$`, state.ollamaWithTimeout)
	sc.Step(`^the mock server delays responses by (\d+)ms$`, state.mockServerDelays)
	sc.Step(`^the mock server responds immediately$`, state.mockServerImmediate)
	sc.Step(`^a request is sent to the OpenAI provider$`, state.sendToOpenAI)
	sc.Step(`^a request is sent to the Ollama provider$`, state.sendToOllama)
	sc.Step(`^the request should fail with ErrLLMUnreachable$`, state.requestFailsUnreachable)
	sc.Step(`^the request should succeed with content "([^"]*)"$`, state.requestSucceedsWithContent)
}

func noopGW(context.Context) error { return nil }

// Resilience steps

func (s *gatewayScenario) primaryProviderInvalid(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		err:          fmt.Errorf("provider rejected request: status 401"),
	})
	return nil
}

func (s *gatewayScenario) secondaryProviderOK(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		resp:         AIResponse{Content: "local", ProviderUsed: strings.ToLower(name)},
	})
	return nil
}

func (s *gatewayScenario) tertiaryProviderOK(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		resp:         AIResponse{Content: "crowd", ProviderUsed: strings.ToLower(name)},
	})
	return nil
}

func (s *gatewayScenario) providerFails(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: strings.ToLower(name),
		err:          fmt.Errorf("%s down", strings.ToLower(name)),
	})
	return nil
}

func (s *gatewayScenario) generateRequest(context.Context) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...)
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "generate"}},
	})
	return nil
}

func (s *gatewayScenario) providerAttempted(_ context.Context, name string) error {
	target := strings.ToLower(name)
	for _, p := range s.providers {
		if p.providerName == target && p.calls > 0 {
			return nil
		}
	}
	return fmt.Errorf("provider %q was not attempted", name)
}

func (s *gatewayScenario) providerUsed(_ context.Context, want string) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	if s.aiResp.ProviderUsed != want {
		return fmt.Errorf("ProviderUsed = %q, want %q", s.aiResp.ProviderUsed, want)
	}
	return nil
}

func (s *gatewayScenario) allProvidersAttempted(context.Context) error {
	for _, p := range s.providers {
		if p.calls == 0 {
			return fmt.Errorf("provider %q was not attempted", p.providerName)
		}
	}
	return nil
}

func (s *gatewayScenario) errorMentionsAllProviders(context.Context) error {
	if s.aiErr == nil {
		return fmt.Errorf("Generate() error = nil, want error")
	}
	for _, p := range s.providers {
		if !strings.Contains(s.aiErr.Error(), p.providerName) {
			return fmt.Errorf("error %q missing provider %q", s.aiErr.Error(), p.providerName)
		}
	}
	return nil
}

// Truncation strategy steps

func registerToolSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^a mock OpenAI provider$`, state.toolMockProvider)
	sc.Step(`^a mock OpenAI provider that returns tool_calls$`, state.toolMockProviderWithToolCalls)
	sc.Step(`^a mock OpenAI provider that returns null content with tool_calls$`, state.toolMockProviderWithNullContentAndToolCalls)
	sc.Step(`^Generate is called with a tool definition that has no parameters$`, state.toolGenerateWithNoParams)
	sc.Step(`^the request should include the tool with parameters field present$`, state.toolReqHasParameters)
	sc.Step(`^the parameters should be a valid JSON Schema object$`, state.toolParamsIsEmptyObject)
	sc.Step(`^Generate is called with a request containing tool definitions$`, state.toolGenerateWithTools)
	sc.Step(`^the request should include tools serialized in OpenAI format$`, state.toolReqHasTools)
	sc.Step(`^the response should contain the tool_calls from the model$`, state.toolRespHasToolCalls)
	sc.Step(`^the response should contain the tool_calls$`, state.toolRespHasToolCalls)
	sc.Step(`^the content should be empty$`, state.toolContentEmpty)
	sc.Step(`^Generate is called with JSONMode enabled and tools present$`, state.toolGenerateWithJSONModeAndTools)
	sc.Step(`^the request should not include response_format$`, state.toolReqNoResponseFormat)
	sc.Step(`^the request should include the tools$`, state.toolReqHasTools)
}

func (s *gatewayScenario) toolMockProvider(_ context.Context) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: "openai",
		resp: AIResponse{
			Content:      `{"result":"ok"}`,
			ProviderUsed: "openai",
		},
	})
	return nil
}

func (s *gatewayScenario) toolGenerateWithJSONModeAndTools(_ context.Context) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...)
	s.toolReq = AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		JSONMode: true,
		Tools: []ToolDefinition{{
			Name:        "test_func",
			Description: "A test function",
		}},
	}
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), s.toolReq)
	return nil
}

func (s *gatewayScenario) toolMockProviderWithToolCalls(_ context.Context) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: "openai",
		resp: AIResponse{
			Content:      "",
			ProviderUsed: "openai",
			ToolCalls: []spec.ToolCall{{
				ID:   "call_123",
				Type: "function",
				Function: spec.ToolCallFunction{
					Name:      "test_func",
					Arguments: `{"arg": "value"}`,
				},
			}},
		},
	})
	return nil
}

func (s *gatewayScenario) toolMockProviderWithNullContentAndToolCalls(_ context.Context) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: "openai",
		resp: AIResponse{
			Content:      "",
			ProviderUsed: "openai",
			ToolCalls: []spec.ToolCall{{
				ID:   "call_456",
				Type: "function",
				Function: spec.ToolCallFunction{
					Name:      "ping",
					Arguments: `{}`,
				},
			}},
		},
	})
	return nil
}

func (s *gatewayScenario) toolGenerateWithNoParams(_ context.Context) error {
	var ok bool
	for _, p := range s.providers {
		if p.providerName == "openai" {
			ok = true
			break
		}
	}
	if !ok {
		s.providers = append(s.providers, &fakeProvider{providerName: "openai"})
	}

	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...)
	s.toolReq = AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		Tools: []ToolDefinition{{
			Name:        "ping",
			Description: "Ping the service",
			Parameters:  &FunctionParameters{Type: "object", Properties: map[string]any{}, Required: []string{}},
		}},
	}
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), s.toolReq)
	return nil
}

func (s *gatewayScenario) toolReqHasParameters(_ context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	var provider *fakeProvider
	for _, p := range s.providers {
		if p.providerName == s.aiResp.ProviderUsed {
			provider = p
			break
		}
	}
	if provider == nil {
		return fmt.Errorf("provider %q not found", s.aiResp.ProviderUsed)
	}
	if len(provider.lastRequest.Tools) == 0 || provider.lastRequest.Tools[0].Parameters == nil {
		return fmt.Errorf("tool parameters not present in downstream request")
	}
	return nil
}

func (s *gatewayScenario) toolParamsIsEmptyObject(_ context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	var provider *fakeProvider
	for _, p := range s.providers {
		if p.providerName == s.aiResp.ProviderUsed {
			provider = p
			break
		}
	}
	if provider == nil {
		return fmt.Errorf("provider %q not found", s.aiResp.ProviderUsed)
	}
	if len(provider.lastRequest.Tools) == 0 || provider.lastRequest.Tools[0].Parameters == nil {
		return fmt.Errorf("tool parameters not present in downstream request")
	}
	params := provider.lastRequest.Tools[0].Parameters
	if params.Type != "object" {
		return fmt.Errorf("parameters.type = %q, want object", params.Type)
	}
	if len(params.Properties) != 0 {
		return fmt.Errorf("parameters.properties = %v, want empty", params.Properties)
	}
	if len(params.Required) != 0 {
		return fmt.Errorf("parameters.required = %v, want empty", params.Required)
	}
	return nil
}

func (s *gatewayScenario) toolGenerateWithTools(_ context.Context) error {
	provs := make([]providers.Backend, len(s.providers))
	for i, p := range s.providers {
		provs[i] = p
	}
	s.router = NewRouter(provs...)
	s.toolReq = AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "test"}},
		Tools: []ToolDefinition{{
			Name:        "test_func",
			Description: "A test function",
			Parameters: &FunctionParameters{
				Type:       "object",
				Properties: map[string]any{"arg": map[string]any{"type": "string"}},
				Required:   []string{"arg"},
			},
		}},
	}
	s.aiResp, s.aiErr = s.router.Generate(context.Background(), s.toolReq)
	return nil
}

func (s *gatewayScenario) toolReqHasTools(_ context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	var provider *fakeProvider
	for _, p := range s.providers {
		if p.providerName == s.aiResp.ProviderUsed {
			provider = p
			break
		}
	}
	if provider == nil {
		return fmt.Errorf("provider %q not found", s.aiResp.ProviderUsed)
	}
	if len(provider.lastRequest.Tools) == 0 {
		return fmt.Errorf("tools not present in downstream request")
	}
	return nil
}

func (s *gatewayScenario) toolRespHasToolCalls(_ context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	if len(s.aiResp.ToolCalls) == 0 {
		return fmt.Errorf("tool_calls not present in response")
	}
	return nil
}

func (s *gatewayScenario) toolContentEmpty(_ context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	if s.aiResp.Content != "" {
		return fmt.Errorf("content = %q, want empty", s.aiResp.Content)
	}
	return nil
}

func (s *gatewayScenario) toolReqNoResponseFormat(_ context.Context) error {
	if s.aiErr != nil {
		return fmt.Errorf("Generate() error = %v", s.aiErr)
	}
	var provider *fakeProvider
	for _, p := range s.providers {
		if p.providerName == s.aiResp.ProviderUsed {
			provider = p
			break
		}
	}
	if provider == nil {
		return fmt.Errorf("provider %q not found", s.aiResp.ProviderUsed)
	}
	if len(provider.lastRequest.Tools) == 0 {
		return fmt.Errorf("expected tools in downstream request")
	}
	if !provider.lastRequest.JSONMode {
		return fmt.Errorf("expected JSONMode=true in downstream request")
	}
	return nil
}
