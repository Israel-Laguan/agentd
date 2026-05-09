# Guardrails

This repository uses strict guardrails to keep code focused, testable, and maintainable.

## Size Limits

| Limit | Value | Rationale |
| --- | --- | --- |
| Function length | 60 lines max | Functions longer than 60 lines typically do more than one thing. Splitting improves readability and testability. Enforced by `funlen` and `revive` `function-length`. |
| Function statements | 40 statements max | Tight statement budget forces early extraction of helpers and keeps the call stack shallow. Enforced by `revive` `function-length`. |
| File size | 300 lines max | Large files accumulate hidden coupling. Splitting by behavior keeps packages navigable. Enforced by `revive` `file-length-limit` and `scripts/check_loc.py`. |
| Cyclomatic complexity | 15 max | High cyclomatic complexity correlates with defect density. Lower complexity makes branch coverage achievable. Enforced by `cyclop`. |

## Layer Boundaries

Packages in `internal/` follow inward-facing dependency rules:

```
 ┌──────────────────────────────────────────────────────────────┐
 │  Outward Layers (may import inward, never the reverse)      │
 │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐       │
 │  │   api    │ │ services │ │  kanban  │ │  config  │       │
 │  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘       │
 │       │             │            │             │             │
 │  ┌────┴─────┐ ┌────┴─────┐ ┌────┴─────┐ ┌────┴─────┐       │
  │  │   bus    │ │  memory  │ │  gateway │ │  queue   │       │
  │  └────┬─────┘ └────┬─────┘ └────┬─────┘ └────┬─────┘       │
  │       │             │            │             │             │
  │       └─────────────┴─────┬──────┴─────────────┘             │
  │                           │                                 │
  │                  ┌────────┴────────┐                        │
  │                  │     models      │  ← Core domain layer   │
  │                  │  (no outward    │    Pure data +         │
  │                  │   imports)      │    interfaces only     │
  │                  └─────────────────┘                        │
  └──────────────────────────────────────────────────────────────┘
```

- `internal/models` is the core domain layer. It must **never** import from outward layers.
- All other packages may import `models`, but `models` must not import them.
- Dependency direction always moves inward toward domain abstractions, never outward to delivery or infrastructure.

Current lint enforcement: `depguard` blocks imports from `internal/models` to outward layers.

## Local Quality Workflow

Run these checks before committing:

```sh
make lint        # static checks and architecture guardrails
make test        # race-enabled tests for internal/
make check       # loc + lint + test (full quality gate)
```

## Pre-commit Checklist

- [ ] `make check` passes with zero warnings.
- [ ] No new blanket linter suppressions (`//nolint`, `// revive:disable`).
- [ ] New functions are under 60 lines and 40 statements.
- [ ] New files are under 300 lines (or split by behavior).
- [ ] `internal/models` has no outward imports.
- [ ] Tests accompany behavior changes (unit tests first for pure logic).

## Engineering Practices

- **Split by behavior, not by size alone.** A 200-line file that does one thing is fine; a 100-line file doing three things needs splitting.
- **Prefer interfaces at boundaries.** Inject dependencies rather than constructing them inline. This is how `AIGateway`, `KanbanStore`, and `Truncator` are consumed.
- **Write tests alongside behavior changes.** Unit tests first for pure logic; integration/godog BDD tests for flows.
- **Keep linter warnings at zero.** If a lint rule fires, fix the code — do not suppress it globally.
