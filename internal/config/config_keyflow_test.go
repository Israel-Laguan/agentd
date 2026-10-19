package config

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"strings"
)

// B-065 key guard, tracing half. keyFlow resolves the key every viper read in
// internal/config asks for (constants, concatenation, locals, helper
// parameters, loops over literal slices) and follows the value it returns up
// the syntax tree to the struct field it lands in. A field counts as consumed
// when non-test code outside internal/config reads it, or when internal/config
// passes it on to a consumed field or to a function called from outside.
type keyFlow struct {
	info       *types.Info
	cons       consumers
	parent     map[ast.Node]ast.Node
	uses       map[types.Object][]*ast.Ident
	assigns    map[types.Object][]ast.Expr
	ranges     map[types.Object][]ast.Expr
	params     map[types.Object]paramRef
	calls      map[types.Object][]*ast.CallExpr
	fieldReads map[string][]ast.Expr
	writes     map[ast.Expr]bool
	viperCalls []*ast.CallExpr
	reads      map[string][]flowDest
	structs    map[string][]types.Type
	busy, done map[string]bool
}

type paramRef struct {
	fn  types.Object
	idx int
}

// keyRead is one key a read may ask for; via lists the helper call sites the
// key came through, innermost first.
type keyRead struct {
	key string
	via []*ast.CallExpr
}

// flowDest is where a read value ends up: a field, or the result of a
// function called from outside internal/config.
type flowDest struct {
	field    string
	external bool
}

var viperReadMethods = map[string]bool{
	"Get": true, "GetString": true, "GetBool": true, "GetInt": true, "GetInt32": true, "GetInt64": true,
	"GetUint": true, "GetUint16": true, "GetUint32": true, "GetUint64": true, "GetFloat64": true,
	"GetDuration": true, "GetTime": true, "GetSizeInBytes": true, "GetIntSlice": true,
	"GetStringSlice": true, "GetStringMap": true, "GetStringMapString": true,
	"GetStringMapStringSlice": true, "IsSet": true, "UnmarshalKey": true, "Sub": true,
}

func newKeyFlow(pkg *srcPkg, cons consumers) *keyFlow {
	k := &keyFlow{
		info: pkg.info, cons: cons, parent: map[ast.Node]ast.Node{},
		uses: map[types.Object][]*ast.Ident{}, assigns: map[types.Object][]ast.Expr{},
		ranges: map[types.Object][]ast.Expr{}, params: map[types.Object]paramRef{},
		calls: map[types.Object][]*ast.CallExpr{}, fieldReads: map[string][]ast.Expr{},
		writes: writtenExprs(pkg.files), reads: map[string][]flowDest{},
		structs: map[string][]types.Type{}, busy: map[string]bool{}, done: map[string]bool{},
	}
	for _, f := range pkg.files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				k.parent[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			k.index(n)
			return true
		})
	}
	k.collectReads()
	return k
}

func (k *keyFlow) index(n ast.Node) {
	switch n := n.(type) {
	case *ast.FuncDecl:
		k.indexParams(n)
	case *ast.Ident:
		if obj := k.info.Uses[n]; obj != nil && !k.writes[n] {
			k.uses[obj] = append(k.uses[obj], n)
		}
	case *ast.CallExpr:
		k.indexCall(n)
	case *ast.SelectorExpr:
		if s := k.info.Selections[n]; s != nil && s.Kind() == types.FieldVal && !k.writes[n] {
			path := fieldPath(s.Recv(), s.Index())
			k.fieldReads[path] = append(k.fieldReads[path], n)
		}
	case *ast.AssignStmt, *ast.ValueSpec, *ast.RangeStmt:
		k.indexAssign(n)
	}
}

// indexAssign records the values locals are given, for eval.
func (k *keyFlow) indexAssign(n ast.Node) {
	switch n := n.(type) {
	case *ast.AssignStmt:
		if len(n.Lhs) == len(n.Rhs) {
			for i, lhs := range n.Lhs {
				k.addAssign(lhs, n.Rhs[i])
			}
		}
	case *ast.ValueSpec:
		for i, name := range n.Names {
			if i < len(n.Values) {
				k.addAssign(name, n.Values[i])
			}
		}
	case *ast.RangeStmt:
		if id, ok := n.Value.(*ast.Ident); ok {
			k.ranges[k.objOf(id)] = append(k.ranges[k.objOf(id)], n.X)
		}
	}
}

func (k *keyFlow) indexParams(fd *ast.FuncDecl) {
	fn := k.info.Defs[fd.Name]
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return
	}
	for i := 0; i < sig.Params().Len(); i++ {
		k.params[sig.Params().At(i)] = paramRef{fn: fn, idx: i}
	}
}

func (k *keyFlow) indexCall(call *ast.CallExpr) {
	if fn := k.callee(call); fn != nil {
		k.calls[fn] = append(k.calls[fn], call)
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if ok && viperReadMethods[sel.Sel.Name] && len(call.Args) > 0 &&
		types.TypeString(k.info.TypeOf(sel.X), nil) == "*github.com/spf13/viper.Viper" {
		k.viperCalls = append(k.viperCalls, call)
	}
}

func (k *keyFlow) addAssign(lhs, rhs ast.Expr) {
	if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
		k.assigns[k.objOf(id)] = append(k.assigns[k.objOf(id)], rhs)
	}
}

func (k *keyFlow) objOf(id *ast.Ident) types.Object {
	if obj := k.info.Defs[id]; obj != nil {
		return obj
	}
	return k.info.Uses[id]
}

// callee is the internal/config function or method a call invokes, if any.
func (k *keyFlow) callee(call *ast.CallExpr) types.Object {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	default:
		return nil
	}
	fn, ok := k.info.Uses[id].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != configPkgPath {
		return nil
	}
	return fn.Origin()
}

func (k *keyFlow) collectReads() {
	for _, call := range k.viperCalls {
		for _, r := range k.eval(call.Args[0], 0) {
			key := strings.ToLower(r.key)
			k.reads[key] = append(k.reads[key], k.readDests(call, r.via)...)
			k.recordStruct(key, call)
		}
	}
}

// recordStruct notes the struct type an UnmarshalKey decodes key into, so
// sub-keys can be matched to its fields.
func (k *keyFlow) recordStruct(key string, call *ast.CallExpr) {
	sel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if sel.Sel.Name != "UnmarshalKey" || len(call.Args) < 2 {
		return
	}
	if st, _ := structOf(k.info.TypeOf(call.Args[1])); st != nil {
		k.structs[key] = append(k.structs[key], k.info.TypeOf(call.Args[1]))
	}
}

func (k *keyFlow) readDests(call *ast.CallExpr, via []*ast.CallExpr) []flowDest {
	w := &walk{k: k, seen: map[walkStep]bool{}}
	if sel := ast.Unparen(call.Fun).(*ast.SelectorExpr); sel.Sel.Name == "UnmarshalKey" && len(call.Args) > 1 {
		w.target(call.Args[1], via)
	} else {
		w.up(call, 0, via)
	}
	return w.out
}

// eval returns the string values e can hold, or nil when it cannot tell.
func (k *keyFlow) eval(e ast.Expr, depth int) []keyRead {
	if depth > 8 {
		return nil
	}
	if tv, ok := k.info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return []keyRead{{key: constant.StringVal(tv.Value)}}
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return k.eval(e.X, depth+1)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return concatReads(k.eval(e.X, depth+1), k.eval(e.Y, depth+1))
		}
	case *ast.Ident:
		return k.evalVar(k.info.Uses[e], depth+1)
	}
	return nil
}

func (k *keyFlow) evalVar(obj types.Object, depth int) []keyRead {
	var out []keyRead
	if p, ok := k.params[obj]; ok {
		for _, call := range k.calls[p.fn] {
			if p.idx < len(call.Args) {
				for _, r := range k.eval(call.Args[p.idx], depth) {
					out = append(out, keyRead{key: r.key, via: append([]*ast.CallExpr{call}, r.via...)})
				}
			}
		}
		return out
	}
	for _, v := range k.assigns[obj] {
		out = append(out, k.eval(v, depth)...)
	}
	for _, x := range k.ranges[obj] {
		for _, el := range k.elems(x) {
			out = append(out, k.eval(el, depth)...)
		}
	}
	return out
}

// elems are the elements of a slice literal, given directly or through a
// local it was assigned to.
func (k *keyFlow) elems(x ast.Expr) []ast.Expr {
	if lit, ok := ast.Unparen(x).(*ast.CompositeLit); ok {
		return lit.Elts
	}
	id, ok := ast.Unparen(x).(*ast.Ident)
	if !ok {
		return nil
	}
	var out []ast.Expr
	for _, v := range k.assigns[k.info.Uses[id]] {
		if lit, ok := v.(*ast.CompositeLit); ok {
			out = append(out, lit.Elts...)
		}
	}
	return out
}

func concatReads(left, right []keyRead) []keyRead {
	var out []keyRead
	for _, l := range left {
		for _, r := range right {
			out = append(out, keyRead{key: l.key + r.key, via: append(append([]*ast.CallExpr{}, l.via...), r.via...)})
		}
	}
	return out
}

// consumed reports whether a field is read outside internal/config, directly
// or after internal/config passes its value on.
func (k *keyFlow) consumed(field string) bool {
	if k.cons.fields[field] || k.done[field] {
		return true
	}
	if k.busy[field] {
		return false
	}
	k.busy[field] = true
	defer delete(k.busy, field)
	for _, sel := range k.fieldReads[field] {
		w := &walk{k: k, seen: map[walkStep]bool{}}
		w.up(sel, 0, nil)
		if k.anyConsumed(w.out) {
			k.done[field] = true
			return true
		}
	}
	return false
}

func (k *keyFlow) anyConsumed(dests []flowDest) bool {
	for _, d := range dests {
		if d.external || k.consumed(d.field) {
			return true
		}
	}
	return false
}

// leafError says why a documented key fails the guard; nil when its value
// reaches a consumer.
func (k *keyFlow) leafError(key string) []string {
	key = strings.ToLower(key)
	readKey, ok := k.readKeyFor(key)
	if !ok {
		return []string{key + ": documented, but internal/config never reads it"}
	}
	if readKey != key && len(k.structs[readKey]) > 0 {
		return k.structLeafError(key, readKey)
	}
	if !k.anyConsumed(k.reads[readKey]) {
		return []string{key + ": read, but nothing outside internal/config uses the value (lands in " + destNames(k.reads[readKey]) + ")"}
	}
	return nil
}

// structLeafError checks a key under one decoded whole into a struct: the
// struct needs a field for it, and that field needs a consumer.
func (k *keyFlow) structLeafError(key, readKey string) []string {
	path := strings.Split(key[len(readKey)+1:], ".")
	for _, t := range k.structs[readKey] {
		field, ok := nestedField(t, path)
		if !ok {
			return []string{key + ": decoded with " + readKey + ", but " + types.TypeString(t, nil) + " has no field for it"}
		}
		if !k.consumed(field) {
			return []string{key + ": read, but nothing outside internal/config uses the value (lands in " + strings.TrimPrefix(field, "agentd/internal/") + ")"}
		}
	}
	return nil
}

// readKeyFor returns key when it is read, else the nearest parent key read
// whole (a map, a list or a struct).
func (k *keyFlow) readKeyFor(key string) (string, bool) {
	for {
		if _, ok := k.reads[key]; ok {
			return key, true
		}
		i := strings.LastIndex(key, ".")
		if i < 0 {
			return "", false
		}
		key = key[:i]
	}
}

// nestedField follows a dotted sub-key through struct fields the way
// mapstructure decodes it: the mapstructure tag, else the field name in any
// case.
func nestedField(t types.Type, path []string) (string, bool) {
	for i, seg := range path {
		st, owner := structOf(t)
		if st == nil {
			return "", false
		}
		f := fieldByKey(st, seg)
		if f == nil {
			return "", false
		}
		if i == len(path)-1 {
			return owner + "." + f.Name(), true
		}
		t = f.Type()
	}
	return "", false
}

func fieldByKey(st *types.Struct, key string) *types.Var {
	for i := 0; i < st.NumFields(); i++ {
		tag := reflect.StructTag(st.Tag(i)).Get("mapstructure")
		name, _, _ := strings.Cut(tag, ",")
		if name == key || (name == "" && strings.EqualFold(st.Field(i).Name(), key)) {
			return st.Field(i)
		}
	}
	return nil
}

func destNames(dests []flowDest) string {
	seen := map[string]bool{}
	var names []string
	for _, d := range dests {
		name := strings.TrimPrefix(d.field, "agentd/internal/")
		if !d.external && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "no field"
	}
	return strings.Join(names, ", ")
}
