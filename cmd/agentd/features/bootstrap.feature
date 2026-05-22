Feature: Bootstrap Initialization and Startup
  Goal: The init and start commands initialize the local runtime reliably
  and surface actionable, human-readable errors on every known failure mode.

  Scenario: Fresh init creates all required runtime artifacts
    Given a fresh agentd home directory
    When the init command is run
    Then the home directory should exist
    And the database file should exist
    And the database should use WAL journal mode
    And the crontab file should exist
    And the default agent profiles should be seeded

  Scenario: Re-init is idempotent
    Given a fresh agentd home directory
    When the init command is run
    And the init command is run again
    Then the database file should exist
    And the default agent profiles should be seeded

  Scenario: Init with verbose flag prints the configuration summary
    Given a fresh agentd home directory
    When the init command is run with the verbose flag
    Then the output should contain "home="
    And the output should contain "db_path="

  Scenario: Start reports a human-readable hint when no LLM provider is configured
    Given a fresh agentd home directory
    And init has been run
    And no LLM provider API keys are configured
    When the start command is attempted
    Then the start command should have failed
    And the error should describe an LLM provider configuration problem

  Scenario: Start reports a human-readable hint when a data directory is not writable
    Given a fresh agentd home directory
    And init has been run
    And an OpenAI API key is configured
    And the home directory is read-only
    When the start command is attempted
    Then the start command should have failed
    And the error should describe a write permissions problem

  Scenario: Start reports a human-readable hint when the LLM warmup is rejected
    Given a fresh agentd home directory
    And init has been run
    And an OpenAI provider pointing to a mock server that returns 500 is configured
    When the start command is attempted
    Then the start command should have failed
    And the error should describe an LLM warmup problem

  Scenario: Start reports a human-readable hint when the API port is already in use
    Given a fresh agentd home directory
    And init has been run
    And an OpenAI provider pointing to a mock server that returns 200 is configured
    And the configured API port is already bound
    When the start command is attempted
    Then the start command should have failed
    And the error should describe an address binding problem
