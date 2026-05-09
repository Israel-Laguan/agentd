Feature: The Circuit Breaker (LLM Outage Shield)
  Goal: Prevent the system from rapid-failing all tasks when an external API goes down.

  Scenario: Tripping the circuit breaker on network failure
    Given the Circuit Breaker is in the CLOSED state
    And a mock AI Gateway is configured to return ErrLLMUnreachable
    When 3 separate workers fail their tasks due to this error
    Then the Circuit Breaker should transition to the OPEN state
    And the first outage tasks should be READY and the trip task should be handed to HUMAN
    And the Daemon should pause polling the database for new tasks

  Scenario: Half-Open recovery testing
    Given the Circuit Breaker is in the OPEN state
    And the configured timeout period has elapsed
    When the Daemon ticks
    Then the Circuit Breaker should transition to HALF_OPEN
    And the Daemon should pick exactly 1 READY task to test the connection
    When that test task succeeds
    Then the Circuit Breaker should transition to CLOSED
    And normal polling should resume
