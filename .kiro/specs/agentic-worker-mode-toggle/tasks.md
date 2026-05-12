# Implementation Plan: agentic-worker-mode-toggle

## Overview

This feature is already implemented in the codebase. The tasks below verify the implementation is correct and complete according to the requirements, focusing on test creation and verification.

## Tasks

- [x] 1. Verify AgenticMode field has proper documentation in AgentProfile
  - Read `internal/models/entities.go` around line 84-96
  - Check if `AgenticMode` field has a code comment explaining its purpose
  - Add comment if missing (per Requirement 8.1)
  - _Requirements: 8.1_

- [x] 2. Verify providerSupportsAgentic method has documentation
  - Read `internal/queue/worker/worker.go` around line 494-497
  - Check if method has comments explaining behavior
  - Add comment if missing for consistency
  - _Requirements: 8.2_

- [x] 3. Verify Worker.Process routing logic has comments
  - Read `internal/queue/worker/worker.go` lines 145-155
  - Confirm routing logic for agentic vs legacy mode is documented
  - _Requirements: 8.2_

- [x] 4. Create Gherkin feature test for agentic mode toggle
  - Create `internal/queue/worker/features/agentic_mode_toggle.feature`
  - Feature: Agentic mode toggle controls worker behavior
  - Scenarios:
    - Given agentic mode is disabled (default), when worker processes task, then use legacy JSON command path
    - Given agentic mode is enabled and provider is OpenAI, when worker processes task, then use agentic loop with tools
    - Given agentic mode is enabled but provider is not OpenAI, when worker processes task, then fall back to legacy mode with warning
  - Create step implementations in `internal/queue/worker/worker_feature_test.go`
  - _Requirements: 1, 2, 3, 4, 6.2_

- [x] 5. Write unit tests for Worker.Process routing logic
  - Create `internal/queue/worker/worker_routing_test.go`
  - Tests:
    - `TestProcess_RoutesToLegacyWhenAgenticModeFalse`
    - `TestProcess_RoutesToAgenticWhenAgenticModeTrueAndProviderSupports`
    - `TestProcess_FallsBackToLegacyWhenAgenticModeTrueAndProviderNotSupported`
    - `TestProviderSupportsAgentic_ReturnsTrueForOpenAI`
    - `TestProviderSupportsAgentic_ReturnsFalseForOtherProviders`
  - _Requirements: 1, 3, 4, 6.2_

- [x] 6. Write unit tests for processAgentic entry point
  - Add tests in `internal/queue/worker/worker_agentic_test.go`
  - Tests:
    - `TestProcessAgentic_CreatesToolExecutor`
    - `TestProcessAgentic_BuildsAgenticMessages`
    - `TestProcessAgentic_CallsGatewayWithTools`
    - `TestProcessAgentic_ExecutesToolCalls`
    - `TestProcessAgentic_CommitsTextWhenNoToolCalls`
  - _Requirements: 5, 6.2_

- [x] 7. Write unit test for AgentProfile with AgenticMode
  - Create `internal/models/agent_profile_test.go`
  - Tests:
    - `TestAgentProfile_DefaultAgenticModeIsFalse`
    - `TestAgentProfile_AgenticModeCanBeSet`
  - _Requirements: 2, 7_

- [x] 8. Run Gherkin feature tests
  - Run: `go test -v ./internal/queue/worker/... -run "Feature: agentic mode toggle"`
  - Confirm all feature scenarios pass
  - _Requirements: 6.1, 6.2_

- [x] 9. Run unit tests
  - Run: `go test -v ./internal/queue/worker/... -run "TestProcess|TestProviderSupportsAgentic|TestAgentProfile"`
  - Confirm all unit tests pass
  - _Requirements: 6.1, 6.2_

- [x] 10. Run broader test suite to ensure no regressions
  - Run: `go test ./internal/queue/...`
  - Confirm all worker tests pass (25+ tests)
  - _Requirements: 6.1_

- [x] 11. Verify documentation in docs/agentic-harness.md is current
  - Read `docs/agentic-harness.md`
  - Confirm it describes the toggle behavior (AgenticMode flag)
  - No update needed if it references the toggle correctly
  - _Requirements: 8.3_

- [x] 12. Verify fallback handling for unsupported providers
  - Confirm code logs warning when agentic mode enabled but provider doesn't support it
  - Current implementation in worker.go lines 149-154
  - _Requirements: 3.2, 4.1, 4.2_

- [x] 13. Checkpoint - Review all verification results
  - Ensure all tasks above show verification complete
  - Ask user if questions arise before proceeding

## Notes

- This feature is already implemented; tasks focus on test creation and verification
- The default `AgenticMode = false` ensures legacy behavior is unchanged (Requirement 2)
- Only OpenAI provider supports agentic mode (Requirement 3.3)
- When agentic mode is enabled but provider doesn't support it, system falls back to legacy mode with warning log
- Gherkin feature tests follow the pattern in `internal/api/features/` and `cmd/agentd/features/`