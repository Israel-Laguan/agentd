package config

import (
	"go/ast"
	"go/types"
)

// walk follows one value up the syntax tree of internal/config: through
// expressions and call results, into locals (then their reads), out of
// returns (to the caller the key came through, or every caller), and stops
// at the struct field it is stored in. Calls over-approximate: an argument
// is taken to flow into the call's result.
type walk struct {
	k    *keyFlow
	seen map[walkStep]bool
	out  []flowDest
}

type walkStep struct {
	n   ast.Node
	idx int
}

// up moves from e to its parent. idx is the tuple position e's value holds
// when e is a call returning several values.
func (w *walk) up(e ast.Node, idx int, via []*ast.CallExpr) {
	if w.seen[walkStep{e, idx}] {
		return
	}
	w.seen[walkStep{e, idx}] = true
	switch p := w.k.parent[e].(type) {
	case *ast.CallExpr:
		if p.Fun != e {
			w.up(p, 0, via)
		}
	case *ast.SelectorExpr:
		if s := w.k.info.Selections[p]; s != nil && s.Kind() == types.MethodVal {
			w.up(p, 0, via)
		}
	case *ast.KeyValueExpr:
		w.upKeyValue(p, e, via)
	case *ast.AssignStmt:
		w.upAssign(p, e, idx, via)
	case *ast.ValueSpec:
		w.upValueSpec(p, e, via)
	case *ast.ReturnStmt:
		w.upReturn(p, e, idx, via)
	case *ast.RangeStmt, *ast.SwitchStmt:
		w.upControl(p, e, via)
	default:
		w.upExpr(p, e, via)
	}
}

// upExpr passes a value through the expressions that carry it unchanged
// enough to matter: parens, operators, conversions, slicing, indexing into
// it, and slice or map literals holding it.
func (w *walk) upExpr(p ast.Node, e ast.Node, via []*ast.CallExpr) {
	switch p := p.(type) {
	case *ast.ParenExpr, *ast.StarExpr, *ast.UnaryExpr, *ast.BinaryExpr, *ast.TypeAssertExpr, *ast.SliceExpr:
		w.up(p, 0, via)
	case *ast.IndexExpr:
		if p.X == e {
			w.up(p, 0, via)
		}
	case *ast.CompositeLit:
		w.up(p, 0, via)
	}
}

func (w *walk) upKeyValue(kv *ast.KeyValueExpr, e ast.Node, via []*ast.CallExpr) {
	lit, ok := w.k.parent[kv].(*ast.CompositeLit)
	if !ok || kv.Value != e {
		return
	}
	st, owner := structOf(w.k.info.TypeOf(lit))
	key, isIdent := kv.Key.(*ast.Ident)
	if st == nil || !isIdent {
		w.up(lit, 0, via)
		return
	}
	w.out = append(w.out, flowDest{field: owner + "." + key.Name})
}

func (w *walk) upAssign(as *ast.AssignStmt, e ast.Node, idx int, via []*ast.CallExpr) {
	if sw, ok := w.k.parent[as].(*ast.TypeSwitchStmt); ok && sw.Assign == as {
		for _, clause := range sw.Body.List {
			if obj := w.k.info.Implicits[clause]; obj != nil {
				w.follow(obj, via)
			}
		}
		return
	}
	pos := position(as.Rhs, e)
	if pos < 0 {
		return
	}
	if _, isCall := ast.Unparen(as.Rhs[0]).(*ast.CallExpr); len(as.Lhs) != len(as.Rhs) && !isCall {
		for _, lhs := range as.Lhs { // comma-ok: ok depends on the value too
			w.target(lhs, via)
		}
		return
	}
	if len(as.Lhs) != len(as.Rhs) {
		pos = idx
	}
	if pos < len(as.Lhs) {
		w.target(as.Lhs[pos], via)
	}
}

func (w *walk) upValueSpec(vs *ast.ValueSpec, e ast.Node, via []*ast.CallExpr) {
	pos := position(vs.Values, e)
	if pos < 0 {
		return
	}
	if len(vs.Names) != len(vs.Values) {
		pos = 0
	}
	w.target(vs.Names[pos], via)
}

func (w *walk) upReturn(ret *ast.ReturnStmt, e ast.Node, idx int, via []*ast.CallExpr) {
	pos := position(ret.Results, e)
	if pos < 0 {
		return
	}
	if len(ret.Results) == 1 {
		pos = max(pos, idx)
	}
	fn := w.enclosingFunc(ret)
	if fn == nil {
		return
	}
	if w.k.cons.funcs[fn.(*types.Func).FullName()] {
		w.out = append(w.out, flowDest{external: true})
	}
	if len(via) > 0 && w.k.callee(via[0]) == fn {
		w.up(via[0], pos, via[1:])
		return
	}
	for _, call := range w.k.calls[fn] {
		w.up(call, pos, via)
	}
}

// upControl follows a value a statement controls: a range over it into the
// loop variables, a switch on it into what its cases return.
func (w *walk) upControl(p ast.Node, e ast.Node, via []*ast.CallExpr) {
	switch p := p.(type) {
	case *ast.RangeStmt:
		if p.X == e {
			w.target(p.Key, via)
			w.target(p.Value, via)
		}
	case *ast.SwitchStmt:
		if p.Tag == e {
			w.upCaseReturns(p, via)
		}
	}
}

// upCaseReturns follows a switch tag into the values its cases return: the
// tag picks the result (as in TruncationConfig.StrategyImpl).
func (w *walk) upCaseReturns(sw *ast.SwitchStmt, via []*ast.CallExpr) {
	ast.Inspect(sw.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			for _, r := range ret.Results {
				w.upReturn(ret, r, 0, via)
			}
		}
		_, isLit := n.(*ast.FuncLit)
		return !isLit
	})
}

func (w *walk) enclosingFunc(n ast.Node) types.Object {
	for n != nil {
		switch f := n.(type) {
		case *ast.FuncLit:
			return nil
		case *ast.FuncDecl:
			return w.k.info.Defs[f.Name]
		}
		n = w.k.parent[n]
	}
	return nil
}

// target records where an assigned value goes: a field, or a local whose
// reads are followed in turn.
func (w *walk) target(lhs ast.Expr, via []*ast.CallExpr) {
	switch x := ast.Unparen(lhs).(type) {
	case *ast.Ident:
		if obj := w.k.objOf(x); obj != nil && x.Name != "_" {
			w.follow(obj, via)
		}
	case *ast.SelectorExpr:
		if s := w.k.info.Selections[x]; s != nil && s.Kind() == types.FieldVal {
			w.out = append(w.out, flowDest{field: fieldPath(s.Recv(), s.Index())})
		}
	case *ast.IndexExpr:
		w.target(x.X, via)
	case *ast.StarExpr:
		w.target(x.X, via)
	case *ast.UnaryExpr:
		w.target(x.X, via)
	}
}

func (w *walk) follow(obj types.Object, via []*ast.CallExpr) {
	for _, use := range w.k.uses[obj] {
		w.up(use, 0, via)
	}
}

func position(list []ast.Expr, e ast.Node) int {
	for i, x := range list {
		if x == e {
			return i
		}
	}
	return -1
}
