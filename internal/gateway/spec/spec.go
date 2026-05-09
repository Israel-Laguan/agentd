// Package spec defines wire types and interfaces shared by gateway subpackages.
package spec

import (
	"context"
	"errors"
	"time"

	"agentd/internal/models"
)

// PromptMessage follows the common chat completion message shape.
type PromptMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Role classifies the caller so the gateway can route to the optimal
// provider/model combination (Flow 6.3: Specialized Model Routing).
type Role string

const (
	RoleChat   Role = "chat"
	RoleWorker Role = "worker"
	RoleMemory Role = "memory"
)

// RoleTarget maps a Role to a preferred provider and model override.
type RoleTarget struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// AIRequest is the provider-neutral input shape for model calls.
type AIRequest struct {
	Messages    []PromptMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
	JSONMode    bool            `json:"json_mode"`
	AgentID     string          `json:"agent_id"`
	Provider    string          `json:"provider,omitempty"`
	Model       string          `json:"model,omitempty"`
	Role        Role            `json:"role,omitempty"`
	TaskID      string          `json:"task_id,omitempty"`
	// SkipTruncation is used by internal middleware calls, such as summarization,
	// to avoid recursively applying the truncator to its own request.
	SkipTruncation bool `json:"-"`
}

// Validatable is implemented by types that need semantic validation beyond
// JSON parsing (Flow 6.4: Structured Output Enforcement).
type Validatable interface {
	Validate() error
}

// AIResponse is the provider-neutral output shape for model calls.
type AIResponse struct {
	Content      string `json:"content"`
	TokenUsage   int    `json:"token_usage"`
	ProviderUsed string `json:"provider_used"`
	ModelUsed    string `json:"model_used"`
}

// ProviderConfig configures one concrete LLM endpoint.
type ProviderConfig struct {
	Type          string        `json:"type"`
	BaseURL       string        `json:"base_url"`
	APIKey        string        `json:"api_key"`
	Model         string        `json:"model"`
	MaxInputChars int           `json:"max_input_chars"`
	Timeout       time.Duration `json:"timeout"`
	PollInterval  time.Duration `json:"poll_interval"`
}

// Provider identifies an LLM backend implementation.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderOllama    Provider = "ollama"
	ProviderLlamaCpp  Provider = "llamacpp"
	ProviderHorde     Provider = "horde"
)

// ErrContextBudgetExceeded is returned when input cannot fit the configured budget.
var ErrContextBudgetExceeded = errors.New("context budget exceeded")

// BreakerChecker is optional circuit-breaker introspection for summarization.
type BreakerChecker interface {
	IsOpen() bool
}

// BudgetTracker enforces per-task token budgets to prevent runaway costs
// (Danger A: Silent Wallet Drain).
type BudgetTracker interface {
	Reserve(taskID string) error
	Add(taskID string, tokens int)
	Usage(taskID string) int
	Reset(taskID string)
}

// Truncator enforces per-message input budgets before provider calls.
type Truncator interface {
	Apply(ctx context.Context, messages []PromptMessage, budget int) ([]PromptMessage, error)
}

type ScopeAnalysis struct {
	SingleScope bool          `json:"single_scope"`
	Confidence  float64       `json:"confidence"`
	Scopes      []ScopeOption `json:"scopes"`
	Reason      string        `json:"reason"`
}

type ScopeOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// IntentAnalysis is the result of intent classification by the Sales Rep agent.
type IntentAnalysis struct {
	Intent string `json:"intent"`
	Reason string `json:"reason"`
}

// AIGateway abstracts provider fallback and JSON repair for model calls.
type AIGateway interface {
	Generate(ctx context.Context, req AIRequest) (AIResponse, error)
	GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error)
	AnalyzeScope(ctx context.Context, userIntent string) (*ScopeAnalysis, error)
	ClassifyIntent(ctx context.Context, userIntent string) (*IntentAnalysis, error)
}

// ContractAdapter exposes proposal-aligned entry points while Router keeps its
// richer role-aware surface area.
type ContractAdapter interface {
	GenerateText(ctx context.Context, prompt string, limit int) (string, error)
	GenerateStructuredJSON(ctx context.Context, prompt string, target interface{}) error
	TruncateToBudget(input string, maxTokens int) string
}
