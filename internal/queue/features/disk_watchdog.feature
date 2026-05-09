Feature: Disk Space Watchdog
  Goal: Create a HUMAN alert task when free disk space falls below the configured threshold.

  Scenario: Low disk space creates HUMAN alert
    Given the disk free percentage is below the configured threshold
    When the disk watchdog checks free space
    Then a HUMAN task titled "Disk space critical. Please run cleanup or expand storage." should exist under the _system project
    And a DISK_SPACE_CRITICAL event should be emitted

  Scenario: De-duplication prevents repeated disk alerts
    Given the disk free percentage is below the configured threshold
    And a disk alert HUMAN task already exists under the _system project
    When the disk watchdog checks free space again
    Then no additional disk alert task should be created

  Scenario: Sufficient disk space skips alert
    Given the disk free percentage is above the configured threshold
    When the disk watchdog checks free space
    Then no disk alert task should be created
