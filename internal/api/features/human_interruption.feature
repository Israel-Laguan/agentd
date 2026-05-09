Feature: Human Interruption API
  Scenario: Halting a running task via comment
    Given Task "123" is in the RUNNING state
    And a Sandbox worker is currently executing its payload
    When a client sends a comment "Stop, use python 3" to task "123"
    Then the HTTP status code should be 201
    And the database state for Task "123" should be IN_CONSIDERATION
    And the Sandbox worker should receive a cancellation signal
    And the task should not transition to COMPLETED or FAILED
