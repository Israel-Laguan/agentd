package config

const DefaultToolManifestMinConfidence = 0.35

// ToolManifestConfig controls per-task tool list filtering for agentic runs.
type ToolManifestConfig struct {
	Enabled       bool
	MinConfidence float64
	// Mappings maps task type names (summarize, code_gen, doc_qa, web_research, full_agent)
	// to tool names advertised to the model. Empty slice means no tools; "*" or nil means all tools.
	Mappings map[string][]string
}

const DefaultCapabilityRoutingMinConfidence = 0.35

// CapabilityRoutingConfig maps classified task intents to external capability adapters.
type CapabilityRoutingConfig struct {
	Enabled       bool
	MinConfidence float64
	// Mappings maps intent names (generate_image, real_time_search, ...) to adapter registry names.
	Mappings map[string]string
	// Tools maps intent names to tool names; when omitted for an intent, the intent name is used.
	Tools map[string]string
}
