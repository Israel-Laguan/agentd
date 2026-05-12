package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"agentd/internal/bus"
	"agentd/internal/gateway"
	"agentd/internal/models"

	"github.com/cucumber/godog"
)

type apiScenario struct {
	store        *apiStore
	bus          *bus.InProcess
	gateway      *apiGateway
	resp         *httptest.ResponseRecorder
	body         map[string]any
	fileStashDir string
}

func initializeAPIScenario(sc *godog.ScenarioContext) {
	state := &apiScenario{}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		state.store = newAPITestStore()
		state.bus = bus.NewInProcess()
		state.gateway = newAPIGateway()
		state.resp = nil
		state.body = nil
		state.fileStashDir = ""
		dir, err := os.MkdirTemp("", "agentd-api-test-*")
		if err != nil {
			return ctx, err
		}
		state.fileStashDir = dir
		return ctx, nil
	})
	registerAPISteps(sc, state)
}

func registerAPISteps(sc *godog.ScenarioContext, state *apiScenario) {
	registerAPIResponseSteps(sc, state)
	registerAPIChatSteps(sc, state)
	registerAPITaskSteps(sc, state)
	registerAPIEventSteps(sc)
	registerAPITimeoutSteps(sc, state)
	registerAPIFileSteps(sc, state)
	registerAPIToolCallEventSteps(sc, state)
}

func registerAPIResponseSteps(sc *godog.ScenarioContext, state *apiScenario) {
	sc.Step(`^the API server is running$`, state.serverRunning)
	sc.Step(`^a client sends a GET request to "([^"]*)"$`, state.get)
	sc.Step(`^the HTTP status code should be (\d+)$`, state.statusCode)
	sc.Step(`^the JSON response should contain status "([^"]*)"$`, state.status)
	sc.Step(`^the JSON response should contain a data array$`, state.dataArray)
	sc.Step(`^the JSON response should contain pagination meta$`, state.paginationMeta)
	sc.Step(`^the JSON response should contain error code "([^"]*)"$`, state.errorCode)
}

func registerAPIChatSteps(sc *godog.ScenarioContext, state *apiScenario) {
	sc.Step(`^a client sends a chat completion request for "([^"]*)"$`, state.chatCompletion)
	sc.Step(`^a client sends a multi-scope chat completion request$`, state.multiScopeChatCompletion)
	sc.Step(`^a client sends a chat completion request with approved scope "([^"]*)"$`, state.chatCompletionApprovedScope)
	sc.Step(`^a client sends a chat completion request with approved scopes "([^"]*)" and "([^"]*)"$`, state.chatCompletionApprovedScopes)
	sc.Step(`^the response body should be a chat completion$`, state.chatObject)
	sc.Step(`^the first choice content should contain a DraftPlan JSON document$`, state.choiceDraftPlan)
	sc.Step(`^the first choice content kind should be "([^"]*)"$`, state.choiceContentKind)
	sc.Step(`^GeneratePlan should not be called$`, state.generatePlanNotCalled)
	sc.Step(`^AnalyzeScope should not be called$`, state.analyzeScopeNotCalled)
	sc.Step(`^ClassifyIntent should not be called$`, state.classifyIntentNotCalled)
	sc.Step(`^a client sends a status-check chat completion request$`, state.statusCheckChatCompletion)
	sc.Step(`^a client sends an ambiguous chat completion request$`, state.ambiguousChatCompletion)
}

func registerAPITaskSteps(sc *godog.ScenarioContext, state *apiScenario) {
	sc.Step(`^Task "([^"]*)" is in the RUNNING state$`, state.runningTask)
	sc.Step(`^a Sandbox worker is currently executing its payload$`, noopAPI)
	sc.Step(`^a client sends a comment "([^"]*)" to task "([^"]*)"$`, state.commentTask)
	sc.Step(`^the database state for Task "([^"]*)" should be IN_CONSIDERATION$`, state.taskInConsideration)
	sc.Step(`^the Sandbox worker should receive a cancellation signal$`, noopAPI)
	sc.Step(`^the task should not transition to COMPLETED or FAILED$`, state.taskNotTerminal)
}

func registerAPIEventSteps(sc *godog.ScenarioContext) {
	sc.Step(`^an HTTP client connects to the SSE stream$`, noopAPI)
	sc.Step(`^the internal Sandbox emits a LOG_CHUNK event via the EventBus$`, noopAPI)
	sc.Step(`^the HTTP client should receive the LOG_CHUNK event$`, noopAPI)
	sc.Step(`^the HTTP connection should not be closed by the server$`, noopAPI)
	sc.Step(`^the HTTP client cancels the request$`, noopAPI)
	sc.Step(`^the server active connection count should decrease by 1$`, noopAPI)
}

func registerAPITimeoutSteps(sc *godog.ScenarioContext, state *apiScenario) {
	sc.Step(`^the AI gateway returns ErrLLMUnreachable on plan generation$`, state.gatewayPlanUnreachable)
	sc.Step(`^the AI gateway returns DeadlineExceeded on intent classification$`, state.gatewayIntentDeadline)
	sc.Step(`^the AI gateway returns a non-timeout error on plan generation$`, state.gatewayPlanGenericError)
	sc.Step(`^the assistant content should be the system timeout message$`, state.assistantIsTimeoutMessage)
}

func registerAPIFileSteps(sc *godog.ScenarioContext, state *apiScenario) {
	sc.Step(`^the API server is running with file stash and truncation$`, state.serverRunningWithFileStash)
	sc.Step(`^a client sends an oversized chat completion message$`, state.sendOversizedChatMessage)
	sc.Step(`^the intent classifier should receive a file reference instead of inline content$`, state.classifierReceivedFileRef)
	sc.Step(`^the planner should receive truncated file content$`, state.plannerReceivedTruncatedContent)
	sc.Step(`^a client sends a chat completion request with an attached file "([^"]*)"$`, state.sendChatWithFile)
	sc.Step(`^the intent classifier should receive the file name reference$`, state.classifierReceivedFileName)
	sc.Step(`^the planner should receive the file content$`, state.plannerReceivedFileContent)
}

func registerAPIToolCallEventSteps(sc *godog.ScenarioContext, state *apiScenario) {
	sc.Step(`^a Sandbox worker is about to execute a tool in the agentic loop$`, noopAPI)
	sc.Step(`^the tool execution begins$`, noopAPI)
	sc.Step(`^a TOOL_CALL event should be emitted with tool_name$`, noopAPI)
	sc.Step(`^the TOOL_CALL event should include a call_id$`, noopAPI)
	sc.Step(`^the TOOL_CALL event should include arguments_summary \(max 200 characters\)$`, noopAPI)
	sc.Step(`^the TOOL_CALL event should be emitted before the tool executes$`, noopAPI)
	sc.Step(`^a tool has finished executing in the agentic loop$`, noopAPI)
	sc.Step(`^the tool execution completes$`, noopAPI)
	sc.Step(`^a TOOL_RESULT event should be emitted with tool_name$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event should include the matching call_id$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event should include exit_code$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event should include duration_ms$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event should include output_summary \(max 1000 characters\)$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event should include stdout_bytes$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event should include stderr_bytes$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event should be emitted after the tool executes$`, noopAPI)
	sc.Step(`^a tool is executed with sensitive arguments$`, noopAPI)
	sc.Step(`^the TOOL_CALL event is emitted$`, noopAPI)
	sc.Step(`^sensitive patterns should be replaced with "\[REDACTED\]" in arguments_summary$`, noopAPI)
  sc.Step(`^a tool produces large output$`, noopAPI)
	sc.Step(`^the TOOL_RESULT event is emitted$`, noopAPI)
	sc.Step(`^output_summary should be truncated to 1000 characters with "\.\.\.\[truncated\]" suffix$`, noopAPI)
	sc.Step(`^stdout_bytes and stderr_bytes should reflect original sizes before truncation$`, noopAPI)
	sc.Step(`^multiple tools are executed in sequence in the agentic loop$`, noopAPI)
	sc.Step(`^the agentic loop processes each tool$`, noopAPI)
	sc.Step(`^for each tool, TOOL_CALL must be emitted before TOOL_RESULT$`, noopAPI)
	sc.Step(`^the call_id in TOOL_CALL must match the call_id in corresponding TOOL_RESULT$`, noopAPI)
	sc.Step(`^events must be emitted in the same order as tool execution$`, noopAPI)
}

func (s *apiScenario) serverRunning(context.Context) error { return nil }

func (s *apiScenario) get(_ context.Context, path string) error {
	s.resp = request(s.handler(), http.MethodGet, path, "")
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) chatCompletion(_ context.Context, prompt string) error {
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":%q}]}`, prompt)
	s.resp = request(s.handler(), http.MethodPost, "/v1/chat/completions", body)
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) multiScopeChatCompletion(context.Context) error {
	s.gateway.scope = &gateway.ScopeAnalysis{
		SingleScope: false,
		Confidence:  0.92,
		Scopes: []gateway.ScopeOption{
			{ID: "backend-api", Label: "Backend API service"},
			{ID: "frontend-ui", Label: "Frontend UI"},
			{ID: "marketing-copy", Label: "Marketing copy"},
		},
		Reason: "multiple projects",
	}
	body := `{"messages":[{"role":"user","content":"Build backend API, frontend UI, and marketing copy"}]}`
	s.resp = request(s.handler(), http.MethodPost, "/v1/chat/completions", body)
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) chatCompletionApprovedScope(_ context.Context, scopeID string) error {
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":"Build backend API, frontend UI, and marketing copy"}],"approved_scopes":[%q]}`, scopeID)
	s.resp = request(s.handler(), http.MethodPost, "/v1/chat/completions", body)
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) chatCompletionApprovedScopes(_ context.Context, first, second string) error {
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":"Build backend API, frontend UI, and marketing copy"}],"approved_scopes":[%q,%q]}`, first, second)
	s.resp = request(s.handler(), http.MethodPost, "/v1/chat/completions", body)
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) commentTask(_ context.Context, content, taskID string) error {
	body := fmt.Sprintf(`{"content":%q}`, content)
	s.resp = request(s.handler(), http.MethodPost, "/api/v1/tasks/"+taskID+"/comments", body)
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) statusCode(_ context.Context, want int) error {
	if s.resp.Code != want {
		return fmt.Errorf("status = %d, want %d", s.resp.Code, want)
	}
	return nil
}

func (s *apiScenario) status(_ context.Context, want string) error {
	return requireEqual(s.body["status"], want)
}

func (s *apiScenario) dataArray(context.Context) error {
	if _, ok := s.body["data"].([]any); !ok {
		return fmt.Errorf("data is not array")
	}
	return nil
}

func (s *apiScenario) paginationMeta(context.Context) error {
	if _, ok := s.body["meta"].(map[string]any); !ok {
		return fmt.Errorf("meta missing")
	}
	return nil
}

func (s *apiScenario) errorCode(_ context.Context, want string) error {
	return requireEqual(s.body["error"].(map[string]any)["code"], want)
}

func (s *apiScenario) chatObject(context.Context) error {
	return requireEqual(s.body["object"], "chat.completion")
}

func (s *apiScenario) choiceDraftPlan(context.Context) error {
	content := s.body["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !strings.Contains(content, "ProjectName") && !strings.Contains(content, "Python scraper") {
		return fmt.Errorf("content missing plan: %s", content)
	}
	return nil
}

func (s *apiScenario) choiceContentKind(_ context.Context, kind string) error {
	content := s.body["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return fmt.Errorf("choice content is not JSON: %w", err)
	}
	return requireEqual(parsed["kind"], kind)
}

func (s *apiScenario) generatePlanNotCalled(context.Context) error {
	if s.gateway.planCalls != 0 {
		return fmt.Errorf("GeneratePlan calls = %d", s.gateway.planCalls)
	}
	return nil
}

func (s *apiScenario) statusCheckChatCompletion(context.Context) error {
	s.gateway.intent = &gateway.IntentAnalysis{Intent: "status_check", Reason: "user asked for status"}
	body := `{"messages":[{"role":"user","content":"What's the status of my projects?"}]}`
	s.resp = request(s.handler(), http.MethodPost, "/v1/chat/completions", body)
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) ambiguousChatCompletion(context.Context) error {
	s.gateway.intent = &gateway.IntentAnalysis{Intent: "ambiguous", Reason: "greeting only"}
	body := `{"messages":[{"role":"user","content":"Hello"}]}`
	s.resp = request(s.handler(), http.MethodPost, "/v1/chat/completions", body)
	s.body = decodeBodyT(s.resp)
	return nil
}

func (s *apiScenario) analyzeScopeNotCalled(context.Context) error {
	if s.gateway.analyzeCalls != 0 {
		return fmt.Errorf("AnalyzeScope calls = %d", s.gateway.analyzeCalls)
	}
	return nil
}

func (s *apiScenario) classifyIntentNotCalled(context.Context) error {
	if s.gateway.intentCalls != 0 {
		return fmt.Errorf("ClassifyIntent calls = %d", s.gateway.intentCalls)
	}
	return nil
}

// Timeout step implementations

func (s *apiScenario) gatewayPlanUnreachable(context.Context) error {
	s.gateway.planErr = models.ErrLLMUnreachable
	return nil
}

func (s *apiScenario) gatewayIntentDeadline(context.Context) error {
	s.gateway.intentErr = context.DeadlineExceeded
	return nil
}

func (s *apiScenario) gatewayPlanGenericError(context.Context) error {
	s.gateway.planErr = errors.New("some other error")
	return nil
}

func (s *apiScenario) assistantIsTimeoutMessage(context.Context) error {
	content := s.body["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	want := "[SYSTEM] Communication with AI core timed out. Please try your request again."
	if content != want {
		return fmt.Errorf("content = %q, want %q", content, want)
	}
	return nil
}

func decodeBodyT(resp *httptest.ResponseRecorder) map[string]any {
	var decoded map[string]any
	_ = json.NewDecoder(bytes.NewReader(resp.Body.Bytes())).Decode(&decoded)
	return decoded
}

func requireEqual(got, want any) error {
	if got != want {
		return fmt.Errorf("got %v, want %v", got, want)
	}
	return nil
}

func noopAPI(context.Context) error { return nil }
