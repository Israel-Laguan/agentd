package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var mdLinkRE = regexp.MustCompile(`!?\[[^\]]*\]\(([^)]+)\)`)

func TestMarkdownInternalLinksResolve(t *testing.T) {
	root := repoRoot(t)
	files, err := markdownFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no markdown files found")
	}

	var bad []string
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", rel(root, f), err)
		}
		for _, m := range mdLinkRE.FindAllStringSubmatch(string(content), -1) {
			target := strings.TrimSpace(m[1])
			if isExternalLink(target) {
				continue
			}
			pathPart := strings.Split(target, "#")[0]
			if pathPart == "" {
				continue
			}
			if !isDocCrossReference(pathPart) {
				continue
			}
			var resolved string
			if strings.HasPrefix(pathPart, "/") {
				resolved = filepath.Clean(filepath.Join(root, pathPart))
			} else {
				resolved = filepath.Clean(filepath.Join(filepath.Dir(f), pathPart))
			}
			if !isVerifiableDocTarget(resolved, root) {
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				bad = append(bad, rel(root, f)+": link "+target+" -> "+rel(root, resolved))
			}
		}
	}
	if len(bad) > 0 {
		t.Fatalf("%d broken internal link(s):\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
}

func TestProviderToolCallingDocParity(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "docs", "provider-tool-calling.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read provider-tool-calling.md: %v", err)
	}
	text := string(content)
	for _, want := range []string{"SupportsChatTools", "Backend.Capabilities()"} {
		if !strings.Contains(text, want) {
			t.Errorf("provider-tool-calling.md missing %q", want)
		}
	}
}

func markdownFiles(root string) ([]string, error) {
	var out []string
	readme := filepath.Join(root, "README.md")
	if _, err := os.Stat(readme); err == nil {
		out = append(out, readme)
	}
	docsDir := filepath.Join(root, "docs")
	err := filepath.Walk(docsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func isExternalLink(target string) bool {
	return strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:")
}

// isDocCrossReference reports whether a markdown link target looks like a doc file.
func isDocCrossReference(pathPart string) bool {
	base := filepath.Base(pathPart)
	if base == "config.reference.yaml" {
		return true
	}
	return strings.HasSuffix(pathPart, ".md")
}

// isVerifiableDocTarget limits checks to README, config.reference.yaml, and docs/**.
func isVerifiableDocTarget(resolved, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(resolved))
	if err != nil {
		return false
	}
	switch rel {
	case "README.md", "config.reference.yaml":
		return true
	default:
		return strings.HasPrefix(rel, "docs"+string(filepath.Separator))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root (go.mod) not found")
		}
		dir = parent
	}
}

func rel(root, path string) string {
	if r, err := filepath.Rel(root, path); err == nil {
		return r
	}
	return path
}
