package config

import (
	"math"
	"time"

	"github.com/spf13/viper"
)

// parseViperDuration reads key when IsSet; otherwise returns fallback.
// String values use time.ParseDuration (e.g. "200ms", "60s").
// Bare numeric values are multiplied by bareNumberUnit (milliseconds for retries, seconds for timeouts).
func parseViperDuration(v *viper.Viper, key string, fallback, bareNumberUnit time.Duration) time.Duration {
	if !v.IsSet(key) {
		return fallback
	}
	switch raw := v.Get(key).(type) {
	case string:
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return fallback
		}
		return d
	case int:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case int64:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case int32:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case float64:
		return scaleBareNumber(raw, bareNumberUnit, fallback)
	case float32:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case time.Duration:
		if raw > 0 {
			return raw
		}
		return fallback
	default:
		return fallback
	}
}

func scaleBareNumber(n float64, unit, fallback time.Duration) time.Duration {
	if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return fallback
	}
	return time.Duration(n * float64(unit))
}
