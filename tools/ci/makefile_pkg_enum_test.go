package ci_test

import (
	"os"
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
	// A Go-only checkout has no web/node_modules: create it for the fixture
	// and remove only what this test created.
	nm := filepath.Join(root, "web", "node_modules")
	cleanup := filepath.Join(nm, "zz-b075-broken")
	if _, err := os.Stat(nm); os.IsNotExist(err) {
		cleanup = nm
	}
	bad := filepath.Join(nm, "zz-b075-broken")
	if err := os.MkdirAll(bad, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(cleanup) })
	if err := os.WriteFile(filepath.Join(bad, "bad.go"), []byte("not go\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr := printPkg(t, root)
	if strings.Contains(stderr, "zz-b075-broken") {
		t.Fatalf("go list enumerated web/node_modules: %s", stderr)
	}
}
