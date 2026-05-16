// Package paths provides filesystem path helpers that do not perform I/O.
//
// It owns tilde-prefix expansion for user-supplied paths (see ExpandTildePrefix).
// It does not load configuration, resolve project roots, or touch the filesystem
// beyond what callers pass in — those responsibilities live in config, worker,
// and other packages.
package paths
