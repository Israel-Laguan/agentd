Feature: Daemon Concurrency & Semaphore Control
  Goal: Ensure the Queue Daemon respects hardware limits and never spawns more workers than allowed.

  Scenario: Enforcing the worker limit
    Given the Queue Daemon is configured with a MaxWorkers limit of 2
    And the database contains 10 tasks in the READY state
    When the Daemon starts and processes the first tick
    Then exactly 2 tasks should transition to the QUEUED state
    And the internal semaphore should report 0 available slots
    And the next tick should ignore the remaining 8 tasks
    When 1 worker finishes its task and releases the slot
    Then the next tick should pick exactly 1 new task from the database
