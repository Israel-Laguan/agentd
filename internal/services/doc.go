// Package services contains the API service layer that sits between the
// HTTP controllers and the persistence/store layer. Services orchestrate
// business workflows (project materialization, comment intake, task
// state changes, system snapshots) and translate store-level errors into
// the sentinels that controllers map to HTTP status codes.
//
// Services do not perform Go-side state guards that would race the store;
// state transitions are enforced inside SQL transactions (see
// internal/kanban). Services also do not directly cancel workers; the
// store's models.TaskCanceller hook fires after commit.
package services
