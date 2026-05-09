Feature: Permission and Sudo Detection
  Goal: Block sudo commands before execution and hand off permission failures as HUMAN tasks.

  Scenario: Sudo command blocked before execution
    Given a task whose agent returns a command starting with "sudo"
    When the sandbox receives the command
    Then the sandbox should reject the command with ErrSandboxViolation
    And a SANDBOX_VIOLATION event should be emitted

  Scenario: Permission denied output creates HUMAN handoff
    Given a running task with a failed sandbox command
    And the sandbox output contains "Permission denied"
    When the worker processes the failed result
    Then a PERMISSION_DETECTED event should be emitted
    And the parent task should be BLOCKED
    And a HUMAN child task should be created with title "Manual action required: privileged command"
    And a PERMISSION_HANDOFF event should be emitted

  Scenario: Successful output containing permission text is not treated as failure
    Given a running task with a successful sandbox result
    And the sandbox output contains "Permission denied" in a non-error context
    When the worker processes the successful result
    Then no PERMISSION_DETECTED event should be emitted
    And the task should be completed successfully
