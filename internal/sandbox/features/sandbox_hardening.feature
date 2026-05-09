Feature: Sandbox Hardening Guardrails
  Goal: Ensure Box 4 can execute commands safely without leaking secrets or exhausting host resources.

  Background:
    Given a WorkspacePath exists at "~/.agentd/projects/test-uuid/"
    And the Sandbox config includes:
      | inactivity_timeout | 60s     |
      | wall_timeout       | 10m     |
      | kill_grace         | 2s      |
      | max_log_bytes      | 5242880 |

  Scenario: Secrets are scrubbed before persistence and result return
    Given an ExecutionPayload defines the command "echo 'sk-abc123abc123abc123abc123abc123abc123abc1'"
    When the Sandbox executes the payload
    Then each emitted "LOG_CHUNK" should mask secrets as "[REDACTED]"
    And the ExecutionResult stdout should not include the original secret token
    And the ExecutionResult stdout should include "[REDACTED]"

  Scenario: Excessive output is capped with truncation marker
    Given an ExecutionPayload defines the command "for i in $(seq 1 100000); do echo Hello; done"
    When the Sandbox executes the payload
    Then the ExecutionResult stdout length should be capped near "sandbox.max_log_bytes"
    And the ExecutionResult stdout should include "[... N bytes truncated ...]"

  Scenario: Disallowed absolute path access is blocked
    Given an ExecutionPayload defines the command "cat /etc/passwd"
    When the Sandbox attempts to execute the payload
    Then the Execution should fail with ErrSandboxViolation
    And the system logs should flag a directory escape attempt

  Scenario: Home directory references are blocked
    Given an ExecutionPayload defines the command "cat $HOME/.ssh/id_rsa"
    When the Sandbox attempts to execute the payload
    Then the Execution should fail with ErrSandboxViolation

  Scenario: Environment is allow-listed
    Given the parent process environment contains "OPENAI_API_KEY=secret"
    And the ExecutionPayload defines the command "env"
    When the Worker builds sandbox payload env vars
    Then only allow-listed keys should be forwarded
    And "OPENAI_API_KEY" should not be present in payload env vars
    And "CI=true" and "DEBIAN_FRONTEND=noninteractive" should be present

  Scenario: Resource limits are applied before command execution
    Given sandbox limits include:
      | address_space_bytes | 2147483648 |
      | cpu_seconds         | 600        |
      | open_files          | 1024       |
      | processes           | 256        |
    When the Sandbox builds the execution command on Unix
    Then the command should include ulimit guards for memory, CPU, open files, and process count
