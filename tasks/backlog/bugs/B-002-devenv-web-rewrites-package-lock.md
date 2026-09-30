# B-002: `make dev-up` rewrites web/package-lock.json

| Field | Value |
| --- | --- |
| Type | bug |
| Status | fixed (2026-09-30) |
| Priority | P3 |
| Sprint | S07-e2e-journeys |
| Severity | minor |
| Links | devenv/compose.yaml (web service) |

## Fix (2026-09-30)

The web service ran `npm install --legacy-peer-deps` against the bind-mounted
`../web`, which rewrote the lockfile on every container start. It now runs
`npm ci --legacy-peer-deps`, which installs exactly what `package-lock.json`
pins and never rewrites it. Verified: `make dev-up` leaves `git status` clean
(only the compose change itself), and `git diff main -- web/package-lock.json`
is empty after removing the accidental lockfile commit (78781774) from branch
history via `git rebase --onto`.

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
