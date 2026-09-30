# B-002: `make dev-up` rewrites web/package-lock.json

| Field | Value |
| --- | --- |
| Type | bug |
| Status | backlog |
| Priority | P3 |
| Sprint | backlog |
| Severity | minor |
| Links | devenv/compose.yaml (web service) |

## Symptoms

After `make dev-up`, `git status` shows `web/package-lock.json` modified
(about 1100 lines removed). It was committed by accident once via `git add -A`.

## Repro

1. Clean tree. 2. `make dev-up`. 3. `git status --short`.

## Expected

Starting the dev stack leaves the working tree clean.

## Actual

The web service runs `npm install --legacy-peer-deps` against the bind-mounted
`../web`, which rewrites the lockfile.

## Notes

- Options: `npm ci`, or mount `node_modules` and the lockfile read-only.
