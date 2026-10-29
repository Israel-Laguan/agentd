// Package spec defines wire types and interfaces shared by gateway subpackages.
package spec

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"agentd/internal/models"
)

// PromptMessage follows the common chat completion message shape.
type PromptMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Section labels what the message is made of, for the call record's
	// context mix. Never sent to a provider.
	Section Section `json:"-"`
}

// Role classifies the caller so the gateway can route to the optimal
// provider/model combination (Flow 6.3: Specialized Model Routing).
type Role string

const (
	RoleChat   Role = "chat"
	RoleWorker Role = "worker"
	RoleMemory Role = "memory"
)

// FunctionParameters describes the parameters for a tool function in JSON Schema format.
type FunctionParameters struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

func (fp FunctionParameters) MarshalJSON() ([]byte, error) {
	if len(fp.Properties) == 0 && len(fp.Required) == 0 {
		return []byte(`{"type":"object","properties":{},"required":[],"additionalProperties":false}`), nil
	}
	type Alias FunctionParameters
	return json.Marshal(Alias(fp))
}

// ToolDefinition defines a function that the model can call.
type ToolDefinition struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Parameters  *FunctionParameters `json:"parameters"`
	Cacheable   bool                `json:"cacheable,omitempty"`
}

func (t ToolDefinition) MarshalJSON() ([]byte, error) {
	if t.Parameters == nil {
		t.Parameters = &FunctionParameters{}
	}
	type Alias ToolDefinition
	return json.Marshal(Alias(t))
}

// ToolCallFunction represents a function call from the model.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall represents a tool call from the model.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// AIRequest is the provider-neutral input shape for model calls.
type AIRequest struct {
	Messages    []PromptMessage  `json:"messages"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
	JSONMode    bool             `json:"json_mode"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	AgentID     string           `json:"agent_id"`
	Provider    string           `json:"provider,omitempty"`
	Model       string           `json:"model,omitempty"`
	Role        Role             `json:"role,omitempty"`
	TaskID      string           `json:"task_id,omitempty"`
	// Purpose says why this call is made (SP-023 taxonomy). Empty is recorded
	// as PurposeUnlabelled.
	Purpose Purpose `json:"-"`
	// Attempt is set by retry loops above the router (AttemptJSONRepair) so the
	// record counts a repair separately from the first try.
	Attempt AttemptKind `json:"-"`
	// TruncatedBy names the layers above the router that shortened the input
	// (for example "agentic_head"); it only labels the call record.
	TruncatedBy string `json:"-"`
	// Attrs are the task facts the routing rules match. Empty when the call has
	// no task.
	Attrs TaskAttrs `json:"-"`
	// ProviderRouted is set when routing injected Provider from a stage route.
	// Routed providers keep cascade-style tool fallback (JSON mode) instead of
	// the explicit-provider error path.
	ProviderRouted bool `json:"-"`
	// SkipTruncation is used by internal middleware calls, such as summarization,
	// to avoid recursively applying the truncator to its own request.
	SkipTruncation bool `json:"-"`
}

// Validatable is implemented by types that need semantic validation beyond
// JSON parsing (Flow 6.4: Structured Output Enforcement).
type Validatable interface {
	Validate() error
}

// UsageDetails is a re-export alias to the definition in models (shared bottom
// layer). This lets gateway code keep using spec.UsageDetails while persistence
// (kanban) and queue interfaces can depend on models instead of gateway/spec.
type UsageDetails = models.UsageDetails

// AIResponse is the provider-neutral output shape for model calls.
type AIResponse struct {
	Content      string        `json:"content"`
	TokenUsage   int           `json:"token_usage"`
	ProviderUsed string        `json:"provider_used"`
	ModelUsed    string        `json:"model_used"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	UsageDetails *UsageDetails `json:"usage_details,omitempty"`
}

// ProviderCapabilities declares optional backend capabilities for a provider entry.
type ProviderCapabilities struct {
	// ChatTools overrides adapter-default tool support when set.
	ChatTools *bool `json:"chat_tools,omitempty" mapstructure:"chat_tools"`
}

// ProviderConfig configures one concrete LLM endpoint.
type ProviderConfig struct {
	Name          string               `json:"name,omitempty" mapstructure:"name"`
	Adapter       string               `json:"adapter" mapstructure:"adapter"`
	BaseURL       string               `json:"base_url" mapstructure:"base_url"`
	APIKey        string               `json:"api_key" mapstructure:"api_key"`
	APIKeyEnv     string               `json:"api_key_env,omitempty" mapstructure:"api_key_env"`
	Model         string               `json:"model" mapstructure:"model"`
	MaxInputChars int                  `json:"max_input_chars" mapstructure:"max_input_chars"`
	Timeout       time.Duration        `json:"timeout" mapstructure:"timeout"`
	Health        string               `json:"health,omitempty" mapstructure:"health"`
	Capabilities  ProviderCapabilities `json:"capabilities,omitempty" mapstructure:"capabilities"`
	// Options holds adapter-specific configuration keys. Each adapter reads its
	// own known keys; unrecognized keys are logged as warnings and ignored.
	Options map[string]any `json:"options,omitempty" mapstructure:"options"`
}

// Provider identifies an LLM backend implementation.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderOllama    Provider = "ollama"
	ProviderLlamaCpp  Provider = "llamacpp"
	ProviderHorde     Provider = "horde"
	ProviderGemini    Provider = "gemini"
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

// EmbedRequest is the provider-neutral input for embedding API calls.
type EmbedRequest struct {
	Input   []string `json:"input"`
	Model   string   `json:"model,omitempty"`
	Purpose Purpose  `json:"-"`
}

// EmbedResponse holds embedding vectors aligned with EmbedRequest.Input order.
type EmbedResponse struct {
	Vectors      [][]float32 `json:"vectors"`
	ProviderUsed string      `json:"provider_used"`
	ModelUsed    string      `json:"model_used"`
}

// AIGateway abstracts provider fallback and JSON repair for model calls.
type AIGateway interface {
	Generate(ctx context.Context, req AIRequest) (AIResponse, error)
	Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error)
}
