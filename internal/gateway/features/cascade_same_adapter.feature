Feature: Cascade between two providers sharing the same adapter
  Goal: Verify that two providers using the same adapter (e.g. adapter: openai) at
  different base_url values are treated as independent backends and cascade
  independently (Task 09 — fixed config schema, one slot per vendor).

  Scenario: First OpenAI-compatible endpoint fails, falls back to second
    Given the provider "synth-openai-a" returns an error
    And the provider "synth-openai-b" is configured and available
    When the router processes a request
    Then the response should indicate ProviderUsed: "synth-openai-b"

  Scenario: Both OpenAI-compatible endpoints fail wraps ErrLLMUnreachable
    Given the provider "synth-openai-a" returns an error
    And the provider "synth-openai-b" returns an error
    When the router processes a request
    Then the error should wrap ErrLLMUnreachable
