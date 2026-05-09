package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

func overrideFromExplicitConfig(v *viper.Viper, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	values := map[string]any{}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		return err
	}
	flattened := map[string]any{}
	flattenYAML("", values, flattened)
	for key, value := range flattened {
		v.Set(key, value)
	}
	return nil
}

func flattenYAML(prefix string, values map[string]any, out map[string]any) {
	for key, value := range values {
		fullKey := key
		if prefix != "" {
			fullKey = fmt.Sprintf("%s.%s", prefix, key)
		}
		child, ok := value.(map[string]any)
		if ok {
			flattenYAML(fullKey, child, out)
			continue
		}
		out[fullKey] = value
	}
}
