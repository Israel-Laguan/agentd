Feature: Phase Planning Task Cap
  Goal: Cap plan output and chain oversized plans through continuation tasks.

  Scenario: Plan Phase task is recognized
    Given a task titled "Plan Phase 2"
    Then it should be recognized as a phase-planning task

  Scenario: Non-planning task is not recognized
    Given a task titled "Build the API"
    Then it should not be recognized as a phase-planning task

  Scenario: Phase continuation increments the phase number
    Given a planning task titled "Plan Phase 2"
    When continuation tasks are retitled
    Then the next continuation should be titled "Plan Phase 3"
