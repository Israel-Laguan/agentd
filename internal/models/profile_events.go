package models

// EventTypeProfileUpgraded records a startup upgrade of an agent profile that
// was still in non-agentic mode (cmd/agentd, upgradeLegacyProfile).
const EventTypeProfileUpgraded EventType = "PROFILE_UPGRADED"
