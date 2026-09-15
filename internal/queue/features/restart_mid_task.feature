Feature: Restart mid-task recovery (Beat 1)
  Goal: After an unclean daemon kill, boot reconcile must not leave tasks stuck RUNNING.
  See docs/harness-reliability.md Beat 1.

  Ghost reconciliation scenarios are covered by ghost_reconciliation.feature
  and heartbeat_reconciliation.feature. This feature documents the Beat 1
  operator-visible contract without re-listing identical Given/When/Then steps.
