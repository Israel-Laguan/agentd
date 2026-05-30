package wfilecontext

import "errors"

var (
	ErrPathRequired           = errors.New("path is required")
	ErrAbsolutePathNotAllowed = errors.New("absolute paths are not allowed")
	ErrPathEscapesWorkspace   = errors.New("path escapes workspace")
)
