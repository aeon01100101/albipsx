package main

import (
	"fmt"
	"strconv"
	"strings"
)

// ---------- JSON parsing (mirrors Python json.loads) ----------

type jsonParser struct {
	s   []rune
	pos int
}

func jsonErr(msg string) *AlbError {
	return &AlbError{Kind: "JSONDecodeError", Msg: msg}
}

func parseJSON(s string) Value {
	p := &jsonParser{s: []rune(s)}
	p.skipWs()
	v := p.value()
	p.skipWs()
	if p.pos != len(p.s) {
		panic(jsonErr(fmt.Sprintf("Extra data at column %d", p.pos+1)))
	}
	return v
}

func (p *jsonParser) skipWs() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) value() Value {
	if p.pos >= len(p.s) {
		panic(jsonErr("Expecting value at end of input"))
	}
	switch c := p.s[p.pos]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		return p.str()
	case c == 't':
		p.expectLit("true")
		return true
	case c == 'f':
		p.expectLit("false")
		return false
	case c == 'n':
		p.expectLit("null")
		return nil
	default:
		return p.number()
	}
}

func (p *jsonParser) expectLit(lit string) {
	for _, r := range lit {
		if p.pos >= len(p.s) || p.s[p.pos] != r {
			panic(jsonErr(fmt.Sprintf("Expecting value at column %d", p.pos+1)))
		}
		p.pos++
	}
}

func (p *jsonParser) object() Value {
	d := NewDict()
	p.pos++ // {
	p.skipWs()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return d
	}
	for {
		p.skipWs()
		if p.pos >= len(p.s) || p.s[p.pos] != '"' {
			panic(jsonErr(fmt.Sprintf("Expecting property name at column %d", p.pos+1)))
		}
		k := p.str()
		p.skipWs()
		if p.pos >= len(p.s) || p.s[p.pos] != ':' {
			panic(jsonErr(fmt.Sprintf("Expecting ':' delimiter at column %d", p.pos+1)))
		}
		p.pos++
		p.skipWs()
		d.Set(k, p.value())
		p.skipWs()
		if p.pos < len(p.s) && p.s[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.pos < len(p.s) && p.s[p.pos] == '}' {
			p.pos++
			return d
		}
		panic(jsonErr(fmt.Sprintf("Expecting ',' delimiter at column %d", p.pos+1)))
	}
}

func (p *jsonParser) array() Value {
	lst := &List{}
	p.pos++ // [
	p.skipWs()
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return lst
	}
	for {
		p.skipWs()
		lst.Elems = append(lst.Elems, p.value())
		p.skipWs()
		if p.pos < len(p.s) && p.s[p.pos] == ',' {
			p.pos++
			continue
		}
		if p.pos < len(p.s) && p.s[p.pos] == ']' {
			p.pos++
			return lst
		}
		panic(jsonErr(fmt.Sprintf("Expecting ',' delimiter at column %d", p.pos+1)))
	}
}

func (p *jsonParser) str() string {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		p.pos++
		switch c {
		case '"':
			return b.String()
		case '\\':
			if p.pos >= len(p.s) {
				break
			}
			e := p.s[p.pos]
			p.pos++
			switch e {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case '/':
				b.WriteByte('/')
			case 'b':
				b.WriteByte(8)
			case 'f':
				b.WriteByte(12)
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				if p.pos+3 < len(p.s)+1 && p.pos+4 <= len(p.s) {
					v := 0
					ok := true
					for k := 0; k < 4; k++ {
						h := hexValRune(p.s[p.pos+k])
						if h < 0 {
							ok = false
							break
						}
						v = v*16 + h
					}
					if ok {
						p.pos += 4
						// surrogate pair
						if v >= 0xD800 && v <= 0xDBFF && p.pos+5 < len(p.s) && p.s[p.pos] == '\\' && p.s[p.pos+1] == 'u' {
							v2 := 0
							ok2 := true
							for k := 0; k < 4; k++ {
								h := hexValRune(p.s[p.pos+2+k])
								if h < 0 {
									ok2 = false
									break
								}
								v2 = v2*16 + h
							}
							if ok2 && v2 >= 0xDC00 && v2 <= 0xDFFF {
								p.pos += 6
								v = 0x10000 + (v-0xD800)*0x400 + (v2 - 0xDC00)
							}
						}
						b.WriteRune(rune(v))
					} else {
						panic(jsonErr("Invalid \\uXXXX escape"))
					}
				} else {
					panic(jsonErr("Invalid \\uXXXX escape"))
				}
			default:
				panic(jsonErr(fmt.Sprintf("Invalid \\escape at column %d", p.pos)))
			}
		default:
			b.WriteRune(c)
		}
	}
	panic(jsonErr("Unterminated string"))
}

func hexValRune(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10
	}
	return -1
}

func (p *jsonParser) number() Value {
	start := p.pos
	if p.pos < len(p.s) && p.s[p.pos] == '-' {
		p.pos++
	}
	isFloat := false
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c >= '0' && c <= '9' {
			p.pos++
		} else if c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
			if c == '.' || c == 'e' || c == 'E' {
				isFloat = true
			}
			p.pos++
		} else {
			break
		}
	}
	text := string(p.s[start:p.pos])
	if text == "" || text == "-" {
		panic(jsonErr(fmt.Sprintf("Expecting value at column %d", start+1)))
	}
	if !isFloat {
		if iv, err := strconv.ParseInt(text, 10, 64); err == nil {
			return iv
		}
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		panic(jsonErr(fmt.Sprintf("Expecting value at column %d", start+1)))
	}
	return f
}

// ---------- JSON dumping (mirrors Python json.dumps) ----------

func dumpJSON(v Value, indent int) string {
	var b strings.Builder
	writeJSONValue(&b, v, indent, 0)
	return b.String()
}

func jsonKeyString(k Value) string {
	switch t := k.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return pyFloatRepr(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	}
	return stringify(k)
}

func writeJSONValue(b *strings.Builder, v Value, indent int, level int) {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int64:
		b.WriteString(strconv.FormatInt(t, 10))
	case float64:
		if isNaNInf(t) {
			b.WriteString(pyJSONFloat(t))
		} else {
			b.WriteString(pyFloatRepr(t))
		}
	case string:
		writeJSONString(b, t)
	case *List:
		if len(t.Elems) == 0 {
			b.WriteString("[]")
			return
		}
		if indent < 0 {
			b.WriteString("[")
			for i, e := range t.Elems {
				if i > 0 {
					b.WriteString(", ")
				}
				writeJSONValue(b, e, indent, level)
			}
			b.WriteString("]")
			return
		}
		b.WriteString("[\n")
		pad := strings.Repeat(" ", indent*(level+1))
		for i, e := range t.Elems {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			writeJSONValue(b, e, indent, level+1)
		}
		b.WriteString("\n" + strings.Repeat(" ", indent*level) + "]")
	case *Dict:
		if t.Len() == 0 {
			b.WriteString("{}")
			return
		}
		if indent < 0 {
			b.WriteString("{")
			for i, k := range t.Keys {
				if i > 0 {
					b.WriteString(", ")
				}
				writeJSONString(b, jsonKeyString(k))
				b.WriteString(": ")
				writeJSONValue(b, t.Vals[i], indent, level)
			}
			b.WriteString("}")
			return
		}
		b.WriteString("{\n")
		pad := strings.Repeat(" ", indent*(level+1))
		for i, k := range t.Keys {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(pad)
			writeJSONString(b, jsonKeyString(k))
			b.WriteString(": ")
			writeJSONValue(b, t.Vals[i], indent, level+1)
		}
		b.WriteString("\n" + strings.Repeat(" ", indent*level) + "}")
	default:
		// Python dumps with default=str
		writeJSONString(b, stringify(v))
	}
}

func isNaNInf(f float64) bool {
	return f != f || f > 1.7976931348623157e308 || f < -1.7976931348623157e308
}

func pyJSONFloat(f float64) string {
	if f != f {
		return "NaN"
	}
	if f > 0 && (f > 1.7976931348623157e308) {
		return "Infinity"
	}
	if f < 0 && (f < -1.7976931348623157e308) {
		return "-Infinity"
	}
	return pyFloatRepr(f)
}

func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		case 8:
			b.WriteString("\\b")
		case 12:
			b.WriteString("\\f")
		default:
			if r < 0x20 || r > 0x7E {
				// ensure_ascii: escape non-ASCII, with surrogate pairs
				if r > 0xFFFF {
					r -= 0x10000
					hi := 0xD800 + (r >> 10)
					lo := 0xDC00 + (r & 0x3FF)
					fmt.Fprintf(b, "\\u%04x\\u%04x", hi, lo)
				} else {
					fmt.Fprintf(b, "\\u%04x", r)
				}
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
