Feature: Per-Task Token Budget Enforcement
  Goal: Prevent runaway costs by hard-blocking requests once a task
  has consumed its allocated token budget (Danger A: Silent Wallet Drain).

  Scenario: Task blocked after exceeding its token budget
    Given a budget tracker with a cap of 1000 tokens
    And a mock provider that reports 800 tokens per call
    And a router wired with the budget tracker
    When a request is sent for task "task-1"
    Then the request should succeed
    And the recorded usage for "task-1" should be 800
    When a second request consuming 300 tokens is sent for task "task-1"
    Then the request should succeed
    And the recorded usage for "task-1" should be 1100
    When a third request is sent for task "task-1"
    Then the request should fail with ErrBudgetExceeded
    And the provider should not have been called for the third request

  Scenario: Independent tasks have independent budgets
    Given a budget tracker with a cap of 1000 tokens
    And a mock provider that reports 900 tokens per call
    And a router wired with the budget tracker
    When a request is sent for task "task-A"
    Then the request should succeed
    When a request is sent for task "task-B"
    Then the request should succeed

  Scenario: Requests without task ID bypass budget enforcement
    Given a budget tracker with a cap of 1 tokens
    And a mock provider that reports 9999 tokens per call
    And a router wired with the budget tracker
    When a request is sent without a task ID
    Then the request should succeed
