// Package plugin implements the plugin manifest schema, directory
// scanner, and loader infrastructure. A plugin is a directory inside
// the configured plugins path that contains a manifest.json declaring
// hooks, capability adapters, and required environment variables.
//
// The PluginLoader scans the plugins directory at startup, validates
// manifests, checks required env vars, and mounts hooks into the
// worker HookChain and capabilities into the capabilities Registry.
//
// Dynamic runtime loading is not supported; a restart is required to
// pick up new or changed plugins.
package plugin
