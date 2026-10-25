package stage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"agentd/internal/gateway/spec"
)

type fakeTarget struct {
	Name string `json:"name"`
}

func (fakeTarget) SchemaHint() string { return `{"name": string}` }

func (t *fakeTarget) Validate() error {
	if t.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

// scriptedGateway answers Generate from replies in order; other AIGateway
// methods are never called on this path, so the embedded nil interface is safe.
type scriptedGateway struct {
	spec.AIGateway
	replies []string
	calls   int
}

func (g *scriptedGateway) Generate(context.Context, spec.AIRequest) (spec.AIResponse, error) {
	reply := g.replies[g.calls]
	g.calls++
	return spec.AIResponse{Content: reply}, nil
}

func validRequest() StageRequest {
	return StageRequest{
		Stage:    StageIntake,
		Purpose:  spec.PurposeIntakeClassifyIntent,
		Contract: NewContract[fakeTarget](),
	}
}

func TestStageRequestValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*StageRequest)
		wantErr bool
	}{
		{"complete request is valid", func(*StageRequest) {}, false},
		{"stage is required", func(r *StageRequest) { r.Stage = "" }, true},
		{"purpose is required", func(r *StageRequest) { r.Purpose = "" }, true},
		{"contract is required", func(r *StageRequest) { r.Contract = Contract{} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := validRequest()
			tt.mutate(&req)
			if err := req.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWorkspaceAccessZeroValueIsNone(t *testing.T) {
	t.Parallel()
	var access WorkspaceAccess
	if access != AccessNone {
		t.Fatalf("zero WorkspaceAccess = %v, want AccessNone", access)
	}
}

func TestContractSchemaHintComesFromTarget(t *testing.T) {
	t.Parallel()
	if got, want := NewContract[fakeTarget]().hint, (fakeTarget{}).SchemaHint(); got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}

func TestContractDecodeRetriesUntilValid(t *testing.T) {
	t.Parallel()
	gw := &scriptedGateway{replies: []string{`{"name":""}`, `{"name":"agentd"}`}}
	raw, _, _, err := NewContract[fakeTarget]().Decode(context.Background(), gw, spec.AIRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if gw.calls != 2 {
		t.Fatalf("gateway calls = %d, want 2 (one corrective retry)", gw.calls)
	}
	var got fakeTarget
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "agentd" {
		t.Fatalf("decoded name = %q, want agentd", got.Name)
	}
}
