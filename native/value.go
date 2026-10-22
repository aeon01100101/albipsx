package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Value is any Albish runtime value.
type Value = interface{}

type List struct{ Elems []Value }

type Dict struct {
	Keys  []Value
	Vals  []Value
	index map[string]int
}

func NewDict() *Dict { return &Dict{index: map[string]int{}} }

func dictKeyHash(v Value) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "n:", true
	case bool:
		if t {
			return "b:1", true
		}
		return "b:0", true
	case int64:
		return "i:" + strconv.FormatInt(t, 10), true
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) && math.Abs(t) < 9.3e18 {
			return "i:" + strconv.FormatInt(int64(t), 10), true
		}
		return "f:" + strconv.FormatUint(math.Float64bits(t), 10), true
	case string:
		return "s:" + t, true
	}
	return "", false
}

func (d *Dict) Set(k, v Value) {
	h, ok := dictKeyHash(k)
	if !ok {
		panic(&AlbError{Kind: "TypeError", Msg: "unhashable type: '" + typeName(k) + "'"})
	}
	if i, exists := d.index[h]; exists {
		d.Vals[i] = v
		return
	}
	d.index[h] = len(d.Keys)
	d.Keys = append(d.Keys, k)
	d.Vals = append(d.Vals, v)
}

func (d *Dict) Get(k Value) (Value, bool) {
	h, ok := dictKeyHash(k)
	if !ok {
		return nil, false
	}
	if i, exists := d.index[h]; exists {
		return d.Vals[i], true
	}
	return nil, false
}

func (d *Dict) Len() int { return len(d.Keys) }

type Builtin struct {
	Name string
	Fn   func(in *Interp, args []Value, kwargs map[string]Value) Value
}

type Function struct {
	Name     string
	Params   []string
	Body     Node
	Env      *Env
	IsMethod bool
}

type Class struct {
	Name    string
	Bases   []Value
	Methods map[string]*Function
	Env     *Env
}

type Instance struct {
	Cls    *Class
	Fields map[string]Value
}

type BoundMethod struct {
	Inst *Instance
	Fn   *Function
}

type Module struct {
	Name string
	Env  *Env
}

type AlbError struct {
	Kind string
	Msg  string
}

func (e *AlbError) Error() string {
	if e.Msg == "" {
		return e.Kind
	}
	return e.Kind + ": " + e.Msg
}

// control-flow signals (panicked, mirroring Python exceptions)
type returnSignal struct{ v Value }
type breakSignal struct{}
type continueSignal struct{}

// caughtSignal stands in for a caught control-flow exception object.
type caughtSignal struct{ label string }

func errName(name string) *AlbError {
	return &AlbError{Kind: "NameError", Msg: fmt.Sprintf("name '%s' is not defined", name)}
}

func typeName(v Value) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case int64:
		return "int"
	case float64:
		return "float"
	case string:
		return "string"
	case *List:
		return "list"
	case *Dict:
		return "dict"
	case *Function:
		return "function"
	case *Class:
		return "class"
	case *Instance:
		return "instance"
	case *Module:
		return "module"
	case *Builtin:
		return "builtin_function_or_method"
	case *BoundMethod:
		return "method"
	case *AlbError:
		return "exception"
	}
	return "object"
}

func truthy(v Value) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case int64:
		return t != 0
	case float64:
		return t != 0
	case string:
		return len(t) > 0
	case *List:
		return len(t.Elems) > 0
	case *Dict:
		return t.Len() > 0
	}
	return true
}

// pyFloatRepr renders a float exactly like CPython repr(float).
func pyFloatRepr(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if math.IsNaN(f) {
		return "nan"
	}
	neg := false
	if f < 0 || (f == 0 && math.Signbit(f)) {
		neg = true
		f = math.Abs(f)
	}
	if f == 0 {
		if neg {
			return "-0.0"
		}
		return "0.0"
	}
	// shortest round-trip digits via 'e' format: d.dddde±XX
	s := strconv.FormatFloat(f, 'e', -1, 64)
	ei := strings.IndexByte(s, 'e')
	mant := s[:ei]
	exp, _ := strconv.Atoi(s[ei+1:])
	digits := strings.Replace(mant, ".", "", 1)
	var out string
	if exp >= -4 && exp <= 15 {
		point := exp + 1
		switch {
		case point >= len(digits):
			out = digits + strings.Repeat("0", point-len(digits)) + ".0"
		case point > 0:
			out = digits[:point] + "." + digits[point:]
		default:
			out = "0." + strings.Repeat("0", -point) + digits
		}
	} else {
		m := digits[:1]
		if len(digits) > 1 {
			m += "." + digits[1:]
		}
		sign := "+"
		ae := exp
		if exp < 0 {
			sign = "-"
			ae = -exp
		}
		es := strconv.Itoa(ae)
		if ae < 10 {
			es = "0" + es
		}
		out = m + "e" + sign + es
	}
	if neg {
		return "-" + out
	}
	return out
}

// stringify mirrors Albish _stringify (used by print and str()).
func stringify(v Value) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return pyFloatRepr(t)
	case string:
		return t
	case *List:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = stringify(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Dict:
		parts := make([]string, len(t.Keys))
		for i, k := range t.Keys {
			parts[i] = stringify(k) + ": " + stringify(t.Vals[i])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *Function:
		name := t.Name
		if name == "" {
			name = "<lambda>"
		}
		return "<function " + name + ">"
	case *Class:
		return "<class " + t.Name + ">"
	case *Instance:
		return "<" + t.Cls.Name + " instance>"
	case *Module:
		return "<module " + t.Name + ">"
	case *BoundMethod:
		return "<bound method " + t.Fn.Name + ">"
	case *Builtin:
		return "<built-in function " + t.Name + ">"
	case *AlbError:
		return t.Msg
	case *caughtSignal:
		return t.label
	}
	return fmt.Sprintf("%v", v)
}

// pyStr mirrors Python str() (used by + concatenation, join, write, raise).
func pyStr(v Value) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return pyFloatRepr(t)
	case string:
		return t
	case *List:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = pyRepr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Dict:
		parts := make([]string, len(t.Keys))
		for i, k := range t.Keys {
			parts[i] = pyRepr(k) + ": " + pyRepr(t.Vals[i])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return stringify(v)
}

// pyRepr mirrors Python repr() for container elements.
func pyRepr(v Value) string {
	if s, ok := v.(string); ok {
		return pyReprString(s)
	}
	return pyStr(v)
}

func pyReprString(s string) string {
	quote := byte('\'')
	if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case '\r':
			b.WriteString("\\r")
		default:
			if byte(r) == quote {
				b.WriteByte('\\')
				b.WriteRune(r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte(quote)
	return b.String()
}

func isNumeric(v Value) bool {
	switch v.(type) {
	case int64, float64, bool:
		return true
	}
	return false
}

func toFloat(v Value) float64 {
	switch t := v.(type) {
	case int64:
		return float64(t)
	case float64:
		return t
	case bool:
		if t {
			return 1
		}
		return 0
	}
	panic(&AlbError{Kind: "TypeError", Msg: "not a number: " + typeName(v)})
}

func toIntValue(v Value) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case float64:
		return int64(t), true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// pyCompare returns -1/0/1 for orderable pairs, ok=false if not comparable.
func pyCompare(l, r Value) (int, bool) {
	if isNumeric(l) && isNumeric(r) {
		a, b := toFloat(l), toFloat(r)
		switch {
		case a < b:
			return -1, true
		case a > b:
			return 1, true
		}
		return 0, true
	}
	if ls, ok := l.(string); ok {
		if rs, ok := r.(string); ok {
			return strings.Compare(ls, rs), true
		}
	}
	return 0, false
}

func pyEquals(l, r Value) bool {
	if isNumeric(l) && isNumeric(r) {
		return toFloat(l) == toFloat(r)
	}
	switch lt := l.(type) {
	case nil:
		return r == nil
	case bool:
		rb, ok := r.(bool)
		return ok && lt == rb
	case string:
		rs, ok := r.(string)
		return ok && lt == rs
	case *List:
		rt, ok := r.(*List)
		if !ok || len(lt.Elems) != len(rt.Elems) {
			return false
		}
		for i := range lt.Elems {
			if !pyEquals(lt.Elems[i], rt.Elems[i]) {
				return false
			}
		}
		return true
	case *Dict:
		rt, ok := r.(*Dict)
		if !ok || lt.Len() != rt.Len() {
			return false
		}
		for i, k := range lt.Keys {
			rv, ok := rt.Get(k)
			if !ok || !pyEquals(lt.Vals[i], rv) {
				return false
			}
		}
		return true
	}
	return l == r
}

func pyIs(l, r Value) bool {
	if l == nil || r == nil {
		return l == nil && r == nil
	}
	if isNumeric(l) || isNumeric(r) {
		// Python identity for numbers across types is False; same-type value
		// equality is the practical approximation for cached small values.
		switch l.(type) {
		case int64:
			_, ok := r.(int64)
			return ok && pyEquals(l, r)
		case float64:
			_, ok := r.(float64)
			return ok && pyEquals(l, r)
		case bool:
			_, ok := r.(bool)
			return ok && pyEquals(l, r)
		}
		return false
	}
	if ls, ok := l.(string); ok {
		rs, ok := r.(string)
		return ok && ls == rs
	}
	return l == r
}
