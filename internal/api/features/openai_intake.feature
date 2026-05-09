Feature: OpenAI-Compatible Intake
  Scenario: Processing a standard chat completion request
    Given the API server is running
    When a client sends a chat completion request for "A Python script to scrape a website"
    Then the HTTP status code should be 200
    And the response body should be a chat completion
    And the first choice content should contain a DraftPlan JSON document

  Scenario: Multi-project request returns scope clarification
    Given the API server is running
    When a client sends a multi-scope chat completion request
    Then the HTTP status code should be 200
    And the response body should be a chat completion
    And the first choice content kind should be "scope_clarification"
    And GeneratePlan should not be called

  Scenario: Approved single scope plans only that project
    Given the API server is running
    When a client sends a chat completion request with approved scope "backend-api"
    Then the HTTP status code should be 200
    And the response body should be a chat completion
    And the first choice content should contain a DraftPlan JSON document

  Scenario: Multiple approved scopes are rejected
    Given the API server is running
    When a client sends a chat completion request with approved scopes "backend-api" and "frontend-ui"
    Then the HTTP status code should be 400
    And the JSON response should contain error code "BAD_REQUEST"

  Scenario: Status check returns board summary
    Given the API server is running
    When a client sends a status-check chat completion request
    Then the HTTP status code should be 200
    And the response body should be a chat completion
    And the first choice content kind should be "status_report"
    And GeneratePlan should not be called
    And AnalyzeScope should not be called

  Scenario: Ambiguous intent returns clarification
    Given the API server is running
    When a client sends an ambiguous chat completion request
    Then the HTTP status code should be 200
    And the response body should be a chat completion
    And the first choice content kind should be "intent_clarification"
    And GeneratePlan should not be called

  Scenario: Approved scope bypasses intent classification
    Given the API server is running
    When a client sends a chat completion request with approved scope "backend-api"
    Then the HTTP status code should be 200
    And the first choice content should contain a DraftPlan JSON document
    And ClassifyIntent should not be called
