package ci_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// B-075 review: PKG must not enumerate web/node_modules at all. A broken Go
// file in an npm package used to make `go list ./...` fail, and the grep after
// it hid the failure.
func TestMakePkgDoesNotEnumerateNodeModules(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	nm := filepath.Join(root, "web", "node_modules")
	if _, err := os.Stat(nm); err != nil {
		t.Skip("web/node_modules not installed")
	}
	bad := filepath.Join(nm, "zz-b075-broken")
	if err := os.MkdirAll(bad, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(bad) })
	if err := os.WriteFile(filepath.Join(bad, "bad.go"), []byte("not go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("make", "-s", "-C", root, "print-pkg")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("make print-pkg: %v: %s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "zz-b075-broken") {
		t.Fatalf("go list enumerated web/node_modules: %s", stderr.String())
	}
}
