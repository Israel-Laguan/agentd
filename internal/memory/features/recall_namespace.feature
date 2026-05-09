Feature: Memory Recall Namespace Isolation
  Goal: Ensure recalled memories respect project scope boundaries (Danger D mitigation).

  Scenario: Global and current-project memories are returned
    Given a memory tagged "GLOBAL" with symptom "global fix"
    And a memory tagged project "project_2" with symptom "proj2 fix"
    And a memory tagged project "project_1" with symptom "proj1 fix"
    When the retriever recalls for project "project_2"
    Then the recall should include "global fix"
    And the recall should include "proj2 fix"
    And the recall should NOT include "proj1 fix"

  Scenario: User preferences are included when user ID matches
    Given a user preference for user "alice" with text "use tabs"
    And a user preference for user "bob" with text "use spaces"
    When the retriever recalls for user "alice"
    Then the recall should include "use tabs"
    And the recall should NOT include "use spaces"
