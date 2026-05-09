package server

import (
	"net/http"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
)

// APIResponse is re-exported from httpx so callers can write
// api.APIResponse[T] without pulling in the helper sub-package.
type APIResponse[T any] = httpx.APIResponse[T]

// Envelope is the untyped form of APIResponse, kept for older call sites.
type Envelope = httpx.Envelope

// Meta is the wire pagination block.
type Meta = httpx.Meta

// APIError is the wire error payload.
type APIError = httpx.APIError

// WriteSuccess delegates to httpx.WriteSuccess.
func WriteSuccess(w http.ResponseWriter, status int, data any, meta *Meta) {
	httpx.WriteSuccess(w, status, data, meta)
}

// WriteError delegates to httpx.WriteError.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	httpx.WriteError(w, status, code, message)
}

// WriteValidationError delegates to httpx.WriteValidationError.
func WriteValidationError(w http.ResponseWriter, status int, code, message string, details []string) {
	httpx.WriteValidationError(w, status, code, message, details)
}

// WriteMappedError delegates to httpx.WriteMappedError.
func WriteMappedError(w http.ResponseWriter, err error) {
	httpx.WriteMappedError(w, err)
}

// WriteJSON delegates to httpx.WriteJSON.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	httpx.WriteJSON(w, status, value)
}

// MetaFromPagination delegates to httpx.MetaFromPagination.
func MetaFromPagination(params models.PaginationParams, total int) *Meta {
	return httpx.MetaFromPagination(params, total)
}
