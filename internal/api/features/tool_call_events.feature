Feature: Tool call events are emitted for agentic loop visibility
  As an operator,
  I want to see tool execution traces in real-time,
  So that I can understand what the agent is doing at each iteration.

  Scenario: TOOL_CALL event emitted before tool execution
    Given a Sandbox worker is about to execute a tool in the agentic loop
    When the tool execution begins
    Then a TOOL_CALL event should be emitted with tool_name
    And the TOOL_CALL event should include a call_id
    And the TOOL_CALL event should include arguments_summary (max 200 characters)
    And the TOOL_CALL event should be emitted before the tool executes
# Validates: Requirements 1.3, 1.4

  Scenario: TOOL_RESULT event emitted after tool execution
    Given a tool has finished executing in the agentic loop
    When the tool execution completes
    Then a TOOL_RESULT event should be emitted with tool_name
    And the TOOL_RESULT event should include the matching call_id
    And the TOOL_RESULT event should include exit_code
    And the TOOL_RESULT event should include duration_ms
    And the TOOL_RESULT event should include output_summary (max 1000 characters)
    And the TOOL_RESULT event should include stdout_bytes
    And the TOOL_RESULT event should include stderr_bytes
    And the TOOL_RESULT event should be emitted after the tool executes
# Validates: Requirements 2.3, 2.4, 7.2, 7.4

  Scenario: Events include scrubbed and truncated data
    Given a tool is executed with sensitive arguments
    When the TOOL_CALL event is emitted
    Then sensitive patterns should be replaced with "[REDACTED]" in arguments_summary
    Given a tool produces large output
    When the TOOL_RESULT event is emitted
    Then output_summary should be truncated to 1000 characters with "...[truncated]" suffix
    And stdout_bytes and stderr_bytes should reflect original sizes before truncation
# Validates: Requirements 3.1, 3.2, 3.3, 4.1, 4.2, 4.3

  Scenario: Event ordering is correct (TOOL_CALL before TOOL_RESULT)
    Given multiple tools are executed in sequence in the agentic loop
    When the agentic loop processes each tool
    Then for each tool, TOOL_CALL must be emitted before TOOL_RESULT
    And the call_id in TOOL_CALL must match the call_id in corresponding TOOL_RESULT
    And events must be emitted in the same order as tool execution
# Validates: Requirements 6.1, 6.2, 6.3