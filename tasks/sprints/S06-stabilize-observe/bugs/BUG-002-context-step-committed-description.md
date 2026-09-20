# BUG-002: Context step reads unpopulated committed.Description

| Field | Value |
| --- | --- |
| Type | bug |
| Status | ready |
| Priority | P1 |
| Sprint | S06-stabilize-observe |
| Persona | contributor |
| Links | [S05 retro](../../../S05-tiered-integration/retro/RETRO.md) |

## Symptom

`parseAndConfigurePack` (context step, shipped in T-017) reads `committed.Description` for the pack JSON, which is never populated by any commit path in prod or test. 

## Expected Behavior

Read the context pack from the `RESULT` event, similar to how the verify step's LLM output parsing was fixed in S05.
