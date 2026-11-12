Feature: The Circuit Breaker (LLM Outage Shield)
  Goal: Prevent the system from rapid-failing all tasks when an external API goes down.

  # The trip scenario this file used to open with drove three workers through a
  # gateway that returned ErrLLMUnreachable. After T-070 a task that reaches
  # exec.turn runs on an external agent and agentd makes no provider request of
  # its own for it, so an unreachable provider can no longer be produced from the
  # exec path; the global breaker is fed by the L1 calls agentd still makes and
  # is pinned by outage_trip_handoff_test.go. The probe below is an ACP task, so
  # its completion must not be read as provider recovery.

  Scenario: A half-open probe without a provider request does not close the breaker
    Given the Circuit Breaker is in the OPEN state
    And the configured timeout period has elapsed
    When the Daemon ticks
    Then the Circuit Breaker should transition to HALF_OPEN
    And the Daemon should pick exactly 1 READY task to test the connection
    When that test task succeeds
    Then the Circuit Breaker should not close