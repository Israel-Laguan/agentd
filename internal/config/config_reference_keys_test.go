package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// B-065: every key in config.reference.yaml must have a consumer or say it has
// none. Documented keys nothing reads (a rate limit, an approval gate) look
// like safety controls and do nothing.
//
// consumedKeys maps a key prefix to a regexp that must match non-test Go code:
// outside internal/config for most keys (the component that acts on the value),
// inside it for keys the config package turns into gateway wiring itself.
// notImplementedKeys are prefixes the reference file and the docs must say are
// not implemented. Prefixes cover their sub-keys.
type keyConsumer struct{ pattern string }

var consumedKeys = map[string]keyConsumer{
	"home": {`cfg\.HomeDir`}, "db_path": {`cfg\.DBPath`}, "projects_dir": {`cfg\.ProjectsDir`},
	"uploads_dir": {`cfg\.UploadsDir`}, "log_level": {`LogLevel`},
	"api.address": {`cfg\.API\.Address`}, "api.materialize_token": {`cfg\.API\.MaterializeToken`},
	"api.allowed_hosts": {`cfg\.API\.AllowedHosts`}, "api.ui_origin": {`cfg\.API\.UIOrigin`},
	"api.source_root": {`SourceRoot`},
	"gateway.warmup_enabled": {`WarmupEnabled`}, "gateway.max_tasks_per_phase": {`MaxTasksPerPhase`},
	"gateway.order": {`Gateway\.Order|c\.Order`}, "gateway.role_models": {`RoleModels\.Chat`},
	"gateway.truncator": {`Truncator\.MaxInputChars`}, "gateway.truncation.stash_threshold": {`Truncation\.StashThreshold`},
	"gateway.openai": {`ProviderConfigs`}, "gateway.anthropic": {`ProviderConfigs`}, "gateway.gemini": {`ProviderConfigs`},
	"gateway.ollama": {`ProviderConfigs`}, "gateway.llamacpp": {`ProviderConfigs`}, "gateway.horde": {`ProviderConfigs`},
	"agentic.context_warning_threshold": {`Agentic\.ContextWarningThreshold`}, "agentic.tool_failure_streak": {`Agentic\.ToolFailureStreak`},
	"agentic.topic_guard": {`Agentic\.TopicGuard`}, "agentic.audit": {`Agentic\.Audit`},
	"agentic.external_tools": {`Agentic\.ExternalTools`}, "agentic.file_context": {`Agentic\.FileContext`},
	"agentic.planning": {`Agentic\.Planning`}, "agentic.model_routing": {`Agentic\.ModelRouting`},
	"agentic.tool_manifest": {`Agentic\.ToolManifest`}, "agentic.batching": {`Agentic\.Batching`},
	"agentic.capability_routing": {`Agentic\.CapabilityRouting`}, "agentic.scheduler.tick_interval": {`Agentic\.Scheduler\.TickInterval`},
	"agentic.scheduler.enabled": {`Agentic\.Scheduler\.Enabled`}, "agentic.scheduler.project_id": {`Agentic\.Scheduler`},
	"agentic.scheduler.tasks": {`Agentic\.Scheduler`},
	"sandbox.backend": {`Sandbox\.Backend`}, "sandbox.inactivity_timeout": {`Sandbox\.InactivityTimeout`},
	"sandbox.wall_timeout": {`Sandbox\.WallTimeout`}, "sandbox.kill_grace": {`Sandbox\.KillGrace`},
	"sandbox.max_log_bytes": {`Sandbox\.MaxLogBytes`}, "sandbox.env_allowlist": {`Sandbox\.EnvAllowlist`},
	"sandbox.extra_env": {`Sandbox\.ExtraEnv`}, "sandbox.scrub_patterns": {`Sandbox\.ScrubPatterns`},
	"sandbox.limits": {`Sandbox\.Limits`},
	"healing": {`cfg\.Healing`}, "breaker": {`cfg\.Breaker`}, "recovery": {`cfg\.Recovery`},
	"disk": {`cfg\.Disk`}, "channel": {`cfg\.Channel`}, "heartbeat": {`cfg\.Heartbeat`},
	"queue.task_deadline": {`Queue\.TaskDeadline`}, "queue.queued_reconcile_after": {`Queue\.QueuedReconcileAfter`},
	"queue.poll_max_interval": {`Queue\.PollMaxInterval`}, "queue.max_tool_iterations": {`Queue\.MaxToolIterations`},
	"queue.token_budget": {`Queue\.TokenBudget`}, "queue.rolling_token_window": {`Queue\.RollingTokenWindow`},
	"queue.rolling_token_limit": {`Queue\.RollingTokenLimit`}, "queue.agentic_truncator_max": {`Queue\.AgenticTruncatorMax`},
	"queue.agentic_character_budget": {`Queue\.AgenticCharacterBudget`}, "queue.agentic_context": {`Queue\.AgenticContext`},
	"queue.instructions": {`Queue\.Instructions`}, "queue.skills": {`Queue\.Skills`},
	"queue.hitl": {`Queue\.HITL`}, "queue.legacy": {`Queue\.Legacy`},
	"queue.tool_timeouts": {`Queue\.ToolTimeouts`}, "queue.tool_retries": {`Queue\.ToolRetries`},
	"librarian": {`cfg\.Librarian`}, "tiered": {`cfg\.Tiered`}, "mcp": {`cfg\.MCP`},
	"plugins": {`cfg\.Plugins|Plugins\.`},
}

// notImplementedKeys are documented, parsed and ignored. Remove an entry when
// its feature is wired; add a consumedKeys entry in the same change.
var notImplementedKeys = []string{
	"agentic.review", "agentic.tool_rate_limits", "agentic.approval_gates", "agentic.skills",
	"gateway.budget", "gateway.truncation.strategy", "gateway.truncation.head_ratio",
	"gateway.truncation.max_input_chars",
}

func TestConfigReferenceKeysHaveConsumers(t *testing.T) {
	root := repoRoot(t)
	keys := referenceKeys(t, filepath.Join(root, "config.reference.yaml"))
	consumerSrc, configSrc := goSources(t, root)

	used := map[string]bool{}
	for _, key := range keys {
		if prefix, ok := longestPrefix(key, notImplementedKeys); ok {
			used[prefix] = true
			continue
		}
		prefix, ok := longestPrefix(key, mapKeys(consumedKeys))
		if !ok {
			t.Errorf("%s is documented but neither consumed nor marked not implemented", key)
			continue
		}
		used[prefix] = true
	}
	for prefix, c := range consumedKeys {
		if !used[prefix] {
			t.Errorf("consumedKeys entry %q matches no key in config.reference.yaml", prefix)
		}
		if !regexp.MustCompile(c.pattern).MatchString(consumerSrc + configSrc) {
			t.Errorf("%s: no non-test code matches %s", prefix, c.pattern)
		}
	}
	for _, prefix := range notImplementedKeys {
		if !used[prefix] {
			t.Errorf("notImplementedKeys entry %q matches no key in config.reference.yaml", prefix)
		}
	}
}

func TestConfigReferenceMarksNotImplementedKeys(t *testing.T) {
	root := repoRoot(t)
	ref := readFile(t, filepath.Join(root, "config.reference.yaml"))
	docs := readFile(t, filepath.Join(root, "docs", "config-reference.md"))
	for _, key := range notImplementedKeys {
		leaf := key[strings.LastIndex(key, ".")+1:]
		if !hasMarker(ref, leaf+":", "#") {
			t.Errorf("config.reference.yaml: %s lacks a 'not implemented' comment", key)
		}
		if !hasDocMarker(docs, leaf) {
			t.Errorf("docs/config-reference.md: %s is not listed as not implemented", key)
		}
	}
}

// hasDocMarker reports whether a docs line names the key (bare or dotted, in
// backticks) and says "not implemented".
func hasDocMarker(docs, leaf string) bool {
	name := regexp.MustCompile("`([^`]*\\.)?" + regexp.QuoteMeta(leaf) + "`")
	for _, line := range strings.Split(docs, "\n") {
		if name.MatchString(line) && strings.Contains(strings.ToLower(line), "not implemented") {
			return true
		}
	}
	return false
}

// hasMarker reports whether some line containing token also says "not
// implemented" (after the comment marker when one is given).
func hasMarker(text, token, commentMark string) bool {
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, token) {
			continue
		}
		if commentMark != "" {
			idx := strings.Index(line, commentMark)
			if idx < 0 {
				continue
			}
			line = line[idx:]
		}
		if strings.Contains(strings.ToLower(line), "not implemented") {
			return true
		}
	}
	return false
}

func referenceKeys(t *testing.T, path string) []string {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var keys []string
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		m, ok := v.(map[string]any)
		if !ok || len(m) == 0 {
			keys = append(keys, prefix)
			return
		}
		for k, child := range m {
			walk(prefix+"."+k, child)
		}
	}
	for k, v := range doc {
		walk(k, v)
	}
	for i := range keys {
		keys[i] = strings.TrimPrefix(keys[i], ".")
	}
	sort.Strings(keys)
	return keys
}

func goSources(t *testing.T, root string) (consumers, config string) {
	t.Helper()
	var c, cfg strings.Builder
	for _, dir := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			if strings.Contains(filepath.ToSlash(p), "/internal/config/") {
				cfg.Write(b)
			} else {
				c.Write(b)
			}
			return nil
		})
	}
	return c.String(), cfg.String()
}

func longestPrefix(key string, prefixes []string) (string, bool) {
	best := ""
	for _, p := range prefixes {
		if (key == p || strings.HasPrefix(key, p+".")) && len(p) > len(best) {
			best = p
		}
	}
	return best, best != ""
}

func mapKeys(m map[string]keyConsumer) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
