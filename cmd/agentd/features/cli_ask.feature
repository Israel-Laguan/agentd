Feature: The CLI Draft & Approve Loop
  Scenario: Approving a drafted plan via the CLI
    Given the internal API server is running on localhost
    When the CLI is executed with agentd ask "Build a node app"
    Then the CLI should print the proposed tasks
    And the CLI should prompt for approval
    When the user approves the plan
    Then the CLI should call the materialize endpoint
    And the CLI should print a project started message

  Scenario: Rejecting a drafted plan via the CLI
    Given the CLI has printed a drafted plan and is waiting for input
    When the user rejects the plan
    Then the CLI should not call the materialize endpoint
    And the CLI should exit gracefully
