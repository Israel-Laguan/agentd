Feature: Task Deadline Reaper
  Goal: Prevent permanent lockups by enforcing a wall-clock timeout on every dispatched worker.

  Scenario: Reaper cancels a stuck worker after the deadline
    Given the task deadline is set to 200ms
    And 1 READY task exists with a blocking sandbox
    When the Daemon dispatches the task
    Then the sandbox should start executing
    And the sandbox should be cancelled within the deadline
    And the semaphore slot should be released

  Scenario: Fast worker completes before the deadline
    Given the task deadline is set to 1m
    And 1 READY task exists with a fast sandbox
    When the Daemon dispatches the task
    Then the semaphore slot should be released
    And the task should be COMPLETED
