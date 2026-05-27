package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/cucumber/godog"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
)

func registerToolSteps(sc *godog.ScenarioContext, state *gatewayScenario) {
	sc.Step(`^a mock provider "([^"]*)" using the openai wire protocol$`, state.toolMockProvider)
	sc.Step(`^a mock provider "([^"]*)" using the openai wire protocol that returns tool_calls$`, state.toolMockProviderWithToolCalls)
	sc.Step(`^a mock provider "([^"]*)" using the openai wire protocol that returns null content with tool_calls$`, state.toolMockProviderWithNullContentAndToolCalls)
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

func (s *gatewayScenario) toolMockProvider(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: name,
		resp: AIResponse{
			Content:      `{"result":"ok"}`,
			ProviderUsed: name,
		},
	})
	return nil
}

func (s *gatewayScenario) toolGenerateWithJSONModeAndTools(_ context.Context) error {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.lastHTTPBody = reqBody
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "gpt-test",
			"choices": []map[string]any{{
				"message": PromptMessage{Role: "assistant", Content: `{"result":"ok"}`},
			}},
			"usage": map[string]int{"total_tokens": 4},
		})
	}))
	defer srv.Close()

	openai := NewOpenAI(ProviderConfig{BaseURL: srv.URL + "/v1", Model: "gpt-test"}, srv.Client())
	s.router = NewRouter(openai)
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

func (s *gatewayScenario) toolMockProviderWithToolCalls(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: name,
		resp: AIResponse{
			Content:      "",
			ProviderUsed: name,
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

func (s *gatewayScenario) toolMockProviderWithNullContentAndToolCalls(_ context.Context, name string) error {
	s.providers = append(s.providers, &fakeProvider{
		providerName: name,
		resp: AIResponse{
			Content:      "",
			ProviderUsed: name,
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
		if p.providerName == "synth-openai" {
			ok = true
			break
		}
	}
	if !ok {
		s.providers = append(s.providers, &fakeProvider{providerName: "synth-openai"})
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
	if s.lastHTTPBody != nil {
		tools, ok := s.lastHTTPBody["tools"].([]any)
		if !ok || len(tools) == 0 {
			return fmt.Errorf("tools not present in downstream HTTP request")
		}
		return nil
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
	if s.lastHTTPBody != nil {
		if _, ok := s.lastHTTPBody["response_format"]; ok {
			return fmt.Errorf("expected response_format to be omitted")
		}
		tools, ok := s.lastHTTPBody["tools"].([]any)
		if !ok || len(tools) == 0 {
			return fmt.Errorf("expected tools in downstream HTTP request")
		}
		return nil
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
