Feature: MCP Capabilities
  Goal: Support MCP (Model Context Protocol) as external tool providers.
  MCP servers can expose tools that agentd can discover and call.

  Scenario: Registry aggregates tools from multiple MCP adapters
    Given a capability registry
    And an MCP adapter named "github" with tools "get_issue", "create_issue"
    And an MCP adapter named "filesystem" with tools "read_file", "write_file"
    When GetTools is called
    Then the result should contain 4 tools
    And the tools should include "github:get_issue"
    And the tools should include "github:create_issue"
    And the tools should include "filesystem:read_file"
    And the tools should include "filesystem:write_file"

  Scenario: Registry returns empty list when no adapters registered
    Given a capability registry with no adapters
    When GetTools is called
    Then the result should be empty

  Scenario: Tool calls are routed to correct adapter
    Given a capability registry
    And an MCP adapter named "github" that returns "issue #123" for "get_issue"
    When CallTool is called for adapter "github" tool "get_issue" with args {"id": "123"}
    Then the result should be "issue #123"

  Scenario: Adapter can be unregistered
    Given a capability registry
    And an MCP adapter named "temp" with tool "temporary_tool"
    When the adapter "temp" is unregistered
    And GetTools is called
    Then the result should not contain "temporary_tool"
