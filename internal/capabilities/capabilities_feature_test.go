package capabilities

import (
	"context"
	"fmt"

	"agentd/internal/gateway"

	"github.com/cucumber/godog"
)

type capabilitiesScenario struct {
	registry   *Registry
	adapter    *mockAdapter
	tools      []gateway.ToolDefinition
	result     any
	resultErr  error
	adapterMap map[string]*mockAdapter
}

func initializeCapabilitiesScenario(sc *godog.ScenarioContext) {
	state := &capabilitiesScenario{
		adapterMap: make(map[string]*mockAdapter),
	}
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		state.registry = NewRegistry()
		state.adapterMap = make(map[string]*mockAdapter)
		return ctx, nil
	})
	registerCapabilitySteps(sc, state)
}

func registerCapabilitySteps(sc *godog.ScenarioContext, state *capabilitiesScenario) {
	sc.Step(`^a capability registry$`, state.stepCapabilityRegistry)
	sc.Step(`^a capability registry with no adapters$`, state.stepEmptyRegistry)
	sc.Step(`^an MCP adapter named "([^"]*)" with tools? "([^"]*)"$`, state.stepRegisterAdapterWithTools)
	sc.Step(`^an MCP adapter named "([^"]*)" that returns "([^"]*)" for "([^"]*)"$`, state.stepRegisterAdapterWithResult)
	sc.Step(`^When GetTools is called$`, state.stepGetTools)
	sc.Step(`^the result should contain (\d+) tools?$`, state.stepToolsCount)
	sc.Step(`^the tools should include "([^"]*)"$`, state.stepToolsContain)
	sc.Step(`^the result should not contain "([^"]*)"$`, state.stepToolsNotContain)
	sc.Step(`^the result should be empty$`, state.stepToolsEmpty)
	sc.Step(`^When CallTool is called for adapter "([^"]*)" tool "([^"]*)" with args (.+)$`, state.stepCallTool)
	sc.Step(`^the result should be "([^"]*)"$`, state.stepResultEquals)
	sc.Step(`^the adapter "([^"]*)" is unregistered$`, state.stepUnregisterAdapter)
}

func (s *capabilitiesScenario) stepCapabilityRegistry(_ context.Context) error {
	s.registry = NewRegistry()
	return nil
}

func (s *capabilitiesScenario) stepEmptyRegistry(_ context.Context) error {
	s.registry = NewRegistry()
	return nil
}

func (s *capabilitiesScenario) stepRegisterAdapterWithTools(_ context.Context, name, toolsStr string) error {
	tools := parseToolNames(toolsStr)
	mocked := &mockAdapter{name: name, tools: make([]gateway.ToolDefinition, len(tools))}
	for i, t := range tools {
		mocked.tools[i] = gateway.ToolDefinition{Name: t}
	}
	s.adapterMap[name] = mocked
	s.registry.Register(name, mocked)
	return nil
}

func (s *capabilitiesScenario) stepRegisterAdapterWithResult(_ context.Context, name, result, toolName string) error {
	mocked := &mockAdapter{name: name, callResult: result}
	s.adapterMap[name] = mocked
	s.registry.Register(name, mocked)
	return nil
}

func (s *capabilitiesScenario) stepGetTools(_ context.Context) error {
	s.tools, s.resultErr = s.registry.GetTools(context.Background())
	return nil
}

func (s *capabilitiesScenario) stepToolsCount(_ context.Context, count int) error {
	if len(s.tools) != count {
		return fmt.Errorf("expected %d tools, got %d", count, len(s.tools))
	}
	return nil
}

func (s *capabilitiesScenario) stepToolsContain(_ context.Context, tool string) error {
	for _, t := range s.tools {
		if t.Name == tool {
			return nil
		}
	}
	return fmt.Errorf("tool %q not found in %v", tool, s.tools)
}

func (s *capabilitiesScenario) stepToolsNotContain(_ context.Context, tool string) error {
	for _, t := range s.tools {
		if t.Name == tool {
			return fmt.Errorf("tool %q should not be present", tool)
		}
	}
	return nil
}

func (s *capabilitiesScenario) stepToolsEmpty(_ context.Context) error {
	if len(s.tools) != 0 {
		return fmt.Errorf("expected empty tools, got %d", len(s.tools))
	}
	return nil
}

func (s *capabilitiesScenario) stepCallTool(_ context.Context, adapterName, toolName, argsStr string) error {
	s.result, s.resultErr = s.registry.CallTool(context.Background(), adapterName, toolName, nil)
	return nil
}

func (s *capabilitiesScenario) stepResultEquals(_ context.Context, expected string) error {
	if s.result != expected {
		return fmt.Errorf("expected %q, got %v", expected, s.result)
	}
	return nil
}

func (s *capabilitiesScenario) stepUnregisterAdapter(_ context.Context, name string) error {
	s.registry.Unregister(name)
	return nil
}

func parseToolNames(toolsStr string) []string {
	if toolsStr == "" {
		return nil
	}
	var tools []string
	for _, t := range splitAndTrim(toolsStr, ", ") {
		tools = append(tools, t)
	}
	return tools
}

func splitAndTrim(s, sep string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for _, part := range []string{s} {
		result = append(result, part)
	}
	return result
}
