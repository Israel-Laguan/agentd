package config

import "github.com/spf13/viper"

const defaultAPIAddress = "127.0.0.1:8765"

type APIConfig struct {
	Address string
	// MaterializeToken, when non-empty, requires clients to send the same value
	// in header X-Agentd-Materialize-Token on POST /api/v1/projects/materialize.
	MaterializeToken string
}

func setAPIDefaults(v *viper.Viper) {
	v.SetDefault("api.address", defaultAPIAddress)
	v.SetDefault("api.materialize_token", "")
}

func loadAPIConfig(v *viper.Viper) APIConfig {
	return APIConfig{
		Address:          v.GetString("api.address"),
		MaterializeToken: v.GetString("api.materialize_token"),
	}
}
