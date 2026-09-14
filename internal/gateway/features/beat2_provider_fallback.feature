Feature: Beat 2 — Provider fallback (harness reliability)
  Goal: Document S02 Beat 2 acceptance — cascade to healthy secondary, or
  exhaust to ErrLLMUnreachable (breaker-class failure). See docs/harness-reliability.md.

  Scenario: Unreachable primary cascades to healthy secondary
    Given the provider "primary" returns an error
    And the provider "secondary" is configured and available
    When the router processes a request
    Then the response should indicate ProviderUsed: "secondary"

  Scenario: Unreachable-only exhausts with ErrLLMUnreachable
    Given the provider "primary" returns an error
    When the router processes a request
    Then the error should wrap ErrLLMUnreachable
