package wsession

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// SecretStore provides read-only access to credentials keyed by tool name.
// Implementations must be safe for concurrent use.
type SecretStore interface {
	// Get returns the credential value for the given tool, or ("", false)
	// if the tool has no credential configured.
	Get(tool string) (string, bool)

	// Has reports whether a credential is registered for tool (regardless
	// of whether the backing value is present in the environment).
	Has(tool string) bool

	// RequiredTools returns the tool names that have credential mappings.
	RequiredTools() []string

	// EnvVar returns the environment variable name mapped to tool, or ("", false)
	// if the tool has no mapping.
	EnvVar(tool string) (string, bool)

	// Validate checks that every required credential is present. Returns an error
	// listing missing tools/env vars when any mapped credential is unset.
	Validate() error
}

// ToolCredentialMapping maps a tool name to the environment variable
// that holds its credential. Parsed from config key
// agentic.tool_credentials (map[string]string).
type ToolCredentialMapping struct {
	ToolName string
	EnvVar   string
}

// EnvSecretStore reads credentials from environment variables based on
// a static mapping of tool-name to env-var. All methods are safe for
// concurrent use.
type EnvSecretStore struct {
	mu       sync.RWMutex
	mappings map[string]string // tool name -> env var name
}

// compile-time interface check
var _ SecretStore = (*EnvSecretStore)(nil)

// NewEnvSecretStore creates a store from tool-name to env-var mappings.
// A nil or empty map produces a store that returns false for every Get.
func NewEnvSecretStore(toolToEnvVar map[string]string) *EnvSecretStore {
	m := make(map[string]string, len(toolToEnvVar))
	for k, v := range toolToEnvVar {
		m[k] = v
	}
	return &EnvSecretStore{mappings: m}
}

// Get returns the credential value for tool by reading the mapped
// environment variable. Returns ("", false) when the tool has no
// mapping or the env var is unset/empty.
func (s *EnvSecretStore) Get(tool string) (string, bool) {
	s.mu.RLock()
	envVar, ok := s.mappings[tool]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	val := os.Getenv(envVar)
	if val == "" {
		return "", false
	}
	return val, true
}

// Has reports whether a credential mapping exists for the tool.
func (s *EnvSecretStore) Has(tool string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.mappings[tool]
	return ok
}

// EnvVar returns the environment variable name mapped to tool.
func (s *EnvSecretStore) EnvVar(tool string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	envVar, ok := s.mappings[tool]
	return envVar, ok
}

// RequiredTools returns the tool names with credential mappings.
func (s *EnvSecretStore) RequiredTools() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tools := make([]string, 0, len(s.mappings))
	for t := range s.mappings {
		tools = append(tools, t)
	}
	return tools
}

// Validate checks that every mapped environment variable is set and
// non-empty. Returns an error listing all missing credentials.
func (s *EnvSecretStore) Validate() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var missing []string
	for tool, envVar := range s.mappings {
		if os.Getenv(envVar) == "" {
			missing = append(missing, fmt.Sprintf("%s (env: %s)", tool, envVar))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing credentials for tools: %s", strings.Join(missing, ", "))
	}
	return nil
}
