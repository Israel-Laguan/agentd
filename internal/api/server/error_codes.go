package server

import "agentd/internal/api/httpx"

const (
	CodeBadRequest    = httpx.CodeBadRequest
	CodeValidation    = httpx.CodeValidation
	CodeNotFound      = httpx.CodeNotFound
	CodeStateConflict = httpx.CodeStateConflict
	CodeForbidden     = httpx.CodeForbidden
	CodeInternal      = httpx.CodeInternal
)

// MapError delegates to httpx.MapError. See its docs for details.
func MapError(err error) (int, string, string) {
	return httpx.MapError(err)
}
