Feature: Comment intake
  Scenario: Human comment becomes follow-up tasks
    Given a task is IN_CONSIDERATION after a human comment
    When the daemon comment intake loop processes the comment
    Then the gateway should convert the comment into a plan
    And the new tasks should be linked under the original task
    And the original task should return to READY for system execution
