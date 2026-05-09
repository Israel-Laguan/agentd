Feature: Worker execution handoff
  Scenario: Successful sandbox execution completes a task
    Given a queued task with an agent profile and project workspace
    When the worker executes a successful sandbox command
    Then the task should be marked completed
    And dependent tasks should be unlocked by the Kanban store
