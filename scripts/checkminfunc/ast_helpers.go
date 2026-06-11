package main

import (
	"go/ast"
	"strings"
)

func typeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + typeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return typeName(t.X) + "." + t.Sel.Name
	case *ast.IndexExpr:
		return typeName(t.X) + "[" + typeName(t.Index) + "]"
	case *ast.IndexListExpr:
		var buf strings.Builder
		buf.WriteString(typeName(t.X))
		buf.WriteString("[")
		for i, idx := range t.Indices {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(typeName(idx))
		}
		buf.WriteString("]")
		return buf.String()
	default:
		return ""
	}
}
