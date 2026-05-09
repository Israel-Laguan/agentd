Feature: Event Bus Streaming (The Pulse)
  Goal: Ensure that execution logs are broadcasted to the rest of the application (for SSE) in real-time.

  Scenario: Publish and subscribe to live execution logs
    Given the internal EventBus is initialized
    And a subscriber channel is listening for events on project_id: 123
    When the Sandbox executes a command that prints 3 separate lines to stdout with 1-second delays
    Then the events table should record 3 separate LOG_CHUNK entries
    And the subscriber channel should receive the 3 events sequentially, exactly as they are printed
    And the subscriber should not have to wait for the command to finish to receive the first line
