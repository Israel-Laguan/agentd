Feature: Network Outage Handoff
  Goal: Create a HUMAN diagnostic task when the LLM circuit breaker remains open beyond the configured threshold.

  Scenario: Breaker open beyond threshold creates HUMAN task
    Given the circuit breaker has been open for longer than the handoff threshold
    When the daemon checks for outage handoff
    Then a HUMAN task titled "System Offline: Please check AI API connections." should exist under the _system project
    And an LLM_OUTAGE_HANDOFF event should be emitted

  Scenario: De-duplication prevents repeated HUMAN tasks
    Given the circuit breaker has been open for longer than the handoff threshold
    And an outage HUMAN task already exists under the _system project
    When the daemon checks for outage handoff again
    Then no additional outage task should be created

  Scenario: Breaker not open long enough skips handoff
    Given the circuit breaker has been open for less than the handoff threshold
    When the daemon checks for outage handoff
    Then no outage task should be created
