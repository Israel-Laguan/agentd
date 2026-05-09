Feature: Project materialization
  Scenario: Successfully create a project with multiple tasks
    Given I have a valid DraftPlan with 3 tasks: "Init", "Build", "Test"
    And "Build" depends on "Init"
    And "Test" depends on "Build"
    When I call MaterializePlan with this plan
    Then a new project should be created in the projects table
    And 3 tasks should be created in the tasks table
    And the "Init" task should have the state READY
    And the "Build" and "Test" tasks should have the state PENDING
    And the task_relations table should have 2 entries mapping the dependencies

  Scenario: Prevent circular dependencies in a plan
    Given I have a DraftPlan where Task A depends on Task B
    And Task B depends on Task A
    When I call MaterializePlan
    Then the operation should return a CircularDependencyError
    And no records should be written to the projects or tasks tables
