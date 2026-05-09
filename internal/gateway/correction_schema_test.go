package gateway

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"agentd/internal/models"
)

type validatableStruct struct {
	Name  string `json:"name"`
	Tasks []struct {
		Title string `json:"title"`
	} `json:"tasks"`
}

func (v *validatableStruct) Validate() error {
	if v.Name == "" {
		return fmt.Errorf("missing required field: name")
	}
	if len(v.Tasks) == 0 {
		return fmt.Errorf("missing required field: tasks")
	}
	return nil
}

func TestGenerateJSON_SchemaValidation_SelfCorrects(t *testing.T) {
	gw := &sequenceGateway{values: []string{
		`{"name":"","tasks":[]}`,
		`{"name":"project","tasks":[{"title":"t1"}]}`,
	}}
	got, err := GenerateJSON[validatableStruct](context.Background(), gw, sampleAIRequest())
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}
	if got.Name != "project" {
		t.Fatalf("Name = %q, want project", got.Name)
	}
	if len(gw.requests) != 2 {
		t.Fatalf("calls = %d, want 2", len(gw.requests))
	}
}

func TestGenerateJSON_SchemaValidation_FailsAfterRetries(t *testing.T) {
	gw := &sequenceGateway{values: []string{
		`{"name":"","tasks":[]}`,
		`{"name":"","tasks":[]}`,
		`{"name":"","tasks":[]}`,
	}}
	_, err := GenerateJSON[validatableStruct](context.Background(), gw, sampleAIRequest())
	if !errors.Is(err, models.ErrInvalidJSONResponse) {
		t.Fatalf("error = %v, want ErrInvalidJSONResponse", err)
	}
}

func TestGenerateJSON_NoValidatable_SkipsValidation(t *testing.T) {
	type plain struct {
		Value string `json:"value"`
	}
	gw := &sequenceGateway{values: []string{`{"value":"ok"}`}}
	got, err := GenerateJSON[plain](context.Background(), gw, sampleAIRequest())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got.Value != "ok" {
		t.Fatalf("Value = %q", got.Value)
	}
}

func TestGenerateJSON_MixedParseAndValidationErrors(t *testing.T) {
	gw := &sequenceGateway{values: []string{
		`{"name":"project"`,
		`{"name":"","tasks":[]}`,
		`{"name":"done","tasks":[{"title":"ok"}]}`,
	}}
	got, err := GenerateJSON[validatableStruct](context.Background(), gw, sampleAIRequest())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if got.Name != "done" || len(got.Tasks) != 1 {
		t.Fatalf("got = %+v", got)
	}
	if len(gw.requests) != 3 {
		t.Fatalf("calls = %d, want 3", len(gw.requests))
	}
}
