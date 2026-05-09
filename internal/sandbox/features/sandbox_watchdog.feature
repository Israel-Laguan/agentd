Feature: Sandbox Watchdog (Zombie Process Killing)
  Goal: Ensure scripts waiting for human input do not freeze the background daemon permanently.

  Scenario: Terminate a hanging process
    Given an ExecutionPayload defines the command "sleep 300"
    And the Sandbox is configured with a timeout of 2 seconds
    When the Sandbox executes the payload
    Then the Sandbox should wait exactly 2 seconds
    And the Sandbox should send SIGTERM to the process group (PGID)
    And the Sandbox should wait for kill grace duration
    And the Sandbox should send SIGKILL to the process group (PGID) if the process is still alive
    And the ExecutionResult should return TimedOut: true
    And the process "sleep 300" should no longer exist on the host OS process list

  Scenario: Process exits during SIGTERM grace period
    Given an ExecutionPayload defines the command "trap 'exit 0' TERM; sleep 60"
    And the Sandbox wall-timeout is 1 second
    And the Sandbox kill grace is 2 seconds
    When the Sandbox executes the payload
    Then the Sandbox should send SIGTERM to the process group (PGID)
    And the process should exit before hard kill escalation
    And the process should no longer exist on the host OS process list
