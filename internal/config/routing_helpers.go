package config

import "fmt"

func onlyKeys(m map[string]any, where string, allowed ...string) error {
	for key := range m {
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
