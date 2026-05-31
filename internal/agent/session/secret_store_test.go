package session

import (
	"sort"
	"strings"
	"testing"
)

func TestEnvSecretStore_Get_Present(t *testing.T) {
	const envKey = "TEST_SECRET_STORE_GET_PRESENT"
	t.Setenv(envKey, "secret-value-123")

	store := NewEnvSecretStore(map[string]string{"github": envKey})
	val, ok := store.Get("github")
	if !ok {
		t.Fatal("Get(github) returned false, want true")
	}
	if val != "secret-value-123" {
		t.Fatalf("Get(github) = %q, want %q", val, "secret-value-123")
	}
}

func TestEnvSecretStore_Get_Missing(t *testing.T) {
	t.Setenv("NONEXISTENT_ENV_VAR_XYZ_12345", "")
	store := NewEnvSecretStore(map[string]string{"github": "NONEXISTENT_ENV_VAR_XYZ_12345"})

	val, ok := store.Get("github")
	if ok {
		t.Fatalf("Get(github) returned true with value %q, want false", val)
	}
}

func TestEnvSecretStore_Get_NoMapping(t *testing.T) {
	t.Parallel()
	store := NewEnvSecretStore(map[string]string{"github": "SOME_VAR"})
	val, ok := store.Get("jira")
	if ok {
		t.Fatalf("Get(jira) returned true with value %q, want false", val)
	}
	if val != "" {
		t.Fatalf("Get(jira) = %q, want empty", val)
	}
}

func TestEnvSecretStore_EnvVar(t *testing.T) {
	t.Parallel()
	store := NewEnvSecretStore(map[string]string{"github": "GH_TOKEN"})
	envVar, ok := store.EnvVar("github")
	if !ok || envVar != "GH_TOKEN" {
		t.Fatalf("EnvVar(github) = (%q, %v), want (GH_TOKEN, true)", envVar, ok)
	}
	if _, ok := store.EnvVar("jira"); ok {
		t.Fatal("EnvVar(jira) = true, want false")
	}
}

func TestEnvSecretStore_Has(t *testing.T) {
	t.Parallel()
	store := NewEnvSecretStore(map[string]string{"github": "GH_TOKEN"})
	if !store.Has("github") {
		t.Fatal("Has(github) = false, want true")
	}
	if store.Has("jira") {
		t.Fatal("Has(jira) = true, want false")
	}
}

func TestEnvSecretStore_RequiredTools(t *testing.T) {
	t.Parallel()
	store := NewEnvSecretStore(map[string]string{
		"github": "GH_TOKEN",
		"jira":   "JIRA_KEY",
		"slack":  "SLACK_TOKEN",
	})
	tools := store.RequiredTools()
	sort.Strings(tools)
	want := []string{"github", "jira", "slack"}
	if len(tools) != len(want) {
		t.Fatalf("RequiredTools() = %v, want %v", tools, want)
	}
	for i, got := range tools {
		if got != want[i] {
			t.Fatalf("RequiredTools()[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func TestEnvSecretStore_Validate_AllPresent(t *testing.T) {
	const envKey = "TEST_SECRET_STORE_VALIDATE_OK"
	t.Setenv(envKey, "present")

	store := NewEnvSecretStore(map[string]string{"github": envKey})
	if err := store.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestEnvSecretStore_Validate_Missing(t *testing.T) {
	t.Setenv("MISSING_CRED_XYZ_99999", "")
	store := NewEnvSecretStore(map[string]string{"github": "MISSING_CRED_XYZ_99999"})
	err := store.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error for missing credential")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Fatalf("Validate() error %q does not mention tool name", err.Error())
	}
	if !strings.Contains(err.Error(), "MISSING_CRED_XYZ_99999") {
		t.Fatalf("Validate() error %q does not mention env var", err.Error())
	}
}

func TestEnvSecretStore_NilMap(t *testing.T) {
	t.Parallel()
	store := NewEnvSecretStore(nil)
	if _, ok := store.Get("anything"); ok {
		t.Fatal("Get on nil-map store returned true")
	}
	if store.Has("anything") {
		t.Fatal("Has on nil-map store returned true")
	}
	if _, ok := store.EnvVar("anything"); ok {
		t.Fatal("EnvVar on nil-map store returned true")
	}
	if len(store.RequiredTools()) != 0 {
		t.Fatal("RequiredTools on nil-map store returned non-empty")
	}
	if err := store.Validate(); err != nil {
		t.Fatalf("Validate on nil-map store = %v, want nil", err)
	}
}
