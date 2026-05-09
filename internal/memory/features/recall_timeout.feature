Feature: Memory Recall Timeout Fallback
  Goal: Chat proceeds without context when CortexDB is slow (Danger B mitigation).

  Scenario: Slow database returns no memories within timeout
    Given the memory store hangs indefinitely
    When the retriever recalls with a 50ms timeout
    Then the recall should return empty
    And the recall should complete within 1 second
