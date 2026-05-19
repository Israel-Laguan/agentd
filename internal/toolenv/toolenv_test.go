package toolenv

import (
	"context"
	"testing"
)

func TestCredentialValue_LastWins(t *testing.T) {
	t.Parallel()
	env := []string{
		CredentialEnvKey + "=first",
		"TRACE=1",
		CredentialEnvKey + "=second",
	}
	if got := CredentialValue(env); got != "second" {
		t.Fatalf("CredentialValue() = %q, want second", got)
	}
}

func TestFrom_DefensiveCopy(t *testing.T) {
	t.Parallel()
	ctx := With(context.Background(), []string{CredentialEnvKey + "=secret", "TRACE=1"})
	got := From(ctx)
	if len(got) != 2 {
		t.Fatalf("From() len = %d, want 2", len(got))
	}
	got[0] = "mutated=1"
	again := From(ctx)
	if again[0] != CredentialEnvKey+"=secret" {
		t.Fatalf("From() after mutation = %q, want original value preserved", again[0])
	}
}

func TestCredentialValue_IgnoresOtherKeys(t *testing.T) {
	t.Parallel()
	env := []string{"TRACE=1", "GITHUB_TOKEN=ignored"}
	if got := CredentialValue(env); got != "" {
		t.Fatalf("CredentialValue() = %q, want empty", got)
	}
}
