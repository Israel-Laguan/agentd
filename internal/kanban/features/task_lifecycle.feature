Feature: Task selection and lifecycle
  Scenario: Pick only READY tasks for the worker pool
    Given the database has 1 task in READY state
    And 1 task in PENDING state waiting on a dependency
    And 1 task in COMPLETED state
    When the Queue calls ClaimNextReadyTasks with a limit of 10
    Then only the 1 QUEUED task should be returned
    And its state in the database should now be QUEUED

  Scenario: Unlock dependent tasks on completion
    Given Task A is RUNNING
    And Task B is PENDING and depends on Task A
    When the Worker calls UpdateTaskResult for Task A with Success=true
    Then Task A should move to COMPLETED
    And Task B should automatically move from PENDING to READY
