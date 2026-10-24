Feature: Agentic mode toggle controls worker behavior

  Scenario: Given agentic mode is disabled (default), when worker processes task, then use legacy JSON command path
    Given agentic mode is disabled
    And the worker has a task to process
    When the worker processes the task
    Then the worker should use the legacy JSON command path

  Scenario: Given agentic mode is enabled and provider supports tools, when worker processes task, then use agentic loop with tools
    Given agentic mode is enabled
    And the provider is "synth-tools"
    And the worker has a task to process
    When the worker processes the task
    Then the worker should use the agentic loop with tools

  Scenario: Given agentic mode is enabled but provider lacks tool support, when worker processes task, then fail the task without a legacy fallback
    Given agentic mode is enabled
    And the provider is "synth-no-tools"
    And the worker has a task to process
    When the worker processes the task
    Then the worker should fail the task for lack of tool support
    And the gateway should receive no requests
