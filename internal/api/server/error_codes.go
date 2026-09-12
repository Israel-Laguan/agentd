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
