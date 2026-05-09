Feature: Dream Agent Memory Consolidation
  Goal: Redundant memories are merged during the dreaming cycle (Flow 5.3).

  Scenario: Five similar memories are merged into one
    Given 5 memories about "Fix CORS error by adding Header A"
    And the AI gateway returns a merged summary
    When the dream agent runs
    Then only 1 unsuperseded memory should remain
    And the 5 original memories should be superseded
