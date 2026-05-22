package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
)

func reportCommandError(err error) {
	summary, hint := describeCommandError(err)
	fmt.Fprintln(os.Stderr, summary)
	if hint != "" {
		fmt.Fprintln(os.Stderr, hint)
	}
	fmt.Fprintln(os.Stderr, "technical details:", err)
	slog.Error("command failed", "summary", summary, "hint", hint, "error", err)
}

func describeCommandError(err error) (string, string) {
	if err == nil {
		return "", ""
	}

	errText := err.Error()
	switch {
	case strings.Contains(errText, "load configuration:") || strings.Contains(errText, "read config:"):
		return "agentd could not read its configuration file.", "Check the config path, file format, and any values loaded from .env or AGENTD_* variables."
	case strings.Contains(errText, "bind: address already in use"):
		return "Another process is already using the configured API address.", "Stop that process or set AGENTD_API_ADDRESS to a free port."
	case strings.Contains(errText, "directory not writable"):
		return "agentd could not write to one of its data directories.", "Check the permissions for AGENTD_HOME, the database directory, and the projects directory."
	case strings.Contains(errText, "LLM warmup failed"):
		return "agentd reached your LLM provider, but the startup warmup failed.", "Check the provider order, API key, model name, and network access."
	case strings.Contains(errText, "no LLM providers available"):
		return "agentd could not find a usable LLM provider.", "Set an API key or configure a local OpenAI-compatible provider in your config."
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "listen" {
		return "agentd could not bind its API port.", "Choose a free port with AGENTD_API_ADDRESS or stop the process using that port."
	}

	return "agentd could not complete the requested command.", "Review the technical details below, or rerun with --verbose for step-by-step logs."
}