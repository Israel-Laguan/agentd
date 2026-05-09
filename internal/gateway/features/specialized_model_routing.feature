Feature: Specialized Model Routing
  Goal: Route requests to the optimal provider/model based on the caller
  role so chat gets high reasoning, workers get fast code generation, and
  memory gets cheap summarization (Flow 6.3).

  Scenario: Chat role routes to the smart provider
    Given role routes map chat to provider "openai" with model "gpt-4o"
    And role routes map worker to provider "anthropic" with model "claude-3-haiku"
    And role routes map memory to provider "ollama" with model "llama3-8b"
    And three providers "openai", "anthropic", "ollama" are configured
    When a request with role "chat" is sent
    Then the response should indicate ProviderUsed: "openai"

  Scenario: Worker role routes to the coding provider
    Given role routes map chat to provider "openai" with model "gpt-4o"
    And role routes map worker to provider "anthropic" with model "claude-3-haiku"
    And role routes map memory to provider "ollama" with model "llama3-8b"
    And three providers "openai", "anthropic", "ollama" are configured
    When a request with role "worker" is sent
    Then the response should indicate ProviderUsed: "anthropic"

  Scenario: Memory role routes to the cheap provider
    Given role routes map chat to provider "openai" with model "gpt-4o"
    And role routes map worker to provider "anthropic" with model "claude-3-haiku"
    And role routes map memory to provider "ollama" with model "llama3-8b"
    And three providers "openai", "anthropic", "ollama" are configured
    When a request with role "memory" is sent
    Then the response should indicate ProviderUsed: "ollama"

  Scenario: Explicit provider on request overrides role routing
    Given role routes map chat to provider "ollama" with model "llama3-8b"
    And two providers "openai", "ollama" are configured
    When a request with role "chat" and explicit provider "openai" is sent
    Then the response should indicate ProviderUsed: "openai"
