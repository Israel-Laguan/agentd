Feature: Self-Healing Parameter Tuning
  Goal: Retry failing tasks through a deterministic healing ladder before human handoff.

  Scenario: First retry lowers temperature
    Given a task that has failed 1 time with self-healing enabled
    When the worker applies the healing ladder for retry 1
    Then the healing action should be "tune" with step "lower_temperature"
    And a TUNE event should be emitted

  Scenario: Healing ladder escalates through steps
    Given a task that has failed 4 times with self-healing enabled
    When the worker applies the healing ladder for retry 4
    Then the healing action should be "tune" with step "upgrade_model"

  Scenario: Split escalation triggers task breakdown
    Given a task that has failed 5 times with self-healing enabled
    When the worker applies the healing ladder for retry 5
    Then the healing action should be "split" with step "split_task"

  Scenario: Healing exhaustion creates HUMAN handoff
    Given a task that has failed 6 times with self-healing enabled
    When the worker applies the healing ladder for retry 6
    Then the healing action should be "human" with step "human_handoff"
