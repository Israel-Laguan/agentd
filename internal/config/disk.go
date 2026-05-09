package config

import "github.com/spf13/viper"

const defaultDiskFreeThresholdPercent = 10.0

type DiskConfig struct {
	FreeThresholdPercent float64
}

func setDiskDefaults(v *viper.Viper) {
	v.SetDefault("disk.free_threshold_percent", defaultDiskFreeThresholdPercent)
}

func loadDiskConfig(v *viper.Viper) DiskConfig {
	return DiskConfig{FreeThresholdPercent: v.GetFloat64("disk.free_threshold_percent")}
}
