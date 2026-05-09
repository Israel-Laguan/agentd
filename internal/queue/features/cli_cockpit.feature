Feature: CLI cockpit
  Scenario: Human controls the queue from terminal
    Given agentd has an initialized home directory
    When the user runs agentd status
    Then the command should print task state counts
    When the user runs agentd comment for a task
    Then a human comment should be added to that task
