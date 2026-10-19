// Package api provides the HTTP daemon server, routing, and response helpers.
package api

import (
	"net/http"
	"time"

	"agentd/internal/api/httpx"
	"agentd/internal/api/server"
)

// Server and handler wiring.
type ServerDeps = server.ServerDeps

var (
	NewServer = func(deps server.ServerDeps) *http.Server {
		return &http.Server{
			Addr: deps.Addr, Handler: server.NewHandler(deps),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       60 * time.Second,
			IdleTimeout:       2 * time.Minute,
			// No WriteTimeout: the SSE event stream stays open for hours.
		}
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
