// Package httpx contains HTTP response envelopes and error mapping shared
// by the api package and its controllers. It lives in a sub-package so
// both can import it without forming an import cycle (internal/api ->
// internal/api/controllers -> internal/api/httpx).
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"agentd/internal/models"
)

// APIResponse is the canonical envelope returned by every HTTP handler.
// The JSON wire format is preserved exactly as before: callers see
// {status, data, meta?, error?} with the same field names.
type APIResponse[T any] struct {
	Status string    `json:"status"`
	Data   T         `json:"data,omitempty"`
	Meta   *Meta     `json:"meta,omitempty"`
	Error  *APIError `json:"error,omitempty"`
}

// Envelope is retained as an untyped alias so existing call sites continue
// to compile while we migrate to the generic form.
type Envelope = APIResponse[any]

// Meta is the pagination block on list responses. The wire shape is kept
// at {page, per_page, total} for backwards compatibility; conversion from
// the store's offset/limit world happens via MetaFromPagination.
type Meta struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// APIError is the error payload returned with non-2xx responses. Details
// is optional and is populated for VALIDATION_FAILED responses where
// multiple field-level reasons need to be returned together.
type APIError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

const (
	CodeBadRequest    = "BAD_REQUEST"
	CodeValidation    = "VALIDATION_FAILED"
	CodeNotFound      = "NOT_FOUND"
	CodeStateConflict = "STATE_CONFLICT"
	CodeForbidden     = "FORBIDDEN"
	CodeInternal      = "INTERNAL_ERROR"
)

// WriteSuccess emits a 2xx envelope. Pass a nil meta when the response
// is a single resource (no pagination block).
func WriteSuccess(w http.ResponseWriter, status int, data any, meta *Meta) {
	WriteJSON(w, status, Envelope{Status: "success", Data: data, Meta: meta})
}

// WriteError emits an error envelope with a single human-readable message.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, Envelope{Status: "error", Error: &APIError{Code: code, Message: message}})
}

// WriteValidationError emits an error envelope that includes a list of
// field-level reasons, useful for collecting all validation failures in
// a single response.
func WriteValidationError(w http.ResponseWriter, status int, code, message string, details []string) {
	WriteJSON(w, status, Envelope{Status: "error", Error: &APIError{Code: code, Message: message, Details: details}})
}

// WriteMappedError translates a Go error to an HTTP status + code via
// MapError and writes the envelope.
func WriteMappedError(w http.ResponseWriter, err error) {
	status, code, message := MapError(err)
	WriteError(w, status, code, message)
}

// WriteJSON serializes value as JSON and writes the response with the
// given HTTP status code.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// MetaFromPagination converts a store-side PaginationParams + total into
// the wire-compatible {page, per_page, total} block. Page is 1-indexed.
// When Limit is zero (no pagination requested), Page falls back to 1 and
// PerPage to total so the meta block stays semantically valid.
func MetaFromPagination(params models.PaginationParams, total int) *Meta {
	limit := params.Limit
	if limit <= 0 {
		return &Meta{Page: 1, PerPage: total, Total: total}
	}
	page := params.Offset/limit + 1
	if page < 1 {
		page = 1
	}
	return &Meta{Page: page, PerPage: limit, Total: total}
}

// MapError translates a Go error to an HTTP status code, a stable error
// code, and a human-readable message. Sentinels are matched with
// errors.Is so wrapped errors from the store layer (e.g. fmt.Errorf
// wrapping ErrTaskNotFound) still resolve to their canonical mapping.
func MapError(err error) (int, string, string) {
	switch {
	case errors.Is(err, models.ErrProjectNotFound):
		return http.StatusNotFound, CodeNotFound, "project not found"
	case errors.Is(err, models.ErrTaskNotFound):
		return http.StatusNotFound, CodeNotFound, "task not found"
	case errors.Is(err, models.ErrAgentProfileNotFound):
		return http.StatusNotFound, CodeNotFound, err.Error()
	case errors.Is(err, models.ErrAgentProfileProtected),
		errors.Is(err, models.ErrAgentProfileInUse):
		return http.StatusConflict, CodeStateConflict, err.Error()
	case errors.Is(err, models.ErrStateConflict),
		errors.Is(err, models.ErrInvalidStateTransition),
		errors.Is(err, models.ErrOptimisticLock),
		errors.Is(err, models.ErrTaskBlocked):
		return http.StatusConflict, CodeStateConflict, err.Error()
	case errors.Is(err, models.ErrInvalidDraftPlan),
		errors.Is(err, models.ErrCircularDependency):
		return http.StatusBadRequest, CodeValidation, err.Error()
	case errors.Is(err, models.ErrSandboxViolation):
		return http.StatusForbidden, CodeForbidden, err.Error()
	default:
		return http.StatusInternalServerError, CodeInternal, fmt.Sprintf("internal error: %v", err)
	}
}
