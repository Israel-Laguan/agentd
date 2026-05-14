package plugin

import "errors"

var (
	// ErrManifestNotFound is returned when a plugin directory lacks a
	// manifest.json file.
	ErrManifestNotFound = errors.New("manifest.json not found")

	// ErrManifestInvalid is returned when a manifest.json cannot be
	// parsed or fails structural validation.
	ErrManifestInvalid = errors.New("invalid manifest")

	// ErrMissingEnvVar is returned when a required environment
	// variable declared in the manifest is not set.
	ErrMissingEnvVar = errors.New("missing required environment variable")

	// ErrPluginDirNotFound is returned when the configured plugins
	// directory does not exist.
	ErrPluginDirNotFound = errors.New("plugins directory not found")
)
