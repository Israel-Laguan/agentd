Feature: Cascading Fallback with Anthropic Provider
  Goal: Ensure the router cascades through all providers including Anthropic
  and wraps the final exhaustion error with ErrLLMUnreachable (Flow 6.2).

  Scenario: OpenAI fails, falls back to Anthropic
    Given the provider "OpenAI" returns an error
    And the provider "Anthropic" is configured and available
    When the router processes a request
    Then the response should indicate ProviderUsed: "anthropic"

  Scenario: OpenAI and Anthropic fail, falls back to Ollama
    Given the provider "OpenAI" returns an error
    And the provider "Anthropic" returns an error
    And the provider "Ollama" is configured and available
    When the router processes a request
    Then the response should indicate ProviderUsed: "ollama"

  Scenario: All four providers fail wraps ErrLLMUnreachable
    Given the provider "OpenAI" returns an error
    And the provider "Anthropic" returns an error
    And the provider "Ollama" returns an error
    And the provider "Horde" returns an error
    When the router processes a request
    Then the error should wrap ErrLLMUnreachable
    And the error should mention all four provider names
