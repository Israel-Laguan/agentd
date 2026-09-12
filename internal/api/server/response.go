package server

import "agentd/internal/api/httpx"

// APIResponse is re-exported from httpx so callers can write
// api.APIResponse[T] without pulling in the helper sub-package.
type APIResponse[T any] = httpx.APIResponse[T]

// Envelope is the untyped form of APIResponse, kept for older call sites.
type Envelope = httpx.Envelope

// Meta is the wire pagination block.
type Meta = httpx.Meta

// APIError is the wire error payload.
type APIError = httpx.APIError
