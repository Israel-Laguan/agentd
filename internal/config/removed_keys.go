package config

import (
	"errors"
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
}

// removedKeys is the one table of keys that Load refuses. A PR that removes a
// key adds it here, so the error names what replaces it.
var removedKeys = []removedKey{
	{Key: "queue.legacy.max_breakdown_depth", Replacement: "queue.split.max_depth"},
	{Key: "queue.legacy.max_subtasks_per_breakdown", Replacement: "queue.split.max_subtasks"},
	{Key: "queue.legacy.preflight_score"},
	{Key: "queue.legacy.reject_score"},
	{Key: "queue.legacy.max_description_len"},
}

// checkRemovedKeys fails when the config sets a removed key. IsSet sees file
// and env values but not defaults, so a removed key trips it only when the
// user set it.
func checkRemovedKeys(v *viper.Viper) error {
	var problems []string
	for _, r := range removedKeys {
		if v.IsSet(r.Key) {
			problems = append(problems, r.problem())
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New("removed config key: " + strings.Join(problems, "; "))
}

func (r removedKey) problem() string {
	if r.Replacement == "" {
		return fmt.Sprintf("%s was removed and has no replacement; delete it", r.Key)
	}
	return fmt.Sprintf("%s was removed; use %s instead", r.Key, r.Replacement)
}
