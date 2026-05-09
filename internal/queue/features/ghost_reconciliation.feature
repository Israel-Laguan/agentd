Feature: Startup ghost reconciliation
  Goal: Ensure the database matches the reality of the operating system after a hard crash or power loss.

  Scenario: Reverting dead tasks on boot
    Given the database contains a task in the RUNNING state with os_process_id: 99999
    And the operating system confirms that PID 99999 does not exist
    When the agentd start command initiates the Boot Sequence
    Then the Ghost Reconciler should detect the discrepancy
    And the task should be reverted to the READY state
    And the os_process_id should be set to NULL
    And a system event log should be recorded stating "Recovered Ghost Task"

  Scenario: Ignoring alive tasks on boot
    Given the database contains a task in the RUNNING state with os_process_id: 1234
    And the operating system confirms that PID 1234 is actively running
    When the agentd start command initiates the Boot Sequence
    Then the task should remain in the RUNNING state
    And the Ghost Reconciler should not modify the task
