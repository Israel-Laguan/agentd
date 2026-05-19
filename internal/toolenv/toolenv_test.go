package toolenv

import "testing"

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

func TestCredentialValue_IgnoresOtherKeys(t *testing.T) {
	t.Parallel()
	env := []string{"TRACE=1", "GITHUB_TOKEN=ignored"}
	if got := CredentialValue(env); got != "" {
		t.Fatalf("CredentialValue() = %q, want empty", got)
	}
}
