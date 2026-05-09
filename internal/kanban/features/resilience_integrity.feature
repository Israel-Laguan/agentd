Feature: System resilience and integrity
  Scenario: Recover ghost tasks after a crash
    Given a task in the database is marked RUNNING with os_process_id 999
    And the operating system reports that PID 999 does not exist
    When the daemon runs ReconcileGhostTasks on startup
    Then the task state should be reset to READY
    And a RECOVERY event should be logged for that task

  Scenario: Cascade delete project
    Given a project exists with 10 tasks and 50 events
    When I delete the project from the projects table
    Then all associated tasks should be deleted from the tasks table
    And all associated events should be deleted from the events table
    And all associated relations should be deleted from the task_relations table
