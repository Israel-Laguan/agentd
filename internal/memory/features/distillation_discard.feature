Feature: Distillation Discard Rule
  Goal: Empty or junk extractions are discarded, not stored (Danger A mitigation).

  Scenario: Empty extraction is discarded
    Given a completed task with 3 events in the store
    And the AI gateway returns an empty symptom and solution
    When the librarian curates the task
    Then no memory should be recorded
    And a MEMORY_DISCARDED event should be emitted
    And the events should be marked as curated

  Scenario: Junk extraction is discarded
    Given a completed task with 3 events in the store
    And the AI gateway returns junk tokens "N/A" and "none"
    When the librarian curates the task
    Then no memory should be recorded
    And a MEMORY_DISCARDED event should be emitted
