package config

import (
	"errors"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// B-065 key guard, loading half: type-check every package under cmd/ and
// internal/ from source (non-test files), with dependencies from the export
// data `go list -export` leaves in the build cache.

const configPkgPath = "agentd/internal/config"

// srcPkg is one module package type-checked from its non-test files.
type srcPkg struct {
	path  string
	files []*ast.File
	names []string // repo-relative, slash-separated, one per file
	info  *types.Info
}

// printOnlyFiles read config fields only to print them (`agentd config`);
// printing a value is not using it.
var printOnlyFiles = map[string]bool{"cmd/agentd/config.go": true}

type listedPkg struct {
	path, export, dir string
	files             []string
}

func loadModule(t *testing.T, root string) []*srcPkg {
	t.Helper()
	listed := listPackages(t, root)
	exports := make(map[string]string, len(listed))
	for _, p := range listed {
		exports[p.path] = p.export
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		return os.Open(exports[path])
	})
	var pkgs []*srcPkg
	for _, p := range listed {
		rel, err := filepath.Rel(root, p.dir)
		if err == nil && (strings.HasPrefix(rel, "cmd") || strings.HasPrefix(rel, "internal")) && len(p.files) > 0 {
			pkgs = append(pkgs, checkPackage(t, fset, imp, p, filepath.ToSlash(rel)))
		}
	}
	return pkgs
}

func hasFile(pkgs []*srcPkg, name string) bool {
	for _, p := range pkgs {
		for _, n := range p.names {
			if n == name {
				return true
			}
		}
	}
	return false
}

func listPackages(t *testing.T, root string) []listedPkg {
	t.Helper()
	format := "{{.ImportPath}}\t{{.Export}}\t{{.Dir}}\t{{join .GoFiles \",\"}}\t{{.Standard}}"
	cmd := exec.Command("go", "list", "-export", "-deps", "-f", format, "./cmd/...", "./internal/...")
	cmd.Dir = root
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("go list -export: %v\n%s", err, exitErr.Stderr)
	}
	if err != nil {
		t.Fatalf("go list -export: %v", err)
	}
	var pkgs []listedPkg
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		p := listedPkg{path: f[0], export: f[1], dir: f[2]}
		if f[4] == "false" && f[3] != "" {
			p.files = strings.Split(f[3], ",")
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func checkPackage(t *testing.T, fset *token.FileSet, imp types.Importer, p listedPkg, rel string) *srcPkg {
	t.Helper()
	sp := &srcPkg{path: p.path, info: newTypesInfo()}
	for _, name := range p.files {
		f, err := parser.ParseFile(fset, filepath.Join(p.dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		sp.files = append(sp.files, f)
		sp.names = append(sp.names, rel+"/"+name)
	}
	conf := types.Config{Importer: imp}
	if _, err := conf.Check(p.path, fset, sp.files, sp.info); err != nil {
		t.Fatalf("type-check %s: %v", p.path, err)
	}
	return sp
}

func newTypesInfo() *types.Info {
	return &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
	}
}

// consumers is what non-test code outside internal/config reads: struct
// fields, keyed by fieldPath, and internal/config functions it calls.
type consumers struct{ fields, funcs map[string]bool }

func indexConsumers(pkgs []*srcPkg) consumers {
	c := consumers{fields: map[string]bool{}, funcs: map[string]bool{}}
	for _, p := range pkgs {
		if p.path == configPkgPath {
			continue
		}
		for i, f := range p.files {
			if !printOnlyFiles[p.names[i]] {
				c.addFile(p.info, f)
			}
		}
	}
	return c
}

func (c consumers) addFile(info *types.Info, f *ast.File) {
	writes := writtenExprs([]*ast.File{f})
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if s := info.Selections[n]; s != nil && s.Kind() == types.FieldVal && !writes[n] {
				c.fields[fieldPath(s.Recv(), s.Index())] = true
			}
		case *ast.Ident:
			if fn, ok := info.Uses[n].(*types.Func); ok && fn.Pkg() != nil && fn.Pkg().Path() == configPkgPath {
				c.funcs[fn.Origin().FullName()] = true
			}
		}
		return true
	})
}

// writtenExprs are the selectors and identifiers assigned to, which are
// writes, not reads.
func writtenExprs(files []*ast.File) map[ast.Expr]bool {
	out := map[ast.Expr]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					out[assignedPlace(lhs)] = true
				}
			case *ast.IncDecStmt:
				out[assignedPlace(n.X)] = true
			}
			return true
		})
	}
	return out
}

// assignedPlace strips index, deref and parens: in `c.M[k] = v` the place
// written is c.M.
func assignedPlace(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.IndexExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return e
		}
	}
}

// fieldPath names the field a selection reaches as "<pkg>.<Type>.<Field>",
// the type being the struct that declares it (embedded fields resolved).
func fieldPath(recv types.Type, index []int) string {
	t := recv
	for i, idx := range index {
		st, owner := structOf(t)
		if st == nil {
			return ""
		}
		if i == len(index)-1 {
			return owner + "." + st.Field(idx).Name()
		}
		t = st.Field(idx).Type()
	}
	return ""
}

func structOf(t types.Type) (*types.Struct, string) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	owner := "struct"
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil {
		owner = n.Obj().Pkg().Path() + "." + n.Obj().Name()
	}
	st, _ := t.Underlying().(*types.Struct)
	return st, owner
}
