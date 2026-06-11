package main

import (
	"go/ast"
	"go/token"
	"strings"
)

func typeName(expr ast.Expr) string {
	if name, ok := typeNameBasic(expr); ok {
		return name
	}
	return typeNameComposite(expr)
}

func typeNameBasic(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, true
	case *ast.SelectorExpr:
		return typeName(t.X) + "." + t.Sel.Name, true
	case *ast.ParenExpr:
		return "(" + typeName(t.X) + ")", true
	case *ast.BasicLit:
		return t.Value, true
	case *ast.Ellipsis:
		return "..." + typeName(t.Elt), true
	case *ast.UnaryExpr:
		return typeNameUnary(t), true
	case *ast.BinaryExpr:
		return typeName(t.X) + binarySeparator(t.Op) + typeName(t.Y), true
	default:
		return "", false
	}
}

func typeNameComposite(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + typeName(t.X)
	case *ast.IndexExpr:
		return typeName(t.X) + "[" + typeName(t.Index) + "]"
	case *ast.IndexListExpr:
		return indexListTypeName(t)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeName(t.Elt)
		}
		return "[" + typeName(t.Len) + "]" + typeName(t.Elt)
	case *ast.MapType:
		return "map[" + typeName(t.Key) + "]" + typeName(t.Value)
	case *ast.FuncType:
		return "func" + fieldList(t.Params, false, false) + fieldListResults(t.Results)
	case *ast.StructType:
		return "struct" + fieldList(t.Fields, true, false)
	case *ast.InterfaceType:
		return "interface" + fieldList(t.Methods, true, true)
	case *ast.ChanType:
		return chanPrefix(t.Dir) + typeName(t.Value)
	default:
		return ""
	}
}

func typeNameUnary(t *ast.UnaryExpr) string {
	if t.Op == token.TILDE {
		return "~" + typeName(t.X)
	}
	return t.Op.String() + " " + typeName(t.X)
}

func indexListTypeName(t *ast.IndexListExpr) string {
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
}

func chanPrefix(dir ast.ChanDir) string {
	switch dir {
	case ast.SEND:
		return "chan<- "
	case ast.RECV:
		return "<-chan "
	default:
		return "chan "
	}
}

func binarySeparator(op token.Token) string {
	if op == token.OR {
		return " | "
	}
	return " " + op.String() + " "
}

func fieldList(fields *ast.FieldList, braced bool, interfaceMethods bool) string {
	if fields == nil {
		return "()"
	}

	var parts []string
	for _, field := range fields.List {
		if field == nil {
			continue
		}
		parts = append(parts, fieldName(field, interfaceMethods))
	}

	if !braced {
		return "(" + strings.Join(parts, ", ") + ")"
	}
	return "{" + strings.Join(parts, "; ") + "}"
}

func fieldListResults(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) == 0 {
		return ""
	}
	if len(fields.List) == 1 {
		field := fields.List[0]
		if field != nil && len(field.Names) == 0 && field.Tag == nil {
			return " " + typeName(field.Type)
		}
	}
	return fieldList(fields, false, false)
}

func fieldName(field *ast.Field, interfaceMethod bool) string {
	if interfaceMethod && len(field.Names) == 1 {
		if fn, ok := field.Type.(*ast.FuncType); ok {
			return field.Names[0].Name + fieldList(fn.Params, false, false) + fieldListResults(fn.Results)
		}
	}

	typ := typeName(field.Type)
	if len(field.Names) == 0 {
		if field.Tag != nil {
			return typ + " " + field.Tag.Value
		}
		return typ
	}

	names := make([]string, 0, len(field.Names))
	for _, name := range field.Names {
		names = append(names, name.Name)
	}
	if field.Tag != nil {
		return strings.Join(names, ", ") + " " + typ + " " + field.Tag.Value
	}
	return strings.Join(names, ", ") + " " + typ
}
