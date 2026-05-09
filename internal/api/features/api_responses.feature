Feature: Unified API Responses
  Scenario: Standardized successful response
    Given the API server is running
    When a client sends a GET request to "/api/v1/projects"
    Then the HTTP status code should be 200
    And the JSON response should contain status "success"
    And the JSON response should contain a data array
    And the JSON response should contain pagination meta

  Scenario: Standardized error response
    Given the API server is running
    When a client sends a GET request to "/api/v1/projects/invalid-id"
    Then the HTTP status code should be 404
    And the JSON response should contain status "error"
    And the JSON response should contain error code "NOT_FOUND"
