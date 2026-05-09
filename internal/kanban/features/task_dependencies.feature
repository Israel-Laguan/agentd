Feature: Task dependency scheduling
  Scenario: Completing a prerequisite unlocks dependent tasks
    Given a draft plan with tasks A, B, C, D, and E
    And B and C depend on A
    And D depends on B and C
    When the plan is materialized
    Then tasks A and E should be ready
    And tasks B, C, and D should be pending
    When task A is completed
    Then tasks B and C should be claimable

  Scenario: Worker breaks down a complex task into sub-tasks
    Given a draft plan with tasks A
    When the plan is materialized
    And task A is blocked with subtasks B and C
    Then tasks A should be blocked
    And tasks B and C should be ready
    When task B is completed
    Then tasks A should be blocked
    When task C is completed
    Then tasks A should be ready

  Scenario: Reject cyclic dependency edge at runtime
    Given a draft plan with tasks A, B, and C
    And B depends on A
    And C depends on B
    When the plan is materialized
    And a dependency edge from C to A is tested for cycles
    Then the operation should return a CircularDependencyError
