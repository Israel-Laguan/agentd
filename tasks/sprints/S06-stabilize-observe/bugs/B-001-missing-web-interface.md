# B-001: Missing Web Interface / Dashboard

| Field | Value |
| --- | --- |
| Type | bug |
| Status | ready |
| Priority | P3 |
| Sprint | S06-stabilize-observe |
| Persona | adopter |
| Links | [api server](../../../../internal/api/server/server.go) |

## Symptom

Accessing the agentd API root (`/`) or common discovery paths (`/docs`, `/health`) returns `404 page not found`. 

## Analysis

Current implementation of the API server in `internal/api/server/server.go` only registers endpoints under the `/api/v1/` prefix. There is no static file server, index page, or interactive documentation (like Swagger/OpenAPI) served from the root or common paths.

## Expected Behavior

Users should be able to access a basic landing page, health check endpoint, or an interactive API documentation interface to verify the daemon is running and explore available endpoints.

## Notes

- If a web UI was intended, this is a missing feature.
- If only a headless API was intended, the root should still return a helpful message or a basic health check.
