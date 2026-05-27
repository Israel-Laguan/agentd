Feature: Cascading fallback across providers
  Goal: Ensure the router cascades through configured providers
  and wraps the final exhaustion error with ErrLLMUnreachable (Flow 6.2).

  Scenario: First provider fails, falls back to second
    Given the provider "synth-chat" returns an error
    And the provider "synth-worker" is configured and available
    When the router processes a request
    Then the response should indicate ProviderUsed: "synth-worker"

  Scenario: First two providers fail, falls back to third
    Given the provider "synth-chat" returns an error
    And the provider "synth-worker" returns an error
    And the provider "synth-memory" is configured and available
    When the router processes a request
    Then the response should indicate ProviderUsed: "synth-memory"

  Scenario: All four providers fail wraps ErrLLMUnreachable
    Given the provider "synth-chat" returns an error
    And the provider "synth-worker" returns an error
    And the provider "synth-memory" returns an error
    And the provider "synth-horde" returns an error
    When the router processes a request
    Then the error should wrap ErrLLMUnreachable
    And the error should mention all four provider names
