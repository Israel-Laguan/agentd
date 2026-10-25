package intake

import (
	"context"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/stage"
)

func TestClassifyIntent_normalizesUnknownIntent(t *testing.T) {
	gw := &fakeGateway{replies: []string{`{"intent":"not_a_real_intent","reason":"unclear"}`}}
	got, err := ClassifyIntent(context.Background(), stage.NewModelAgent(gw), "build me an api")
	if err != nil {
		t.Fatalf("ClassifyIntent() error = %v", err)
	}
	if got.Intent != "ambiguous" {
		t.Fatalf("intent = %q, want ambiguous", got.Intent)
	}
}

func TestClassifyIntent_validIntent(t *testing.T) {
	gw := &fakeGateway{replies: []string{`{"intent":"plan_request","reason":"build app"}`}}
	got, err := ClassifyIntent(context.Background(), stage.NewModelAgent(gw), "build me an api")
	if err != nil {
		t.Fatalf("ClassifyIntent() error = %v", err)
	}
	if got.Intent != "plan_request" {
		t.Fatalf("intent = %q, want plan_request", got.Intent)
	}
}

func TestClassifyIntent_sendsItsPurposeAndTemperature(t *testing.T) {
	gw := &fakeGateway{replies: []string{`{"intent":"plan_request","reason":"x"}`}}
	if _, err := ClassifyIntent(context.Background(), stage.NewModelAgent(gw), "build"); err != nil {
		t.Fatal(err)
	}
	if got := gw.seen[0].Purpose; got != spec.PurposeIntakeClassifyIntent {
		t.Fatalf("purpose = %q, want %q", got, spec.PurposeIntakeClassifyIntent)
	}
	if got := gw.seen[0].Temperature; got != 0.1 {
		t.Fatalf("temperature = %v, want 0.1", got)
	}
}
