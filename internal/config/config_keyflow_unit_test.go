package config

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"testing"
)

// The tracer on small loaders, without the module: src is type-checked as
// internal/config, and consumed lists the fields taken as read elsewhere.

// var a, b = pair(v) gives the read's value (tuple position 1) to b. Only a
// is consumed, so the key must fail.
func TestKeyFlow_MultiValueVarTracksTuplePosition(t *testing.T) {
	flow := keyFlowForSource(t, `package config

import "github.com/spf13/viper"

type Out struct{ Used, Unused string }

func pair(v *viper.Viper) (string, string) { return "", v.GetString("k.leaf") }

func load(v *viper.Viper) Out {
	var a, b = pair(v)
	return Out{Used: a, Unused: b}
}
`, configPkgPath+".Out.Used")

	if errs := flow.leafError("k.leaf"); len(errs) == 0 {
		t.Fatal("k.leaf lands in Out.Unused, which nothing reads, but the guard accepted it")
	}
}

func keyFlowForSource(t *testing.T, src string, consumed ...string) *keyFlow {
	t.Helper()
	exports := map[string]string{}
	for _, p := range listPackages(t, repoRoot(t)) {
		exports[p.path] = p.export
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "loader.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(exports[path])
	})
	info := newTypesInfo()
	conf := types.Config{Importer: imp}
	if _, err := conf.Check(configPkgPath, fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	cons := consumers{fields: map[string]bool{}, funcs: map[string]bool{}}
	for _, field := range consumed {
		cons.fields[field] = true
	}
	return newKeyFlow(&srcPkg{path: configPkgPath, files: []*ast.File{f}, info: info}, cons)
}
