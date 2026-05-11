package frontdesk

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

func TestScopeClarification(t *testing.T) {
	sc := ScopeClarification{
		Kind:    "scope_clarification",
		Message: "Multiple projects detected",
		Scopes:  []gateway.ScopeOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
	}
	if sc.Kind != "scope_clarification" {
		t.Errorf("Kind = %v", sc.Kind)
	}
	if len(sc.Scopes) != 2 {
		t.Errorf("Scopes length = %v", len(sc.Scopes))
	}
}

func TestIntentClarification(t *testing.T) {
	ic := IntentClarification{
		Kind:    "intent_clarification",
		Message: "Not sure what you need",
	}
	if ic.Kind != "intent_clarification" {
		t.Errorf("Kind = %v", ic.Kind)
	}
}

func TestFeasibilityClarification(t *testing.T) {
	fc := FeasibilityClarification{
		Kind:    "feasibility_clarification",
		Message: "Cannot be planned",
		Reason:  "vague request",
	}
	if fc.Kind != "feasibility_clarification" {
		t.Errorf("Kind = %v", fc.Kind)
	}
	if fc.Reason != "vague request" {
		t.Errorf("Reason = %v", fc.Reason)
	}
}

func TestErrMultipleApprovedScopes(t *testing.T) {
	if ErrMultipleApprovedScopes.Error() != "invalid approved scopes request" {
		t.Errorf("Error() = %v", ErrMultipleApprovedScopes.Error())
	}
}

func TestPlanner_Defaults(t *testing.T) {
	p := Planner{
		Gateway:    nil,
		Budget:     12000,
		Truncator:  nil,
	}
	if p.Budget != 12000 {
		t.Errorf("Budget = %v, want 12000", p.Budget)
	}
}

func TestPlanner_CanCreateWithGateway(t *testing.T) {
	p := Planner{
		Gateway:    &mockGateway{},
		Budget:     12000,
		Truncator:  nil,
	}
	if p.Gateway == nil {
		t.Error("Gateway should not be nil")
	}
}

func TestPlanner_PlanContent_MultipleScopesError(t *testing.T) {
	p := Planner{
		Gateway: &mockGateway{},
	}
	_, err := p.PlanContent(context.Background(), []string{"a", "b"}, "test", nil)
	if err != ErrMultipleApprovedScopes {
		t.Errorf("expected ErrMultipleApprovedScopes, got %v", err)
	}
}

func TestMarshalContent(t *testing.T) {
	data := map[string]string{"key": "value"}
	bytes, err := marshalContent(data)
	if err != nil {
		t.Errorf("marshalContent() error = %v", err)
	}
	if len(bytes) == 0 {
		t.Error("bytes should not be empty")
	}
}

type mockGateway struct{}

func (m *mockGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{}, nil
}

func (m *mockGateway) GeneratePlan(ctx context.Context, intent string) (*models.DraftPlan, error) {
	return &models.DraftPlan{}, nil
}

func (m *mockGateway) AnalyzeScope(ctx context.Context, intent string) (*spec.ScopeAnalysis, error) {
	return &spec.ScopeAnalysis{SingleScope: true}, nil
}

func (m *mockGateway) ClassifyIntent(ctx context.Context, intent string) (*spec.IntentAnalysis, error) {
	return &spec.IntentAnalysis{Intent: "plan_request"}, nil
}

func (m *mockGateway) GenerateText(ctx context.Context, prompt string, limit int) (string, error) {
	return "", nil
}

func (m *mockGateway) GenerateStructuredJSON(ctx context.Context, prompt string, target interface{}) error {
	return nil
}

func (m *mockGateway) TruncateToBudget(input string, maxTokens int) string {
	return input
}