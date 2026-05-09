Feature: Interactive Prompt Recovery
  Goal: Detect interactive prompts in timed-out sandbox output and recover when safe.

  Scenario: Detecting an interactive prompt after timeout
    Given a running task with a sandbox command "apt install nginx"
    And the sandbox times out with output containing "[y/N]"
    When the worker processes the timeout result
    Then a PROMPT_DETECTED event should be emitted
    And the worker should attempt recovery with a non-interactive flag

  Scenario: Allowlisted command recovery succeeds
    Given a running task with a sandbox command "apt install nginx"
    And the sandbox times out with output containing "[y/N]"
    And the recovered command succeeds on retry
    When the worker processes the timeout result
    Then the task should be completed successfully

  Scenario: Non-recoverable prompt creates HUMAN handoff
    Given a running task with a sandbox command "unknown-tool setup"
    And the sandbox times out with output containing "Are you sure"
    When the worker processes the timeout result
    Then the parent task should be BLOCKED
    And a HUMAN child task should be created with title "Manual action required: command waiting for input"
    And a PROMPT_HANDOFF event should be emitted

  Scenario: Normal timeout without prompt follows standard retry
    Given a running task with a sandbox command "make build"
    And the sandbox times out with no prompt-like output
    When the worker processes the timeout result
    Then no PROMPT_DETECTED event should be emitted
    And the task should follow the standard retry path
