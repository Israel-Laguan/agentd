Feature: Specialized Model Routing
  Goal: Route requests to the optimal provider/model based on the caller
  role so chat gets high reasoning, workers get fast code generation, and
  memory gets cheap summarization (Flow 6.3).

  Scenario: Chat role routes to the smart provider
    Given role routes map chat to provider "synth-chat" with model "gpt-4o"
    And role routes map worker to provider "synth-worker" with model "claude-3-haiku"
    And role routes map memory to provider "synth-memory" with model "llama3-8b"
    And three providers "synth-chat", "synth-worker", "synth-memory" are configured
    When a request with role "chat" is sent
    Then the response should indicate ProviderUsed: "synth-chat"

  Scenario: Worker role routes to the coding provider
    Given role routes map chat to provider "synth-chat" with model "gpt-4o"
    And role routes map worker to provider "synth-worker" with model "claude-3-haiku"
    And role routes map memory to provider "synth-memory" with model "llama3-8b"
    And three providers "synth-chat", "synth-worker", "synth-memory" are configured
    When a request with role "worker" is sent
    Then the response should indicate ProviderUsed: "synth-worker"

  Scenario: Memory role routes to the cheap provider
    Given role routes map chat to provider "synth-chat" with model "gpt-4o"
    And role routes map worker to provider "synth-worker" with model "claude-3-haiku"
    And role routes map memory to provider "synth-memory" with model "llama3-8b"
    And three providers "synth-chat", "synth-worker", "synth-memory" are configured
    When a request with role "memory" is sent
    Then the response should indicate ProviderUsed: "synth-memory"

  Scenario: Explicit provider on request overrides role routing
    Given role routes map chat to provider "synth-memory" with model "llama3-8b"
    And two providers "synth-chat", "synth-memory" are configured
    When a request with role "chat" and explicit provider "synth-chat" is sent
    Then the response should indicate ProviderUsed: "synth-chat"

  Scenario: Role-assigned provider fails, falls back to next in gateway order
    Given role routes map worker to provider "synth-worker-fail" with model "worker-model"
    And the provider "synth-worker-fail" returns an error
    And the provider "synth-horde" is configured and available
    When a request with role "worker" is sent
    Then the response should indicate ProviderUsed: "synth-horde"
