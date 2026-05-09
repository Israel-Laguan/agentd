// Package api provides the HTTP daemon server, routing, and response helpers.
package api

import "agentd/internal/api/server"

// Server and handler wiring.
type ServerDeps = server.ServerDeps

var (
	NewServer  = server.NewServer
	NewHandler = server.NewHandler
)

// Response and pagination (re-exported from server for stable imports).
type (
	APIResponse[T any] = server.APIResponse[T]
	Envelope           = server.Envelope
	Meta               = server.Meta
	APIError           = server.APIError
)

var (
	WriteSuccess         = server.WriteSuccess
	WriteError           = server.WriteError
	WriteValidationError = server.WriteValidationError
	WriteMappedError     = server.WriteMappedError
	WriteJSON            = server.WriteJSON
	MetaFromPagination   = server.MetaFromPagination
	MapError             = server.MapError
)

const (
	CodeBadRequest    = server.CodeBadRequest
	CodeValidation    = server.CodeValidation
	CodeNotFound      = server.CodeNotFound
	CodeStateConflict = server.CodeStateConflict
	CodeForbidden     = server.CodeForbidden
	CodeInternal      = server.CodeInternal
)
