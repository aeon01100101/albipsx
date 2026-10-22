package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

func needArgs(name string, args []Value, n int) {
	if len(args) < n {
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("%s() missing args", name)})
	}
}

func (in *Interp) setupBuiltins() {
	g := in.Global
	def := func(name string, fn func(in *Interp, args []Value, kwargs map[string]Value) Value) {
		g.Define(name, &Builtin{Name: name, Fn: fn})
	}
	printFn := func(in *Interp, args []Value, kwargs map[string]Value) Value {
		sep := " "
		end := "\n"
		if kwargs != nil {
			if v, ok := kwargs["sep"]; ok {
				sep = pyStr(v)
			}
			if v, ok := kwargs["end"]; ok {
				end = pyStr(v)
			}
		}
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = stringify(a)
		}
		in.out(strings.Join(parts, sep) + end)
		return nil
	}
	def("print", printFn)
	def("say", printFn)
	def("len", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("len", args, 1)
		switch t := args[0].(type) {
		case string:
			return int64(len([]rune(t)))
		case *List:
			return int64(len(t.Elems))
		case *Dict:
			return int64(t.Len())
		}
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("object of type '%s' has no len()", typeName(args[0]))})
	})
	def("type", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("type", args, 1)
		return typeName(args[0])
	})
	def("str", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		if len(args) == 0 {
			return ""
		}
		return stringify(args[0])
	})
	def("int", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("int", args, 1)
		switch t := args[0].(type) {
		case int64:
			return t
		case float64:
			return int64(t)
		case bool:
			if t {
				return int64(1)
			}
			return int64(0)
		case string:
			s := strings.TrimSpace(t)
			if iv, err := strconv.ParseInt(s, 10, 64); err == nil {
				return iv
			}
			panic(&AlbError{Kind: "ValueError", Msg: fmt.Sprintf("invalid literal for int() with base 10: %s", pyRepr(t))})
		}
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("int() argument must be a string or a number, not '%s'", typeName(args[0]))})
	})
	def("float", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("float", args, 1)
		switch t := args[0].(type) {
		case int64:
			return float64(t)
		case float64:
			return t
		case bool:
			if t {
				return float64(1)
			}
			return float64(0)
		case string:
			s := strings.TrimSpace(t)
			switch strings.ToLower(s) {
			case "inf", "+inf", "infinity", "+infinity":
				return math.Inf(1)
			case "-inf", "-infinity":
				return math.Inf(-1)
			case "nan":
				return math.NaN()
			}
			if fv, err := strconv.ParseFloat(s, 64); err == nil {
				return fv
			}
			panic(&AlbError{Kind: "ValueError", Msg: fmt.Sprintf("could not convert string to float: %s", pyRepr(t))})
		}
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("float() argument must be a string or a number, not '%s'", typeName(args[0]))})
	})
	def("bool", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		if len(args) == 0 {
			return false
		}
		return truthy(args[0])
	})
	def("list", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		if len(args) == 0 || args[0] == nil {
			return &List{}
		}
		switch t := args[0].(type) {
		case *List:
			out := &List{}
			out.Elems = append(out.Elems, t.Elems...)
			return out
		case string:
			out := &List{}
			for _, r := range t {
				out.Elems = append(out.Elems, string(r))
			}
			return out
		case *Dict:
			out := &List{}
			out.Elems = append(out.Elems, t.Keys...)
			return out
		}
		panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'%s' object is not iterable", typeName(args[0]))})
	})
	def("dict", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		if len(args) == 0 || args[0] == nil {
			return NewDict()
		}
		if d, ok := args[0].(*Dict); ok {
			out := NewDict()
			for i, k := range d.Keys {
				out.Set(k, d.Vals[i])
			}
			return out
		}
		panic(&AlbError{Kind: "TypeError", Msg: "'dict' object is not iterable"})
	})
	def("range", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		if len(args) < 1 || len(args) > 3 {
			panic(&AlbError{Kind: "TypeError", Msg: "range expected 1 to 3 arguments"})
		}
		var start, stop, step int64 = 0, 0, 1
		nums := make([]int64, len(args))
		for i, a := range args {
			iv, ok := toIntValue(a)
			if !ok || isFloatVal(a) {
				panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'%s' object cannot be interpreted as an integer", typeName(a))})
			}
			nums[i] = iv
		}
		switch len(nums) {
		case 1:
			stop = nums[0]
		case 2:
			start, stop = nums[0], nums[1]
		case 3:
			start, stop, step = nums[0], nums[1], nums[2]
		}
		if step == 0 {
			panic(&AlbError{Kind: "ValueError", Msg: "range() arg 3 must not be zero"})
		}
		out := &List{}
		if step > 0 {
			for i := start; i < stop; i += step {
				out.Elems = append(out.Elems, i)
			}
		} else {
			for i := start; i > stop; i += step {
				out.Elems = append(out.Elems, i)
			}
		}
		return out
	})
	def("input", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		prompt := ""
		if len(args) > 0 {
			prompt = pyStr(args[0])
		}
		in.out(prompt)
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			panic(&AlbError{Kind: "EOFError", Msg: "EOF when reading a line"})
		}
		return strings.TrimRight(line, "\r\n")
	})
	def("abs", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("abs", args, 1)
		switch t := args[0].(type) {
		case int64:
			if t < 0 {
				return -t
			}
			return t
		case float64:
			return math.Abs(t)
		}
		panic(&AlbError{Kind: "TypeError", Msg: "bad operand type for abs(): '" + typeName(args[0]) + "'"})
	})
	minMax := func(wantMin bool) func(in *Interp, args []Value, kwargs map[string]Value) Value {
		return func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs("min/max", args, 1)
			var items []Value
			if len(args) > 1 {
				items = args
			} else {
				items = in.iterate(args[0])
			}
			if len(items) == 0 {
				panic(&AlbError{Kind: "ValueError", Msg: "min/max() arg is an empty sequence"})
			}
			best := items[0]
			for _, it := range items[1:] {
				c, ok := pyCompare(it, best)
				if !ok {
					panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'<' not supported between instances of '%s' and '%s'", typeName(it), typeName(best))})
				}
				if (wantMin && c < 0) || (!wantMin && c > 0) {
					best = it
				}
			}
			return best
		}
	}
	def("min", minMax(true))
	def("max", minMax(false))
	def("sum", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("sum", args, 1)
		items := in.iterate(args[0])
		var isum int64
		var fsum float64
		isFloat := false
		for _, it := range items {
			if isFloatVal(it) {
				isFloat = true
			}
			if !isNumeric(it) {
				panic(&AlbError{Kind: "TypeError", Msg: "unsupported operand type(s) for +: 'int' and '" + typeName(it) + "'"})
			}
			if iv, ok := toIntValue(it); ok {
				isum += iv
			}
			fsum += toFloat(it)
		}
		if isFloat {
			return fsum
		}
		return isum
	})
	def("round", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("round", args, 1)
		n := 0
		if len(args) > 1 {
			if iv, ok := toIntValue(args[1]); ok {
				n = int(iv)
			}
		}
		switch t := args[0].(type) {
		case int64:
			if n >= 0 {
				return t
			}
			f := pyRoundFloat(float64(t), n)
			return int64(f)
		case float64:
			return pyRoundFloat(t, n)
		}
		panic(&AlbError{Kind: "TypeError", Msg: "type " + typeName(args[0]) + " doesn't define __round__ method"})
	})
	def("sorted", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("sorted", args, 1)
		items := in.iterate(args[0])
		out := &List{Elems: append([]Value{}, items...)}
		rev := false
		if kwargs != nil {
			if v, ok := kwargs["reverse"]; ok {
				rev = truthy(v)
			}
		}
		sortValues(out.Elems, rev)
		return out
	})
	def("map", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("map", args, 2)
		items := in.iterate(args[1])
		out := &List{}
		for _, it := range items {
			out.Elems = append(out.Elems, in.callValue(args[0], []Value{it}, nil))
		}
		return out
	})
	def("filter", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("filter", args, 2)
		items := in.iterate(args[1])
		out := &List{}
		for _, it := range items {
			if truthy(in.callValue(args[0], []Value{it}, nil)) {
				out.Elems = append(out.Elems, it)
			}
		}
		return out
	})
	def("any", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("any", args, 1)
		for _, it := range in.iterate(args[0]) {
			if truthy(it) {
				return true
			}
		}
		return false
	})
	def("all", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("all", args, 1)
		for _, it := range in.iterate(args[0]) {
			if !truthy(it) {
				return false
			}
		}
		return true
	})
	def("read", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("read", args, 1)
		path := pyStr(args[0])
		data, err := os.ReadFile(path)
		if err != nil {
			panic(&AlbError{Kind: "OSError", Msg: err.Error()})
		}
		return string(data)
	})
	def("write", func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("write", args, 2)
		path := pyStr(args[0])
		content := pyStr(args[1])
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			panic(&AlbError{Kind: "OSError", Msg: err.Error()})
		}
		return nil
	})
	g.Define("true", true)
	g.Define("false", false)
	g.Define("null", nil)
	g.Define("None", nil)

	// math module
	mathEnv := NewEnv(nil)
	mathEnv.Define("pi", math.Pi)
	mathEnv.Define("e", math.E)
	mathEnv.Define("tau", math.Pi*2)
	mathEnv.Define("inf", math.Inf(1))
	mathEnv.Define("nan", math.NaN())
	mfn := func(name string, f func(x float64) float64) {
		mathEnv.Define(name, &Builtin{Name: name, Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 1)
			return f(toFloat(args[0]))
		}})
	}
	mfn("sin", math.Sin)
	mfn("cos", math.Cos)
	mfn("tan", math.Tan)
	mfn("sqrt", math.Sqrt)
	mfn("log", math.Log)
	mfn("log10", math.Log10)
	mfn("exp", math.Exp)
	mfn("fabs", math.Abs)
	mathEnv.Define("floor", &Builtin{Name: "floor", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("floor", args, 1)
		return int64(math.Floor(toFloat(args[0])))
	}})
	mathEnv.Define("ceil", &Builtin{Name: "ceil", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("ceil", args, 1)
		return int64(math.Ceil(toFloat(args[0])))
	}})
	mathEnv.Define("pow", &Builtin{Name: "pow", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("pow", args, 2)
		return math.Pow(toFloat(args[0]), toFloat(args[1]))
	}})
	mathEnv.Define("factorial", &Builtin{Name: "factorial", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("factorial", args, 1)
		n := in.toIntStrict(args[0])
		if n < 0 {
			panic(&AlbError{Kind: "ValueError", Msg: "factorial() not defined for negative values"})
		}
		r := int64(1)
		for i := int64(2); i <= n; i++ {
			r *= i
		}
		return r
	}})
	in.Modules["math"] = &Module{Name: "math", Env: mathEnv}

	// random module
	randEnv := NewEnv(nil)
	randEnv.Define("random", &Builtin{Name: "random", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		return in.Rand.Float64()
	}})
	randEnv.Define("randint", &Builtin{Name: "randint", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("randint", args, 2)
		a := in.toIntStrict(args[0])
		b := in.toIntStrict(args[1])
		if b < a {
			panic(&AlbError{Kind: "ValueError", Msg: "empty range for randrange()"})
		}
		return a + in.Rand.Int63n(b-a+1)
	}})
	randEnv.Define("choice", &Builtin{Name: "choice", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("choice", args, 1)
		items := in.iterate(args[0])
		if len(items) == 0 {
			panic(&AlbError{Kind: "IndexError", Msg: "Cannot choose from an empty sequence"})
		}
		return items[in.Rand.Intn(len(items))]
	}})
	in.Modules["random"] = &Module{Name: "random", Env: randEnv}

	// json module
	jsonEnv := NewEnv(nil)
	jsonEnv.Define("loads", &Builtin{Name: "loads", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("loads", args, 1)
		s, _ := args[0].(string)
		return parseJSON(s)
	}})
	jsonEnv.Define("dumps", &Builtin{Name: "dumps", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("dumps", args, 1)
		indent := -1
		if len(args) > 1 && args[1] != nil {
			if iv, ok := toIntValue(args[1]); ok {
				indent = int(iv)
			}
		}
		if kwargs != nil {
			if v, ok := kwargs["indent"]; ok && v != nil {
				if iv, ok2 := toIntValue(v); ok2 {
					indent = int(iv)
				}
			}
		}
		return dumpJSON(args[0], indent)
	}})
	in.Modules["json"] = &Module{Name: "json", Env: jsonEnv}

	// os module
	osEnv := NewEnv(nil)
	osEnv.Define("getcwd", &Builtin{Name: "getcwd", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		wd, err := os.Getwd()
		if err != nil {
			panic(&AlbError{Kind: "OSError", Msg: err.Error()})
		}
		return wd
	}})
	osEnv.Define("listdir", &Builtin{Name: "listdir", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		path := "."
		if len(args) > 0 {
			path = pyStr(args[0])
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			panic(&AlbError{Kind: "OSError", Msg: err.Error()})
		}
		out := &List{}
		for _, e := range entries {
			out.Elems = append(out.Elems, e.Name())
		}
		return out
	}})
	osEnv.Define("path_exists", &Builtin{Name: "path_exists", Fn: func(in *Interp, args []Value, kwargs map[string]Value) Value {
		needArgs("path_exists", args, 1)
		_, err := os.Stat(pyStr(args[0]))
		return err == nil
	}})
	in.Modules["os"] = &Module{Name: "os", Env: osEnv}
}

func pyRoundFloat(x float64, n int) float64 {
	p := math.Pow(10, float64(n))
	if math.IsInf(p, 0) || p == 0 {
		return x
	}
	return math.RoundToEven(x*p) / p
}

// sortValues sorts like Python's sorted() for homogeneous numbers or strings.
func sortValues(elems []Value, reverse bool) {
	less := func(i, j int) bool {
		c, ok := pyCompare(elems[i], elems[j])
		if !ok {
			panic(&AlbError{Kind: "TypeError", Msg: fmt.Sprintf("'<' not supported between instances of '%s' and '%s'", typeName(elems[j]), typeName(elems[i]))})
		}
		if reverse {
			return c > 0
		}
		return c < 0
	}
	sort.SliceStable(elems, less)
}

func (in *Interp) listMethod(lst *List, name string) Value {
	mk := func(n string, fn func(in *Interp, args []Value, kwargs map[string]Value) Value) Value {
		return &Builtin{Name: n, Fn: fn}
	}
	switch name {
	case "append", "push":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 1)
			lst.Elems = append(lst.Elems, args[0])
			return lst
		})
	case "pop":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			i := int64(-1)
			if len(args) > 0 {
				iv, ok := toIntValue(args[0])
				if !ok {
					panic(&AlbError{Kind: "TypeError", Msg: "pop indices must be integers"})
				}
				i = iv
			}
			if len(lst.Elems) == 0 {
				panic(&AlbError{Kind: "IndexError", Msg: "pop from empty list"})
			}
			if i < 0 {
				i += int64(len(lst.Elems))
			}
			if i < 0 || i >= int64(len(lst.Elems)) {
				panic(&AlbError{Kind: "IndexError", Msg: "pop index out of range"})
			}
			v := lst.Elems[i]
			lst.Elems = append(lst.Elems[:i], lst.Elems[i+1:]...)
			return v
		})
	case "extend":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 1)
			items := in.iterate(args[0])
			lst.Elems = append(lst.Elems, items...)
			return lst
		})
	case "sort":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			rev := false
			if len(args) > 0 {
				rev = truthy(args[0])
			}
			if kwargs != nil {
				if v, ok := kwargs["reverse"]; ok {
					rev = truthy(v)
				}
			}
			sortValues(lst.Elems, rev)
			return lst
		})
	case "reverse":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			for i, j := 0, len(lst.Elems)-1; i < j; i, j = i+1, j-1 {
				lst.Elems[i], lst.Elems[j] = lst.Elems[j], lst.Elems[i]
			}
			return lst
		})
	case "copy":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			out := &List{}
			out.Elems = append(out.Elems, lst.Elems...)
			return out
		})
	}
	panic(&AlbError{Kind: "AttributeError", Msg: fmt.Sprintf("'list' object has no attribute '%s'", name)})
}

func (in *Interp) strMethod(s string, name string) Value {
	mk := func(n string, fn func(in *Interp, args []Value, kwargs map[string]Value) Value) Value {
		return &Builtin{Name: n, Fn: fn}
	}
	switch name {
	case "upper":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			return strings.ToUpper(s)
		})
	case "lower":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			return strings.ToLower(s)
		})
	case "strip":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			return strings.TrimSpace(s)
		})
	case "split":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			out := &List{}
			if len(args) == 0 || args[0] == nil {
				for _, f := range strings.Fields(s) {
					out.Elems = append(out.Elems, f)
				}
				return out
			}
			sep := pyStr(args[0])
			if sep == "" {
				panic(&AlbError{Kind: "ValueError", Msg: "empty separator"})
			}
			for _, part := range strings.Split(s, sep) {
				out.Elems = append(out.Elems, part)
			}
			return out
		})
	case "replace":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 2)
			return strings.ReplaceAll(s, pyStr(args[0]), pyStr(args[1]))
		})
	case "startswith":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 1)
			return strings.HasPrefix(s, pyStr(args[0]))
		})
	case "endswith":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 1)
			return strings.HasSuffix(s, pyStr(args[0]))
		})
	case "find":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 1)
			sub := pyStr(args[0])
			idx := strings.Index(s, sub)
			if idx < 0 {
				return int64(-1)
			}
			return int64(len([]rune(s[:idx])))
		})
	case "join":
		return mk(name, func(in *Interp, args []Value, kwargs map[string]Value) Value {
			needArgs(name, args, 1)
			items := in.iterate(args[0])
			parts := make([]string, len(items))
			for i, it := range items {
				parts[i] = pyStr(it)
			}
			return strings.Join(parts, s)
		})
	}
	panic(&AlbError{Kind: "AttributeError", Msg: fmt.Sprintf("'str' object has no attribute '%s'", name)})
}
