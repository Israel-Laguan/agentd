# T-040: Real-browser e2e tier for the web UI

| Field | Value |
| --- | --- |
| Type | task |
| Status | ready |
| Priority | P3 |
| Sprint | S16-real-browser-tier |
| Parent | none |
| Estimate | M (feasibility check first; re-estimate after it) |
| Links | docs/testing/journeys.md (J06), .github/workflows/ci-e2e.yml, devenv/compose.yaml, Makefile (`dev-up`, `test-e2e`), web/package.json |

## Goal

Prove what J06's component tests cannot: CSS layout and click-through in a real browser against the running stack. J06 covers the task drawer with vitest + jsdom (`web/app/components/task/task-event-list.test.tsx`, `task-drawer.test.tsx`), which renders components and simulates events, but has no layout engine and no real page navigation.

Until S15 this was an accepted gap held open as bug B-027 ("a product decision, never done"), restated as out of scope in S09, S10 and S11. S15 reclassified it: it is test infrastructure to build, not a bug, so it moved here and B-027 was deleted.

## Concern: Playwright under rootless podman (check this first)

Whether Playwright runs well in this project's CI is unknown, and it gates the whole task. The stack is podman-only: `devenv/compose.yaml` (via `make dev-up`) must work on rootless podman-compose, and Docker Engine is not a target. Playwright's docs and its official container image mostly assume Docker. Specific risks:

- **Where the browser runs.** On the Ubuntu runner directly (`npx playwright install --with-deps`) or in Playwright's image under podman. If it runs in a container, it must reach the stack's published ports, which differs between rootless podman networking and Docker.
- **Runner time and size.** `ci-e2e.yml` already takes about five minutes of test time plus image builds, and runs weekly, on demand, and on PRs touching `test/e2e/**` or `devenv/**`. Browser downloads (hundreds of MB) and browser startup add to that. Caching and running a single browser (Chromium) matter.
- **Version pinning.** Playwright and its browsers must be pinned together, the way `ci-e2e.yml` pins `podman-compose==1.3.0`, or a release can break CI unannounced.
- **Flakiness.** Real-browser tests against a live stack are the flakiest tier. Each flow needs explicit waits on UI state, not sleeps, or the tier will be skipped in practice.

If the check shows Playwright cannot run reliably in the podman CI job at reasonable cost, stop and record why in `journeys.md`. Run it locally only, or drop the tier; that is a product call for the owner, not something to force.

## Done when

- [ ] **Feasibility check (first, timeboxed to about half a day):** one trivial Playwright test opens the web UI from `make dev-up` and passes in a `ci-e2e.yml` run on GitHub Actions, with versions pinned. Record the runner-time cost. If it fails, stop as described above.
- [ ] 2–3 core UI flows covered in a real browser, for example: open a project board, open a task drawer and see its event log, follow a task to its result. Choose them by what jsdom cannot prove (layout, navigation).
- [ ] Runs in `ci-e2e.yml` with the existing triggers, plus PRs touching `web/**` if the runtime allows; documented in `docs/testing/journeys.md`.
- [ ] `journeys.md` drops the "Explicit gap: no real-browser tier" note.
- [ ] **Fixture drift check** (same J06 area): the J06 fixtures are static snapshots of `GET /api/v1/tasks/{id}/events` and "do not detect later drift in the endpoint". Add a Go test that checks the handler's real response still has the fixture's shape, so an endpoint change fails CI instead of leaving the UI tests on stale data. This one is independent of Playwright and can land first.

## Notes

- No demo scripts: proofs are tests or merge gates (project rule).
- Keep the PR under 100 files and 1000 changed lines; the fixture-drift test can be its own PR.
- Origin: B-027 (S11, P3), retired in S15.
