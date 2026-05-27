Feature: Provider Request Cancellation
  Goal: Ensure provider requests honor cfg.Timeout and propagate context
  cancellation so slow local models do not hog resources indefinitely
  (Danger C: Local Model Timeout / Resource Hogging).

  Scenario: OpenAI-adapter request cancelled when cfg.Timeout expires
    Given provider "synth-openai" using the openai adapter configured with a 10ms timeout
    And the mock server delays responses by 200ms
    When a request is sent to provider "synth-openai"
    Then the request should fail with ErrLLMUnreachable

  Scenario: Ollama-adapter request cancelled when cfg.Timeout expires
    Given provider "synth-ollama" using the ollama adapter configured with a 10ms timeout
    And the mock server delays responses by 200ms
    When a request is sent to provider "synth-ollama"
    Then the request should fail with ErrLLMUnreachable

  Scenario: Zero timeout does not enforce a deadline
    Given provider "synth-openai" using the openai adapter configured with a 0ms timeout
    And the mock server responds immediately
    When a request is sent to provider "synth-openai"
    Then the request should succeed with content "fast"
