package config

import (
	"fmt"
	"sort"
)

func onlyKeys(m map[string]any, where string, allowed ...string) error {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !containsString(allowed, key) {
			return fmt.Errorf("%s: unknown key %q", where, key)
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
