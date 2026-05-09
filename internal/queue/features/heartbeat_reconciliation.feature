Feature: Continuous Heartbeat Reconciliation
  Goal: Periodically reconcile running tasks against OS PIDs and heartbeat timestamps.

  Scenario: Stale heartbeat resets task to READY
    Given a running task with a heartbeat older than the stale threshold
    And the OS PID for the task is not alive
    When the heartbeat reconciliation loop runs
    Then the task should be reset to READY
    And a HEARTBEAT_RECONCILE event should be emitted

  Scenario: Healthy task with fresh heartbeat is not modified
    Given a running task with a recent heartbeat
    And the OS PID for the task is alive
    When the heartbeat reconciliation loop runs
    Then the task should remain RUNNING
    And no HEARTBEAT_RECONCILE event should be emitted

  Scenario: Missing PID resets task even with fresh heartbeat
    Given a running task with a recent heartbeat
    And the OS PID for the task is not alive
    When the heartbeat reconciliation loop runs
    Then the task should be reset to READY
