Feature: Poison pill eviction
  Goal: Ensure tasks that cause continuous agent/sandbox failures are isolated so they don't block the queue forever.

  Scenario: Evicting a failing task after max retries
    Given a task is in the READY state with a RetryCount of 2
    And the Maximum Retry Limit is set to 3
    When a worker processes the task and the Sandbox returns ExitCode: 1 (Failure)
    Then the task's RetryCount should increment to 3
    And the task should transition to the FAILED_REQUIRES_HUMAN state
    And the system should emit a POISON_PILL_HANDOFF event
    And the Daemon should never pick up this task again automatically
