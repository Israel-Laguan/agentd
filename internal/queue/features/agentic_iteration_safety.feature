Feature: Agentic Iteration Budget and Safety
  Goal: Prevent runaway inner loops by enforcing iteration caps,
  token budgets, and wall-clock deadlines in agentic mode.

  Background:
    Given the worker is configured with agentic mode enabled

  Scenario: Iteration limit reached without final response
    Given max tool iterations is set to 2
    And the gateway returns tool calls on each request
    When the agentic loop runs for 3 iterations
    Then the iteration guard should be exceeded
    And a final message should be injected
    And one additional call should be allowed

  Scenario: Token budget exceeded during inner loop
    Given token budget is set to 100 tokens
    And the gateway reports 60 tokens per call
    When the agentic loop makes 2 calls
    Then the second call should succeed (120 total, under 200 reserve)
    When the agentic loop makes a third call
    Then the request should fail with ErrBudgetExceeded

  Scenario: Task deadline expires before iteration
    Given the task context has a deadline 1 second in the past
    When the agentic loop attempts to start an iteration
    Then the request should fail with "deadline already expired"

  Scenario: Iteration limit not reached, normal completion
    Given max tool iterations is set to 10
    And the gateway returns no tool calls on the first request
    When the agentic loop runs
    Then the loop should complete successfully
    And the result should be committed

  Scenario: Multiple tasks have independent budgets
    Given token budget is set to 100 tokens
    When task "task-A" uses 80 tokens
    Then task "task-B" should still have full 100 token budget available
    And task "task-A" should be blocked from further calls
