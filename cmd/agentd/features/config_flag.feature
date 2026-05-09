Feature: Explicit Config File Flag
  Goal: Allow agentd to run with an explicit config file via --config.

  Scenario: Explicit config overrides environment variable
    Given a config file with "gateway.max_tasks_per_phase: 12"
    And the environment variable AGENTD_GATEWAY_MAX_TASKS_PER_PHASE is set to "5"
    When config is loaded with the explicit config file
    Then gateway.max_tasks_per_phase should be 12

  Scenario: Missing explicit config file returns an error
    Given a non-existent config file path
    When config is loaded with the explicit config file
    Then an error should be returned

  Scenario: Absent config keys fall through to environment
    Given a config file with "healing.enabled: true"
    And the environment variable AGENTD_GATEWAY_MAX_TASKS_PER_PHASE is set to "9"
    When config is loaded with the explicit config file
    Then gateway.max_tasks_per_phase should be 9
    And healing.enabled should be true
