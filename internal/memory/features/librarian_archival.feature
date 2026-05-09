Feature: Librarian Two-Phase Log Archival
  Goal: Archive completed task logs, summarize via map-reduce, and record durable memories.

  Scenario: Curating a completed task with events
    Given a completed task with 3 events in the store
    And the AI gateway is available
    When the librarian curates the task
    Then a LOG_ARCHIVED event should be emitted
    And a durable memory should be recorded with symptom and solution
    And a MEMORY_INGESTED event should be emitted
    And the events should be marked as curated

  Scenario: Breaker-open fallback extracts head and tail
    Given a completed task with 3 events in the store
    And the circuit breaker is open
    When the librarian curates the task
    Then a LOG_ARCHIVED event should be emitted
    And a durable memory should be recorded using fallback extraction
    And the events should be marked as curated

  Scenario: Task with no events is skipped
    Given a completed task with 0 events in the store
    When the librarian curates the task
    Then no LOG_ARCHIVED event should be emitted
    And no memory should be recorded

  Scenario: Curated events are purged after archive grace expires
    Given a completed task with 3 events in the store
    And the AI gateway is available
    When the librarian curates the task
    And stale archives are cleaned and events purged
    Then an EVENTS_PURGED event should be emitted
