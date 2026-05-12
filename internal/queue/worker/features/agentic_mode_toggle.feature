Feature: Agentic mode toggle controls worker behavior

  Scenario: Given agentic mode is disabled (default), when worker processes task, then use legacy JSON command path
    Given agentic mode is disabled
    And the worker has a task to process
    When the worker processes the task
    Then the worker should use the legacy JSON command path

  Scenario: Given agentic mode is enabled and provider is OpenAI, when worker processes task, then use agentic loop with tools
    Given agentic mode is enabled
    And the provider is "openai"
    And the worker has a task to process
    When the worker processes the task
    Then the worker should use the agentic loop with tools

  Scenario: Given agentic mode is enabled but provider is not OpenAI, when worker processes task, then fall back to legacy mode with warning
    Given agentic mode is enabled
    And the provider is "anthropic"
    And the worker has a task to process
    When the worker processes the task
    Then the worker should fall back to legacy mode
    And a warning should be logged about unsupported provider