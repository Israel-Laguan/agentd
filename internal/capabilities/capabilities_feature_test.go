package capabilities

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"

	"github.com/cucumber/godog"
)

// capabilitiesScenario is used by godog via reflection.
type capabilitiesScenario struct { //nolint:unused
	registry   *Registry
	adapter    *mockAdapter
	tools      []gateway.ToolDefinition
	result     any
	resultErr  error
	adapterMap map[string]*mockAdapter
}

// initializeCapabilitiesScenario is used by godog via reflection.
func initializeCapabilitiesScenario(sc *godog.ScenarioContext) { //nolint:unused
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

// registerCapabilitySteps is used by godog via reflection.
func registerCapabilitySteps(sc *godog.ScenarioContext, state *capabilitiesScenario) { //nolint:unused
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

func (s *capabilitiesScenario) stepCapabilityRegistry(_ context.Context) error { //nolint:unused
	s.registry = NewRegistry()
	return nil
}

func (s *capabilitiesScenario) stepEmptyRegistry(_ context.Context) error { //nolint:unused
	s.registry = NewRegistry()
	return nil
}

func (s *capabilitiesScenario) stepRegisterAdapterWithTools(_ context.Context, name, toolsStr string) error { //nolint:unused
	tools := parseToolNames(toolsStr)
	mocked := &mockAdapter{name: name, tools: make([]gateway.ToolDefinition, len(tools))}
	for i, t := range tools {
		mocked.tools[i] = gateway.ToolDefinition{Name: t}
	}
	s.adapterMap[name] = mocked
	s.registry.Register(name, mocked)
	return nil
}

func (s *capabilitiesScenario) stepRegisterAdapterWithResult(_ context.Context, name, result, toolName string) error { //nolint:unused
	mocked := &mockAdapter{name: name, callResult: result}
	s.adapterMap[name] = mocked
	s.registry.Register(name, mocked)
	return nil
}

func (s *capabilitiesScenario) stepGetTools(_ context.Context) error { //nolint:unused
	s.tools, s.resultErr = s.registry.GetTools(context.Background())
	return nil
}

func (s *capabilitiesScenario) stepToolsCount(_ context.Context, count int) error { //nolint:unused
	if len(s.tools) != count {
		return fmt.Errorf("expected %d tools, got %d", count, len(s.tools))
	}
	return nil
}

func (s *capabilitiesScenario) stepToolsContain(_ context.Context, tool string) error { //nolint:unused
	for _, t := range s.tools {
		if t.Name == tool {
			return nil
		}
	}
	return fmt.Errorf("tool %q not found in %v", tool, s.tools)
}

func (s *capabilitiesScenario) stepToolsNotContain(_ context.Context, tool string) error { //nolint:unused
	for _, t := range s.tools {
		if t.Name == tool {
			return fmt.Errorf("tool %q should not be present", tool)
		}
	}
	return nil
}

func (s *capabilitiesScenario) stepToolsEmpty(_ context.Context) error { //nolint:unused
	if len(s.tools) != 0 {
		return fmt.Errorf("expected empty tools, got %d", len(s.tools))
	}
	return nil
}

func (s *capabilitiesScenario) stepCallTool(_ context.Context, adapterName, toolName, argsStr string) error { //nolint:unused
	s.result, s.resultErr = s.registry.CallTool(context.Background(), adapterName, toolName, nil)
	return nil
}

func (s *capabilitiesScenario) stepResultEquals(_ context.Context, expected string) error { //nolint:unused
	if s.result != expected {
		return fmt.Errorf("expected %q, got %v", expected, s.result)
	}
	return nil
}

func (s *capabilitiesScenario) stepUnregisterAdapter(_ context.Context, name string) error { //nolint:unused
	s.registry.Unregister(name)
	return nil
}

// parseToolNames is used by stepRegisterAdapterWithTools.
func parseToolNames(toolsStr string) []string { //nolint:unused
	if toolsStr == "" {
		return nil
	}
	return splitAndTrim(toolsStr, ",")
}

// splitAndTrim is used by parseToolNames.
func splitAndTrim(s, sep string) []string { //nolint:unused
	if s == "" {
		return nil
	}
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
