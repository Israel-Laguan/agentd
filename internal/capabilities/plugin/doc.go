// Package plugin implements the plugin manifest schema, directory
// scanner, and loader infrastructure. A plugin is a directory inside
// a plugins path that contains a manifest.json declaring hooks,
// capability adapters, and required environment variables.
//
// Plugins support three activation scopes:
//   - Global: loaded from the config-level plugins/ directory at startup.
//   - Project: loaded from <workspace>/.agentd/plugins/ per project.
//   - Session: activated by name via AgentProfile.Plugins.
//
// The PluginLoader scans plugin directories, validates manifests,
// checks required env vars, and mounts hooks into the worker HookChain
// and capabilities into the capabilities Registry. The Mounter type
// implements worker.PluginMounter for project and session scopes.
//
// Dynamic runtime loading is not supported; a restart is required to
// pick up new or changed plugins.
package plugin
