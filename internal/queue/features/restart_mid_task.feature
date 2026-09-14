Feature: Restart mid-task recovery (Beat 1)
  Goal: After an unclean daemon kill, boot reconcile must not leave tasks stuck RUNNING.
  See docs/harness-reliability.md Beat 1.

  Scenario: Unclean kill leaves RUNNING with dead PID; boot returns READY
    Given the database contains a task in the RUNNING state with os_process_id: 99999
    And the operating system confirms that PID 99999 does not exist
    When the agentd start command initiates the Boot Sequence
    Then the Ghost Reconciler should detect the discrepancy
    And the task should be reverted to the READY state
    And the os_process_id should be set to NULL
    And a system event log should be recorded stating "Recovered Ghost Task"

  Scenario: Alive worker PID is not reset on boot
    Given the database contains a task in the RUNNING state with os_process_id: 1234
    And the operating system confirms that PID 1234 is actively running
    When the agentd start command initiates the Boot Sequence
    Then the task should remain in the RUNNING state
    And the Ghost Reconciler should not modify the task

  Scenario: Continuous loop resets missing PID even with fresh heartbeat
    Given a running task with a recent heartbeat
    And the OS PID for the task is not alive
    When the heartbeat reconciliation loop runs
    Then the task should be reset to READY
