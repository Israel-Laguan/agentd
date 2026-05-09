package config

import (
	"time"

	"github.com/spf13/viper"
)

const defaultBreakerHandoffAfter = 2 * time.Minute

type BreakerConfig struct {
	HandoffAfter time.Duration
}

func setBreakerDefaults(v *viper.Viper) {
	v.SetDefault("breaker.handoff_after", defaultBreakerHandoffAfter.String())
}

func loadBreakerConfig(v *viper.Viper) BreakerConfig {
	return BreakerConfig{HandoffAfter: v.GetDuration("breaker.handoff_after")}
}
