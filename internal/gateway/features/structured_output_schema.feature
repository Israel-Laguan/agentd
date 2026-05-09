Feature: Structured Output Schema Enforcement
  Goal: Ensure AI responses are validated against a schema after JSON parsing.
  Missing required fields trigger the same self-correction loop as parse errors
  (Flow 6.4: Structured Output Enforcement).

  Scenario: Missing required field triggers correction then succeeds
    Given a mock gateway returns invalid-schema JSON on the first call
    And the mock gateway returns valid-schema JSON on the second call
    When GenerateJSON is called with a validatable type
    Then the result should contain the corrected values
    And the gateway should have been called exactly 2 times

  Scenario: Stubbornly invalid schema fails after max retries
    Given a mock gateway returns invalid-schema JSON on all 3 calls
    When GenerateJSON is called with a validatable type
    Then the error should be ErrInvalidJSONResponse

  Scenario: JSON parse error followed by schema error then success
    Given a mock gateway returns broken JSON on the first call
    And the mock gateway returns invalid-schema JSON on the second call
    And the mock gateway returns valid-schema JSON on the third call
    When GenerateJSON is called with a validatable type
    Then the result should contain the corrected values
    And the gateway should have been called exactly 3 times
