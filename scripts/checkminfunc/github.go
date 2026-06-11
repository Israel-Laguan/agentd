package main

import (
	"fmt"
	"os"
	"strings"
)

const maxGitHubAnnotations = 10

func emitStepSummary(violations []violation) {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return
	}
	lines := []string{
		"### Function length check failed",
		"",
		fmt.Sprintf("%d function(s) have fewer than %d lines:", len(violations), *minLines),
		"",
		"| Lines | Function | File |",
		"| ---: | --- | --- |",
	}
	limit := len(violations)
	if limit > maxGitHubAnnotations {
		limit = maxGitHubAnnotations
	}
	for i := 0; i < limit; i++ {
		v := violations[i]
		lines = append(lines, fmt.Sprintf("| %d | `%s` | `%s:%d` |", v.lines, v.name, v.file, v.line))
	}
	remaining := len(violations) - maxGitHubAnnotations
	if remaining > 0 {
		lines = append(lines, "", fmt.Sprintf("_%d more function(s) not shown; see step log._", remaining))
	}
	lines = append(lines, "")
	f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "checkminfunc: close summary file: %v\n", err)
		}
	}()
	_, _ = f.WriteString(strings.Join(lines, "\n") + "\n")
}

func emitGitHubAnnotations(violations []violation) {
	if os.Getenv("GITHUB_ACTIONS") == "" {
		return
	}
	limit := len(violations)
	if limit > maxGitHubAnnotations {
		limit = maxGitHubAnnotations
	}
	for i := 0; i < limit; i++ {
		v := violations[i]
		fmt.Printf(
			"::error file=%s,line=%d,endLine=%d,title=Function too short::Function %s has only %d lines (min %d)\n",
			v.file, v.line, v.line, v.name, v.lines, *minLines,
		)
	}
	remaining := len(violations) - maxGitHubAnnotations
	if remaining > 0 {
		fmt.Printf(
			"::error title=Function length check failed::checkminfunc: %d more function(s) not shown\n",
			remaining,
		)
	}
}
