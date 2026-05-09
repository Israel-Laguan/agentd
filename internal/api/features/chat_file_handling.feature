Feature: Chat File Handling and Stashing
  Goal: Ensure oversized user content is stashed to disk and planning receives truncated file content.

  Scenario: Oversized user message is stashed and truncated for planning
    Given the API server is running with file stash and truncation
    When a client sends an oversized chat completion message
    Then the HTTP status code should be 200
    And the intent classifier should receive a file reference instead of inline content
    And the planner should receive truncated file content

  Scenario: Explicit file content references are passed through
    Given the API server is running with file stash and truncation
    When a client sends a chat completion request with an attached file "spec.txt"
    Then the HTTP status code should be 200
    And the intent classifier should receive the file name reference
    And the planner should receive the file content
