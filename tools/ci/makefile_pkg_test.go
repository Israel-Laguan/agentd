package ci_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMakePkgExcludesNodeModules pins B-075: npm packages may ship Go code
// (web/node_modules/flatted/golang), which must stay out of the default PKG
// list used by make test, lint and coverage.
func TestMakePkgExcludesNodeModules(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("make", "-s", "-C", root, "print-pkg").Output()
	if err != nil {
		t.Fatalf("make print-pkg: %v", err)
	}
	pkgs := strings.Fields(string(out))
	if len(pkgs) == 0 {
		t.Fatal("PKG is empty")
	}
	for _, p := range pkgs {
		if strings.Contains(p, "node_modules") {
			t.Errorf("PKG includes third-party package %s", p)
		}
	}
}
