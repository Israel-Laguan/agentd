Feature: Context Truncation Strategies
  Goal: Ensure the gateway enforces input budgets and supports multiple truncation policies.

  Scenario: Middle-out truncation keeps head and tail
    Given a message with 1000 characters of content
    And the truncation strategy is "middle_out"
    When truncation is applied with a budget of 200 characters
    Then the output should be at most 200 characters
    And the output should start with the first portion of the original
    And the output should end with the last portion of the original
    And the output should contain the truncation marker

  Scenario: Head-tail truncation with configurable ratio
    Given a message with 1000 characters of content
    And the truncation strategy is "head_tail" with head ratio 0.8
    When truncation is applied with a budget of 200 characters
    Then the output should be at most 200 characters
    And the head portion should be larger than the tail portion
    And the output should contain the truncation marker

  Scenario: Reject policy blocks oversized messages
    Given a message with 1000 characters of content
    And the truncation policy is "reject"
    When truncation is applied with a budget of 200 characters
    Then the truncation should return ErrContextBudgetExceeded

  Scenario: Messages within budget pass through unchanged
    Given a message with 100 characters of content
    And the truncation strategy is "middle_out"
    When truncation is applied with a budget of 200 characters
    Then the output should equal the original content
