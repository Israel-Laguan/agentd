# Style Guide

Go coding conventions for the `agentd` project.

## Import Ordering

Group imports in three sections, separated by blank lines:

1. Standard library
2. External dependencies
3. Internal packages (`agentd/internal/...`)

```go
import (
    "context"
    "errors"
    "fmt"

    "github.com/google/uuid"

    "agentd/internal/models"
    "agentd/internal/kanban"
)
```

## Error Handling

### Sentinel Errors

Define sentinel errors as package-level `var` blocks in an `errors.go` file:

```go
var (
    ErrTaskNotFound           = errors.New("task not found")
    ErrInvalidStateTransition = errors.New("invalid task state transition")
)
```

### Wrapping

Always wrap errors with `%w` and include a contextual prefix. Chain to the appropriate sentinel:

```go
// Wrap with sentinel + context
return fmt.Errorf("%w: project name is required", models.ErrInvalidDraftPlan)

// Wrap low-level error with sentinel
return nil, fmt.Errorf("%w: send request: %v", models.ErrLLMUnreachable, err)

// Aggregate multiple errors
return fmt.Errorf("%w: %v", models.ErrLLMUnreachable, errors.Join(errs...))
```

### Checking

Use `errors.Is` for sentinel checks, never string comparison:

```go
if errors.Is(err, models.ErrTaskNotFound) {
    // handle not found
}
```

## Naming Conventions

### Interfaces

Three patterns are used:

- **`er` suffix** for actor/doer interfaces: `EventSink`, `TaskCanceller`
- **`Contract` suffix** for domain boundary contracts: `KanbanBoardContract`, `AIGatewayContract`
- **Plain descriptive names** for service interfaces: `KanbanStore`, `Truncator`, `Executor`, `Bus`

### Compile-Time Interface Checks

Add a compile-time assertion for every implementation:

```go
var _ spec.AIGateway = (*Router)(nil)
var _ models.KanbanStore = (*Store)(nil)
```

### Packages

Package names are short, lowercase, single words: `models`, `kanban`, `gateway`, `queue`, `sandbox`, `memory`, `services`, `api`, `bus`, `config`.

Avoid `util`, `common`, or `helpers` packages. If a function doesn't belong in an existing package, it probably needs a new focused package.

## Function Signatures

`context.Context` is always the first parameter:

```go
func (s *Store) GetTask(ctx context.Context, id string) (*Task, error)
func (r *Router) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error)
```

## Package Documentation

Every package has a `doc.go` file with a `// Package <name> ...` comment describing its role and what it does **not** own:

```go
// Package services contains the API service layer that sits between the
// HTTP controllers and the persistence/store layer. Services orchestrate
// business workflows (project materialization, comment intake, task
// state changes, system snapshots) and translate store-level errors into
// the sentinels that controllers map to HTTP status codes.
//
// Services do not perform Go-side state guards that would race the store;
// state transitions are enforced inside SQL transactions (see
// internal/kanban).
package services
```

## Test Conventions

### Unit Tests

- Test files: `*_test.go` alongside the code they test.
- Use table-driven tests for multiple input/output cases:

```go
func TestDetectPrompt(t *testing.T) {
    tests := []struct {
        name    string
        stdout  string
        stderr  string
        want    bool
    }{
        {name: "yes no prompt", stdout: "Install package? [y/N]", want: true},
        {name: "clean output", stdout: "done", want: false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := DetectPrompt(tt.stdout, tt.stderr)
            if got.Detected != tt.want {
                t.Fatalf("Detected = %v, want %v", got.Detected, tt.want)
            }
        })
    }
}
```

- Test helper functions must call `t.Helper()`.
- Use `t.Fatalf` for setup failures, `t.Errorf` for assertion failures.

### BDD Tests (Godog)

- Feature files: `*.feature` using Gherkin syntax.
- Step definitions: `*_feature_steps_test.go` files.
- Test runner: `*_feature_test.go` bootstrapping `godog.TestSuite`.
- Each feature file starts with a `Feature:` title and `Goal:` description.
- Scenarios use `Given/When/And/Then` steps.

## Comment Conventions

- Exported identifiers must have doc comments starting with the identifier name:

```go
// TaskState represents the lifecycle state of a Kanban task.
type TaskState string

// ErrInvalidStateTransition is returned when a task cannot move from its
// current state to the requested state.
var ErrInvalidStateTransition = errors.New("invalid task state transition")
```

- Unexported identifiers need comments only when the intent is not obvious from the name.
- Avoid inline comments that restate what the code does. Use comments for "why" decisions.

## Layer Boundaries

- `internal/models` must never import outward layers. It contains pure data definitions and interfaces.
- All other packages may import `models`.
- Enforced by `depguard` in `golangci-lint`.

See [`docs/guardrails.md`](docs/guardrails.md) for the full layer diagram and size limits.
