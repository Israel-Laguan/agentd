Feature: Agentic mode inner loop with tool calling
  As an operator who enables agentic mode
  I want the worker to repeatedly call the gateway with tools
  So that complex tasks can be completed through tool execution

  Background:
    Given the worker is configured with agentic mode enabled
    And the provider is "openai"

  Scenario: Gateway returns tool calls then final text
    Given the gateway will return tool calls on first call
    And the gateway will return plain text on second call
    When the worker processes a task
    Then the worker shall call the gateway with tool definitions
    And the gateway returns a response with tool calls
    When the worker executes the tool calls
    Then the worker shall append tool result messages to the conversation
    When the gateway returns a response without tool calls
    Then the worker shall commit the final text as the task result

  Scenario: Worker respects max iterations
    Given the maximum tool iterations is set to 3
    And the gateway always returns tool calls
    When the worker processes a task
    Then the worker shall stop after 3 iterations
    And the worker shall trigger a retry for the task