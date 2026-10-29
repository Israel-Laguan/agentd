package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// removedKey is a config key this build no longer reads. Viper ignores unknown
// keys, so a user's old setting would stop working without a word.
// Replacement names the key that now does its job; it is empty when none does.
type removedKey struct {
	Key         string
	Replacement string
	Nested      bool
}

// removedKeys is the one table of keys that Load refuses. A PR that removes a
// key adds it here, so the error names what replaces it.
var removedKeys = []removedKey{
	{Key: "agentic.capability_routing", Nested: true},
	{Key: "agentic.model_routing", Nested: true},
	{Key: "gateway.role_models", Replacement: "stages (routes keyed by purpose) and agents", Nested: true},
	{Key: "queue.legacy.max_breakdown_depth", Replacement: "queue.split.max_depth"},
	{Key: "queue.legacy.max_subtasks_per_breakdown", Replacement: "queue.split.max_subtasks"},
	{Key: "queue.legacy.preflight_score"},
	{Key: "queue.legacy.reject_score"},
	{Key: "queue.legacy.max_description_len"},
}

// checkRemovedKeys fails when the config sets a removed key in the config file,
// the process environment or a .env file. IsSet sees file and env values but not
// defaults, and it treats an empty env var as unset. applyDotEnvToViper forwards
// only keys viper already knows. So the environment and .env are checked by each
// key's AGENTD_* name.
func checkRemovedKeys(v *viper.Viper, process, dotenv map[string]string) error {
	var problems []string
	for _, r := range removedKeys {
		envName := envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(r.Key, ".", "_"))
		_, inProcess := process[envName]
		_, inDotEnv := dotenv[envName]
		inNestedProcess := r.Nested && hasEnvPrefix(process, envName+"_")
		inNestedDotEnv := r.Nested && hasEnvPrefix(dotenv, envName+"_")
		if v.IsSet(r.Key) || inProcess || inDotEnv || inNestedProcess || inNestedDotEnv {
			problems = append(problems, r.problem())
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: removed config key: %s", ErrConfigRead, strings.Join(problems, "; "))
}

func hasEnvPrefix(values map[string]string, prefix string) bool {
	for name := range values {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (r removedKey) problem() string {
	if r.Replacement == "" {
		return fmt.Sprintf("%s was removed and has no replacement; delete it", r.Key)
	}
	return fmt.Sprintf("%s was removed; use %s instead", r.Key, r.Replacement)
}
