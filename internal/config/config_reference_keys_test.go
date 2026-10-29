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
// The check is per leaf, with no manifest of consumers: keyFlow (see
// config_keyflow_test.go) finds the viper read for each key in internal/config
// and the struct field its value lands in, and requires non-test code outside
// internal/config to read that field. A key under a map or list read whole
// (queue.tool_timeouts, tool_manifest.mappings) is covered by that read.
// notImplementedKeys are prefixes that are documented, parsed and ignored.
// The reference file and the docs must say they are not implemented; each
// entry covers its sub-keys. Remove an entry in the change that wires its
// feature to a consumer.
var notImplementedKeys = []string{
	"agentic.review", "agentic.tool_rate_limits", "agentic.approval_gates", "agentic.skills",
	"gateway.budget", "gateway.truncation.max_input_chars",
	"mcp.http_addr", "librarian.preferences_top_k", "plugins",
}

func TestConfigReferenceKeysHaveConsumers(t *testing.T) {
	root := repoRoot(t)
	keys := referenceKeys(t, filepath.Join(root, "config.reference.yaml"))
	for _, msg := range keyGuardErrors(t, root, keys, notImplementedKeys) {
		t.Error(msg)
	}
}

// The guard must check leaves, not only groups: a key nothing reads, under a
// group whose other keys are consumed, still fails.
func TestConfigReferenceKeysRejectsUnreadLeaf(t *testing.T) {
	root := repoRoot(t)
	keys := referenceKeys(t, filepath.Join(root, "config.reference.yaml"))
	bogusLeaves := []string{"healing.nope", "queue.split.nope", "gateway.openai.nope", "gateway.truncator.nope"}
	for _, bogus := range bogusLeaves {
		errs := keyGuardErrors(t, root, append(append([]string{}, keys...), bogus), notImplementedKeys)
		if !strings.Contains(strings.Join(errs, "\n"), bogus) {
			t.Errorf("guard accepted %s, a leaf nothing reads", bogus)
		}
	}
}

// Each not-implemented key is parsed and ignored, so dropping its marker
// must fail the guard: read-but-unused fails as well as never-read.
func TestConfigReferenceKeysRejectsUnusedValue(t *testing.T) {
	root := repoRoot(t)
	keys := referenceKeys(t, filepath.Join(root, "config.reference.yaml"))
	for i, entry := range notImplementedKeys {
		rest := append(append([]string{}, notImplementedKeys[:i]...), notImplementedKeys[i+1:]...)
		if !strings.Contains(strings.Join(keyGuardErrors(t, root, keys, rest), "\n"), entry) {
			t.Errorf("guard accepted %s without its not-implemented marker", entry)
		}
	}
}

func keyGuardErrors(t *testing.T, root string, keys, notImplemented []string) []string {
	t.Helper()
	flow := moduleKeyFlow(t, root)
	var errs []string
	used := map[string]bool{}
	for _, key := range keys {
		if prefix, ok := longestPrefix(key, notImplemented); ok {
			used[prefix] = true
			if len(flow.leafError(key)) == 0 {
				errs = append(errs, key+" is marked not implemented, but its value reaches a consumer")
			}
			continue
		}
		errs = append(errs, flow.leafError(key)...)
	}
	for _, prefix := range notImplemented {
		if !used[prefix] {
			errs = append(errs, "notImplementedKeys entry "+prefix+" matches no key in config.reference.yaml")
		}
	}
	return errs
}

var keyFlows = map[string]*keyFlow{}

// moduleKeyFlow type-checks the module once per test binary.
func moduleKeyFlow(t *testing.T, root string) *keyFlow {
	t.Helper()
	if flow := keyFlows[root]; flow != nil {
		return flow
	}
	pkgs := loadModule(t, root)
	for name := range printOnlyFiles {
		if !hasFile(pkgs, name) {
			t.Fatalf("printOnlyFiles entry %s matches no file under cmd/ or internal/", name)
		}
	}
	for _, p := range pkgs {
		if p.path == configPkgPath {
			keyFlows[root] = newKeyFlow(p, indexConsumers(pkgs))
			return keyFlows[root]
		}
	}
	t.Fatalf("%s not among the loaded packages", configPkgPath)
	return nil
}

func TestConfigReferenceMarksNotImplementedKeys(t *testing.T) {
	root := repoRoot(t)
	ref := readFile(t, filepath.Join(root, "config.reference.yaml"))
	docs := readFile(t, filepath.Join(root, "docs", "config-reference.md")) +
		readFile(t, filepath.Join(root, "docs", "mcp-board-export.md"))
	for _, key := range notImplementedKeys {
		leaf := key[strings.LastIndex(key, ".")+1:]
		if !hasMarker(ref, leaf+":", "#") {
			t.Errorf("config.reference.yaml: %s lacks a 'not implemented' comment", key)
		}
		if !hasDocMarker(docs, leaf) {
			t.Errorf("docs: %s is not listed as not implemented", key)
		}
	}
}

// docs/reference.md tables every key: a row for a not-implemented key must
// say so rather than describe it as active.
func TestReferenceTableMarksNotImplementedKeys(t *testing.T) {
	table := readFile(t, filepath.Join(repoRoot(t), "docs", "reference.md"))
	for _, line := range strings.Split(table, "\n") {
		key, ok := tableKey(line)
		if _, marked := longestPrefix(key, notImplementedKeys); ok && marked &&
			!strings.Contains(strings.ToLower(line), "not implemented") {
			t.Errorf("docs/reference.md: %s is not implemented, but its row does not say so", key)
		}
	}
}

// tableKey returns the key a "| `key` | ..." table row documents.
func tableKey(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "| `")
	if !ok {
		return "", false
	}
	key, _, found := strings.Cut(rest, "`")
	return key, found
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

func longestPrefix(key string, prefixes []string) (string, bool) {
	best := ""
	for _, p := range prefixes {
		if (key == p || strings.HasPrefix(key, p+".")) && len(p) > len(best) {
			best = p
		}
	}
	return best, best != ""
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
