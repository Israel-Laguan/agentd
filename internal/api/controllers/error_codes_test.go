package controllers_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestControllersUseNamedErrorCodes pins T-054: every error envelope a
// controller writes takes its code from an httpx.Code* constant, never a
// string literal.
func TestControllersUseNamedErrorCodes(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			idx := errorCodeArg(sel.Sel.Name)
			if idx < 0 || len(call.Args) <= idx {
				return true
			}
			checked++
			if lit, ok := call.Args[idx].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				v, _ := strconv.Unquote(lit.Value)
				t.Errorf("%s: literal error code %q; use an httpx.Code* constant", fset.Position(lit.Pos()), v)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatal("found no httpx error writers; the scan is broken")
	}
}

// errorCodeArg is the index of the code argument of an httpx writer, or -1.
func errorCodeArg(name string) int {
	switch name {
	case "WriteError", "WriteValidationError":
		return 2
	}
	return -1
}
