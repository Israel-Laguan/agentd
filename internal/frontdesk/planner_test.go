package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/testutil"
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
		Gateway:   nil,
		Budget:    12000,
		Truncator: nil,
	}
	if p.Budget != 12000 {
		t.Errorf("Budget = %v, want 12000", p.Budget)
	}
}

func TestPlanner_CanCreateWithGateway(t *testing.T) {
	p := Planner{
		Gateway:   &mockGateway{},
		Budget:    12000,
		Truncator: nil,
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

func TestPlanner_PlanContent_statusReport(t *testing.T) {
	store := testutil.NewFakeStore()
	materializePlannerStore(t, store)
	p := newTestPlanner(t, store, &mockGateway{
		intent: &spec.IntentAnalysis{Intent: "status_check"},
	})

	out, err := p.PlanContent(context.Background(), nil, "status?", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	var report StatusReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("unmarshal status report: %v", err)
	}
	if report.Kind != "status_report" {
		t.Fatalf("Kind = %q, want status_report", report.Kind)
	}
	if report.Summary.TotalProjects != 1 {
		t.Fatalf("TotalProjects = %d, want 1", report.Summary.TotalProjects)
	}
}

func TestPlanner_PlanContent_planRequest_singleScope(t *testing.T) {
	gw := &mockGateway{intent: &spec.IntentAnalysis{Intent: "plan_request"}}
	p := newTestPlanner(t, testutil.NewFakeStore(), gw)

	out, err := p.PlanContent(context.Background(), nil, "build api", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	var plan models.DraftPlan
	if err := json.Unmarshal(out, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	if gw.analyzeCalls != 1 {
		t.Fatalf("AnalyzeScope calls = %d, want 1", gw.analyzeCalls)
	}
	if gw.planCalls != 1 {
		t.Fatalf("GeneratePlan calls = %d, want 1", gw.planCalls)
	}
}

func TestPlanner_PlanContent_planRequest_multiScope(t *testing.T) {
	gw := &mockGateway{
		intent: &spec.IntentAnalysis{Intent: "plan_request"},
		scope: &spec.ScopeAnalysis{
			SingleScope: false,
			Scopes:      []gateway.ScopeOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		},
	}
	p := newTestPlanner(t, testutil.NewFakeStore(), gw)

	out, err := p.PlanContent(context.Background(), nil, "build two apps", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	var clar ScopeClarification
	if err := json.Unmarshal(out, &clar); err != nil {
		t.Fatalf("unmarshal scope clarification: %v", err)
	}
	if clar.Kind != "scope_clarification" {
		t.Fatalf("Kind = %q, want scope_clarification", clar.Kind)
	}
	if len(clar.Scopes) != 2 {
		t.Fatalf("Scopes len = %d, want 2", len(clar.Scopes))
	}
	if gw.planCalls != 0 {
		t.Fatalf("GeneratePlan calls = %d, want 0", gw.planCalls)
	}
}

func TestPlanner_PlanContent_approvedScope(t *testing.T) {
	gw := &mockGateway{}
	p := newTestPlanner(t, testutil.NewFakeStore(), gw)

	_, err := p.PlanContent(context.Background(), []string{"backend-api"}, "build api", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	if gw.intentCalls != 0 {
		t.Fatalf("ClassifyIntent calls = %d, want 0", gw.intentCalls)
	}
	if gw.planCalls != 1 {
		t.Fatalf("GeneratePlan calls = %d, want 1", gw.planCalls)
	}
	if !strings.Contains(gw.lastPlanIntent, "Restrict planning to scope: backend-api") {
		t.Fatalf("lastPlanIntent = %q, want approved scope suffix", gw.lastPlanIntent)
	}
}

func TestPlanner_PlanContent_outOfScope(t *testing.T) {
	gw := &mockGateway{
		intent: &spec.IntentAnalysis{Intent: "out_of_scope", Reason: "too vague"},
	}
	p := newTestPlanner(t, testutil.NewFakeStore(), gw)

	out, err := p.PlanContent(context.Background(), nil, "hello", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	var clar FeasibilityClarification
	if err := json.Unmarshal(out, &clar); err != nil {
		t.Fatalf("unmarshal feasibility: %v", err)
	}
	if clar.Kind != "feasibility_clarification" {
		t.Fatalf("Kind = %q, want feasibility_clarification", clar.Kind)
	}
	if clar.Reason != "too vague" {
		t.Fatalf("Reason = %q, want too vague", clar.Reason)
	}
}

func TestPlanner_PlanContent_ambiguousIntent(t *testing.T) {
	gw := &mockGateway{intent: &spec.IntentAnalysis{Intent: "greeting"}}
	p := newTestPlanner(t, testutil.NewFakeStore(), gw)

	out, err := p.PlanContent(context.Background(), nil, "hi", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	var clar IntentClarification
	if err := json.Unmarshal(out, &clar); err != nil {
		t.Fatalf("unmarshal intent clarification: %v", err)
	}
	if clar.Kind != "intent_clarification" {
		t.Fatalf("Kind = %q, want intent_clarification", clar.Kind)
	}
}

func TestPlanner_PlanContent_structuredJSONPlan(t *testing.T) {
	gw := &contractMockGateway{
		mockGateway:   mockGateway{},
		useStructured: true,
		plan:          &models.DraftPlan{ProjectName: "from-structured"},
	}
	p := newTestPlannerWithGateway(t, testutil.NewFakeStore(), gw)

	out, err := p.PlanContent(context.Background(), []string{"scope-a"}, "build", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	var plan models.DraftPlan
	if err := json.Unmarshal(out, &plan); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	if plan.ProjectName != "from-structured" {
		t.Fatalf("ProjectName = %q, want from-structured", plan.ProjectName)
	}
	if gw.planCalls != 0 {
		t.Fatalf("GeneratePlan calls = %d, want 0 when structured JSON succeeds", gw.planCalls)
	}
}

func TestPlanner_ctxWithHouseRules(t *testing.T) {
	store := testutil.NewFakeStore()
	if err := store.SetSetting(context.Background(), models.SettingKeyHouseRules, "be concise"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	gw := &mockGateway{intent: &spec.IntentAnalysis{Intent: "plan_request"}}
	p := newTestPlanner(t, store, gw)
	p.SettingsStore = store

	_, err := p.PlanContent(context.Background(), nil, "plan work", nil)
	if err != nil {
		t.Fatalf("PlanContent() error = %v", err)
	}
	if gw.lastHouseRules != "be concise" {
		t.Fatalf("lastHouseRules = %q, want be concise", gw.lastHouseRules)
	}
}

func newTestPlanner(t *testing.T, store models.KanbanStore, gw *mockGateway) Planner {
	t.Helper()
	return newTestPlannerWithGateway(t, store, gw)
}

func newTestPlannerWithGateway(t *testing.T, store models.KanbanStore, gw gateway.AIGateway) Planner {
	t.Helper()
	return Planner{
		Gateway:    gw,
		Summarizer: NewStatusSummarizer(store),
		Stash:      &FileStash{Dir: t.TempDir(), StashThreshold: 10_000},
		Budget:     12_000,
	}
}

func materializePlannerStore(t *testing.T, store *testutil.FakeKanbanStore) {
	t.Helper()
	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "test-project",
		Description: "test",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "Task A"},
			{TempID: "b", Title: "Task B"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
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

type mockGateway struct {
	intent          *spec.IntentAnalysis
	intentErr       error
	intentCalls     int
	scope           *spec.ScopeAnalysis
	scopeErr        error
	analyzeCalls    int
	plan            *models.DraftPlan
	planErr         error
	planCalls       int
	lastPlanIntent  string
	lastHouseRules  string
}

type contractMockGateway struct {
	mockGateway
	useStructured bool
	structuredErr error
	plan          *models.DraftPlan
}

func (m *mockGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{}, nil
}

func (m *mockGateway) GeneratePlan(ctx context.Context, intent string) (*models.DraftPlan, error) {
	m.planCalls++
	m.lastPlanIntent = intent
	if rules := gateway.HouseRulesFromContext(ctx); rules != "" {
		m.lastHouseRules = rules
	}
	if m.planErr != nil {
		return nil, m.planErr
	}
	if m.plan != nil {
		return m.plan, nil
	}
	return &models.DraftPlan{}, nil
}

func (m *mockGateway) AnalyzeScope(ctx context.Context, intent string) (*spec.ScopeAnalysis, error) {
	m.analyzeCalls++
	if rules := gateway.HouseRulesFromContext(ctx); rules != "" {
		m.lastHouseRules = rules
	}
	if m.scopeErr != nil {
		return nil, m.scopeErr
	}
	if m.scope != nil {
		return m.scope, nil
	}
	return &spec.ScopeAnalysis{SingleScope: true}, nil
}

func (m *mockGateway) ClassifyIntent(_ context.Context, intent string) (*spec.IntentAnalysis, error) {
	m.intentCalls++
	if m.intentErr != nil {
		return nil, m.intentErr
	}
	if m.intent != nil {
		return m.intent, nil
	}
	return &spec.IntentAnalysis{Intent: "plan_request"}, nil
}

func (m *mockGateway) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	return spec.NoopEmbed(ctx, req)
}

func (m *contractMockGateway) GenerateText(context.Context, string, int) (string, error) {
	return "", nil
}

func (m *contractMockGateway) GenerateStructuredJSON(_ context.Context, _ string, target interface{}) error {
	if m.structuredErr != nil {
		return m.structuredErr
	}
	if !m.useStructured {
		return errors.New("structured plan not configured")
	}
	draft, ok := target.(*models.DraftPlan)
	if !ok {
		return errors.New("expected *models.DraftPlan target")
	}
	plan := m.plan
	if plan == nil {
		plan = &models.DraftPlan{}
	}
	*draft = *plan
	return nil
}

func (m *contractMockGateway) TruncateToBudget(input string, _ int) string {
	return input
}
