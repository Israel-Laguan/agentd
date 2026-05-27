Feature: AI Gateway Resilience & Fallback
  Goal: Ensure the Gateway can handle API failures without crashing the system, prioritizing system uptime over a specific model.

  Scenario: Primary provider fails, falls back to local model
    Given the primary provider "synth-chat" is configured with an invalid API key
    And the secondary provider "synth-memory" is configured and running locally
    When a component requests an AIRequest generation from the Gateway
    Then the Gateway should attempt the "synth-chat" provider
    And upon receiving a 401/5xx error, it should catch the error
    And the Gateway should successfully route the request to "synth-memory"
    And the final AIResponse should indicate ProviderUsed: "synth-memory"

  Scenario: First two providers fail, falls back to third
    Given the primary provider "synth-chat" returns an error
    And the secondary provider "synth-memory" returns an error
    And the tertiary provider "synth-horde" is configured and available
    When a component requests an AIRequest generation from the Gateway
    Then the Gateway should cascade through all three providers
    And the final AIResponse should indicate ProviderUsed: "synth-horde"

  Scenario: All providers exhausted returns combined error
    Given the primary provider "synth-chat" returns an error
    And the secondary provider "synth-memory" returns an error
    And the tertiary provider "synth-horde" returns an error
    When a component requests an AIRequest generation from the Gateway
    Then the Gateway should return an error mentioning all failed providers

  Scenario: Token truncation for large contexts
    Given a task execution log is 50000 characters long
    And the AI Gateway has a configured MaxTokens limit equivalent to 10000 characters
    When the text is passed to the Gateway's Truncation middleware
    Then the returned text should be strictly <= 10000 characters
    And the text should contain the first portion of the original log
    And the text should contain the last portion of the original log
    And the text should contain the truncation marker in the middle
