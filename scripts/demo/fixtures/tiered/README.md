# Tiered demo fixtures

These fixtures feed `scripts/demo/tiered-harness.sh`, which only measures
byte size (`wc -c`) of these files as a token-count proxy — it never
parses them into `internal/models` structs. `task.json`'s field names
(`acceptance_criteria`, `complexity_score`, `target_domains`) are
illustrative-only and are not bound to any Go model schema (compare
`models.Task.SuccessCriteria` and `ComplexityScorer`'s 0-10 scale).
