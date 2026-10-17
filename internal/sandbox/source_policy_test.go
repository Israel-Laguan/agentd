package sandbox

import (
	"os"
	"path/filepath"
	"testing"
)

type policyFixture struct {
	root, home, outside string
}

// newPolicyFixture lays out <root>/.agentd (the agentd home), <root>/repo and
// a directory outside root.
func newPolicyFixture(t *testing.T) policyFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := policyFixture{root: root, home: filepath.Join(root, ".agentd"), outside: t.TempDir()}
	for _, d := range []string{filepath.Join(f.home, "projects", "p1"), filepath.Join(root, "repo", "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestSourcePolicyAllowsDirectoriesUnderRoot(t *testing.T) {
	f := newPolicyFixture(t)
	p := SourcePolicy{Root: f.root, AgentdHome: f.home}
	for _, src := range []string{filepath.Join(f.root, "repo"), filepath.Join(f.root, "repo", "sub")} {
		got, err := p.Resolve(src)
		if err != nil || got != src {
			t.Errorf("Resolve(%s) = %q, %v; want the path back", src, got, err)
		}
	}
}

func TestSourcePolicyRefuses(t *testing.T) {
	f := newPolicyFixture(t)
	escape := filepath.Join(f.root, "repo", "escape")
	if err := os.Symlink(f.outside, escape); err != nil {
		t.Fatal(err)
	}
	homeLink := filepath.Join(f.root, "repo", "home-link")
	if err := os.Symlink(f.home, homeLink); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		policy SourcePolicy
		src    string
	}{
		"outside root":                    {SourcePolicy{Root: f.root, AgentdHome: f.home}, f.outside},
		"symlink under root to elsewhere": {SourcePolicy{Root: f.root, AgentdHome: f.home}, escape},
		"agentd home":                     {SourcePolicy{Root: f.root, AgentdHome: f.home}, f.home},
		"inside agentd home":              {SourcePolicy{Root: f.root, AgentdHome: f.home}, filepath.Join(f.home, "projects", "p1")},
		"parent of agentd home":           {SourcePolicy{Root: f.root, AgentdHome: f.home}, f.root},
		"symlink to agentd home":          {SourcePolicy{Root: f.root, AgentdHome: f.home}, homeLink},
		"relative escape":                 {SourcePolicy{Root: f.root, AgentdHome: f.home}, filepath.Join(f.root, "repo", "..", "..")},
		"empty root refuses all":          {SourcePolicy{AgentdHome: f.home}, filepath.Join(f.root, "repo")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := tc.policy.Resolve(tc.src); err == nil {
				t.Fatalf("Resolve(%s) = %q, want refusal", tc.src, got)
			}
		})
	}
}
