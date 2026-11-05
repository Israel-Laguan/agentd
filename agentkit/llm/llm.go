// Package llm holds the errors and usage type that the LLM gateway and its
// callers share. It is a leaf of the agent kit: it imports only the standard
// library, so both the gateway and agentd's models layer can import it.
package llm

import "errors"

var (
	ErrInvalidJSONResponse = errors.New("invalid JSON response")
	ErrLLMQuotaExceeded    = errors.New("LLM provider quota exceeded")
	ErrLLMUnreachable      = errors.New("LLM provider unreachable")
	ErrBudgetExceeded      = errors.New("task token budget exceeded")
)

// UsageDetails carries prompt-cache token fields surfaced by providers that
// support automatic prompt caching. These are observability-only fields: the
// daemon does no hit-rate math on them. CachedTokens counts prompt-cache reads;
// CacheWriteTokens counts prompt-cache writes. Many providers omit these fields
// entirely, in which case both stay zero.
type UsageDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	// Model is the model that answered, as the provider reported it, which can
	// differ from the one requested when a proxy falls back (SP-021). It is
	// recorded in TOKEN_USAGE events, not persisted with the cache counters.
	Model string `json:"model,omitempty"`
}
