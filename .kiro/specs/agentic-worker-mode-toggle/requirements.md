# Requirements Document

## Introduction

This document specifies the requirements for adding an **agentic worker mode toggle** to the agentd system. The toggle allows operators to opt-in to an inner agentic loop (tool calling with accumulated messages inside `Worker.Process`) while keeping the default single-shot JSON worker unchanged.

The toggle ensures **no silent behavior change** for existing deployments—legacy behavior remains the default.

## Glossary

- **Worker**: The component that processes tasks from the queue, located in `internal/queue/worker/worker.go`.
- **AgentProfile**: A configuration entity that defines a model/provider pair, stored in `internal/models/entities.go`.
- **Legacy Mode**: The default worker behavior that uses `GenerateJSON[workerResponse]` with a single sandbox run.
- **Agentic Mode**: The new worker behavior that uses an inner loop with tool calling, message accumulation, and multiple sandbox executions.
- **AgenticMode**: The boolean field in `AgentProfile` that enables agentic mode when set to true.

## Requirements

### Requirement 1: Agentic Mode Flag in AgentProfile

**User Story:** As an operator, I want to configure agentic mode per agent profile, so that I can selectively enable the inner loop for specific profiles.

#### Acceptance Criteria

1. THE AgentProfile SHALL have an `AgenticMode` boolean field to enable agentic worker behavior.
2. THE Worker.Process method SHALL check `profile.AgenticMode` to determine the execution path.
3. WHEN `AgenticMode` is false (default), THE Worker SHALL execute the legacy single-shot JSON command path.
4. WHEN `AgenticMode` is true, THE Worker SHALL execute the agentic inner loop path via `processAgentic`.

### Requirement 2: Legacy Path Unchanged by Default

**User Story:** As a deployment operator, I want the default behavior to remain unchanged, so that existing deployments continue working without modification.

#### Acceptance Criteria

1. THE default value of `AgenticMode` SHALL be false.
2. THE zero value of AgentProfile SHALL result in legacy mode execution.
3. WHEN a task is processed with `AgenticMode` set to false, THE Worker SHALL use `GenerateJSON[workerResponse]` to obtain a command.
4. WHEN a task is processed with `AgenticMode` set to false, THE Worker SHALL execute exactly one sandbox run.

### Requirement 3: Agentic Mode Routing

**User Story:** As a developer, I want the worker to route to the agentic entry point when enabled, so that I can implement the inner loop in task 07.

#### Acceptance Criteria

1. WHEN `AgenticMode` is true AND the provider supports agentic mode, THE Worker SHALL call `processAgentic`.
2. WHEN `AgenticMode` is true AND the provider does not support agentic mode, THE Worker SHALL log a warning and fall back to legacy mode.
3. THE providerSupportsAgentic method SHALL return true only for providers that support tool round-tripping (currently OpenAI).

### Requirement 4: Provider Capability Check

**User Story:** As a system operator, I want the worker to handle cases where agentic mode is enabled but the provider doesn't support it, so that the system degrades gracefully.

#### Acceptance Criteria

1. IF `AgenticMode` is true AND the provider is not OpenAI, THEN THE Worker SHALL log a warning message.
2. IF `AgenticMode` is true AND the provider is not OpenAI, THEN THE Worker SHALL fall back to legacy mode execution.

### Requirement 5: ProcessAgentic Entry Point

**User Story:** As a developer, I want a stub entry point for the agentic loop, so that task 07 can implement the full orchestration.

#### Acceptance Criteria

1. THE Worker SHALL have a `processAgentic` method that can be called when agentic mode is enabled.
2. THE `processAgentic` method SHALL receive the context, task, project, and profile as parameters.
3. THE `processAgentic` method SHALL be implemented to handle the inner loop of tool execution.
4. UNTIL task 07 is implemented, THE `processAgentic` method SHALL return an appropriate not-implemented response or error.

### Requirement 6: Integration Test Regression

**User Story:** As a QA engineer, I want existing integration tests to pass, so that the toggle doesn't break current functionality.

#### Acceptance Criteria

1. THE existing test suite SHALL pass without modification when `AgenticMode` is false (default).
2. WHEN `AgenticMode` is set to true in a test, THE test SHALL verify that the agentic entry point is reached.
3. THE integration tests SHALL cover both the legacy path and the agentic path switch.

### Requirement 7: Configuration Surface

**User Story:** As an operator, I want the agentic mode to be configurable via the same mechanism as other profile flags, so that I can manage it consistently.

#### Acceptance Criteria

1. THE `AgenticMode` field SHALL be stored in the AgentProfile database model.
2. THE `AgenticMode` field SHALL be loadable via the existing profile loading mechanism.
3. THE configuration loading SHALL follow the same pattern as other profile boolean fields.

### Requirement 8: Documentation

**User Story:** As a developer, I want the flag documented in code comments, so that future maintainers understand the behavior.

#### Acceptance Criteria

1. THE `AgenticMode` field in the AgentProfile struct SHALL have a code comment explaining its purpose.
2. THE Worker.Process method SHALL have comments explaining the routing logic for agentic vs legacy mode.
3. THE long-form documentation in `docs/agentic-harness.md` SHALL be updated to reflect the toggle behavior.

## Notes

- The current implementation already has `AgentProfile.AgenticMode` as a boolean field.
- The current implementation already has routing logic in `Worker.Process` that checks `profile.AgenticMode`.
- The current implementation already has a `processAgentic` method that implements the full agentic loop.
- This requirements document validates that the existing implementation matches the expected behavior and ensures backward compatibility.
- Task 07 (worker inner loop orchestration) will fully implement the `processAgentic` method if not already complete.