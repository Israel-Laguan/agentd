Feature: Adaptive Backoff Polling
  Goal: Reduce idle database polling by doubling the dispatch interval when no tasks are available.

  Scenario: Polling interval doubles when no tasks are available
    Given the base poll interval is 1s and the ceiling is 8s
    When the Daemon dispatches and finds 0 tasks 3 times in a row
    Then the polling intervals should be 2s, 4s, 8s

  Scenario: Polling interval resets on a successful claim
    Given the base poll interval is 1s and the ceiling is 16s
    And the Daemon has backed off to 4s
    When the Daemon dispatches and claims 1 task
    Then the polling interval should reset to the base 1s

  Scenario: Polling interval caps at the configured ceiling
    Given the base poll interval is 1s and the ceiling is 3s
    When the Daemon dispatches and finds 0 tasks 10 times in a row
    Then the polling interval should be 3s
