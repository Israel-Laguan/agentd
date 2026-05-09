Feature: User Preferences
  Goal: Human preferences are stored and injected into chat (Flow 5.4).

  Scenario: Saving a user preference
    Given a user "alice" sends preference "Always use tabs, not spaces"
    When the preference is stored
    Then a memory with scope "USER_PREFERENCE" should exist
    And the memory should contain "Always use tabs, not spaces"

  Scenario: Preferences appear in recall
    Given a stored preference for user "alice" with text "Keep updates brief"
    When the retriever recalls for user "alice"
    Then the recall should include "Keep updates brief"
