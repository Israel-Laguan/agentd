package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"

	"agentd/internal/config"
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

	switch {
	case errors.Is(err, config.ErrConfigRead):
		return "agentd could not read its configuration file.", "Check the config path, file format, and any values loaded from .env or AGENTD_* variables."
	case errors.Is(err, config.ErrDirsNotWritable):
		return "agentd could not write to one of its data directories.", "Check the permissions for AGENTD_HOME, projects, uploads, and archives directories."
	case errors.Is(err, config.ErrLLMWarmup):
		return "agentd reached your LLM provider, but the startup warmup failed.", "Check the provider order, API key, model name, and network access."
	case errors.Is(err, config.ErrNoLLMProviders):
		return "agentd could not find a usable LLM provider.", "Set an API key or configure a local OpenAI-compatible provider in your config."
	}

	errText := err.Error()
	if strings.Contains(errText, "bind: address already in use") {
		return "Another process is already using the configured API address.", "Stop that process or set AGENTD_API_ADDRESS to a free port."
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "listen" {
		return "agentd could not bind its API port.", "Choose a free port with AGENTD_API_ADDRESS or stop the process using that port."
	}

	return "agentd could not complete the requested command.", "Review the technical details below, or rerun with --verbose for step-by-step logs."
}
