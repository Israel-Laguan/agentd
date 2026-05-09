Feature: Chat Timeout System Reply
  Goal: Ensure AI-core timeout failures return a deterministic system message instead of an error response.

  Scenario: LLM unreachable returns system timeout message
    Given the API server is running
    And the AI gateway returns ErrLLMUnreachable on plan generation
    When a client sends a chat completion request for "Build me a REST API"
    Then the HTTP status code should be 200
    And the response body should be a chat completion
    And the assistant content should be the system timeout message

  Scenario: Context deadline exceeded returns system timeout message
    Given the API server is running
    And the AI gateway returns DeadlineExceeded on intent classification
    When a client sends a chat completion request for "Build me a REST API"
    Then the HTTP status code should be 200
    And the response body should be a chat completion
    And the assistant content should be the system timeout message

  Scenario: Non-timeout gateway errors still return 500
    Given the API server is running
    And the AI gateway returns a non-timeout error on plan generation
    When a client sends a chat completion request for "Build me a REST API"
    Then the HTTP status code should be 500
