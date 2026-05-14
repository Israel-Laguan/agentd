package worker

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	// SubagentDir is the relative path within a workspace for subagent definitions.
	SubagentDir = ".agentd/subagents"
)

// SubagentLoader reads and parses subagent definition files from the filesystem.
type SubagentLoader struct{}

// LoadAll loads subagent definitions from <workspacePath>/.agentd/subagents/.
// Returns nil (no error) when the directory does not exist.
func (l *SubagentLoader) LoadAll(workspacePath string) ([]*SubagentDefinition, error) {
	if workspacePath == "" {
		return nil, nil
	}
	dir := filepath.Join(workspacePath, SubagentDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read subagent directory %s: %w", dir, err)
	}

	var defs []*SubagentDefinition
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		content, err := os.ReadFile(fullPath)
		if err != nil {
			slog.Warn("failed to read subagent definition file", "path", fullPath, "error", err)
			continue
		}
		def := parseSubagentMD(string(content), fullPath)
		if def.Name == "" {
			slog.Debug("skipping subagent definition with no name", "path", fullPath)
			continue
		}
		defs = append(defs, def)
	}

	slog.Debug("loaded subagent definitions", "count", len(defs))
	return defs, nil
}

// LoadByName loads a single subagent definition by name from the workspace.
func (l *SubagentLoader) LoadByName(workspacePath, name string) (*SubagentDefinition, error) {
	defs, err := l.LoadAll(workspacePath)
	if err != nil {
		return nil, err
	}
	for _, def := range defs {
		if strings.EqualFold(def.Name, name) {
			return def, nil
		}
	}
	return nil, fmt.Errorf("subagent definition %q not found", name)
}

// parseSubagentMD parses a subagent definition markdown file.
// Expected format:
//
//	# Subagent: <name>
//
//	## Purpose
//	<purpose text>
//
//	## Allowed Tools
//	- bash
//	- read
//
//	## Forbidden Tools
//	- write
//
	//	## Max Iterations
	//	30
	//
	//	## Context Budget
	//	12000
	//
	//	## Output Schema
//	<schema description>
//
//	## Termination Criteria
//	<criteria description>
func parseSubagentMD(content, sourcePath string) *SubagentDefinition {
	def := &SubagentDefinition{SourcePath: sourcePath}
	if content == "" {
		return def
	}

	def.Name = extractSubagentName(content)
	sections := splitH2Sections(content)

	for heading, body := range sections {
		normalized := strings.ToLower(strings.TrimSpace(heading))
		trimmedBody := strings.TrimSpace(body)
		switch normalized {
		case "purpose":
			def.Purpose = trimmedBody
		case "allowed tools":
			def.AllowedTools = parseToolList(trimmedBody)
		case "forbidden tools":
			def.ForbiddenTools = parseToolList(trimmedBody)
		case "max iterations":
			def.MaxIterations = parseIntField(trimmedBody)
		case "context budget":
			def.ContextBudget = parseIntField(trimmedBody)
		case "output schema":
			def.OutputSchema = trimmedBody
		case "termination criteria":
			def.TerminationCriteria = trimmedBody
		}
	}

	return def
}

// extractSubagentName finds the first H1 heading matching "# Subagent: <name>".
func extractSubagentName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		heading := strings.TrimPrefix(trimmed, "# ")
		if after, ok := strings.CutPrefix(heading, "Subagent: "); ok {
			return strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(heading, "Subagent:"); ok {
			return strings.TrimSpace(after)
		}
		return strings.TrimSpace(heading)
	}
	return ""
}

// parseToolList parses a markdown list of tool names.
func parseToolList(body string) []string {
	var tools []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			tools = append(tools, trimmed)
		}
	}
	return tools
}

// parseIntField parses a single integer from the body text.
func parseIntField(body string) int {
	trimmed := strings.TrimSpace(body)
	var val int
	if _, err := fmt.Sscanf(trimmed, "%d", &val); err != nil {
		return 0
	}
	return val
}
