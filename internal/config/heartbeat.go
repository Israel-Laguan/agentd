package config

import (
	"time"

	"github.com/spf13/viper"
)

const defaultHeartbeatStaleAfter = 2 * time.Minute

type HeartbeatConfig struct {
	StaleAfter time.Duration
}

func setHeartbeatDefaults(v *viper.Viper) {
	v.SetDefault("heartbeat.stale_after", defaultHeartbeatStaleAfter.String())
}

func loadHeartbeatConfig(v *viper.Viper) HeartbeatConfig {
	return HeartbeatConfig{StaleAfter: v.GetDuration("heartbeat.stale_after")}
}
