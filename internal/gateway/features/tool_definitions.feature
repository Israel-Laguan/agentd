Feature: Tool Definitions in Gateway
  Goal: Support function calling via tool definitions in the AI gateway.
  The gateway should serialize tools in requests and capture tool_calls from responses.

  Scenario: Send tools in request and receive tool_calls in response
    Given a mock OpenAI provider that returns tool_calls
    When Generate is called with a request containing tool definitions
    Then the request should include tools serialized in OpenAI format
    And the response should contain the tool_calls from the model

  Scenario: Tools with no parameters should serialize with a valid JSON Schema
    Given a mock OpenAI provider
    When Generate is called with a tool definition that has no parameters
    Then the request should include the tool with parameters field present
    And the parameters should be a valid JSON Schema object

  Scenario: JSON mode is omitted when tools are present
    Given a mock OpenAI provider
    When Generate is called with JSONMode enabled and tools present
    Then the request should not include response_format
    And the request should include the tools

  Scenario: Null content is handled when tool_calls are present
    Given a mock OpenAI provider that returns null content with tool_calls
    When Generate is called with a request containing tool definitions
    Then the response should contain the tool_calls
    And the content should be empty
