Feature: Conflict resolution
  Scenario: Human comment blocks agent completion
    Given Task "123" is in RUNNING state
    When a Human adds a comment to Task "123"
    Then the state should change to IN_CONSIDERATION
    And if the Agent subsequently tries to call UpdateTaskResult for Task "123"
    Then the database update should fail with a StateConflictError
    And the task state should remain IN_CONSIDERATION
