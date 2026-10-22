package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Env struct {
	Vars    map[string]Value
	History map[string][]Value
	Parent  *Env
}

func NewEnv(parent *Env) *Env {
	return &Env{Vars: map[string]Value{}, History: map[string][]Value{}, Parent: parent}
}

func (e *Env) Get(name string) Value {
	if v, ok := e.Vars[name]; ok {
		return v
	}
	if e.Parent != nil {
		return e.Parent.Get(name)
	}
	panic(errName(name))
}

func (e *Env) GetPast(name string, depth int) Value {
	env := e
	for env != nil {
		if hist, ok := env.History[name]; ok && len(hist) >= depth {
			if depth == 0 {
				if len(hist) > 0 {
					return hist[0]
				}
			} else {
				return hist[len(hist)-depth]
			}
		}
		if depth == 0 {
			if v, ok := env.Vars[name]; ok {
				return v
			}
		}
		env = env.Parent
	}
	panic(&AlbError{Kind: "NameError", Msg: fmt.Sprintf("no past value for '%s' at depth %d", name, depth)})
}

func (e *Env) Set(name string, value Value) {
	env := e
	for env != nil {
		if _, ok := env.Vars[name]; ok {
			hist := append(env.History[name], env.Vars[name])
			if len(hist) > 64 {
				hist = hist[len(hist)-32:]
			}
			env.History[name] = hist
			env.Vars[name] = value
			return
		}
		env = env.Parent
	}
	e.Vars[name] = value
	if _, ok := e.History[name]; !ok {
		e.History[name] = []Value{}
	}
}

func (e *Env) Define(name string, value Value) {
	e.Vars[name] = value
	if _, ok := e.History[name]; !ok {
		e.History[name] = []Value{}
	}
}

func (e *Env) Has(name string) bool {
	if _, ok := e.Vars[name]; ok {
		return true
	}
	if e.Parent != nil {
		return e.Parent.Has(name)
	}
	return false
}

type Interp struct {
	Global       *Env
	Modules      map[string]*Module
	LastValue    Value
	LastList     Value
	LastAssigned Value
	Rand         *rand.Rand
	Filename     string
	Stdout       *os.File
}

func NewInterp(filename string) *Interp {
	in := &Interp{
		Global:   NewEnv(nil),
		Modules:  map[string]*Module{},
		Rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
		Filename: filename,
		Stdout:   os.Stdout,
	}
	in.setupBuiltins()
	return in
}

func (in *Interp) out(s string) {
	fmt.Fprint(in.Stdout, s)
}

func (in *Interp) Run(prog *Program) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(returnSignal); ok {
				return
			}
			panic(r)
		}
	}()
	for _, stmt := range prog.Stmts {
		in.eval(stmt, in.Global)
	}
}

func isCatchable(r interface{}) bool {
	switch r.(type) {
	case *AlbError, returnSignal, breakSignal, continueSignal:
		return true
	}
	return false
}

func signalValue(r interface{}) Value {
	switch s := r.(type) {
	case *AlbError:
		return s
	case returnSignal:
		return &caughtSignal{label: "<ReturnValue object>"}
	case breakSignal:
		return &caughtSignal{label: "<BreakSignal object>"}
	case continueSignal:
		return &caughtSignal{label: "<ContinueSignal object>"}
	}
	return nil
}

func (in *Interp) eval(node Node, env *Env) Value {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case *NumLit:
		return n.V
	case *StrLit:
		return n.V
	case *BoolLit:
		return n.V
	case *NullLit:
		return nil
	case *Ident:
		switch n.Name {
		case "vita":
			return in.LastValue
		case "kole":
			return in.LastList
		case "bond":
			return in.LastAssigned
		}
		return env.Get(n.Name)
	case *ListLit:
		lst := &List{}
		for _, e := range n.Elems {
			lst.Elems = append(lst.Elems, in.eval(e, env))
		}
		return lst
	case *DictLit:
		d := NewDict()
		for _, pr := range n.Pairs {
			k := in.eval(pr[0], env)
			v := in.eval(pr[1], env)
			d.Set(k, v)
		}
		return d
	case *Binary:
		if n.Op == "and" {
			left := in.eval(n.L, env)
			if !truthy(left) {
				return left
			}
			return in.eval(n.R, env)
		}
		if n.Op == "or" {
			left := in.eval(n.L, env)
			if truthy(left) {
				return left
			}
			return in.eval(n.R, env)
		}
		left := in.eval(n.L, env)
		right := in.eval(n.R, env)
		return in.applyBinary(n.Op, left, right)
	case *Unary:
		val := in.eval(n.E, env)
		switch n.Op {
		case "-":
			if iv, ok := val.(int64); ok {
				return -iv
			}
			return -toFloat(val)
		case "!", "not":
			return !truthy(val)
		case "+":
			if iv, ok := toIntValue(val); ok && !isFloatVal(val) {
				return iv
			}
			return toFloat(val)
		}
		panic(&AlbError{Kind: "RuntimeError", Msg: "Unknown unary: " + n.Op})
	case *Index:
		obj := in.eval(n.Obj, env)
		idx := in.eval(n.Idx, env)
		return in.indexGet(obj, idx)
	case *Attr:
		obj := in.eval(n.Obj, env)
		return in.getAttr(obj, n.Name)
	case *Call:
		fn := in.eval(n.Fn, env)
		args := make([]Value, len(n.Args))
		for i, a := range n.Args {
			args[i] = in.eval(a, env)
		}
		var kwargs map[string]Value
		if len(n.Kwargs) > 0 {
			kwargs = map[string]Value{}
			for _, k := range n.KwargOrder {
				kwargs[k] = in.eval(n.Kwargs[k], env)
			}
		}
		return in.callValue(fn, args, kwargs)
	case *Assign:
		value := in.eval(n.V, env)
		in.assign(n.Target, value, env)
		in.LastValue = value
		in.LastAssigned = value
		if _, ok := value.(*List); ok {
			in.LastList = value
		}
		return value
	case *AugAssign:
		current := in.eval(n.Target, env)
		right := in.eval(n.V, env)
		var newVal Value
		switch n.Op {
		case "**=":
			newVal = in.pyPow(current, right)
		case "//=":
			newVal = in.applyBinary("//", current, right)
		default:
			newVal = in.applyBinary(n.Op[:1], current, right)
		}
		in.assign(n.Target, newVal, env)
		return newVal
	case *PrintStmt:
		parts := make([]string, len(n.Exprs))
		for i, e := range n.Exprs {
			parts[i] = stringify(in.eval(e, env))
		}
		in.out(strings.Join(parts, " ") + "\n")
		return nil
	case *IfStmt:
		if truthy(in.eval(n.Cond, env)) {
			return in.eval(n.Then, env)
		}
		for _, el := range n.Elifs {
			if truthy(in.eval(el.Cond, env)) {
				return in.eval(el.Body, env)
			}
		}
		if n.Else != nil {
			return in.eval(n.Else, env)
		}
		return nil
	case *WhileStmt:
		return in.evalWhile(n, env)
	case *ForIn:
		iterable := in.eval(n.Iterable, env)
		items := in.iterate(iterable)
		for _, item := range items {
			in.assign(n.Target, item, env)
			if !in.evalLoopBody(n.Body, env) {
				break
			}
		}
		return nil
	case *ForRange:
		start := in.toIntStrict(in.eval(n.Start, env))
		end := in.toIntStrict(in.eval(n.End, env))
		step := int64(1)
		if n.Step != nil {
			step = in.toIntStrict(in.eval(n.Step, env))
		}
		if step == 0 {
			panic(&AlbError{Kind: "ValueError", Msg: "range() arg 3 must not be zero"})
		}
		stop := end + 1
		if step > 0 {
			for i := start; i < stop; i += step {
				env.Set(n.Var, i)
				if !in.evalLoopBody(n.Body, env) {
					break
				}
			}
		} else {
			for i := start; i > stop; i += step {
				env.Set(n.Var, i)
				if !in.evalLoopBody(n.Body, env) {
					break
				}
			}
		}
		return nil
	case *FuncDef:
		env.Define(n.Name, &Function{Name: n.Name, Params: n.Params, Body: n.Body, Env: env, IsMethod: n.IsMethod})
		return nil
	case *LambdaExpr:
		return &Function{Name: "", Params: n.Params, Body: n.Body, Env: env}
	case *ClassDef:
		methods := map[string]*Function{}
		classEnv := NewEnv(env)
		for _, stmt := range n.Body.Stmts {
			if fd, ok := stmt.(*FuncDef); ok {
				methods[fd.Name] = &Function{Name: fd.Name, Params: fd.Params, Body: fd.Body, Env: classEnv, IsMethod: true}
			} else {
				in.eval(stmt, classEnv)
			}
		}
		bases := make([]Value, len(n.Bases))
		for i, b := range n.Bases {
			bases[i] = in.eval(b, env)
		}
		env.Define(n.Name, &Class{Name: n.Name, Bases: bases, Methods: methods, Env: env})
		return nil
	case *ReturnStmt:
		if n.V != nil {
			panic(returnSignal{v: in.eval(n.V, env)})
		}
		panic(returnSignal{v: nil})
	case *BreakStmt:
		panic(breakSignal{})
	case *ContinueStmt:
		panic(continueSignal{})
	case *PassStmt:
		return nil
	case *RaiseStmt:
		var val Value
		if n.E != nil {
			val = in.eval(n.E, env)
		}
		if ae, ok := val.(*AlbError); ok {
			panic(ae)
		}
		if cs, ok := val.(*caughtSignal); ok {
			panic(&AlbError{Kind: "AlbishError", Msg: cs.label})
		}
		if val != nil {
			panic(&AlbError{Kind: "AlbishError", Msg: pyStr(val)})
		}
		panic(&AlbError{Kind: "AlbishError", Msg: "raised"})
	case *TryStmt:
		return in.evalTry(n, env)
	case *ImportStmt:
		mod := in.loadModule(n.Module)
		name := n.Alias
		if name == "" {
			parts := strings.Split(n.Module, ".")
			name = parts[len(parts)-1]
		}
		env.Define(name, mod)
		return nil
	case *FromImportStmt:
		mod := in.loadModule(n.Module)
		for _, pair := range n.Names {
			name, alias := pair[0], pair[1]
			if alias == "" {
				alias = name
			}
			env.Define(alias, mod.Env.Get(name))
		}
		return nil
	case *Block:
		var result Value
		for _, stmt := range n.Stmts {
			result = in.eval(stmt, env)
		}
		return result
	case *ExprStmt:
		val := in.eval(n.E, env)
		in.LastValue = val
		if _, ok := val.(*List); ok {
			in.LastList = val
		}
		return val
	case *ChanceStmt:
		p := toFloat(in.eval(n.Prob, env))
		if in.Rand.Float64() < p {
			return in.eval(n.Body, env)
		}
		return nil
	case *UnlessStmt:
		if !truthy(in.eval(n.Cond, env)) {
			return in.eval(n.Body, env)
		}
		return nil
	case *EchoStmt:
		val := in.eval(n.E, env)
		in.out(stringify(val) + "\n")
		in.LastValue = val
		return val
	case *PastExpr:
		return env.GetPast(n.Name, n.Depth)
	case *RememberStmt:
		val := in.eval(n.V, env)
		env.Define(n.Name, val)
		env.History[n.Name] = append(env.History[n.Name], val)
		in.LastValue = val
		return val
	case *Program:
		for _, stmt := range n.Stmts {
			in.eval(stmt, env)
		}
		return nil
	}
	panic(&AlbError{Kind: "RuntimeError", Msg: fmt.Sprintf("Unknown node: %T", node)})
}

// evalLoopBody runs a loop body, returning false on break.
func (in *Interp) evalLoopBody(body Node, env *Env) (cont bool) {
	cont = true
	defer func() {
		if r := recover(); r != nil {
			switch r.(type) {
			case breakSignal:
				cont = false
			case continueSignal:
				cont = true
			default:
				panic(r)
			}
		}
	}()
	in.eval(body, env)
	return true
}

func (in *Interp) evalWhile(n *WhileStmt, env *Env) Value {
	for truthy(in.eval(n.Cond, env)) {
		if !in.evalLoopBody(n.Body, env) {
			break
		}
	}
	return nil
}

func (in *Interp) evalTry(n *TryStmt, env *Env) (result Value) {
	// finally runs last (LIFO: registered first)
	defer func() {
		if n.Finally != nil {
			in.eval(n.Finally, env)
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			if isCatchable(r) && len(n.Handlers) > 0 {
				h := n.Handlers[0]
				if h.Name != "" {
					env.Define(h.Name, signalValue(r))
				}
				result = in.eval(h.Body, env)
				return
			}
			panic(r)
		}
	}()
	result = in.eval(n.Body, env)
	return result
}

func isFloatVal(v Value) bool {
	_, ok := v.(float64)
	return ok
}

func (in *Interp) toIntStrict(v Value) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case float64:
		return int64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	}
	panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'%s' object cannot be interpreted as an integer", typeName(v))})
}

func (in *Interp) iterate(v Value) []Value {
	switch t := v.(type) {
	case *List:
		out := make([]Value, len(t.Elems))
		copy(out, t.Elems)
		return out
	case string:
		var out []Value
		for _, r := range t {
			out = append(out, string(r))
		}
		return out
	case *Dict:
		out := make([]Value, len(t.Keys))
		copy(out, t.Keys)
		return out
	}
	panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'%s' object is not iterable", typeName(v))})
}

func (in *Interp) indexGet(obj, idx Value) Value {
	switch o := obj.(type) {
	case *List:
		i := in.listIndex(idx, len(o.Elems), "list index out of range")
		return o.Elems[i]
	case string:
		runes := []rune(o)
		i := in.listIndex(idx, len(runes), "string index out of range")
		return string(runes[i])
	case *Dict:
		if v, ok := o.Get(idx); ok {
			return v
		}
		panic(&AlbError{Kind: "KeyError", Msg: pyRepr(idx)})
	}
	panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'%s' object is not subscriptable", typeName(obj))})
}

func (in *Interp) listIndex(idx Value, length int, errMsg string) int {
	var i int64
	switch t := idx.(type) {
	case int64:
		i = t
	case bool:
		if t {
			i = 1
		}
	default:
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("list indices must be integers, not %s", typeName(idx))})
	}
	if i < 0 {
		i += int64(length)
	}
	if i < 0 || i >= int64(length) {
		panic(&AlbError{Kind: "IndexError", Msg: errMsg})
	}
	return int(i)
}

func (in *Interp) assign(target Node, value Value, env *Env) {
	switch t := target.(type) {
	case *Ident:
		env.Set(t.Name, value)
	case *Index:
		obj := in.eval(t.Obj, env)
		idx := in.eval(t.Idx, env)
		switch o := obj.(type) {
		case *List:
			i := in.listIndex(idx, len(o.Elems), "list assignment index out of range")
			o.Elems[i] = value
		case *Dict:
			o.Set(idx, value)
		default:
			panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'%s' object does not support item assignment", typeName(obj))})
		}
	case *Attr:
		obj := in.eval(t.Obj, env)
		switch o := obj.(type) {
		case *Instance:
			o.Fields[t.Name] = value
		case *Dict:
			o.Set(t.Name, value)
		default:
			panic(&AlbError{Kind: "AttributeError", Msg: fmt.Sprintf("'%s' object has no attribute '%s'", typeName(obj), t.Name)})
		}
	default:
		panic(syntaxErr("Cannot assign"))
	}
}

func (in *Interp) getAttr(obj Value, name string) Value {
	switch o := obj.(type) {
	case *Instance:
		if v, ok := o.Fields[name]; ok {
			return v
		}
		if fn, ok := o.Cls.Methods[name]; ok {
			return &BoundMethod{Inst: o, Fn: fn}
		}
		for _, base := range o.Cls.Bases {
			if bc, ok := base.(*Class); ok {
				if fn, ok := bc.Methods[name]; ok {
					return &BoundMethod{Inst: o, Fn: fn}
				}
			}
		}
		panic(&AlbError{Kind: "AttributeError", Msg: fmt.Sprintf("'%s' object has no attribute '%s'", o.Cls.Name, name)})
	case *Module:
		return o.Env.Get(name)
	case *Class:
		if fn, ok := o.Methods[name]; ok {
			return fn
		}
		panic(&AlbError{Kind: "AttributeError", Msg: name})
	case *Dict:
		if v, ok := o.Get(name); ok {
			return v
		}
		panic(&AlbError{Kind: "KeyError", Msg: pyRepr(name)})
	case *List:
		return in.listMethod(o, name)
	case string:
		return in.strMethod(o, name)
	}
	panic(&AlbError{Kind: "AttributeError", Msg: fmt.Sprintf("'%s' object has no attribute '%s'", typeName(obj), name)})
}

func (in *Interp) callValue(fn Value, args []Value, kwargs map[string]Value) Value {
	switch f := fn.(type) {
	case *BoundMethod:
		return in.callValue(f.Fn, append([]Value{f.Inst}, args...), kwargs)
	case *Function:
		if len(args) < len(f.Params) {
			name := f.Name
			if name == "" {
				name = "<lambda>"
			}
			panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("%s() missing args", name)})
		}
		local := NewEnv(f.Env)
		for i, p := range f.Params {
			local.Define(p, args[i])
		}
		return in.callFunctionBody(f, local)
	case *Class:
		inst := &Instance{Cls: f, Fields: map[string]Value{}}
		if init, ok := f.Methods["init"]; ok {
			in.callValue(&BoundMethod{Inst: inst, Fn: init}, args, kwargs)
		}
		return inst
	case *Builtin:
		return f.Fn(in, args, kwargs)
	}
	panic(&AlbError{Kind: "TypeError", Msg: "not callable"})
}

func (in *Interp) callFunctionBody(f *Function, local *Env) (result Value) {
	defer func() {
		if r := recover(); r != nil {
			if rs, ok := r.(returnSignal); ok {
				result = rs.v
				return
			}
			panic(r)
		}
	}()
	in.eval(f.Body, local) // result deliberately discarded, like the original
	return nil
}

func (in *Interp) loadModule(name string) *Module {
	if m, ok := in.Modules[name]; ok {
		return m
	}
	candidates := []string{name + ".alb", filepath.Join("stdlib", name+".alb")}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		modEnv := NewEnv(in.Global)
		tokens := tokenize(string(data))
		prog := NewParser(tokens, path).Parse()
		for _, stmt := range prog.Stmts {
			in.eval(stmt, modEnv)
		}
		mod := &Module{Name: name, Env: modEnv}
		in.Modules[name] = mod
		return mod
	}
	panic(&AlbError{Kind: "ImportError", Msg: fmt.Sprintf("No module named '%s'", name)})
}

// ---------- binary operators ----------

func zeroDivMsg(l, r Value, kind string) string {
	lf, rf := isFloatVal(l), isFloatVal(r)
	switch kind {
	case "/":
		if lf || rf {
			return "float division by zero"
		}
		return "division by zero"
	case "//":
		if lf || rf {
			return "float floor division by zero"
		}
		return "integer division or modulo by zero"
	case "%":
		if lf || rf {
			return "float modulo"
		}
		return "integer division or modulo by zero"
	}
	return "division by zero"
}

func (in *Interp) applyBinary(op string, l, r Value) Value {
	switch op {
	case "+":
		if _, ok := l.(string); ok {
			return pyStr(l) + pyStr(r)
		}
		if _, ok := r.(string); ok {
			return pyStr(l) + pyStr(r)
		}
		if ll, ok := l.(*List); ok {
			if rl, ok := r.(*List); ok {
				out := &List{}
				out.Elems = append(out.Elems, ll.Elems...)
				out.Elems = append(out.Elems, rl.Elems...)
				return out
			}
		}
		if isNumeric(l) && isNumeric(r) {
			if li, lok := toIntValue(l); lok {
				if ri, rok := toIntValue(r); rok && !isFloatVal(l) && !isFloatVal(r) {
					return li + ri
				}
			}
			return toFloat(l) + toFloat(r)
		}
	case "-":
		if isNumeric(l) && isNumeric(r) {
			if !isFloatVal(l) && !isFloatVal(r) {
				li, _ := toIntValue(l)
				ri, _ := toIntValue(r)
				return li - ri
			}
			return toFloat(l) - toFloat(r)
		}
	case "*":
		if s, ok := l.(string); ok {
			if n, nok := toIntValue(r); nok && !isFloatVal(r) {
				if n <= 0 {
					return ""
				}
				return strings.Repeat(s, int(n))
			}
		}
		if s, ok := r.(string); ok {
			if n, nok := toIntValue(l); nok && !isFloatVal(l) {
				if n <= 0 {
					return ""
				}
				return strings.Repeat(s, int(n))
			}
		}
		if ll, ok := l.(*List); ok {
			if n, nok := toIntValue(r); nok && !isFloatVal(r) {
				out := &List{}
				for i := int64(0); i < n; i++ {
					out.Elems = append(out.Elems, ll.Elems...)
				}
				return out
			}
		}
		if rl, ok := r.(*List); ok {
			if n, nok := toIntValue(l); nok && !isFloatVal(l) {
				out := &List{}
				for i := int64(0); i < n; i++ {
					out.Elems = append(out.Elems, rl.Elems...)
				}
				return out
			}
		}
		if isNumeric(l) && isNumeric(r) {
			if !isFloatVal(l) && !isFloatVal(r) {
				li, _ := toIntValue(l)
				ri, _ := toIntValue(r)
				return li * ri
			}
			return toFloat(l) * toFloat(r)
		}
	case "/":
		if isNumeric(l) && isNumeric(r) {
			if toFloat(r) == 0 {
				panic(&AlbError{Kind: "ZeroDivisionError", Msg: zeroDivMsg(l, r, "/")})
			}
			return toFloat(l) / toFloat(r)
		}
	case "//":
		if isNumeric(l) && isNumeric(r) {
			if toFloat(r) == 0 {
				panic(&AlbError{Kind: "ZeroDivisionError", Msg: zeroDivMsg(l, r, "//")})
			}
			if !isFloatVal(l) && !isFloatVal(r) {
				li, _ := toIntValue(l)
				ri, _ := toIntValue(r)
				q := li / ri
				if (li%ri != 0) && ((li < 0) != (ri < 0)) {
					q--
				}
				return q
			}
			return math.Floor(toFloat(l) / toFloat(r))
		}
	case "%":
		if isNumeric(l) && isNumeric(r) {
			if toFloat(r) == 0 {
				panic(&AlbError{Kind: "ZeroDivisionError", Msg: zeroDivMsg(l, r, "%")})
			}
			if !isFloatVal(l) && !isFloatVal(r) {
				li, _ := toIntValue(l)
				ri, _ := toIntValue(r)
				m := li % ri
				if m != 0 && ((m < 0) != (ri < 0)) {
					m += ri
				}
				return m
			}
			a, b := toFloat(l), toFloat(r)
			return a - math.Floor(a/b)*b
		}
	case "**":
		if isNumeric(l) && isNumeric(r) {
			return in.pyPow(l, r)
		}
	case "==":
		return pyEquals(l, r)
	case "!=":
		return !pyEquals(l, r)
	case "<", ">", "<=", ">=":
		if c, ok := pyCompare(l, r); ok {
			switch op {
			case "<":
				return c < 0
			case ">":
				return c > 0
			case "<=":
				return c <= 0
			case ">=":
				return c >= 0
			}
		}
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'%s' not supported between instances of '%s' and '%s'", op, typeName(l), typeName(r))})
	case "in":
		if rs, ok := r.(string); ok {
			if ls, ok := l.(string); ok {
				return strings.Contains(rs, ls)
			}
			panic(&AlbError{Kind: "TypeError", Msg: "'in <string>' requires string as left operand, not " + typeName(l)})
		}
		if rl, ok := r.(*List); ok {
			for _, e := range rl.Elems {
				if pyEquals(l, e) {
					return true
				}
			}
			return false
		}
		if rd, ok := r.(*Dict); ok {
			_, exists := rd.Get(l)
			return exists
		}
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("argument of type '%s' is not iterable", typeName(r))})
	case "is":
		return pyIs(l, r)
	}
	panic(&AlbError{Kind: "RuntimeError", Msg: "Unknown op " + op})
}

func (in *Interp) pyPow(l, r Value) Value {
	if !isFloatVal(l) && !isFloatVal(r) {
		li, _ := toIntValue(l)
		ri, _ := toIntValue(r)
		if ri >= 0 {
			result := int64(1)
			base := li
			e := ri
			for e > 0 {
				if e&1 == 1 {
					result *= base
				}
				base *= base
				e >>= 1
			}
			return result
		}
	}
	return math.Pow(toFloat(l), toFloat(r))
}
