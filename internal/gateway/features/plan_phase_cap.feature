Feature: Plan Phase Cap Enforcement
  Goal: Plans exceeding the configured task limit are deterministically
  trimmed with a continuation task so the Kanban board never receives
  an unmanageably large batch.

  Scenario: Oversized plan is trimmed to the phase cap
    Given the gateway max_tasks_per_phase is 3
    And the LLM returns a DraftPlan with 5 tasks
    When GeneratePlan processes the intent "build a large project"
    Then the returned DraftPlan should contain exactly 3 tasks
    And the first 2 tasks should match the original order
    And the 3rd task should be titled "Plan Phase 2"
    And the continuation task description should reference the remaining work

  Scenario: Plan within cap passes through unchanged
    Given the gateway max_tasks_per_phase is 7
    And the LLM returns a DraftPlan with 4 tasks
    When GeneratePlan processes the intent "build a small project"
    Then the returned DraftPlan should contain exactly 4 tasks
    And no task should be titled "Plan Phase 2"
