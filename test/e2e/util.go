//go:build e2e

package e2e

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// UniqueProjectName generates a unique project name for a journey.
// Format: "journey-id-randomsuffix" (e.g., "j01-abc123").
func UniqueProjectName(journeyID string) string {
	suffix := randomSuffix(6)
	return fmt.Sprintf("%s-%s", strings.ToLower(journeyID), suffix)
}

// randomSuffix generates a random alphanumeric suffix.
func randomSuffix(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		idx := make([]byte, 1)
		if _, err := rand.Read(idx); err != nil {
			panic(err)
		}
		b[i] = charset[int(idx[0])%len(charset)]
	}
	return string(b)
}
