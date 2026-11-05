package acp

// Preset is how to start one external agent: the argv run in the jail and the
// method sent to authenticate, if any.
type Preset struct {
	Argv       []string
	AuthMethod string
}

// Presets are the agents a config's kind: acp may name. Empty until S19 PR 08
// adds opencode and cline, so no real config routes to an ACP agent before
// then. Tests add the fake agent before anything reads the map; it is not locked.
var Presets = map[string]Preset{}
