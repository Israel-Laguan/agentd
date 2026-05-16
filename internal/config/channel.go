package config

import "github.com/spf13/viper"

const (
	// DefaultMaxMessageSize is the viper default (bytes) for channel.max_message_size
	// and matches the recommended value in config.reference.yaml. It is applied on
	// every config load, including when no channel: section exists in the user's YAML,
	// which enables dispatch-time validation via ChannelGateEnabled. Set
	// channel.max_message_size to 0 in config to disable size enforcement.
	DefaultMaxMessageSize = 1048576

	// DefaultChannelRateLimit is the default maximum submissions per
	// session within the rate window. 0 = unlimited.
	DefaultChannelRateLimit = 0

	// DefaultChannelRateWindow is the default sliding window (seconds)
	// for the per-session rate limiter.
	DefaultChannelRateWindow = 60
)

// ChannelConfig holds channel-level security and validation settings.
type ChannelConfig struct {
	MaxMessageSize int
	RateLimit      int
	RateWindow     int
}

// ChannelGateEnabled reports whether dispatch-time channel validation should run.
// The gate is active when rate limiting or a positive max message size is configured.
// A zero-value ChannelConfig disables the gate; loaded config from viper after
// setChannelDefaults typically has MaxMessageSize == DefaultMaxMessageSize and
// enables the gate even without an explicit channel: block in the user's YAML.
func ChannelGateEnabled(c ChannelConfig) bool {
	return c.RateLimit > 0 || c.MaxMessageSize > 0
}

// NormalizedRateWindow returns the effective rate window in seconds.
// When rate limiting is enabled and the configured window is non-positive,
// DefaultChannelRateWindow is used (same semantics as ChannelGate).
func NormalizedRateWindow(c ChannelConfig) int {
	rateWindow := c.RateWindow
	if c.RateLimit > 0 && rateWindow <= 0 {
		rateWindow = DefaultChannelRateWindow
	}
	return rateWindow
}

func setChannelDefaults(v *viper.Viper) {
	v.SetDefault("channel.max_message_size", DefaultMaxMessageSize)
	v.SetDefault("channel.rate_limit", DefaultChannelRateLimit)
	v.SetDefault("channel.rate_window", DefaultChannelRateWindow)
}

func loadChannelConfig(v *viper.Viper) ChannelConfig {
	return ChannelConfig{
		MaxMessageSize: v.GetInt("channel.max_message_size"),
		RateLimit:      v.GetInt("channel.rate_limit"),
		RateWindow:     v.GetInt("channel.rate_window"),
	}
}
