//go:build live

package acp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

func containsGoVersion(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "go version") || strings.Contains(lower, "go1.")
}

func containsCowsay(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "cowsay") || strings.Contains(text, "<") || strings.Contains(text, "____")
}

func workspaceFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

func opencodeVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
	}
	return strings.TrimSpace(string(out))
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func jsonInline(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	return string(raw)
}

func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "")
	return truncate(s, 500)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func appendError(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func isQuotaText(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, " 429") || strings.Contains(s, "429 ") ||
		strings.Contains(s, "rate limit") || strings.Contains(s, "rate-limit") ||
		strings.Contains(s, "too many requests") || strings.Contains(s, "quota") ||
		strings.Contains(s, "insufficient balance") || strings.Contains(s, "insufficient credits")
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil {
		return v
	}
	return fallback
}

func envList(name, fallback string) []string {
	raw := envOr(name, fallback)
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
