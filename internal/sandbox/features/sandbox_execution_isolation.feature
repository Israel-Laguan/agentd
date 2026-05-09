Feature: Sandbox Execution & Isolation
  Goal: Ensure physical execution of bash commands is restricted to the correct workspace and monitored for deadlocks.

  Scenario: Safe execution and log streaming
    Given a WorkspacePath exists at "~/.agentd/projects/test-uuid/"
    And an ExecutionPayload defines the command "echo \"hello world\""
    When the Sandbox executes the payload
    Then the events table should receive a new row with type "STDOUT" and payload "hello world"
    And the ExecutionResult should return ExitCode: 0
    And the ExecutionResult should return Success: true

  Scenario: Directory traversal attempt blocked
    Given a WorkspacePath exists at "~/.agentd/projects/test-uuid/"
    And the ExecutionPayload defines a malicious command "cat ../../../etc/passwd"
    When the Sandbox attempts to execute the payload
    Then the Execution should fail
    And the result should return an ErrSandboxViolation or a non-zero exit code due to permissions
    And the system logs should flag a directory escape attempt

  Scenario: Absolute path outside workspace is blocked
    Given a WorkspacePath exists at "~/.agentd/projects/test-uuid/"
    And the ExecutionPayload defines a malicious command "cat /etc/passwd"
    When the Sandbox attempts to execute the payload
    Then the Execution should fail with ErrSandboxViolation

  Scenario: Unsafe directory change is blocked
    Given a WorkspacePath exists at "~/.agentd/projects/test-uuid/"
    And the ExecutionPayload defines a malicious command "cd /tmp && ls"
    When the Sandbox attempts to execute the payload
    Then the Execution should fail with ErrSandboxViolation

  Scenario: Sudo command is blocked pre-flight
    Given a WorkspacePath exists at "~/.agentd/projects/test-uuid/"
    And the ExecutionPayload defines a malicious command "sudo apt-get install curl"
    When the Sandbox attempts to execute the payload
    Then the Execution should fail with ErrSandboxViolation
    And the result stderr should include "sudo command blocked"
