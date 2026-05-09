Feature: Dispatch Goroutine Panic Safety
  Goal: Ensure a panic in the dispatch goroutine releases the semaphore slot and does not crash the daemon.

  Scenario: Panic in worker releases the semaphore slot
    Given the Daemon has 1 worker slot
    And the worker panics during task processing
    When the Daemon dispatches 1 READY task
    Then the semaphore slot should be released after the panic
    And no unrecovered panic should propagate to the Daemon
