// Package api provides the HTTP daemon server, routing, and response helpers.
package api

import (
	"net/http"

	"agentd/internal/api/httpx"
	"agentd/internal/api/server"
)

// Server and handler wiring.
type ServerDeps = server.ServerDeps

var (
	NewServer = func(deps server.ServerDeps) *http.Server {
		return &http.Server{Addr: deps.Addr, Handler: server.NewHandler(deps)}
	}
	NewHandler = server.NewHandler
)

// Response and pagination (re-exported from httpx for stable imports).
type (
	APIResponse[T any] = httpx.APIResponse[T]
	Envelope           = httpx.Envelope
	Meta               = httpx.Meta
	APIError           = httpx.APIError
)

var (
	WriteSuccess         = httpx.WriteSuccess
	WriteError           = httpx.WriteError
	WriteValidationError = httpx.WriteValidationError
	WriteMappedError     = httpx.WriteMappedError
	WriteJSON            = httpx.WriteJSON
	MetaFromPagination   = httpx.MetaFromPagination
	MapError             = httpx.MapError
)

const (
	CodeBadRequest    = httpx.CodeBadRequest
	CodeValidation    = httpx.CodeValidation
	CodeNotFound      = httpx.CodeNotFound
	CodeStateConflict = httpx.CodeStateConflict
	CodeForbidden     = httpx.CodeForbidden
	CodeInternal      = httpx.CodeInternal
)
