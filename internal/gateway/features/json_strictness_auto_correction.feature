Feature: JSON Strictness & Auto-Correction
  Goal: Ensure AI formatting hallucinations are caught and corrected internally before breaking the Go JSON parsers.

  Scenario: Auto-correcting malformed JSON
    Given an AIRequest specifies JSONMode: true
    And the mock LLM returns JSON with a trailing comma on the first attempt
    And the mock LLM returns minimal valid JSON on the second attempt
    When the Gateway processes the request
    Then the Gateway should detect the JSON syntax error internally
    And the Gateway should automatically submit a retry prompt asking the LLM to fix the error
    And the final AIResponse should contain the valid JSON string from the second attempt
    And the calling Go function should receive no errors

  Scenario: Failing gracefully after maximum retries
    Given an AIRequest specifies JSONMode: true
    And the mock LLM returns JSON with a trailing comma on three consecutive attempts
    When the Gateway processes the request
    Then the Gateway should stop retrying after the maximum retry limit
    And the Gateway should return a specific ErrInvalidJSONResponse
    And the raw, broken output should be included in the error details
