Feature: User-Editable Cron Schedule
  Goal: The daemon reads agentd.crontab for background job schedules.

  Scenario: Default crontab is created on init
    Given a fresh home directory
    When agentd init is executed
    Then an agentd.crontab file should exist in the home directory
    And the crontab should contain the default schedule entries

  Scenario: Existing user crontab is preserved on re-init
    Given a home directory with a custom agentd.crontab
    When agentd init is executed again
    Then the custom crontab should not be overwritten

  Scenario: Standard cron and @every entries are parsed
    Given a crontab with "@every 3s task-dispatch" and "*/10 * * * * disk-watchdog"
    When the crontab is loaded
    Then the task-dispatch interval should be 3 seconds
    And the disk-watchdog should have a standard cron schedule

  Scenario: Unknown job names are skipped
    Given a crontab with "@every 5s unknown-job"
    When the crontab is loaded
    Then no error should be returned
    And the default task-dispatch interval should be preserved
