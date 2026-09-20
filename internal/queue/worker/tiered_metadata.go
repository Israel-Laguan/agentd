package worker

import (
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/models"
)

// Helper functions for task metadata management. Metadata is stored as a
// JSON object in task.Logs. It is a scratch API for values that only need
// to survive within a single call chain (the cap enforcement in
// escalation.go re-derives its counts from the persisted DAG on every call
// instead of trusting this to survive a task reload, since task.Logs has no
// backing DB column).

func getMetadata(task models.Task, key, defaultVal string) string {
	if strings.TrimSpace(task.Logs) == "" {
		return defaultVal
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(task.Logs), &meta); err != nil {
		return defaultVal
	}
	val, ok := meta[key]
	if !ok {
		return defaultVal
	}
	if s, ok := val.(string); ok {
		return s
	}
	encoded, err := json.Marshal(val)
	if err != nil {
		return defaultVal
	}
	return string(encoded)
}

func setMetadata(task *models.Task, key, value string) {
	// Store in Logs field as JSON or structured format
	if task.Logs == "" {
		task.Logs = "{}"
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(task.Logs), &meta); err != nil || meta == nil {
		meta = make(map[string]interface{})
	}
	meta[key] = value
	if data, err := json.Marshal(meta); err == nil {
		task.Logs = string(data)
	}
}

func getMetadataInt(task models.Task, key string, defaultVal int) int {
	val := getMetadata(task, key, "")
	if val == "" {
		return defaultVal
	}
	var i int
	if _, err := fmt.Sscanf(val, "%d", &i); err != nil {
		return defaultVal
	}
	return i
}

func setMetadataInt(task *models.Task, key string, value int) {
	if task == nil {
		return
	}
	text := fmt.Sprintf("%d", value)
	setMetadata(task, key, text)
}
