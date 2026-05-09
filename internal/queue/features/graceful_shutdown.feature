Feature: Graceful Shutdown & Context Cancellation
  Goal: Ensure CTRL+C stops the daemon safely without leaving orphaned bash processes on the host machine.

  Scenario: Intercepting SIGTERM/SIGINT
    Given the Daemon is running
    And a Worker is currently running a Sandbox process
    When the Go application receives an OS Interrupt signal (SIGINT)
    Then the root context should be cancelled
    And the Sandbox should catch the context cancellation and send a SIGKILL to the sleep 100 process
    And the Go application should exit with code 0 only after the worker has safely released its resources
