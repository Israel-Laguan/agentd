package ci_test

import (
	"bytes"
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
	pkgs, _ := printPkg(t, root)
	for _, p := range pkgs {
		if strings.Contains(p, "node_modules") {
			t.Errorf("PKG includes third-party package %s", p)
		}
	}
}

// printPkg runs make print-pkg and returns the package list and stderr. It
// fails the test when make fails, the list is empty, or go list failed (the
// Makefile then emits ./GO_LIST_FAILED instead of a short list).
func printPkg(t *testing.T, root string) ([]string, string) {
	t.Helper()
	cmd := exec.Command("make", "-s", "-C", root, "print-pkg")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("make print-pkg: %v: %s", err, stderr.String())
	}
	pkgs := strings.Fields(stdout.String())
	if len(pkgs) == 0 {
		t.Fatal("PKG is empty")
	}
	for _, p := range pkgs {
		if p == "./GO_LIST_FAILED" {
			t.Fatalf("go list failed while building PKG: %s", stderr.String())
		}
	}
	return pkgs, stderr.String()
}
